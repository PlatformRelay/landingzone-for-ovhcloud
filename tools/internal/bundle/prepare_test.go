package bundle

import (
	"archive/tar"
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/checks"
)

// The fixture archives under testdata/prepare are written by a deterministic
// generator (evidence/T030.md, *Fixtures*); the expected contents below are
// the bytes it puts in them.

//go:embed testdata/prepare/image.tar
var fixtureImage []byte

const fixtureDir = "testdata/prepare"

// TestMain lets the test binary stand in for crane: Prepare runs its Crane as
// `crane export ... <image> -`, and the stand-in writes the fixture image to
// stdout only when asked for the pinned image (checks.Image) for linux/amd64.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "export" {
		os.Exit(fakeCrane(os.Args[2:]))
	}
	os.Exit(m.Run())
}

func fakeCrane(args []string) int {
	if !asksAmd64(args) || !slices.Contains(args, checks.Image) || args[len(args)-1] != "-" {
		fmt.Fprintln(os.Stderr, "fake crane: linux/amd64 export of the pinned image to stdout expected, got", args)
		return 2
	}
	if _, err := os.Stdout.Write(fixtureImage); err != nil {
		return 1
	}
	return 0
}

// asksAmd64 accepts crane's --platform in either spelling.
func asksAmd64(args []string) bool {
	if slices.Contains(args, "--platform=linux/amd64") {
		return true
	}
	i := slices.Index(args, "--platform")
	return i >= 0 && i+1 < len(args) && args[i+1] == "linux/amd64"
}

func writeTestFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// copyFixture copies a fixture into a private directory, so every input has
// one link and the checkout is never written.
func copyFixture(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	writeTestFile(t, path, readTestFile(t, filepath.Join(fixtureDir, name)), 0o600)
	return path
}

