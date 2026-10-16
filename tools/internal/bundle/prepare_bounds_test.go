package bundle

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/checks"
)

// lowerBound sets one of the preparation's bounds for the test's duration.
func lowerBound[T int | int64](t *testing.T, bound *T, value T) {
	t.Helper()
	saved := *bound
	t.Cleanup(func() { *bound = saved })
	*bound = value
}

// TestProjectBounds: an archive is projected at each bound and refused one
// below it. valid.tar has 8 members, 10 bytes of content and a 14-byte link
// target.
func TestProjectBounds(t *testing.T) {
	for name, row := range map[string]struct {
		set            func(*testing.T, int64)
		accept, refuse int64
	}{
		"members":            {func(t *testing.T, v int64) { lowerBound(t, &maxMembers, int(v)) }, 8, 7},
		"expanded-bytes":     {func(t *testing.T, v int64) { lowerBound(t, &maxExpanded, v) }, 10, 9},
		"link-target-length": {func(t *testing.T, v int64) { lowerBound(t, &maxNameLength, int(v)) }, 14, 13},
	} {
		t.Run(name, func(t *testing.T) {
			row.set(t, row.accept)
			if err := projectFixture(t, "valid.tar", t.TempDir()); err != nil {
				t.Fatalf("control: refused at the bound: %v", err)
			}
			row.set(t, row.refuse)
			if err := projectFixture(t, "valid.tar", t.TempDir()); err == nil {
				t.Errorf("BEHAVIORAL_RED: projected past the %s bound", name)
			}
		})
	}
}

// TestProjectSkipsGlobalHeader: a PAX global header is archive metadata, not
// a member: the archive projects and no file is made for it.
func TestProjectSkipsGlobalHeader(t *testing.T) {
	archive := withMembers(t, &tar.Header{Typeflag: tar.TypeXGlobalHeader, Name: "pax_global_header", PAXRecords: map[string]string{"comment": "fixture"}})
	dest := t.TempDir()
	if err := Project(bytes.NewReader(archive), dest); err != nil {
		t.Fatalf("BEHAVIORAL_RED: archive with a global header refused: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dest, "pax_global_header")); !os.IsNotExist(err) {
		t.Errorf("BEHAVIORAL_RED: global header projected as a file: %v", err)
	}
	if got := readTestFile(t, filepath.Join(dest, "bin/tool")); string(got) != "tool\n" {
		t.Errorf("BEHAVIORAL_RED: members after the header not projected: %q", got)
	}
}

// TestVerifyArchiveInputBound: an input is read only up to its bound and
// never through a special file; the file at the bound is accepted.
func TestVerifyArchiveInputBound(t *testing.T) {
	path := copyFixture(t, "valid.tar")
	data := readTestFile(t, path)
	lowerBound(t, &maxInputBytes, int64(len(data)))
	if err := VerifyArchive(path, Hash(data)); err != nil {
		t.Fatalf("control: refused at the bound: %v", err)
	}
	lowerBound(t, &maxInputBytes, int64(len(data)-1))
	if err := VerifyArchive(path, Hash(data)); err == nil {
		t.Errorf("BEHAVIORAL_RED: input past its bound accepted")
	}
	fifo := filepath.Join(t.TempDir(), "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyArchive(fifo, Hash(nil)); err == nil {
		t.Errorf("BEHAVIORAL_RED: FIFO accepted as an input")
	}
}

// TestVerifyArchiveUncleanPath: a path that is not clean is refused, since
// "link/.." resolves through the link while its cleaned directory looks real.
func TestVerifyArchiveUncleanPath(t *testing.T) {
	base := t.TempDir()
	data := readTestFile(t, copyFixture(t, "valid.tar"))
	real := filepath.Join(base, "real")
	writeTestFile(t, filepath.Join(real, "valid.tar"), data, 0o600)
	for _, dir := range []string{filepath.Join(real, "sub"), filepath.Join(base, "dir")} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(real, "sub"), filepath.Join(base, "dir", "link")); err != nil {
		t.Fatal(err)
	}
	if err := VerifyArchive(filepath.Join(real, "valid.tar"), Hash(data)); err != nil {
		t.Fatalf("control: clean path refused: %v", err)
	}
	if err := VerifyArchive(base+"/dir/link/../valid.tar", Hash(data)); err == nil {
		t.Errorf("BEHAVIORAL_RED: unclean path read through a link")
	}
}

// TestVerifyArchiveLinkedAncestor: a file is not read through a directory
// link anywhere above it, only through real directories.
func TestVerifyArchiveLinkedAncestor(t *testing.T) {
	base := t.TempDir()
	data := readTestFile(t, copyFixture(t, "valid.tar"))
	writeTestFile(t, filepath.Join(base, "real/inner/valid.tar"), data, 0o600)
	if err := os.Symlink(filepath.Join(base, "real"), filepath.Join(base, "link")); err != nil {
		t.Fatal(err)
	}
	if err := VerifyArchive(filepath.Join(base, "real/inner/valid.tar"), Hash(data)); err != nil {
		t.Fatalf("control: path through real directories refused: %v", err)
	}
	if err := VerifyArchive(filepath.Join(base, "link/inner/valid.tar"), Hash(data)); err == nil {
		t.Errorf("BEHAVIORAL_RED: file read through a linked ancestor directory")
	}
}

