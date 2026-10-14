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
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/bundle/bundletest"
)

var (
	entriesMu sync.Mutex
	entries   = map[string][]byte{}
)

// compiledEntry returns a real entry binary with manifestSHA compiled in as
// the reviewed build recipe does, built once per digest.
func compiledEntry(t *testing.T, manifestSHA string) []byte {
	t.Helper()
	entriesMu.Lock()
	defer entriesMu.Unlock()
	if data, ok := entries[manifestSHA]; ok {
		return data
	}
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "go.mod"), []byte("module entry\n\ngo 1.27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "main.go"), []byte("package main\n\nvar manifestSHA string\n\nfunc main() { println(manifestSHA) }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(source, "lz-offline")
	build := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-ldflags", "-X main.manifestSHA="+manifestSHA, "-o", binary, ".")
	build.Dir = source
	build.Env = append(os.Environ(), "GOFLAGS=", "GOPROXY=off", "GOTOOLCHAIN=local", "CGO_ENABLED=0")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building a test entry: %v\n%s", err, output)
	}
	data, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	entries[manifestSHA] = data
	return data
}

func writeEntry(t *testing.T, b bundletest.Bundle, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(b.Dir, "lz-offline"), data, 0o775); err != nil {
		t.Fatal(err)
	}
}