func projectFixture(t *testing.T, name, dest string) error {
	t.Helper()
	archive, err := os.Open(filepath.Join(fixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	return Project(archive, dest)
}

// TestProjectValid: a valid archive is projected with its files and its
// links, absolute links unchanged (they count from the projected root).
func TestProjectValid(t *testing.T) {
	dest := t.TempDir()
	if err := projectFixture(t, "valid.tar", dest); err != nil {
		t.Fatalf("BEHAVIORAL_RED: valid archive refused: %v", err)
	}
	for path, want := range map[string]string{"bin/tool": "tool\n", "etc/conf": "conf\n"} {
		if got, err := os.ReadFile(filepath.Join(dest, path)); err != nil || string(got) != want {
			t.Errorf("BEHAVIORAL_RED: %s = %q, %v; want %q", path, got, err, want)
		}
	}
	for path, want := range map[string]string{"usr/bin/sh": "/bin/tool", "usr/bin/rel": "../../bin/tool"} {
		if got, err := os.Readlink(filepath.Join(dest, path)); err != nil || got != want {
			t.Errorf("BEHAVIORAL_RED: link %s = %q, %v; want %q", path, got, err, want)
		}
	}
}

// TestProjectRefusesEscapes: each archive is valid.tar plus members that
// escape the root through a symlink, a hard link, a write through a link or
// a member's own name. Project refuses it, nothing outside the root is
// created or linked, and no link left under the root resolves outside it.
// valid.tar projecting in the same row shows the refusal is the escape's.
func TestProjectRefusesEscapes(t *testing.T) {
	fixture := func(name string) func(*testing.T, string) []byte {
		return func(t *testing.T, _ string) []byte { return readTestFile(t, filepath.Join(fixtureDir, name+".tar")) }
	}
	for name, archive := range map[string]func(t *testing.T, outside string) []byte{
		"symlink-escape":          fixture("symlink-escape"),          // usr/bin/evil -> ../../../outside/secret
		"symlink-escape-absolute": fixture("symlink-escape-absolute"), // usr/bin/evil -> /../outside/secret
		"symlink-through":         fixture("symlink-through"),         // out -> ../outside, then out/planted
		"hardlink-escape":         fixture("hardlink-escape"),         // bin/evil hard link to ../outside/secret
		"hardlink-absolute":       fixture("hardlink-absolute"),       // bin/evil hard link to /etc/passwd
		// The rows below name the outside directory by its absolute host
		// path, which the test owns, so a projection that uses a name as
		// given does reach it.
		"hardlink-host-path": func(t *testing.T, outside string) []byte {
			return withMembers(t, &tar.Header{Typeflag: tar.TypeLink, Name: "bin/evil", Linkname: filepath.Join(outside, "secret")})
		},
		// The link resolves inside the root when its absolute target counts
		// from the root, as Admit reads it; writing through it on the host
		// lands outside.
		"symlink-write-through": func(t *testing.T, outside string) []byte {
			return withMembers(t,
				&tar.Header{Typeflag: tar.TypeSymlink, Name: "in", Linkname: outside},
				&tar.Header{Typeflag: tar.TypeReg, Name: "in/planted", Mode: 0o600})
		},
		"name-dotdot": func(t *testing.T, _ string) []byte {
			return withMembers(t, &tar.Header{Typeflag: tar.TypeReg, Name: "../outside/planted", Mode: 0o600})
		},
		"name-absolute-host-path": func(t *testing.T, outside string) []byte {
			return withMembers(t, &tar.Header{Typeflag: tar.TypeReg, Name: filepath.Join(outside, "planted"), Mode: 0o600})
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := projectFixture(t, "valid.tar", t.TempDir()); err != nil {
				t.Fatalf("BEHAVIORAL_RED: control: valid archive refused: %v", err)
			}
			base := t.TempDir()
			dest, outside := filepath.Join(base, "dest"), filepath.Join(base, "outside")
			writeTestFile(t, filepath.Join(outside, "secret"), []byte("secret\n"), 0o600)
			if err := os.Mkdir(dest, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := Project(bytes.NewReader(archive(t, outside)), dest); err == nil {
				t.Errorf("BEHAVIORAL_RED: %s projected", name)
			}
			assertOutsideUntouched(t, outside)
			assertNothingEscapes(t, dest)
		})
	}
}

// withMembers is valid.tar with the given members appended; a regular
// member's content is "planted\n".
func withMembers(t *testing.T, members ...*tar.Header) []byte {
	t.Helper()
	reader := tar.NewReader(bytes.NewReader(readTestFile(t, filepath.Join(fixtureDir, "valid.tar"))))
	var out bytes.Buffer
	writer := tar.NewWriter(&out)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(writer, reader); err != nil {
			t.Fatal(err)
		}
	}
	for _, member := range members {
		appendMember(t, writer, member)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func appendMember(t *testing.T, writer *tar.Writer, member *tar.Header) {
	t.Helper()
	content := []byte("planted\n")
	if member.Typeflag == tar.TypeReg {
		member.Size = int64(len(content))
	}
	if err := writer.WriteHeader(member); err != nil {
		t.Fatal(err)
	}
	if member.Typeflag != tar.TypeReg {
		return
	}
	if _, err := writer.Write(content); err != nil {
		t.Fatal(err)
	}
}

func assertOutsideUntouched(t *testing.T, outside string) {
	t.Helper()
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "secret" {
		t.Errorf("BEHAVIORAL_RED: outside the root now holds %v", entries)
	}
	if got := readTestFile(t, filepath.Join(outside, "secret")); string(got) != "secret\n" {
		t.Errorf("BEHAVIORAL_RED: outside file rewritten: %q", got)
	}
	if links := testLinkCount(t, filepath.Join(outside, "secret")); links != 1 {
		t.Errorf("BEHAVIORAL_RED: outside file has %d links", links)
	}
}

func testLinkCount(t *testing.T, path string) uint64 {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return uint64(info.Sys().(*syscall.Stat_t).Nlink)
}

// assertNothingEscapes walks dest: every symlink resolves inside it (an
// absolute target counting from dest, as Admit reads it), and no regular file
// shares its inode with another path (a hard link to anything).
func assertNothingEscapes(t *testing.T, dest string) {
	t.Helper()
	err := filepath.WalkDir(dest, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch {
		case entry.Type()&fs.ModeSymlink != 0:
			if !testLinkInside(t, dest, path) {
				t.Errorf("BEHAVIORAL_RED: escaping link left at %s", path)
			}
		case entry.Type().IsRegular() && testLinkCount(t, path) != 1:
			t.Errorf("BEHAVIORAL_RED: hard link left at %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func testLinkInside(t *testing.T, dest, path string) bool {
	t.Helper()
	target, err := os.Readlink(path)
	if err != nil {
		t.Fatal(err)
	}
	resolved := filepath.Clean(filepath.Join(filepath.Dir(path), target))
	if filepath.IsAbs(target) {
		resolved = filepath.Join(dest, target)
	}
	return resolved == dest || strings.HasPrefix(resolved, dest+"/")
}

// TestVerifyArchive: an archive is accepted only under its own digest. Each
// row first accepts the file under its digest, then refuses the damaged pair.
func TestVerifyArchive(t *testing.T) {
	for name, damage := range map[string]func(t *testing.T, path, sum string) (string, string){
		"archive-changed": func(t *testing.T, path, sum string) (string, string) {
			writeTestFile(t, path, append(readTestFile(t, path), 0), 0o600)
			return path, sum
		},
		"one-digit-differs": func(_ *testing.T, path, sum string) (string, string) {
			return path, sum[:63] + testOtherDigit(sum[63])
		},
		"other-archive-digest": func(t *testing.T, path, _ string) (string, string) {
			return path, Hash(readTestFile(t, filepath.Join(fixtureDir, "image.tar")))
		},
		"truncated-digest": func(_ *testing.T, path, sum string) (string, string) { return path, sum[:63] },
		"empty-digest":     func(_ *testing.T, path, _ string) (string, string) { return path, "" },
		"missing-file":     func(_ *testing.T, path, sum string) (string, string) { return path + ".absent", sum },
		// A link is refused even to the right bytes: what was verified could
		// be swapped behind it before use (a single-link regular file).
		"symlinked-path": func(t *testing.T, path, sum string) (string, string) {
			if err := os.Symlink(path, path+".link"); err != nil {
				t.Fatal(err)
			}
			return path + ".link", sum
		},
		"second-link": func(t *testing.T, path, sum string) (string, string) {
			if err := os.Link(path, path+".second"); err != nil {
				t.Fatal(err)
			}
			return path, sum
		},
	} {
		t.Run(name, func(t *testing.T) {
			path := copyFixture(t, "valid.tar")
			sum := Hash(readTestFile(t, path))
			if err := VerifyArchive(path, sum); err != nil {
				t.Fatalf("BEHAVIORAL_RED: control: archive refused under its own digest: %v", err)
			}
			path, sum = damage(t, path, sum)
			if err := VerifyArchive(path, sum); err == nil {
				t.Errorf("BEHAVIORAL_RED: %s accepted", name)
			}
		})
	}
}

func testOtherDigit(digit byte) string {
	if digit == '0' {
		return "1"
	}
	return "0"
}

// fixturePins names each fetched fixture by its pin-table name: the release
// archives by tool, the packages and the provider by file name.
var fixturePins = map[string]string{
	"go":                        "go.tar.gz",
	"tofu":                      "tofu.tar.gz",
	"terramate":                 "terramate.tar.gz",
	"task":                      "task.tar.gz",
	"tflint":                    "tflint.zip",
	"git-2.56.0-r0.apk":         "git-2.56.0-r0.apk",
	"libpcre2-8-0-10.49-r1.apk": "libpcre2-8-0-10.49-r1.apk",
	"terraform-provider-ovh_2.21.0_linux_amd64.zip": "terraform-provider-ovh_2.21.0_linux_amd64.zip",
}

const fixtureProvider = "terraform-provider-ovh_2.21.0_linux_amd64.zip"

// fixtureToolchain is the qualified toolchain root's content Prepare copies.
var fixtureToolchain = map[string]string{
	"tools/go/go/bin/go":        "go binary\n",
	"tools/go/go/VERSION":       "go1.27.1\n",
	"tools/tofu/tofu":           "tofu binary\n",
	"tools/terramate/terramate": "terramate binary\n",
}

type prepareFixture struct {
	options  PrepareOptions
	launcher string
}

// useFixturePins points the checks pin tables at the fixtures' digests for the
// test's duration, so Prepare and Admit read the same single table. It,
// setFixturePin and dropFixturePin are the only code that knows the tables'
// shape. Packages keeps its rows (kind, version, source) with each digest
// replaced, so the fixtures stand in for the pinned artefacts of each kind.
func useFixturePins(t *testing.T, archives map[string]string) {
	t.Helper()
	artifacts, packages := checks.Artifacts, checks.Packages
	t.Cleanup(func() { checks.Artifacts, checks.Packages = artifacts, packages })
	checks.Artifacts, checks.Packages = map[string]string{}, maps.Clone(packages)
	for name, path := range archives {
		setFixturePin(name, Hash(readTestFile(t, path)))
	}
}

// setFixturePin pins name to sum: a tool's release archive in Artifacts,
// anything else as the digest of its row in Packages.
func setFixturePin(name, sum string) {
	if _, tool := checks.Versions[name]; tool {
		checks.Artifacts[name] = sum
		return
	}
	row := checks.Packages[name]
	row.SHA256 = sum
	checks.Packages[name] = row
}

func dropFixturePin(name string) {
	delete(checks.Artifacts, name)
	delete(checks.Packages, name)
}

// newPrepareFixture lays out a preparation: an empty private root holding
// the Go tree's manifest, a toolchain root, each fetched artefact as a
// private file, pins equal to their digests, a fake launcher and the test
// binary as crane.
func newPrepareFixture(t *testing.T) prepareFixture {
	t.Helper()
	base := t.TempDir()
	root, tools := filepath.Join(base, "prep"), filepath.Join(base, "toolchain")
	var manifest strings.Builder
	for path, content := range fixtureToolchain {
		full := filepath.Join(tools, path)
		writeTestFile(t, full, []byte(content), 0o700)
		if strings.HasPrefix(path, "tools/go/go/") {
			fmt.Fprintf(&manifest, "%s  %s\n", Hash([]byte(content)), full)
		}
	}
	writeTestFile(t, filepath.Join(root, "proof/go-files.sha256"), []byte(manifest.String()), 0o600)
	archives := map[string]string{}
	for name, file := range fixturePins {
		archives[name] = copyFixture(t, file)
	}
	useFixturePins(t, archives)
	launcher := filepath.Join(base, "bwrap")
	writeTestFile(t, launcher, []byte("launcher\n"), 0o700)
	saved := launcherFile
	t.Cleanup(func() { launcherFile = saved })
	launcherFile = launcher
	crane, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return prepareFixture{PrepareOptions{Root: root, Toolchain: tools, Crane: crane, Archives: archives}, launcher}
}

// TestPrepareResourcesAdmitted: on valid fixture archives Prepare writes a
// bundle whose resources.json bundle.Admit accepts, with the resources the
// entry binds into its sandbox holding the fixtures' bytes.
func TestPrepareResourcesAdmitted(t *testing.T) {
	f := newPrepareFixture(t)
	if err := Prepare(f.options); err != nil {
		t.Fatalf("BEHAVIORAL_RED: valid fixtures refused: %v", err)
	}
	dir := filepath.Join(f.options.Root, "bundle")
	manifest := readTestFile(t, filepath.Join(dir, "resources.json"))
	if _, err := Admit(dir, Hash(manifest), f.launcher); err != nil {
		t.Fatalf("BEHAVIORAL_RED: prepared resources.json refused by Admit: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(f.options.Root, "staging")); !os.IsNotExist(err) {
		t.Errorf("BEHAVIORAL_RED: staging left behind: %v", err)
	}
	assertPreparedFiles(t, filepath.Join(dir, "resources"), fixturePrepared())
	assertEntryMounts(t, filepath.Join(dir, "resources"))
}

// fixturePrepared is what the entry binds (lz-offline main.go, bwrap argv),
// with the bytes each fixture put there.
func fixturePrepared() map[string]string {
	return map[string]string{
		"task":                           "task\n",
		"tflint":                         "tflint\n",
		"tofu":                           fixtureToolchain["tools/tofu/tofu"],
		"terramate":                      fixtureToolchain["tools/terramate/terramate"],
		"go/bin/go":                      fixtureToolchain["tools/go/go/bin/go"],
		"go/VERSION":                     fixtureToolchain["tools/go/go/VERSION"],
		"rootfs/bin/busybox":             "busybox\n",
		"rootfs/etc/os-release":          "ID=fixture\n",
		"rootfs/usr/bin/git":             "git\n",
		"rootfs/usr/lib/libpcre2-8.so.0": "pcre\n",
	}
}

func assertPreparedFiles(t *testing.T, resources string, want map[string]string) {
	t.Helper()
	for path, content := range want {
		if got, err := os.ReadFile(filepath.Join(resources, path)); err != nil || string(got) != content {
			t.Errorf("BEHAVIORAL_RED: resources/%s = %q, %v; want %q", path, got, err, content)
		}
	}
	mirrored := filepath.Join(resources, "mirror/registry.opentofu.org/ovh/ovh", fixtureProvider)
	if got, err := os.ReadFile(mirrored); err != nil || !bytes.Equal(got, readTestFile(t, filepath.Join(fixtureDir, fixtureProvider))) {
		t.Errorf("BEHAVIORAL_RED: provider not mirrored as fetched: %v", err)
	}
	if got, err := os.Readlink(filepath.Join(resources, "rootfs/usr/bin/sh")); err != nil || got != "/bin/busybox" {
		t.Errorf("BEHAVIORAL_RED: image link rootfs/usr/bin/sh = %q, %v", got, err)
	}
	if _, err := os.Lstat(filepath.Join(resources, "rootfs/.PKGINFO")); !os.IsNotExist(err) {
		t.Errorf("BEHAVIORAL_RED: package control member installed: %v", err)
	}
	assertTofurc(t, filepath.Join(resources, "tofurc"))
}

// assertTofurc: OpenTofu installs the pinned provider from /mirror only,
// with no direct (network) fallback.
func assertTofurc(t *testing.T, path string) {
	t.Helper()
	tofurc, err := os.ReadFile(path)
	text := string(tofurc)
	if err != nil || !strings.Contains(text, `"/mirror"`) || !strings.Contains(text, `include = ["registry.opentofu.org/ovh/ovh"]`) || strings.Contains(text, "direct") {
		t.Errorf("BEHAVIORAL_RED: tofurc is not the /mirror-only installation of the pinned provider: %q, %v", tofurc, err)
	}
}

// assertEntryMounts: the read-only rootfs carries every directory the
// entry's sandbox mounts over.
func assertEntryMounts(t *testing.T, resources string) {
	t.Helper()
	for _, name := range []string{"proc", "dev", "tmp", "run", "home", "tcb", "tools", "mirror", "candidate"} {
		if info, err := os.Lstat(filepath.Join(resources, "rootfs", name)); err != nil || !info.IsDir() {
			t.Errorf("BEHAVIORAL_RED: rootfs/%s is not a directory: %v", name, err)
		}
	}
}

// TestPrepareRefusesUnpinnedArtefacts: Prepare reads every pin from the
// checks tables and has no table of its own. An artefact the tables lack, a
// pin dropped from them, or a pin they hold differently is refused, as is a
// toolchain Go file its proof manifest does not pin; nothing is left under
// the root. Each row first prepares the unchanged fixtures, so the refusal
// is the row's change.
func TestPrepareRefusesUnpinnedArtefacts(t *testing.T) {
	for name, change := range map[string]func(t *testing.T, o *PrepareOptions){
		"archive-not-in-table": func(t *testing.T, o *PrepareOptions) {
			o.Archives["extra-1.0-r0.apk"] = copyFixture(t, "git-2.56.0-r0.apk")
		},
		"package-pin-dropped": func(*testing.T, *PrepareOptions) {
			dropFixturePin("libpcre2-8-0-10.49-r1.apk")
		},
		"provider-pin-dropped": func(*testing.T, *PrepareOptions) { dropFixturePin(fixtureProvider) },
		"package-pin-differs": func(*testing.T, *PrepareOptions) {
			setFixturePin("git-2.56.0-r0.apk", Hash([]byte("other")))
		},
		"tool-pin-differs": func(*testing.T, *PrepareOptions) {
			setFixturePin("task", Hash([]byte("other")))
		},
		"go-file-differs-from-proof": func(t *testing.T, o *PrepareOptions) {
			writeTestFile(t, filepath.Join(o.Toolchain, "tools/go/go/bin/go"), []byte("other go\n"), 0o700)
		},
		"go-file-not-in-proof": func(t *testing.T, o *PrepareOptions) {
			writeTestFile(t, filepath.Join(o.Toolchain, "tools/go/go/extra"), []byte("extra\n"), 0o600)
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := Prepare(newPrepareFixture(t).options); err != nil {
				t.Fatalf("BEHAVIORAL_RED: control: valid fixtures refused: %v", err)
			}
			f := newPrepareFixture(t)
			f.options.Archives = maps.Clone(f.options.Archives)
			change(t, &f.options)
			if err := Prepare(f.options); err == nil {
				t.Errorf("BEHAVIORAL_RED: %s prepared", name)
			}
			for _, left := range []string{"bundle", "staging"} {
				if _, err := os.Lstat(filepath.Join(f.options.Root, left)); !os.IsNotExist(err) {
					t.Errorf("BEHAVIORAL_RED: %s left under the root after a refusal: %v", left, err)
				}
			}
		})
	}
}

// TestPrepareRefusesExistingDestination: Prepare writes only into absent
// private destinations; an existing bundle or staging directory is refused
// and left as it was, never merged into or replaced.
func TestPrepareRefusesExistingDestination(t *testing.T) {
	for _, existing := range []string{"bundle", "staging"} {
		t.Run(existing, func(t *testing.T) {
			if err := Prepare(newPrepareFixture(t).options); err != nil {
				t.Fatalf("BEHAVIORAL_RED: control: valid fixtures refused: %v", err)
			}
			f := newPrepareFixture(t)
			kept := filepath.Join(f.options.Root, existing, "kept")
			writeTestFile(t, kept, []byte("kept\n"), 0o600)
			if err := Prepare(f.options); err == nil {
				t.Errorf("BEHAVIORAL_RED: prepared over an existing %s", existing)
			}
			entries, err := os.ReadDir(filepath.Dir(kept))
			if err != nil || len(entries) != 1 || string(readTestFile(t, kept)) != "kept\n" {
				t.Errorf("BEHAVIORAL_RED: existing %s changed: %v, %v", existing, entries, err)
			}
		})
	}
}