// TestPrepareExportBound:the image export is accepted at its bound and
// refused one byte below it, with nothing left under the root.
func TestPrepareExportBound(t *testing.T) {
	f := newPrepareFixture(t)
	lowerBound(t, &maxExportBytes, int64(len(fixtureImage)))
	if err := Prepare(f.options); err != nil {
		t.Fatalf("control: refused at the bound: %v", err)
	}
	f = newPrepareFixture(t)
	lowerBound(t, &maxExportBytes, int64(len(fixtureImage)-1))
	if err := Prepare(f.options); err == nil {
		t.Errorf("BEHAVIORAL_RED: export past its bound prepared")
	}
	assertRootHoldsOnlyProof(t, f.options.Root)
}

// TestPreparePublishNeverReplaces: the finished bundle replaces only the
// empty directory Prepare reserved; a bundle filled meanwhile is kept, and the
// receipt written for the refused publish is removed.
func TestPreparePublishNeverReplaces(t *testing.T) {
	f, final, stage, receipt := reservedPreparation(t)
	if err := build(f.options, stage, final, receipt); err != nil {
		t.Fatalf("control: reserved bundle not replaced: %v", err)
	}
	f, final, stage, receipt = reservedPreparation(t)
	writeTestFile(t, filepath.Join(final, "kept"), []byte("kept\n"), 0o600)
	if err := build(f.options, stage, final, receipt); err == nil {
		t.Errorf("BEHAVIORAL_RED: a filled bundle was replaced")
	}
	entries, err := os.ReadDir(final)
	if err != nil || len(entries) != 1 || entries[0].Name() != "kept" {
		t.Errorf("BEHAVIORAL_RED: filled bundle now holds %v, %v", entries, err)
	}
	if _, err := os.Lstat(receipt); !os.IsNotExist(err) {
		t.Errorf("BEHAVIORAL_RED: receipt left for a refused publish: %v", err)
	}
}

// TestPrepareRefusesEmptyBundle: an existing bundle directory is refused
// even when empty, which a rename would otherwise replace.
func TestPrepareRefusesEmptyBundle(t *testing.T) {
	f := newPrepareFixture(t)
	final := filepath.Join(f.options.Root, "bundle")
	if err := os.Mkdir(final, 0o700); err != nil {
		t.Fatal(err)
	}
	before, err := os.Lstat(final)
	if err != nil {
		t.Fatal(err)
	}
	if err := Prepare(f.options); err == nil {
		t.Errorf("BEHAVIORAL_RED: prepared over an existing empty bundle")
	}
	after, err := os.Lstat(final)
	if err != nil || !os.SameFile(before, after) {
		t.Errorf("BEHAVIORAL_RED: existing bundle directory replaced: %v", err)
	}
}

// reservedPreparation is a fixture whose bundle and staging directories
// exist, as Prepare leaves them before it builds.
func reservedPreparation(t *testing.T) (f prepareFixture, final, stage, receipt string) {
	t.Helper()
	f = newPrepareFixture(t)
	final, stage, receipt = filepath.Join(f.options.Root, "bundle"), filepath.Join(f.options.Root, "staging"), filepath.Join(f.options.Root, "prepared.json")
	for _, dir := range []string{final, stage} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return f, final, stage, receipt
}

// TestPrepareReceipt: a preparation writes its receipt beside the bundle,
// naming the manifest it wrote; an existing receipt is refused before
// anything is written and left as it was.
func TestPrepareReceipt(t *testing.T) {
	f := newPrepareFixture(t)
	if err := Prepare(f.options); err != nil {
		t.Fatalf("control: valid fixtures refused: %v", err)
	}
	var receipt map[string]any
	if err := json.Unmarshal(readTestFile(t, filepath.Join(f.options.Root, "prepared.json")), &receipt); err != nil {
		t.Fatal(err)
	}
	manifest := readTestFile(t, filepath.Join(f.options.Root, "bundle/resources.json"))
	if receipt["resources_sha256"] != Hash(manifest) || receipt["image"] != checks.Image || receipt["provider_sha256"] != Hash(readTestFile(t, f.options.Archives[fixtureProvider])) {
		t.Errorf("BEHAVIORAL_RED: receipt does not name what was prepared: %v", receipt)
	}

	f = newPrepareFixture(t)
	old := filepath.Join(f.options.Root, "prepared.json")
	writeTestFile(t, old, []byte("old\n"), 0o600)
	if err := Prepare(f.options); err == nil {
		t.Errorf("BEHAVIORAL_RED: prepared over an existing receipt")
	}
	entries, err := os.ReadDir(f.options.Root)
	if err != nil || len(entries) != 2 || string(readTestFile(t, old)) != "old\n" {
		t.Errorf("BEHAVIORAL_RED: root holds %v after the refusal, receipt %q, %v", entries, readTestFile(t, old), err)
	}
}
