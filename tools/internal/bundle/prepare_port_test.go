package bundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// TestProjectRefusesOtherMembers: besides escapes, Project refuses any hard
// link, even one inside the root (lz-pack refuses a second link to a file, so
// a bundle holding one could not be packed), devices and FIFOs, and a
// duplicate member; valid.tar projects in the same row.
func TestProjectRefusesOtherMembers(t *testing.T) {
	for name, member := range map[string]*tar.Header{
		"hardlink-inside":     {Typeflag: tar.TypeLink, Name: "bin/again", Linkname: "bin/tool"},
		"char-device":         {Typeflag: tar.TypeChar, Name: "etc/null", Mode: 0o600, Devmajor: 1, Devminor: 3},
		"fifo":                {Typeflag: tar.TypeFifo, Name: "etc/fifo", Mode: 0o600},
		"duplicate-directory": {Typeflag: tar.TypeDir, Name: "etc/", Mode: 0o755},
		"duplicate-file":      {Typeflag: tar.TypeReg, Name: "./etc//conf", Mode: 0o600},
	} {
		t.Run(name, func(t *testing.T) {
			if err := projectFixture(t, "valid.tar", t.TempDir()); err != nil {
				t.Fatalf("control: valid archive refused: %v", err)
			}
			dest := t.TempDir()
			if err := Project(bytes.NewReader(withMembers(t, member)), dest); err == nil {
				t.Errorf("BEHAVIORAL_RED: %s projected", name)
			}
			assertNothingEscapes(t, dest)
		})
	}
}

// TestProjectRefusesWriteThroughEarlierLink: a package projected over the
// image may not write through a link the image put there, even one that
// stays inside the root, nor replace it; the same member without the link in
// its way is projected.
func TestProjectRefusesWriteThroughEarlierLink(t *testing.T) {
	image := withMembers(t, &tar.Header{Typeflag: tar.TypeSymlink, Name: "lib", Linkname: "usr"})
	for name, member := range map[string]*tar.Header{
		"through-dir-link": {Typeflag: tar.TypeReg, Name: "lib/planted", Mode: 0o600},
		"dir-through-link": {Typeflag: tar.TypeDir, Name: "lib/sub/", Mode: 0o755},
		"over-link":        {Typeflag: tar.TypeReg, Name: "lib", Mode: 0o600},
	} {
		t.Run(name, func(t *testing.T) {
			if err := projectTwice(t, withMembers(t), packageOf(t, member)); err != nil {
				t.Fatalf("control: member refused without the link in its way: %v", err)
			}
			dest := t.TempDir()
			if err := projectBoth(t, dest, image, packageOf(t, member)); err == nil {
				t.Errorf("BEHAVIORAL_RED: %s projected through the image's link", member.Name)
			}
			entries, err := os.ReadDir(filepath.Join(dest, "usr"))
			if err != nil || len(entries) != 1 || entries[0].Name() != "bin" {
				t.Errorf("BEHAVIORAL_RED: the link's target now holds %v, %v", entries, err)
			}
		})
	}
}

// projectTwice projects first and then second into a new directory and
// returns the second projection's error.
func projectTwice(t *testing.T, first, second []byte) error {
	t.Helper()
	return projectBoth(t, t.TempDir(), first, second)
}

func projectBoth(t *testing.T, dest string, first, second []byte) error {
	t.Helper()
	if err := Project(bytes.NewReader(first), dest); err != nil {
		t.Fatal(err)
	}
	return Project(bytes.NewReader(second), dest)
}

