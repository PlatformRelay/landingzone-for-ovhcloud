// lz-pack packs a prepared runtime bundle and its launcher into a
// single-layer OCI image layout, reproducibly: entries are sorted and carry
// no host owner or time, so the same bundle always yields the same digests.
// The layer holds runtime/ (the bundle with its entry) and host/bwrap.
//
// Before anything is written the bundle passes the entry's own admission
// (internal/bundle) for the manifest digest the entry was built with. After
// writing, the layer is extracted again and what it holds must pass the same
// admission, and its entry must have that digest compiled in, so a layout is
// kept only if the entry it carries admits the bundle it carries.
//
// Every pack also writes provenance.json into the layout, an unsigned in-toto
// statement binding the image to the source commit given with -commit (a
// required flag), resources.json and the entry (FR-017).
//
// `lz-pack prepare` writes that bundle first, from the fetched artefacts
// the pin tables pin (internal/bundle Prepare).
package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/bundle"
)

const source = "https://github.com/PlatformRelay/landingzone-for-ovhcloud"

// packed names what pack wrote: the layer and manifest digests, and the
// digest (bare hex) of the entry the layer carries.
type packed struct{ Layer, Manifest, Entry string }

type descriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
}

// item is one layer entry: its name in the layer and its source on disk.
type item struct {
	header *tar.Header
	path   string
}

func digest(h hash.Hash) string { return "sha256:" + hex.EncodeToString(h.Sum(nil)) }

// header describes one entry without host identity or time. Hard links,
// special files and setuid, setgid or sticky bits are refused: a tar layer
// extracted by an unprivileged user cannot reproduce them faithfully.
func header(name string, info fs.FileInfo) (*tar.Header, error) {
	if info.Mode()&(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky) != 0 {
		return nil, fmt.Errorf("PACK_INPUT: special permission bit: %s", name)
	}
	h := &tar.Header{Name: name, Mode: int64(info.Mode().Perm()), ModTime: time.Unix(0, 0)}
	switch {
	case info.IsDir():
		h.Typeflag, h.Name = tar.TypeDir, name+"/"
	case info.Mode().IsRegular():
		if metadata, ok := info.Sys().(*syscall.Stat_t); !ok || metadata.Nlink != 1 {
			return nil, fmt.Errorf("PACK_INPUT: hard link: %s", name)
		}
		h.Typeflag, h.Size = tar.TypeReg, info.Size()
	case info.Mode()&fs.ModeSymlink != 0:
		h.Typeflag = tar.TypeSymlink
	default:
		return nil, fmt.Errorf("PACK_INPUT: special file: %s", name)
	}
	return h, nil
}

// canonical requires an absolute path without "." or ".." components or
// redundant separators, so that resolving it lexically and the kernel
// following it name the same file.
func canonical(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("PACK_PATH: absolute clean path required: %s", path)
	}
	return nil
}

// outside reports an error unless out lies outside dir after resolving links
// in out's parent, so the layout cannot become part of its own input. Both
// paths are canonical, and admission has required every component of dir to
// be a real directory.
func outside(dir, out string) error {
	parent, err := filepath.EvalSymlinks(filepath.Dir(out))
	if err != nil {
		return fmt.Errorf("PACK_OUTPUT: parent directory required: %w", err)
	}
	target := filepath.Join(parent, filepath.Base(out))
	// An output equal to the bundle already exists and is refused on creation.
	if strings.HasPrefix(target, dir+"/") {
		return fmt.Errorf("PACK_OUTPUT: output inside the bundle")
	}
	return nil
}

