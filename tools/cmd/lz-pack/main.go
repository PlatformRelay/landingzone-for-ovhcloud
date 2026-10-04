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

type packed struct{ Layer, Manifest string }

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

// extract unpacks a layer this program wrote into the new directory dest,
// restoring modes and links; directory modes are set last so a private
// directory can still be filled.
func extract(layer, dest string) error {
	file, err := os.Open(layer)
	if err != nil {
		return err
	}
	defer file.Close()
	unzipped, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	reader := tar.NewReader(unzipped)
	var directories []*tar.Header
	for {
		h, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		// The names are the ones layer wrote: host/ and runtime/ only.
		path := filepath.Join(dest, h.Name)
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.Mkdir(path, 0o700); err != nil {
				return err
			}
			directories = append(directories, h)
		case tar.TypeReg:
			out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if err != nil {
				return err
			}
			_, err = io.Copy(out, reader)
			if closeErr := out.Close(); err == nil {
				err = closeErr
			}
			if err == nil {
				err = os.Chmod(path, fs.FileMode(h.Mode))
			}
			if err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := os.Symlink(h.Linkname, path); err != nil {
				return err
			}
		default:
			return fmt.Errorf("PACK_VERIFY: unexpected entry type: %s", h.Name)
		}
	}
	for i := len(directories) - 1; i >= 0; i-- {
		if err := os.Chmod(filepath.Join(dest, directories[i].Name), fs.FileMode(directories[i].Mode)); err != nil {
			return err
		}
	}
	return nil
}

// verify extracts the written layer beside out and requires that the bundle
// it carries passes admission with the launcher it carries, and that the
// entry it carries was compiled for manifestSHA. It judges the bytes that
// were archived, whatever happened to the inputs meanwhile.
func verify(out, layerDigest, manifestSHA string) error {
	parent, err := filepath.EvalSymlinks(filepath.Dir(out))
	if err != nil {
		return err
	}
	scratch, err := os.MkdirTemp(parent, ".lz-pack-verify-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)
	if err := extract(filepath.Join(out, "blobs", "sha256", strings.TrimPrefix(layerDigest, "sha256:")), scratch); err != nil {
		return err
	}
	runtime := filepath.Join(scratch, "runtime")
	if _, err := bundle.Admit(runtime, manifestSHA, filepath.Join(scratch, "host", "bwrap")); err != nil {
		return fmt.Errorf("PACK_VERIFY: packed bundle refused: %w", err)
	}
	compiled, err := compiledDigest(filepath.Join(runtime, "lz-offline"))
	if err != nil {
		return err
	}
	if compiled != manifestSHA {
		return fmt.Errorf("PACK_VERIFY: entry compiled for manifest %q, not %s", compiled, manifestSHA)
	}
	return nil
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

func pack(dir, launcher, manifestSHA, out string) (result packed, err error) {
	for _, path := range []string{dir, launcher, out} {
		if err := canonical(path); err != nil {
			return packed{}, err
		}
	}
	items, err := plan(dir, launcher, manifestSHA)
	if err != nil {
		return packed{}, err
	}
	if err := outside(dir, out); err != nil {
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
	blobs := filepath.Join(out, "blobs", "sha256")
	if err := os.MkdirAll(blobs, 0o755); err != nil {
		return packed{}, err
	}
	file, err := os.CreateTemp(blobs, "layer-")
	if err != nil {
		return packed{}, err
	}
	beforeWrite()
	layerDigest, layerSize, diffID, err := layer(file, items)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return packed{}, err
	}
	if err := os.Rename(file.Name(), filepath.Join(blobs, strings.TrimPrefix(layerDigest, "sha256:"))); err != nil {
		return packed{}, err
	}
	if err := verify(out, layerDigest, manifestSHA); err != nil {
		return packed{}, err
	}
	config, err := store(out, "application/vnd.oci.image.config.v1+json", map[string]any{
		"architecture": "amd64", "os": "linux", "config": map[string]any{},
		"rootfs": map[string]any{"type": "layers", "diff_ids": []string{diffID}},
	})
	if err != nil {
		return packed{}, err
	}
	manifest, err := store(out, "application/vnd.oci.image.manifest.v1+json", map[string]any{
		"schemaVersion": 2, "mediaType": "application/vnd.oci.image.manifest.v1+json", "config": config,
		"layers":      []descriptor{{"application/vnd.oci.image.layer.v1.tar+gzip", layerDigest, layerSize}},
		"annotations": map[string]string{"org.opencontainers.image.source": source},
	})
	if err != nil {
		return packed{}, err
	}
	index, err := json.Marshal(map[string]any{"schemaVersion": 2, "mediaType": "application/vnd.oci.image.index.v1+json", "manifests": []descriptor{manifest}})
	if err != nil {
		return packed{}, err
	}
	if err := os.WriteFile(filepath.Join(out, "index.json"), index, 0o644); err != nil {
		return packed{}, err
	}
	if err := os.WriteFile(filepath.Join(out, "oci-layout"), []byte(`{"imageLayoutVersion":"1.0.0"}`), 0o644); err != nil {
		return packed{}, err
	}
	return packed{layerDigest, manifest.Digest}, nil
}

func main() {
	dir := flag.String("bundle", "", "prepared runtime bundle directory (absolute)")
	manifestSHA := flag.String("manifest-sha", "", "resources.json digest the bundle's entry was built with")
	launcher := flag.String("bwrap", "/usr/bin/bwrap", "launcher the bundle manifest pins (absolute)")
	out := flag.String("out", "", "new OCI image layout directory (absolute)")
	flag.Parse()
	if *dir == "" || *manifestSHA == "" || *out == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: lz-pack -bundle <dir> -manifest-sha <hex> [-bwrap <file>] -out <new-dir>")
		os.Exit(2)
	}
	result, err := pack(*dir, *launcher, *manifestSHA, *out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("layer %s\nmanifest %s\n", result.Layer, result.Manifest)
}