// packageOf is a one-member archive; a regular member's content is
// "planted\n".
func packageOf(t *testing.T, member *tar.Header) []byte {
	t.Helper()
	var out bytes.Buffer
	writer := tar.NewWriter(&out)
	copied := *member
	appendMember(t, writer, &copied)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// TestProjectImageLeavesOutDev: the image's /dev, devices and all, is left
// out of the projection (the sandbox supplies a private one); the same
// archive projected as given is refused for its device.
func TestProjectImageLeavesOutDev(t *testing.T) {
	image := withMembers(t,
		&tar.Header{Typeflag: tar.TypeDir, Name: "dev/", Mode: 0o755},
		&tar.Header{Typeflag: tar.TypeChar, Name: "dev/null", Mode: 0o666, Devmajor: 1, Devminor: 3})
	if err := Project(bytes.NewReader(image), t.TempDir()); err == nil {
		t.Fatalf("control: device projected as given")
	}
	archive := filepath.Join(t.TempDir(), "image.tar")
	writeTestFile(t, archive, image, 0o600)
	rootfs := t.TempDir()
	if err := projectImage(archive, rootfs); err != nil {
		t.Fatalf("BEHAVIORAL_RED: image with a /dev refused: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(rootfs, "dev")); !os.IsNotExist(err) {
		t.Errorf("BEHAVIORAL_RED: image /dev projected: %v", err)
	}
	if got := readTestFile(t, filepath.Join(rootfs, "etc/conf")); string(got) != "conf\n" {
		t.Errorf("BEHAVIORAL_RED: rest of the image not projected: %q", got)
	}
}

// apkSegment is one gzip member of a fixture apk: its tar members, with no
// end-of-archive blocks unless it is the last, as abuild writes them.
type apkSegment []*tar.Header

// fixtureAPK writes an apk as Wolfi ships one: a signature segment, a control
// segment and a data segment, each its own gzip member. A regular member's
// content is its name and a newline.
func fixtureAPK(t *testing.T, segments ...apkSegment) []byte {
	t.Helper()
	var out bytes.Buffer
	for i, segment := range segments {
		var plain bytes.Buffer
		writer := tar.NewWriter(&plain)
		for _, member := range segment {
			writeAPKMember(t, writer, member)
		}
		finish := writer.Flush
		if i == len(segments)-1 {
			finish = writer.Close
		}
		if err := finish(); err != nil {
			t.Fatal(err)
		}
		stream := gzip.NewWriter(&out)
		if _, err := stream.Write(plain.Bytes()); err != nil {
			t.Fatal(err)
		}
		if err := stream.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return out.Bytes()
}

func writeAPKMember(t *testing.T, writer *tar.Writer, member *tar.Header) {
	t.Helper()
	content := []byte(member.Name + "\n")
	if member.Typeflag == tar.TypeReg {
		member.Size = int64(len(content))
	}
	if err := writer.WriteHeader(member); err != nil {
		t.Fatal(err)
	}
	if member.Typeflag == tar.TypeReg {
		if _, err := writer.Write(content); err != nil {
			t.Fatal(err)
		}
	}
}

// gzipMemberCount counts the gzip members of data.
func gzipMemberCount(t *testing.T, data []byte) int {
	t.Helper()
	source := bytes.NewReader(data)
	stream, err := gzip.NewReader(source)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for {
		stream.Multistream(false)
		if _, err := io.Copy(io.Discard, stream); err != nil {
			t.Fatal(err)
		}
		count++
		if err := stream.Reset(source); errors.Is(err, io.EOF) {
			return count
		} else if err != nil {
			t.Fatal(err)
		}
	}
}

func apkFile(name string, mode int64) *tar.Header {
	return &tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: mode}
}

func apkDir(name string) *tar.Header {
	return &tar.Header{Typeflag: tar.TypeDir, Name: name, Mode: 0o755}
}

// TestPrepareMultiMemberPackage: a package in Wolfi's three-member form is
// unpacked over the image with its data (files, directories the image lacks,
// links) and none of its control members; a package carrying an install
// script is refused with nothing left under the root.
func TestPrepareMultiMemberPackage(t *testing.T) {
	signature := apkSegment{apkFile(".SIGN.RSA.wolfi-signing.rsa.pub", 0o644)}
	data := apkSegment{
		apkDir("usr/"), apkDir("usr/bin/"), apkFile("usr/bin/git", 0o755),
		apkDir("usr/libexec/"), apkDir("usr/libexec/git-core/"), apkFile("usr/libexec/git-core/git-add", 0o755),
		{Typeflag: tar.TypeSymlink, Name: "usr/bin/git-upload-pack", Linkname: "git"},
	}
	const git = "git-2.56.0-r0.apk"
	for name, row := range map[string]struct {
		control  apkSegment
		accepted bool
	}{
		"signed-three-members": {apkSegment{apkFile(".PKGINFO", 0o644), apkFile(".melange.yaml", 0o644)}, true},
		"install-script":       {apkSegment{apkFile(".PKGINFO", 0o644), apkFile(".post-install", 0o755)}, false},
	} {
		t.Run(name, func(t *testing.T) {
			apk := fixtureAPK(t, signature, row.control, data)
			if members := gzipMemberCount(t, apk); members != 3 {
				t.Fatalf("fixture apk has %d gzip members, want 3", members)
			}
			f := newPrepareFixture(t)
			writeTestFile(t, f.options.Archives[git], apk, 0o600)
			setFixturePin(git, Hash(apk))
			err := Prepare(f.options)
			if !row.accepted {
				if err == nil {
					t.Errorf("BEHAVIORAL_RED: package with an install script prepared")
				}
				assertRootHoldsOnlyProof(t, f.options.Root)
				return
			}
			if err != nil {
				t.Fatalf("BEHAVIORAL_RED: three-member package refused: %v", err)
			}
			assertMultiMemberUnpacked(t, f)
		})
	}
}

func assertMultiMemberUnpacked(t *testing.T, f prepareFixture) {
	t.Helper()
	dir := filepath.Join(f.options.Root, "bundle")
	if _, err := Admit(dir, Hash(readTestFile(t, filepath.Join(dir, "resources.json"))), f.launcher); err != nil {
		t.Fatalf("BEHAVIORAL_RED: bundle refused by Admit: %v", err)
	}
	rootfs := filepath.Join(dir, "resources/rootfs")
	for path, want := range map[string]string{
		"usr/bin/git":                  "usr/bin/git\n",
		"usr/libexec/git-core/git-add": "usr/libexec/git-core/git-add\n",
		"etc/os-release":               "ID=fixture\n",
		"usr/lib/libpcre2-8.so.0":      "pcre\n",
	} {
		if got, err := os.ReadFile(filepath.Join(rootfs, path)); err != nil || string(got) != want {
			t.Errorf("BEHAVIORAL_RED: rootfs/%s = %q, %v; want %q", path, got, err, want)
		}
	}
	if got, err := os.Readlink(filepath.Join(rootfs, "usr/bin/git-upload-pack")); err != nil || got != "git" {
		t.Errorf("BEHAVIORAL_RED: package link = %q, %v", got, err)
	}
	for _, control := range []string{".SIGN.RSA.wolfi-signing.rsa.pub", ".PKGINFO", ".melange.yaml"} {
		if _, err := os.Lstat(filepath.Join(rootfs, control)); !os.IsNotExist(err) {
			t.Errorf("BEHAVIORAL_RED: control member %s installed: %v", control, err)
		}
	}
}

// assertRootHoldsOnlyProof: after a refusal the preparation root holds what
// it held before, the proof alone.
func assertRootHoldsOnlyProof(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 || entries[0].Name() != "proof" {
		t.Errorf("BEHAVIORAL_RED: root holds %v after a refusal, %v", entries, err)
	}
}
