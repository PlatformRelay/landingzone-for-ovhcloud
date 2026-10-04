// lz-pack packs a prepared runtime bundle and its launcher into a
// single-layer OCI image layout, reproducibly: entries are sorted and carry
// no host owner or time, so the same bundle always yields the same digests.
// The layer holds runtime/ (the bundle with its entry) and host/bwrap.
package main

import (
	"archive/tar"
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
	"syscall"
	"time"
)

const source = "https://github.com/PlatformRelay/landingzone-for-ovhcloud"

type packed struct{ Layer, Manifest string }

type descriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
}

func digest(h hash.Hash) string { return "sha256:" + hex.EncodeToString(h.Sum(nil)) }

// regular returns the file's metadata if it is a regular file reached
// without following a link.
func regular(path string) (fs.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("PACK_INPUT: regular non-link file required: %s", path)
	}
	return info, nil
}

// header describes one entry without host identity or time.
func header(name string, info fs.FileInfo) (*tar.Header, error) {
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

func add(w *tar.Writer, name, path string, info fs.FileInfo) error {
	h, err := header(name, info)
	if err != nil {
		return err
	}
	if h.Typeflag == tar.TypeSymlink {
		if h.Linkname, err = os.Readlink(path); err != nil {
			return err
		}
	}
	if err := w.WriteHeader(h); err != nil {
		return err
	}
	if h.Typeflag != tar.TypeReg {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	// tar.Writer refuses a file that grew or shrank since its header.
	_, err = io.Copy(w, file)
	return err
}

// layer writes the gzip-compressed tar to file and returns its digest, size
// and the digest of the uncompressed tar (the config's diff_id).
func layer(file *os.File, bundle, launcher string) (string, int64, string, error) {
	compressed, uncompressed := sha256.New(), sha256.New()
	counter := &count{}
	zipper, err := gzip.NewWriterLevel(io.MultiWriter(file, compressed, counter), gzip.BestCompression)
	if err != nil {
		return "", 0, "", err
	}
	archive := tar.NewWriter(io.MultiWriter(zipper, uncompressed))
	launcherInfo, err := regular(launcher)
	if err != nil {
		return "", 0, "", err
	}
	// Only the launcher comes from its host directory, not that directory's mode.
	if err := archive.WriteHeader(&tar.Header{Name: "host/", Typeflag: tar.TypeDir, Mode: 0o755, ModTime: time.Unix(0, 0)}); err != nil {
		return "", 0, "", err
	}
	if err := add(archive, "host/bwrap", launcher, launcherInfo); err != nil {
		return "", 0, "", err
	}
	err = filepath.WalkDir(bundle, func(path string, entry fs.DirEntry, incoming error) error {
		if incoming != nil {
			return incoming
		}
		relative, err := filepath.Rel(bundle, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		name := filepath.ToSlash(filepath.Join("runtime", relative))
		return add(archive, name, path, info)
	})
	if err != nil {
		return "", 0, "", err
	}
	if err := archive.Close(); err != nil {
		return "", 0, "", err
	}
	if err := zipper.Close(); err != nil {
		return "", 0, "", err
	}
	return digest(compressed), counter.n, digest(uncompressed), nil
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

func pack(bundle, launcher, out string) (packed, error) {
	info, err := os.Lstat(bundle)
	if err != nil || !info.IsDir() {
		return packed{}, fmt.Errorf("PACK_INPUT: bundle must be a real directory: %s", bundle)
	}
	for _, name := range []string{"lz-offline", "resources.json"} {
		if _, err := regular(filepath.Join(bundle, name)); err != nil {
			return packed{}, err
		}
	}
	if err := os.Mkdir(out, 0o755); err != nil {
		return packed{}, fmt.Errorf("PACK_OUTPUT: new directory required: %w", err)
	}
	blobs := filepath.Join(out, "blobs", "sha256")
	if err := os.MkdirAll(blobs, 0o755); err != nil {
		return packed{}, err
	}
	file, err := os.CreateTemp(blobs, "layer-")
	if err != nil {
		return packed{}, err
	}
	layerDigest, layerSize, diffID, err := layer(file, bundle, launcher)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return packed{}, err
	}
	if err := os.Rename(file.Name(), filepath.Join(blobs, layerDigest[len("sha256:"):])); err != nil {
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
	bundle := flag.String("bundle", "", "prepared runtime bundle directory")
	launcher := flag.String("bwrap", "/usr/bin/bwrap", "launcher the bundle manifest pins")
	out := flag.String("out", "", "new OCI image layout directory")
	flag.Parse()
	if *bundle == "" || *out == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: lz-pack -bundle <dir> [-bwrap <file>] -out <new-dir>")
		os.Exit(2)
	}
	result, err := pack(*bundle, *launcher, *out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("layer %s\nmanifest %s\n", result.Layer, result.Manifest)
}