// plan admits the bundle as the entry would and returns the layer's entries
// in order: host/, host/bwrap, then the bundle under runtime/ in walk order.
func plan(dir, launcher, manifestSHA string) ([]item, error) {
	if _, err := bundle.Admit(dir, manifestSHA, launcher); err != nil {
		return nil, err
	}
	// Admission has read the launcher as a single-link regular file.
	launcherInfo, err := os.Lstat(launcher)
	if err != nil {
		return nil, err
	}
	launcherHeader, err := header("host/bwrap", launcherInfo)
	if err != nil {
		return nil, err
	}
	// Only the launcher comes from its host directory, not that directory's mode.
	items := []item{{&tar.Header{Name: "host/", Typeflag: tar.TypeDir, Mode: 0o755, ModTime: time.Unix(0, 0)}, ""}, {launcherHeader, launcher}}
	err = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, incoming error) error {
		if incoming != nil {
			return incoming
		}
		relative, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		h, err := header(filepath.ToSlash(filepath.Join("runtime", relative)), info)
		if err != nil {
			return err
		}
		// Admission has checked every link under resources/; it does not look
		// beside the manifest, where no link belongs.
		if h.Typeflag == tar.TypeSymlink {
			if !strings.HasPrefix(filepath.ToSlash(relative), "resources/") {
				return fmt.Errorf("PACK_INPUT: link outside resources: %s", relative)
			}
			if h.Linkname, err = os.Readlink(path); err != nil {
				return err
			}
		}
		items = append(items, item{h, path})
		return nil
	})
	return items, err
}

// layer writes the planned entries as a gzip-compressed tar to file and
// returns its digest, size and the digest of the uncompressed tar (the
// config's diff_id).
func layer(file *os.File, items []item) (string, int64, string, error) {
	compressed, uncompressed := sha256.New(), sha256.New()
	counter := &count{}
	zipper, err := gzip.NewWriterLevel(io.MultiWriter(file, compressed, counter), gzip.BestCompression)
	if err != nil {
		return "", 0, "", err
	}
	archive := tar.NewWriter(io.MultiWriter(zipper, uncompressed))
	for _, it := range items {
		if err := archive.WriteHeader(it.header); err != nil {
			return "", 0, "", err
		}
		if it.header.Typeflag == tar.TypeReg {
			if err := copyFile(archive, it.path); err != nil {
				return "", 0, "", err
			}
		}
	}
	if err := archive.Close(); err != nil {
		return "", 0, "", err
	}
	if err := zipper.Close(); err != nil {
		return "", 0, "", err
	}
	return digest(compressed), counter.n, digest(uncompressed), nil
}

// copyFile streams one file; tar.Writer refuses a file that grew or shrank
// since its header.
func copyFile(w io.Writer, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(w, file)
	return err
}

type count struct{ n int64 }

func (c *count) Write(p []byte) (int, error) { c.n += int64(len(p)); return len(p), nil }

// compiledDigest returns the manifest digest compiled into the entry at
// path: the value of its main.manifestSHA string, read through the symbol
// table, not any bytes that merely appear in the file.
func compiledDigest(path string) (string, error) {
	file, err := elf.Open(path)
	if err != nil {
		return "", fmt.Errorf("PACK_ENTRY: %w", err)
	}
	defer file.Close()
	symbols, err := file.Symbols()
	if err != nil {
		return "", fmt.Errorf("PACK_ENTRY: %w", err)
	}
	read := func(address, size uint64) ([]byte, error) {
		for _, section := range file.Sections {
			// An unset string lies in zero-filled data and reads as empty.
			if address >= section.Addr && address+size <= section.Addr+section.Size {
				data := make([]byte, size)
				_, err := section.ReadAt(data, int64(address-section.Addr))
				return data, err
			}
		}
		return nil, fmt.Errorf("PACK_ENTRY: no initialised data at %#x", address)
	}
	for _, symbol := range symbols {
		if symbol.Name != "main.manifestSHA" {
			continue
		}
		// A Go string is a data pointer and a length.
		header, err := read(symbol.Value, 16)
		if err != nil {
			return "", err
		}
		value, err := read(file.ByteOrder.Uint64(header[:8]), file.ByteOrder.Uint64(header[8:]))
		return string(value), err
	}
	return "", fmt.Errorf("PACK_ENTRY: no main.manifestSHA symbol")
}