// prepared writes a bundle the entry admits, with a name and a link target
// past the 100-byte ustar limit that need PAX records, a probe beside the
// manifest as the real bundle has, and a real entry compiled for its
// manifest.
func prepared(t *testing.T) bundletest.Bundle {
	t.Helper()
	long := filepath.Join("resources/rootfs", strings.Repeat("d", 60), strings.Repeat("f", 60))
	b := bundletest.Write(t, func(dir string) {
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(long)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, long), []byte("long"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("/"+strings.Repeat("d", 60)+"/"+strings.Repeat("f", 60), filepath.Join(dir, "resources/rootfs/bin/long")); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "probe"), []byte("probe"), 0o775); err != nil {
			t.Fatal(err)
		}
	})
	writeEntry(t, b, compiledEntry(t, b.ManifestSHA))
	return b
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
	b := prepared(t)
	dir, launcher := b.Dir, b.Launcher
	out := filepath.Join(t.TempDir(), "layout")
	result, err := pack(dir, launcher, b.ManifestSHA, out)
	if err != nil {
		t.Fatal(err)
	}
	// Verification extracts the layer beside the output and removes it again.
	if siblings, err := os.ReadDir(filepath.Dir(out)); err != nil || len(siblings) != 1 {
		t.Errorf("BEHAVIORAL_RED: beside the output: %v %v", siblings, err)
	}
	if digestOf(t, blob(out, result.Layer)) != result.Layer || digestOf(t, blob(out, result.Manifest)) != result.Manifest {
		t.Fatalf("BEHAVIORAL_RED: blobs are not stored under their digests: %+v", result)
	}
	names, entries := layerEntries(t, blob(out, result.Layer))
	manifest, err := os.ReadFile(filepath.Join(dir, "resources.json"))
	if err != nil {
		t.Fatal(err)
	}
	longDir := "runtime/resources/rootfs/" + strings.Repeat("d", 60) + "/"
	want := []string{"host/", "host/bwrap", "runtime/", "runtime/lz-offline", "runtime/probe", "runtime/resources/", "runtime/resources/rootfs/",
		"runtime/resources/rootfs/bin/", "runtime/resources/rootfs/bin/busybox", "runtime/resources/rootfs/bin/long", longDir, longDir + strings.Repeat("f", 60),
		"runtime/resources/rootfs/usr/", "runtime/resources/rootfs/usr/bin/", "runtime/resources/rootfs/usr/bin/ls", "runtime/resources/rootfs/usr/bin/sh",
		"runtime/resources/rootfs/usr/top", "runtime/resources/tofu", "runtime/resources.json"}
	if strings.Join(names, " ") != strings.Join(want, " ") {
		t.Errorf("BEHAVIORAL_RED: layer entries\n got %v\nwant %v", names, want)
	}
	checks := map[string]entry{
		"runtime/lz-offline":                   {mode: 0o775, kind: tar.TypeReg, content: string(compiledEntry(t, b.ManifestSHA))},
		"runtime/resources.json":               {mode: 0o664, kind: tar.TypeReg, content: string(manifest)},
		"runtime/resources/":                   {mode: 0o700, kind: tar.TypeDir},
		"runtime/resources/rootfs/usr/bin/sh":  {mode: 0o777, kind: tar.TypeSymlink, link: "/bin/busybox"},
		"runtime/resources/rootfs/bin/busybox": {mode: 0o755, kind: tar.TypeReg, content: "busybox"},
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
	b := prepared(t)
	dir, launcher := b.Dir, b.Launcher
	first, err := pack(dir, launcher, b.ManifestSHA, filepath.Join(t.TempDir(), "a"))
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
	second, err := pack(dir, launcher, b.ManifestSHA, filepath.Join(t.TempDir(), "b"))
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
	b := prepared(t)
	dir, launcher := b.Dir, b.Launcher
	out := filepath.Join(t.TempDir(), "layout")
	result, err := pack(dir, launcher, b.ManifestSHA, out)
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

// setBeforeWrite installs a change to run between planning and writing.
func setBeforeWrite(t *testing.T, change func()) {
	t.Helper()
	beforeWrite = change
	t.Cleanup(func() { beforeWrite = func() {} })
}

// A bundle the entry would refuse, an entry built for another manifest, and
// anything a tar layer cannot carry faithfully stop packing, as do paths that
// are not absolute and clean and an output inside the bundle or already
// present. A refused pack leaves no output behind.
func TestPackRefuses(t *testing.T) {
	for name, damage := range map[string]func(t *testing.T, b *bundletest.Bundle, out *string){
		"other manifest digest": func(t *testing.T, b *bundletest.Bundle, out *string) { b.ManifestSHA = strings.Repeat("0", 64) },
		"changed resource": func(t *testing.T, b *bundletest.Bundle, out *string) {
			if err := os.WriteFile(filepath.Join(b.Dir, "resources/tofu"), []byte("TOFU"), 0o700); err != nil {
				t.Fatal(err)
			}
		},
		"other launcher": func(t *testing.T, b *bundletest.Bundle, out *string) {
			if err := os.WriteFile(b.Launcher, []byte("other"), 0o755); err != nil {
				t.Fatal(err)
			}
		},
		"entry compiled for another manifest": func(t *testing.T, b *bundletest.Bundle, out *string) {
			writeEntry(t, *b, compiledEntry(t, strings.Repeat("0", 64)))
		},
		"entry compiled for another manifest with ours appended": func(t *testing.T, b *bundletest.Bundle, out *string) {
			// The digest appears in the file but is not the compiled value.
			writeEntry(t, *b, append(slices.Clone(compiledEntry(t, strings.Repeat("0", 64))), b.ManifestSHA...))
		},
		"entry compiled without a digest": func(t *testing.T, b *bundletest.Bundle, out *string) {
			writeEntry(t, *b, compiledEntry(t, ""))
		},
		"output inside a bundle reached through a linked parent": func(t *testing.T, b *bundletest.Bundle, out *string) {
			alias := filepath.Join(t.TempDir(), "via")
			if err := os.Symlink(filepath.Dir(b.Dir), alias); err != nil {
				t.Fatal(err)
			}
			*out = filepath.Join(b.Dir, "layout")
			b.Dir = filepath.Join(alias, filepath.Base(b.Dir))
		},
		"entry that is not a program": func(t *testing.T, b *bundletest.Bundle, out *string) {
			writeEntry(t, *b, []byte("entry built for "+b.ManifestSHA))
		},
		"resource changed between planning and writing": func(t *testing.T, b *bundletest.Bundle, out *string) {
			// Same length, so only the archived bytes can tell.
			path := filepath.Join(b.Dir, "resources/tofu")
			setBeforeWrite(t, func() {
				if err := os.WriteFile(path, []byte("TOFU"), 0o700); err != nil {
					t.Fatal(err)
				}
			})
		},
		"launcher changed between planning and writing": func(t *testing.T, b *bundletest.Bundle, out *string) {
			launcher := b.Launcher
			setBeforeWrite(t, func() {
				if err := os.WriteFile(launcher, []byte("LAUNCHER"), 0o755); err != nil {
					t.Fatal(err)
				}
			})
		},
		"no entry": func(t *testing.T, b *bundletest.Bundle, out *string) {
			if err := os.Remove(filepath.Join(b.Dir, "lz-offline")); err != nil {
				t.Fatal(err)
			}
		},
		"hard link beside the manifest": func(t *testing.T, b *bundletest.Bundle, out *string) {
			if err := os.Link(filepath.Join(b.Dir, "probe"), filepath.Join(filepath.Dir(b.Dir), "probe-copy")); err != nil {
				t.Fatal(err)
			}
		},
		"named pipe beside the manifest": func(t *testing.T, b *bundletest.Bundle, out *string) {
			if err := syscall.Mkfifo(filepath.Join(b.Dir, "pipe"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"link beside the manifest": func(t *testing.T, b *bundletest.Bundle, out *string) {
			if err := os.Symlink("resources.json", filepath.Join(b.Dir, "alias.json")); err != nil {
				t.Fatal(err)
			}
		},
		"setuid file": func(t *testing.T, b *bundletest.Bundle, out *string) {
			if err := os.Chmod(filepath.Join(b.Dir, "resources/rootfs/bin/busybox"), 0o755|fs.ModeSetuid); err != nil {
				t.Fatal(err)
			}
		},
		"sticky directory": func(t *testing.T, b *bundletest.Bundle, out *string) {
			if err := os.Chmod(filepath.Join(b.Dir, "resources/rootfs/usr"), 0o755|fs.ModeSticky); err != nil {
				t.Fatal(err)
			}
		},
		"launcher is a link": func(t *testing.T, b *bundletest.Bundle, out *string) {
			alias := b.Launcher + "-link"
			if err := os.Symlink(b.Launcher, alias); err != nil {
				t.Fatal(err)
			}
			b.Launcher = alias
		},
		"unreadable file beside the manifest": func(t *testing.T, b *bundletest.Bundle, out *string) {
			// Admission does not read it, so this fails while writing.
			if err := os.Chmod(filepath.Join(b.Dir, "probe"), 0o000); err != nil {
				t.Fatal(err)
			}
		},
		"relative bundle": func(t *testing.T, b *bundletest.Bundle, out *string) {
			t.Chdir(filepath.Dir(b.Dir))
			b.Dir = filepath.Base(b.Dir)
		},
		"bundle with a current-directory reference": func(t *testing.T, b *bundletest.Bundle, out *string) {
			b.Dir = filepath.Dir(b.Dir) + "/./" + filepath.Base(b.Dir)
		},
		"launcher with a current-directory reference": func(t *testing.T, b *bundletest.Bundle, out *string) {
			b.Launcher = filepath.Dir(b.Launcher) + "/./" + filepath.Base(b.Launcher)
		},
		"relative output": func(t *testing.T, b *bundletest.Bundle, out *string) {
			t.Chdir(filepath.Dir(*out))
			*out = filepath.Base(*out)
		},
		"output with a trailing slash": func(t *testing.T, b *bundletest.Bundle, out *string) { *out += "/" },
		"output inside the bundle":     func(t *testing.T, b *bundletest.Bundle, out *string) { *out = filepath.Join(b.Dir, "layout") },
		"output inside the bundle through a link": func(t *testing.T, b *bundletest.Bundle, out *string) {
			alias := filepath.Join(t.TempDir(), "via")
			if err := os.Symlink(b.Dir, alias); err != nil {
				t.Fatal(err)
			}
			*out = filepath.Join(alias, "layout")
		},
		"output through a link and a parent reference": func(t *testing.T, b *bundletest.Bundle, out *string) {
			// Cleaned lexically this names a path outside the bundle; the
			// kernel follows the link first and lands inside resources/.
			alias := filepath.Join(t.TempDir(), "via")
			if err := os.Symlink(filepath.Join(b.Dir, "resources"), alias); err != nil {
				t.Fatal(err)
			}
			*out = alias + "/../layout"
		},
		"output exists": func(t *testing.T, b *bundletest.Bundle, out *string) {
			if err := os.Mkdir(*out, 0o755); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			b := prepared(t)
			out := filepath.Join(t.TempDir(), "layout")
			original := b.Dir
			damage(t, &b, &out)
			if result, err := pack(b.Dir, b.Launcher, b.ManifestSHA, out); err == nil {
				t.Errorf("BEHAVIORAL_RED: %s packed: %+v", name, result)
			}
			// Nothing is left behind, and a directory the pack did not create
			// is never removed.
			_, err := os.Lstat(out)
			if exists := err == nil; exists != (name == "output exists") {
				t.Errorf("BEHAVIORAL_RED: %s left output present=%v", name, exists)
			}
			for _, inside := range []string{"layout", "resources/layout"} {
				if _, err := os.Lstat(filepath.Join(original, inside)); err == nil {
					t.Errorf("BEHAVIORAL_RED: %s left %s inside the bundle", name, inside)
				}
			}
		})
	}
}

// layerOf writes entries as a gzip-compressed tar layer and returns its path.
func layerOf(t *testing.T, headers []*tar.Header) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "layer")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zipper := gzip.NewWriter(file)
	archive := tar.NewWriter(zipper)
	for _, h := range headers {
		if h.Mode == 0 {
			h.Mode = 0o755
		}
		if err := archive.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			if _, err := archive.Write([]byte(strings.Repeat("x", int(h.Size)))); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, c := range []io.Closer{archive, zipper, file} {
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// setExtractLimits lowers the extraction budgets for one test; a zero keeps
// that budget.
func setExtractLimits(t *testing.T, entries int, bytes, stream int64) {
	t.Helper()
	saved := extractLimits
	t.Cleanup(func() { extractLimits = saved })
	if entries != 0 {
		extractLimits.entries = entries
	}
	if bytes != 0 {
		extractLimits.bytes = bytes
	}
	if stream != 0 {
		extractLimits.stream = stream
	}
}

// Extraction only writes the names a layer from this program can hold, never
// through a link and never past its budgets, and only from a blob that
// matches its digest, whatever the layer says: a tampered layout must fail
// verification, not write outside its scratch.
func TestExtractRefuses(t *testing.T) {
	dir := func(name string) *tar.Header { return &tar.Header{Name: name, Typeflag: tar.TypeDir} }
	file := func(name string, size int64) *tar.Header {
		return &tar.Header{Name: name, Typeflag: tar.TypeReg, Size: size, Mode: 0o644}
	}
	link := func(name, target string) *tar.Header {
		return &tar.Header{Name: name, Typeflag: tar.TypeSymlink, Linkname: target}
	}
	for name, c := range map[string]struct {
		headers       func(outside string) []*tar.Header
		entries       int
		bytes, stream int64
		plant         func(t *testing.T, scratch, outside string)
		otherDigest   bool
		want          string
	}{
		"parent reference out of the scratch": {want: "unexpected entry name", headers: func(string) []*tar.Header {
			return []*tar.Header{dir("runtime/"), file("runtime/../../escaped", 1)}
		}},
		"absolute name": {want: "unexpected entry name", headers: func(outside string) []*tar.Header {
			return []*tar.Header{file(filepath.Join(outside, "escaped"), 1)}
		}},
		"name outside host and runtime": {want: "unexpected entry name", headers: func(string) []*tar.Header {
			return []*tar.Header{file("other", 1)}
		}},
		"name that is not clean": {want: "unexpected entry name", headers: func(string) []*tar.Header {
			return []*tar.Header{dir("runtime/"), file("runtime/./escaped", 1)}
		}},
		"file written through a linked directory": {want: "below a link", headers: func(outside string) []*tar.Header {
			return []*tar.Header{dir("runtime/"), link("runtime/out", outside), file("runtime/out/escaped", 1)}
		}},
		"directory made through a linked directory": {want: "below a link", headers: func(outside string) []*tar.Header {
			return []*tar.Header{dir("runtime/"), link("runtime/out", outside), dir("runtime/out/escaped/")}
		}},
		"file written through a relative link": {want: "below a link", headers: func(string) []*tar.Header {
			return []*tar.Header{dir("runtime/"), link("runtime/up", "../.."), file("runtime/up/escaped", 1)}
		}},
		// The names alone look harmless, so only the os.Root stops this.
		"file written through a link already in the scratch": {
			plant: func(t *testing.T, scratch, outside string) {
				if err := os.Symlink(outside, filepath.Join(scratch, "runtime")); err != nil {
					t.Fatal(err)
				}
			},
			headers: func(string) []*tar.Header { return []*tar.Header{file("runtime/escaped", 1)} },
		},
		"more entries than the budget": {entries: 2, want: "entries", headers: func(string) []*tar.Header {
			return []*tar.Header{dir("runtime/"), file("runtime/a", 1), file("runtime/b", 1)}
		}},
		"more bytes than the budget": {bytes: 3, want: "layer files exceed", headers: func(string) []*tar.Header {
			return []*tar.Header{dir("runtime/"), file("runtime/a", 2), file("runtime/b", 2)}
		}},
		"one file larger than the budget": {bytes: 3, want: "layer files exceed", headers: func(string) []*tar.Header {
			return []*tar.Header{dir("runtime/"), file("runtime/a", 4)}
		}},
		// The tar reader consumes extended headers without returning an
		// entry, so only a budget on the whole stream bounds them.
		"metadata past the stream budget": {stream: 64 << 10, want: "layer stream exceeds", headers: func(string) []*tar.Header {
			return []*tar.Header{dir("runtime/" + strings.Repeat("d/", 100_000))}
		}},
		"blob that does not match its digest": {otherDigest: true, want: "does not match", headers: func(string) []*tar.Header {
			return []*tar.Header{dir("runtime/")}
		}},
	} {
		t.Run(name, func(t *testing.T) {
			setExtractLimits(t, c.entries, c.bytes, c.stream)
			root := t.TempDir()
			outside := filepath.Join(root, "outside")
			scratch := filepath.Join(root, "a", "b", "scratch")
			for _, d := range []string{outside, scratch} {
				if err := os.MkdirAll(d, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if c.plant != nil {
				c.plant(t, scratch, outside)
			}
			layer := layerOf(t, c.headers(outside))
			want := digestOf(t, layer)
			if c.otherDigest {
				want = "sha256:" + strings.Repeat("0", 64)
			}
			err := extract(layer, want, scratch)
			if err == nil || !strings.Contains(err.Error(), c.want) || (c.want != "" && !strings.HasPrefix(err.Error(), "PACK_VERIFY: ")) {
				t.Errorf("BEHAVIORAL_RED: %s gave %v, want PACK_VERIFY: …%s", name, err, c.want)
			}
			for _, escaped := range []string{filepath.Join(outside, "escaped"), filepath.Join(root, "a", "b", "escaped"), filepath.Join(root, "a", "escaped"), filepath.Join(root, "escaped")} {
				if _, err := os.Lstat(escaped); err == nil {
					t.Errorf("BEHAVIORAL_RED: %s wrote %s outside the scratch", name, escaped)
				}
			}
		})
	}
}

// The budgets leave room for the real bundle (about 18 000 entries and
// 650 MB in r4), and a layer exactly at the entry and byte budgets still
// extracts.
func TestExtractBudgets(t *testing.T) {
	if extractLimits.entries < 50_000 || extractLimits.bytes < 2<<30 || extractLimits.stream < extractLimits.bytes {
		t.Errorf("BEHAVIORAL_RED: budgets too small for the real bundle: %+v", extractLimits)
	}
	setExtractLimits(t, 3, 3, 0)
	layer := layerOf(t, []*tar.Header{{Name: "runtime/", Typeflag: tar.TypeDir}, {Name: "runtime/a", Typeflag: tar.TypeReg, Size: 2, Mode: 0o644},
		{Name: "runtime/b", Typeflag: tar.TypeReg, Size: 1, Mode: 0o644}})
	if err := extract(layer, digestOf(t, layer), t.TempDir()); err != nil {
		t.Errorf("BEHAVIORAL_RED: a layer at the budgets was refused: %v", err)
	}
	// The stream budget: exactly the decompressed size passes, one byte less
	// fails on the budget.
	file, err := os.Open(layer)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	unzipped, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	size, err := io.Copy(io.Discard, unzipped)
	if err != nil {
		t.Fatal(err)
	}
	setExtractLimits(t, 0, 0, size)
	if err := extract(layer, digestOf(t, layer), t.TempDir()); err != nil {
		t.Errorf("BEHAVIORAL_RED: a layer at the stream budget was refused: %v", err)
	}
	setExtractLimits(t, 0, 0, size-1)
	if err := extract(layer, digestOf(t, layer), t.TempDir()); err == nil || !strings.Contains(err.Error(), "PACK_VERIFY: layer stream exceeds") {
		t.Errorf("BEHAVIORAL_RED: a layer one byte over the stream budget gave %v", err)
	}
}
