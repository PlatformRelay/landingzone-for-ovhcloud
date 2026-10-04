// Package bundletest writes small prepared runtime bundles that the entry's
// admission accepts, for the tests of admission and of lz-pack.
package bundletest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/bundle"
	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/checks"
)

// Bundle is a written bundle: its directory, a launcher outside it, and the
// digest of its manifest, which the entry is built with.
type Bundle struct{ Dir, Launcher, ManifestSHA string }

func write(t testing.TB, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func link(t testing.TB, target, path string) {
	t.Helper()
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

// Write creates a bundle with the shapes the real one has: a private
// resources directory, a rootfs with absolute and relative links, a tool, a
// manifest naming the pinned tools, the fixed launcher and every resource,
// and an entry that carries the manifest digest. extra, if given, adds
// resources before the manifest is written.
func Write(t testing.TB, extra func(dir string)) Bundle {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "bundle")
	write(t, filepath.Join(dir, "resources/rootfs/bin/busybox"), "busybox", 0o755)
	write(t, filepath.Join(dir, "resources/tofu"), "tofu", 0o700)
	if err := os.MkdirAll(filepath.Join(dir, "resources/rootfs/usr/bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	link(t, "/bin/busybox", filepath.Join(dir, "resources/rootfs/usr/bin/sh"))
	link(t, "../../bin/busybox", filepath.Join(dir, "resources/rootfs/usr/bin/ls"))
	// A link may resolve to the rootfs itself.
	link(t, "..", filepath.Join(dir, "resources/rootfs/usr/top"))
	if err := os.Chmod(filepath.Join(dir, "resources"), 0o700); err != nil {
		t.Fatal(err)
	}
	if extra != nil {
		extra(dir)
	}
	launcher := filepath.Join(root, "bwrap")
	write(t, launcher, "launcher", 0o755)
	files, err := bundle.Inventory(dir)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := json.Marshal(bundle.Resources{
		Prepared: checks.Prepared{Versions: checks.Versions, Artifacts: checks.Artifacts, Image: checks.Image},
		Files:    files, Bwrap: bundle.Launcher, BwrapSHA: bundle.Hash([]byte("launcher")),
	})
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "resources.json"), string(manifest), 0o664)
	sha := bundle.Hash(manifest)
	write(t, filepath.Join(dir, "lz-offline"), "entry built for "+sha, 0o775)
	return Bundle{dir, launcher, sha}
}