// extractLimits bounds what extract reads and writes: the number of entries,
// the total size of regular files, and the whole decompressed stream, which
// also counts the extended headers the tar reader consumes between entries.
// The r4 bundle has about 18 000 entries and 650 MB, so these leave room to
// grow and still stop a crafted layer from filling the disk.
var extractLimits = struct {
	entries       int
	bytes, stream int64
}{100_000, 4 << 30, 5 << 30}

// extract unpacks a layer this program wrote into the new directory dest,
// restoring modes and links; directory modes are set last so a private
// directory can still be filled. It treats the layer as untrusted, since a
// layout can change after it was written: every name must be one layer
// writes, nothing is written below a link the layer made, all writes go
// through an os.Root on dest, extractLimits applies, and the blob must hash
// to layerDigest.
func extract(layer, layerDigest, dest string) error {
	root, err := os.OpenRoot(dest)
	if err != nil {
		return err
	}
	defer root.Close()
	file, err := os.Open(layer)
	if err != nil {
		return err
	}
	defer file.Close()
	sum := sha256.New()
	compressed := io.TeeReader(file, sum)
	unzipped, err := gzip.NewReader(compressed)
	if err != nil {
		return err
	}
	reader := tar.NewReader(&budgeted{unzipped, extractLimits.stream})
	var directories []string
	var modes []fs.FileMode
	links := map[string]bool{}
	count, remaining := 0, extractLimits.bytes
	for {
		h, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if count++; count > extractLimits.entries {
			return fmt.Errorf("PACK_VERIFY: layer has more than %d entries", extractLimits.entries)
		}
		name, err := layerName(h.Name, links)
		if err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := root.Mkdir(name, 0o700); err != nil {
				return err
			}
			directories, modes = append(directories, name), append(modes, fs.FileMode(h.Mode))
		case tar.TypeReg:
			if h.Size > remaining {
				return fmt.Errorf("PACK_VERIFY: layer files exceed %d bytes", extractLimits.bytes)
			}
			remaining -= h.Size
			if err := extractFile(root, name, fs.FileMode(h.Mode), reader); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := root.Symlink(h.Linkname, name); err != nil {
				return err
			}
			links[name] = true
		default:
			return fmt.Errorf("PACK_VERIFY: unexpected entry type: %s", h.Name)
		}
	}
	for i := len(directories) - 1; i >= 0; i-- {
		if err := root.Chmod(directories[i], modes[i]); err != nil {
			return err
		}
	}
	// Whatever follows the tar stream is part of the blob too.
	if _, err := io.Copy(io.Discard, compressed); err != nil {
		return err
	}
	if got := digest(sum); got != layerDigest {
		return fmt.Errorf("PACK_VERIFY: layer blob is %s and does not match %s", got, layerDigest)
	}
	return nil
}

// budgeted reads at most left bytes from r and then fails, so a stream longer
// than its budget is an error, never a silent end.
type budgeted struct {
	r    io.Reader
	left int64
}

func (b *budgeted) Read(p []byte) (int, error) {
	if b.left <= 0 {
		if len(p) == 0 {
			return 0, nil
		}
		// Only a byte past the budget proves the stream too long.
		if n, err := b.r.Read(make([]byte, 1)); n == 0 {
			return 0, err
		}
		return 0, fmt.Errorf("PACK_VERIFY: layer stream exceeds %d bytes", extractLimits.stream)
	}
	if int64(len(p)) > b.left {
		p = p[:b.left]
	}
	n, err := b.r.Read(p)
	b.left -= int64(n)
	return n, err
}

// layerName returns raw without its trailing slash when it is a name layer
// writes: local, clean, host or runtime or below them, and not below a link
// made earlier in the same layer.
func layerName(raw string, links map[string]bool) (string, error) {
	name := strings.TrimSuffix(raw, "/")
	top, _, _ := strings.Cut(name, "/")
	if !filepath.IsLocal(name) || filepath.Clean(name) != name || (top != "host" && top != "runtime") {
		return "", fmt.Errorf("PACK_VERIFY: unexpected entry name: %q", raw)
	}
	for parent := filepath.Dir(name); parent != "."; parent = filepath.Dir(parent) {
		if links[parent] {
			return "", fmt.Errorf("PACK_VERIFY: entry below a link: %q", raw)
		}
	}
	return name, nil
}

