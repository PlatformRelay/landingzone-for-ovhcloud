package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// bundle writes a small runtime bundle and launcher with the shapes the real
// one has: an executable entry, a private directory, a manifest and a rootfs
// link that stays inside the bundle.
func bundle(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "bundle")
	for _, d := range []string{"resources/rootfs/bin", "resources/rootfs/usr/bin"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(filepath.Join(dir, "resources"), 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string]os.FileMode{"lz-offline": 0o775, "resources.json": 0o664, "resources/rootfs/bin/busybox": 0o755}
	for name, mode := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("content of "+name), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Join(dir, name), mode); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("/bin/busybox", filepath.Join(dir, "resources/rootfs/usr/bin/sh")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../bin/busybox", filepath.Join(dir, "resources/rootfs/usr/bin/ls")); err != nil {
		t.Fatal(err)
	}
	// A link may resolve to the rootfs itself.
	if err := os.Symlink("..", filepath.Join(dir, "resources/rootfs/usr/top")); err != nil {
		t.Fatal(err)
	}
	// Names and link targets past the 100-byte ustar limit need PAX records.
	long := filepath.Join(dir, "resources/rootfs", strings.Repeat("d", 60), strings.Repeat("f", 60))
	if err := os.MkdirAll(filepath.Dir(long), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(long, []byte("long"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/"+strings.Repeat("d", 60)+"/"+strings.Repeat("f", 60), filepath.Join(dir, "resources/rootfs/bin/long")); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(root, "bwrap")
	if err := os.WriteFile(launcher, []byte("launcher"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir, launcher
}

func digestOf(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func blob(layout, digest string) string {
	return filepath.Join(layout, "blobs", "sha256", strings.TrimPrefix(digest, "sha256:"))
}

type entry struct {
	mode     int64
	kind     byte
	link     string
	content  string
	uid, gid int
	modTime  time.Time
}

func layerEntries(t *testing.T, path string) ([]string, map[string]entry) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	unzipped, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	if !unzipped.ModTime.IsZero() || unzipped.Name != "" || unzipped.Comment != "" || unzipped.Extra != nil {
		t.Errorf("BEHAVIORAL_RED: gzip header carries host data: %+v", unzipped.Header)
	}
	reader := tar.NewReader(unzipped)
	var names []string
	entries := map[string]entry{}
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		content, _ := io.ReadAll(reader)
		names = append(names, header.Name)
		entries[header.Name] = entry{header.Mode, header.Typeflag, header.Linkname, string(content), header.Uid, header.Gid, header.ModTime}
	}
	return names, entries
}

// The layer holds the bundle under runtime/ and the launcher under host/,
// with modes, link targets and contents intact and no host identity.
func TestPackRoundTrip(t *testing.T) {
	dir, launcher := bundle(t)
	out := filepath.Join(t.TempDir(), "layout")
	result, err := pack(dir, launcher, out)
	if err != nil {
		t.Fatal(err)
	}
	if digestOf(t, blob(out, result.Layer)) != result.Layer || digestOf(t, blob(out, result.Manifest)) != result.Manifest {
		t.Fatalf("BEHAVIORAL_RED: blobs are not stored under their digests: %+v", result)
	}
	names, entries := layerEntries(t, blob(out, result.Layer))
	longDir := "runtime/resources/rootfs/" + strings.Repeat("d", 60) + "/"
	want := []string{"host/", "host/bwrap", "runtime/", "runtime/lz-offline", "runtime/resources/", "runtime/resources/rootfs/",
		"runtime/resources/rootfs/bin/", "runtime/resources/rootfs/bin/busybox", "runtime/resources/rootfs/bin/long", longDir, longDir + strings.Repeat("f", 60),
		"runtime/resources/rootfs/usr/", "runtime/resources/rootfs/usr/bin/", "runtime/resources/rootfs/usr/bin/ls", "runtime/resources/rootfs/usr/bin/sh",
		"runtime/resources/rootfs/usr/top", "runtime/resources.json"}
	if strings.Join(names, " ") != strings.Join(want, " ") {
		t.Errorf("BEHAVIORAL_RED: layer entries\n got %v\nwant %v", names, want)
	}
	checks := map[string]entry{
		"runtime/lz-offline":                   {mode: 0o775, kind: tar.TypeReg, content: "content of lz-offline"},
		"runtime/resources.json":               {mode: 0o664, kind: tar.TypeReg, content: "content of resources.json"},
		"runtime/resources/":                   {mode: 0o700, kind: tar.TypeDir},
		"runtime/resources/rootfs/usr/bin/sh":  {mode: 0o777, kind: tar.TypeSymlink, link: "/bin/busybox"},
		"runtime/resources/rootfs/bin/busybox": {mode: 0o755, kind: tar.TypeReg, content: "content of resources/rootfs/bin/busybox"},
		"host/bwrap":                           {mode: 0o755, kind: tar.TypeReg, content: "launcher"},
		"runtime/resources/rootfs/usr/bin/ls":  {mode: 0o777, kind: tar.TypeSymlink, link: "../../bin/busybox"},
		"runtime/resources/rootfs/bin/long":    {mode: 0o777, kind: tar.TypeSymlink, link: "/" + strings.Repeat("d", 60) + "/" + strings.Repeat("f", 60)},
		longDir + strings.Repeat("f", 60):      {mode: 0o644, kind: tar.TypeReg, content: "long"},
	}
	for name, w := range checks {
		got := entries[name]
		if got.mode != w.mode || got.kind != w.kind || got.link != w.link || got.content != w.content {
			t.Errorf("BEHAVIORAL_RED: %s is %+v, want %+v", name, got, w)
		}
	}
	for name, got := range entries {
		if got.uid != 0 || got.gid != 0 || !got.modTime.Equal(time.Unix(0, 0)) {
			t.Errorf("BEHAVIORAL_RED: %s carries host identity or time: %+v", name, got)
		}
	}
}

// The same bundle packs to the same digests, whatever its timestamps, so
// anyone holding the bundle can reproduce the published image.
func TestPackIsReproducible(t *testing.T) {
	dir, launcher := bundle(t)
	first, err := pack(dir, launcher, filepath.Join(t.TempDir(), "a"))
	if err != nil {
		t.Fatal(err)
	}
	later := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, name := range []string{"lz-offline", "resources", "resources/rootfs/bin/busybox"} {
		if err := os.Chtimes(filepath.Join(dir, name), later, later); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(launcher, later, later); err != nil {
		t.Fatal(err)
	}
	second, err := pack(dir, launcher, filepath.Join(t.TempDir(), "b"))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("BEHAVIORAL_RED: digests differ: %+v %+v", first, second)
	}
}

// The layout is a valid OCI image: the index names the manifest, the
// manifest names the config and the layer with their sizes, the config's
// diff_id is the uncompressed layer, and the source annotation links the
// package to this repository.
func TestPackLayout(t *testing.T) {
	dir, launcher := bundle(t)
	out := filepath.Join(t.TempDir(), "layout")
	result, err := pack(dir, launcher, out)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(out, "oci-layout")); err != nil || string(data) != `{"imageLayoutVersion":"1.0.0"}` {
		t.Errorf("BEHAVIORAL_RED: oci-layout %q %v", data, err)
	}
	var index struct {
		SchemaVersion int `json:"schemaVersion"`
		Manifests     []struct {
			MediaType string `json:"mediaType"`
			Digest    string `json:"digest"`
		} `json:"manifests"`
	}
	read := func(path string, value any) {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, value); err != nil {
			t.Fatal(err)
		}
	}
	read(filepath.Join(out, "index.json"), &index)
	if index.SchemaVersion != 2 || len(index.Manifests) != 1 || index.Manifests[0].Digest != result.Manifest || index.Manifests[0].MediaType != "application/vnd.oci.image.manifest.v1+json" {
		t.Errorf("BEHAVIORAL_RED: index %+v", index)
	}
	type descriptor struct {
		MediaType string `json:"mediaType"`
		Digest    string `json:"digest"`
		Size      int64  `json:"size"`
	}
	var manifest struct {
		SchemaVersion int               `json:"schemaVersion"`
		MediaType     string            `json:"mediaType"`
		Config        descriptor        `json:"config"`
		Layers        []descriptor      `json:"layers"`
		Annotations   map[string]string `json:"annotations"`
	}
	read(blob(out, result.Manifest), &manifest)
	size := func(digest string) int64 {
		info, err := os.Stat(blob(out, digest))
		if err != nil {
			t.Fatal(err)
		}
		return info.Size()
	}
	if manifest.SchemaVersion != 2 || manifest.MediaType != "application/vnd.oci.image.manifest.v1+json" ||
		manifest.Config.MediaType != "application/vnd.oci.image.config.v1+json" || manifest.Config.Size != size(manifest.Config.Digest) ||
		len(manifest.Layers) != 1 || manifest.Layers[0].Digest != result.Layer || manifest.Layers[0].Size != size(result.Layer) ||
		manifest.Layers[0].MediaType != "application/vnd.oci.image.layer.v1.tar+gzip" ||
		manifest.Annotations["org.opencontainers.image.source"] != "https://github.com/PlatformRelay/landingzone-for-ovhcloud" {
		t.Errorf("BEHAVIORAL_RED: manifest %+v", manifest)
	}
	if digestOf(t, blob(out, manifest.Config.Digest)) != manifest.Config.Digest {
		t.Errorf("BEHAVIORAL_RED: config not stored under its digest")
	}
	var config struct {
		Architecture string `json:"architecture"`
		OS           string `json:"os"`
		RootFS       struct {
			Type    string   `json:"type"`
			DiffIDs []string `json:"diff_ids"`
		} `json:"rootfs"`
	}
	read(blob(out, manifest.Config.Digest), &config)
	compressed, err := os.Open(blob(out, result.Layer))
	if err != nil {
		t.Fatal(err)
	}
	defer compressed.Close()
	unzipped, err := gzip.NewReader(compressed)
	if err != nil {
		t.Fatal(err)
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, unzipped); err != nil {
		t.Fatal(err)
	}
	diffID := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
	if config.Architecture != "amd64" || config.OS != "linux" || config.RootFS.Type != "layers" || len(config.RootFS.DiffIDs) != 1 || config.RootFS.DiffIDs[0] != diffID {
		t.Errorf("BEHAVIORAL_RED: config %+v, diff_id %s", config, diffID)
	}
}

// Anything the entry would refuse, or that a tar layer cannot carry
// faithfully, stops packing: hard links, special files, a launcher that is
// not a regular file, a missing entry or manifest, and an existing output.
func TestPackRefuses(t *testing.T) {
	for name, damage := range map[string]func(t *testing.T, dir, launcher, out string) (string, string, string){
		"hard link": func(t *testing.T, dir, launcher, out string) (string, string, string) {
			if err := os.Link(filepath.Join(dir, "resources.json"), filepath.Join(dir, "resources/copy")); err != nil {
				t.Fatal(err)
			}
			return dir, launcher, out
		},
		"named pipe": func(t *testing.T, dir, launcher, out string) (string, string, string) {
			if err := syscall.Mkfifo(filepath.Join(dir, "resources/pipe"), 0o600); err != nil {
				t.Fatal(err)
			}
			return dir, launcher, out
		},
		"launcher is a link": func(t *testing.T, dir, launcher, out string) (string, string, string) {
			link := launcher + "-link"
			if err := os.Symlink(launcher, link); err != nil {
				t.Fatal(err)
			}
			return dir, link, out
		},
		"no entry": func(t *testing.T, dir, launcher, out string) (string, string, string) {
			if err := os.Remove(filepath.Join(dir, "lz-offline")); err != nil {
				t.Fatal(err)
			}
			return dir, launcher, out
		},
		"no manifest": func(t *testing.T, dir, launcher, out string) (string, string, string) {
			if err := os.Remove(filepath.Join(dir, "resources.json")); err != nil {
				t.Fatal(err)
			}
			return dir, launcher, out
		},
		"bundle is a link": func(t *testing.T, dir, launcher, out string) (string, string, string) {
			link := dir + "-link"
			if err := os.Symlink(dir, link); err != nil {
				t.Fatal(err)
			}
			return link, launcher, out
		},
		"no resources directory": func(t *testing.T, dir, launcher, out string) (string, string, string) {
			if err := os.RemoveAll(filepath.Join(dir, "resources")); err != nil {
				t.Fatal(err)
			}
			return dir, launcher, out
		},
		"link outside the rootfs": func(t *testing.T, dir, launcher, out string) (string, string, string) {
			if err := os.Symlink("/tmp/tool", filepath.Join(dir, "resources/tool")); err != nil {
				t.Fatal(err)
			}
			return dir, launcher, out
		},
		"link escaping the rootfs": func(t *testing.T, dir, launcher, out string) (string, string, string) {
			if err := os.Symlink("../../../../outside", filepath.Join(dir, "resources/rootfs/bin/out")); err != nil {
				t.Fatal(err)
			}
			return dir, launcher, out
		},
		"absolute link escaping the rootfs": func(t *testing.T, dir, launcher, out string) (string, string, string) {
			// Counted from the rootfs, as the entry does, this leaves it.
			if err := os.Symlink("/../outside", filepath.Join(dir, "resources/rootfs/usr/bin/esc")); err != nil {
				t.Fatal(err)
			}
			return dir, launcher, out
		},
		"output inside a bundle reached through a linked parent": func(t *testing.T, dir, launcher, out string) (string, string, string) {
			link := filepath.Join(t.TempDir(), "via")
			if err := os.Symlink(filepath.Dir(dir), link); err != nil {
				t.Fatal(err)
			}
			return filepath.Join(link, filepath.Base(dir)), launcher, filepath.Join(dir, "layout")
		},
		"link to the rootfs parent": func(t *testing.T, dir, launcher, out string) (string, string, string) {
			if err := os.Symlink("..", filepath.Join(dir, "resources/rootfs/up")); err != nil {
				t.Fatal(err)
			}
			return dir, launcher, out
		},
		"link in the bundle root": func(t *testing.T, dir, launcher, out string) (string, string, string) {
			if err := os.Symlink("resources.json", filepath.Join(dir, "alias.json")); err != nil {
				t.Fatal(err)
			}
			return dir, launcher, out
		},
		"setuid file": func(t *testing.T, dir, launcher, out string) (string, string, string) {
			if err := os.Chmod(filepath.Join(dir, "resources/rootfs/bin/busybox"), 0o755|fs.ModeSetuid); err != nil {
				t.Fatal(err)
			}
			return dir, launcher, out
		},
		"sticky directory": func(t *testing.T, dir, launcher, out string) (string, string, string) {
			if err := os.Chmod(filepath.Join(dir, "resources/rootfs/usr"), 0o777|fs.ModeSticky); err != nil {
				t.Fatal(err)
			}
			return dir, launcher, out
		},
		"output inside the bundle": func(t *testing.T, dir, launcher, out string) (string, string, string) {
			return dir, launcher, filepath.Join(dir, "layout")
		},
		"output inside the bundle through a link": func(t *testing.T, dir, launcher, out string) (string, string, string) {
			link := filepath.Join(t.TempDir(), "via")
			if err := os.Symlink(dir, link); err != nil {
				t.Fatal(err)
			}
			return dir, launcher, filepath.Join(link, "layout")
		},
		"unreadable file": func(t *testing.T, dir, launcher, out string) (string, string, string) {
			if err := os.Chmod(filepath.Join(dir, "resources/rootfs/bin/busybox"), 0o000); err != nil {
				t.Fatal(err)
			}
			return dir, launcher, out
		},
		"output exists": func(t *testing.T, dir, launcher, out string) (string, string, string) {
			if err := os.Mkdir(out, 0o755); err != nil {
				t.Fatal(err)
			}
			return dir, launcher, out
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir, launcher := bundle(t)
			out := filepath.Join(t.TempDir(), "layout")
			dir, launcher, out = damage(t, dir, launcher, out)
			if result, err := pack(dir, launcher, out); err == nil {
				t.Errorf("BEHAVIORAL_RED: %s packed: %+v", name, result)
			}
			// A refused pack leaves nothing behind, and never removes a
			// directory it did not create.
			_, err := os.Lstat(out)
			if exists := err == nil; exists != (name == "output exists") {
				t.Errorf("BEHAVIORAL_RED: %s left output present=%v", name, exists)
			}
		})
	}
}
