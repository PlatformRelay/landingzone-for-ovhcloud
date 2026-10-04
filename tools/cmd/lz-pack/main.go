// lz-pack packs a prepared runtime bundle and its launcher into a
// single-layer OCI image layout, reproducibly: entries are sorted and carry
// no host owner or time, so the same bundle always yields the same digests.
// The layer holds runtime/ (the bundle with its entry) and host/bwrap.
//
// Before anything is written the bundle passes the entry's own admission
// (internal/bundle) for the manifest digest the entry was built with, and the
// entry must carry that digest, so a layout is produced only for a bundle the
// entry admits.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
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

// plan admits the bundle as the entry would, checks that the entry carries
// the manifest digest, and returns the layer's entries in order: host/,
// host/bwrap, then the bundle under runtime/ in walk order.
func plan(dir, launcher, manifestSHA string) ([]item, error) {
	if _, err := bundle.Admit(dir, manifestSHA, launcher); err != nil {
		return nil, err
	}
	entry, err := bundle.Regular(filepath.Join(dir, "lz-offline"))
	if err != nil {
		return nil, err
	}
	if !bytes.Contains(entry, []byte(manifestSHA)) {
		return nil, fmt.Errorf("PACK_INPUT: entry not built for manifest %s", manifestSHA)
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