// extractFile writes one regular file that must not exist yet; the tar
// reader yields exactly the size its header declares.
func extractFile(root *os.Root, name string, mode fs.FileMode, content io.Reader) error {
	out, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, content)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return root.Chmod(name, mode)
}

// verify extracts the written layer beside out and requires that the bundle
// it carries passes admission with the launcher it carries, and that the
// entry it carries was compiled for manifestSHA. It judges the bytes that
// were archived, whatever happened to the inputs meanwhile, and returns the
// digest (bare hex) of that entry's bytes for the provenance statement.
func verify(out, layerDigest, manifestSHA string) (string, error) {
	parent, err := filepath.EvalSymlinks(filepath.Dir(out))
	if err != nil {
		return "", err
	}
	scratch, err := os.MkdirTemp(parent, ".lz-pack-verify-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(scratch)
	if err := extract(filepath.Join(out, "blobs", "sha256", strings.TrimPrefix(layerDigest, "sha256:")), layerDigest, scratch); err != nil {
		return "", err
	}
	runtime := filepath.Join(scratch, "runtime")
	if _, err := bundle.Admit(runtime, manifestSHA, filepath.Join(scratch, "host", "bwrap")); err != nil {
		return "", fmt.Errorf("PACK_VERIFY: packed bundle refused: %w", err)
	}
	entryPath := filepath.Join(runtime, "lz-offline")
	compiled, err := compiledDigest(entryPath)
	if err != nil {
		return "", err
	}
	if compiled != manifestSHA {
		return "", fmt.Errorf("PACK_VERIFY: entry compiled for manifest %q, not %s", compiled, manifestSHA)
	}
	entry, err := bundle.Regular(entryPath)
	if err != nil {
		return "", fmt.Errorf("PACK_VERIFY: %w", err)
	}
	return bundle.Hash(entry), nil
}

// beforeWrite runs between planning and writing; tests use it to change the
// inputs in that gap.
var beforeWrite = func() {}

// store writes data as a blob and returns its descriptor.
func store(out, mediaType string, value any) (descriptor, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return descriptor{}, err
	}
	sum := sha256.Sum256(data)
	d := descriptor{mediaType, "sha256:" + hex.EncodeToString(sum[:]), int64(len(data))}
	return d, os.WriteFile(filepath.Join(out, "blobs", "sha256", hex.EncodeToString(sum[:])), data, 0o644)
}

// pack admits the bundle, creates out and writes the image layout into it.
// A failed pack removes the output it created, and only that.
func pack(dir, launcher, manifestSHA, out string) (result packed, err error) {
	items, err := admitPack(dir, launcher, manifestSHA, out)
	if err != nil {
		return packed{}, err
	}
	if err := os.Mkdir(out, 0o755); err != nil {
		return packed{}, fmt.Errorf("PACK_OUTPUT: new directory required: %w", err)
	}
	// The output was created here, so a failed pack may remove it.
	defer func() {
		if err != nil {
			os.RemoveAll(out)
		}
	}()
	return writeLayout(out, items, manifestSHA)
}

// admitPack checks the paths, admits the bundle and plans the layer before
// anything is written.
func admitPack(dir, launcher, manifestSHA, out string) ([]item, error) {
	for _, path := range []string{dir, launcher, out} {
		if err := canonical(path); err != nil {
			return nil, err
		}
	}
	items, err := plan(dir, launcher, manifestSHA)
	if err != nil {
		return nil, err
	}
	return items, outside(dir, out)
}

// writeLayout writes the layer, verifies what it carries, then writes the
// config, manifest and index that make out an image layout.
func writeLayout(out string, items []item, manifestSHA string) (packed, error) {
	blobs := filepath.Join(out, "blobs", "sha256")
	if err := os.MkdirAll(blobs, 0o755); err != nil {
		return packed{}, err
	}
	layerDigest, layerSize, diffID, err := writeLayer(blobs, items)
	if err != nil {
		return packed{}, err
	}
	entry, err := verify(out, layerDigest, manifestSHA)
	if err != nil {
		return packed{}, err
	}
	manifest, err := writeIndex(out, descriptor{"application/vnd.oci.image.layer.v1.tar+gzip", layerDigest, layerSize}, diffID)
	if err != nil {
		return packed{}, err
	}
	return packed{layerDigest, manifest, entry}, nil
}

// writeLayer writes the layer blob under its digest and returns the digest,
// its size and the uncompressed tar's digest.
func writeLayer(blobs string, items []item) (string, int64, string, error) {
	file, err := os.CreateTemp(blobs, "layer-")
	if err != nil {
		return "", 0, "", err
	}
	beforeWrite()
	layerDigest, layerSize, diffID, err := layer(file, items)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", 0, "", err
	}
	if err := os.Rename(file.Name(), filepath.Join(blobs, strings.TrimPrefix(layerDigest, "sha256:"))); err != nil {
		return "", 0, "", err
	}
	return layerDigest, layerSize, diffID, nil
}

// writeIndex writes the config and manifest blobs for the layer, then
// index.json and oci-layout, and returns the manifest's digest.
func writeIndex(out string, layerBlob descriptor, diffID string) (string, error) {
	config, err := store(out, "application/vnd.oci.image.config.v1+json", map[string]any{
		"architecture": "amd64", "os": "linux", "config": map[string]any{},
		"rootfs": map[string]any{"type": "layers", "diff_ids": []string{diffID}},
	})
	if err != nil {
		return "", err
	}
	manifest, err := store(out, "application/vnd.oci.image.manifest.v1+json", map[string]any{
		"schemaVersion": 2, "mediaType": "application/vnd.oci.image.manifest.v1+json", "config": config,
		"layers":      []descriptor{layerBlob},
		"annotations": map[string]string{"org.opencontainers.image.source": source},
	})
	if err != nil {
		return "", err
	}
	index, err := json.Marshal(map[string]any{"schemaVersion": 2, "mediaType": "application/vnd.oci.image.index.v1+json", "manifests": []descriptor{manifest}})
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(out, "index.json"), index, 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(out, "oci-layout"), []byte(`{"imageLayoutVersion":"1.0.0"}`), 0o644); err != nil {
		return "", err
	}
	return manifest.Digest, nil
}

// attestedPack packs as pack does and writes the image's provenance
// statement into the layout (FR-017). The commit's form is checked before
// anything is written, and a statement that cannot be rendered or written
// removes the layout pack created. Like pack's own removal, this covers
// failures lz-pack sees, not a process killed between the two writes.
func attestedPack(dir, launcher, manifestSHA, out, commit string) (packed, error) {
	if err := checkCommit(commit); err != nil {
		return packed{}, err
	}
	result, err := pack(dir, launcher, manifestSHA, out)
	if err != nil {
		return packed{}, err
	}
	if err := writeStatement(out, result, manifestSHA, commit); err != nil {
		os.RemoveAll(out)
		return packed{}, err
	}
	return result, nil
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "prepare" {
		os.Exit(prepareMain(os.Args[2:], os.Stdout, os.Stderr))
	}
	dir := flag.String("bundle", "", "prepared runtime bundle directory (absolute)")
	manifestSHA := flag.String("manifest-sha", "", "resources.json digest the bundle's entry was built with")
	launcher := flag.String("bwrap", "/usr/bin/bwrap", "launcher the bundle manifest pins (absolute)")
	out := flag.String("out", "", "new OCI image layout directory (absolute)")
	commit := flag.String("commit", "", "full source commit id the bundle was built from, for the provenance statement")
	flag.Parse()
	if *dir == "" || *manifestSHA == "" || *out == "" || *commit == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: lz-pack -bundle <dir> -manifest-sha <hex> [-bwrap <file>] -out <new-dir> -commit <sha>")
		os.Exit(2)
	}
	result, err := attestedPack(*dir, *launcher, *manifestSHA, *out, *commit)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("layer %s\nmanifest %s\n", result.Layer, result.Manifest)
}
