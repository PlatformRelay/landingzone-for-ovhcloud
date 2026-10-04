package bundle_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/bundle"
	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/bundle/bundletest"
)

func TestAdmitValid(t *testing.T) {
	b := bundletest.Write(t, nil)
	r, err := bundle.Admit(b.Dir, b.ManifestSHA, b.Launcher)
	if err != nil {
		t.Fatalf("BEHAVIORAL_RED: prepared bundle refused: %v", err)
	}
	if r.Bwrap != bundle.Launcher || len(r.Files) != 10 {
		t.Errorf("BEHAVIORAL_RED: admitted resources %+v", r)
	}
}

// rewrite replaces the manifest and returns the digest the entry would be
// built with for it, so only the change under test can be refused.
func rewrite(t *testing.T, dir string, change func(map[string]any)) string {
	t.Helper()
	path := filepath.Join(dir, "resources.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	change(manifest)
	if data, err = json.Marshal(manifest); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o664); err != nil {
		t.Fatal(err)
	}
	return bundle.Hash(data)
}

// Each way a bundle can differ from what the entry was built for is refused,
// with the rule that names it.
func TestAdmitRefuses(t *testing.T) {
	for name, c := range map[string]struct {
		damage func(t *testing.T, b *bundletest.Bundle)
		want   string
	}{
		"other manifest digest": {func(t *testing.T, b *bundletest.Bundle) { b.ManifestSHA = strings.Repeat("0", 64) }, "RESOURCE_MANIFEST"},
		"no manifest digest":    {func(t *testing.T, b *bundletest.Bundle) { b.ManifestSHA = "" }, "RESOURCE_MANIFEST"},
		"malformed manifest": {func(t *testing.T, b *bundletest.Bundle) {
			if err := os.WriteFile(filepath.Join(b.Dir, "resources.json"), []byte("{"), 0o664); err != nil {
				t.Fatal(err)
			}
			b.ManifestSHA = bundle.Hash([]byte("{"))
		}, "RESOURCE_MANIFEST"},
		"unknown manifest field": {func(t *testing.T, b *bundletest.Bundle) {
			b.ManifestSHA = rewrite(t, b.Dir, func(m map[string]any) { m["extra"] = true })
		}, "RESOURCE_MANIFEST"},
		"unpinned tool": {func(t *testing.T, b *bundletest.Bundle) {
			b.ManifestSHA = rewrite(t, b.Dir, func(m map[string]any) { m["prepared"].(map[string]any)["Versions"].(map[string]any)["tofu"] = "1.12.0" })
		}, "PIN_VERSION"},
		"other launcher path": {func(t *testing.T, b *bundletest.Bundle) {
			b.ManifestSHA = rewrite(t, b.Dir, func(m map[string]any) { m["bwrap"] = "/usr/local/bin/bwrap" })
		}, "RESOURCE_HELPER"},
		"other launcher": {func(t *testing.T, b *bundletest.Bundle) {
			if err := os.WriteFile(b.Launcher, []byte("other"), 0o755); err != nil {
				t.Fatal(err)
			}
		}, "RESOURCE_HELPER"},
		"launcher is a link": {func(t *testing.T, b *bundletest.Bundle) {
			alias := b.Launcher + "-link"
			if err := os.Symlink(b.Launcher, alias); err != nil {
				t.Fatal(err)
			}
			b.Launcher = alias
		}, "RESOURCE_HELPER"},
		"changed resource": {func(t *testing.T, b *bundletest.Bundle) {
			if err := os.WriteFile(filepath.Join(b.Dir, "resources/tofu"), []byte("TOFU"), 0o700); err != nil {
				t.Fatal(err)
			}
		}, "RESOURCE_IDENTITY"},
		"changed mode": {func(t *testing.T, b *bundletest.Bundle) {
			if err := os.Chmod(filepath.Join(b.Dir, "resources/tofu"), 0o755); err != nil {
				t.Fatal(err)
			}
		}, "RESOURCE_IDENTITY"},
		"changed link target": {func(t *testing.T, b *bundletest.Bundle) {
			path := filepath.Join(b.Dir, "resources/rootfs/usr/bin/sh")
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("/bin/other", path); err != nil {
				t.Fatal(err)
			}
		}, "RESOURCE_IDENTITY"},
		"extra resource": {func(t *testing.T, b *bundletest.Bundle) {
			if err := os.WriteFile(filepath.Join(b.Dir, "resources/extra"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}, "RESOURCE_IDENTITY"},
		"renamed resource": {func(t *testing.T, b *bundletest.Bundle) {
			// One missing and one unlisted resource keep the count.
			if err := os.Rename(filepath.Join(b.Dir, "resources/tofu"), filepath.Join(b.Dir, "resources/tofu2")); err != nil {
				t.Fatal(err)
			}
		}, "RESOURCE_IDENTITY"},
		"missing resource": {func(t *testing.T, b *bundletest.Bundle) {
			if err := os.Remove(filepath.Join(b.Dir, "resources/tofu")); err != nil {
				t.Fatal(err)
			}
		}, "RESOURCE_IDENTITY"},
		"link outside the rootfs": {func(t *testing.T, b *bundletest.Bundle) {
			if err := os.Symlink("rootfs/bin/busybox", filepath.Join(b.Dir, "resources/alias")); err != nil {
				t.Fatal(err)
			}
		}, "RESOURCE_LINK"},
		"relative link escaping the rootfs": {func(t *testing.T, b *bundletest.Bundle) {
			if err := os.Symlink("../../../tofu", filepath.Join(b.Dir, "resources/rootfs/bin/out")); err != nil {
				t.Fatal(err)
			}
		}, "RESOURCE_LINK"},
		"absolute link escaping the rootfs": {func(t *testing.T, b *bundletest.Bundle) {
			// Counted from the rootfs this leaves it; counted from its own
			// directory it would not.
			if err := os.Symlink("/../tofu", filepath.Join(b.Dir, "resources/rootfs/usr/bin/out")); err != nil {
				t.Fatal(err)
			}
		}, "RESOURCE_LINK"},
		"named pipe": {func(t *testing.T, b *bundletest.Bundle) {
			if err := syscall.Mkfifo(filepath.Join(b.Dir, "resources/pipe"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, "RESOURCE_TYPE"},
		"manifest with a second link": {func(t *testing.T, b *bundletest.Bundle) {
			if err := os.Link(filepath.Join(b.Dir, "resources.json"), filepath.Join(filepath.Dir(b.Dir), "copy.json")); err != nil {
				t.Fatal(err)
			}
		}, "INPUT_FILE"},
		"bundle reached through a link": {func(t *testing.T, b *bundletest.Bundle) {
			alias := filepath.Join(t.TempDir(), "via")
			if err := os.Symlink(b.Dir, alias); err != nil {
				t.Fatal(err)
			}
			b.Dir = alias
		}, "INPUT_PATH"},
	} {
		t.Run(name, func(t *testing.T) {
			b := bundletest.Write(t, nil)
			c.damage(t, &b)
			_, err := bundle.Admit(b.Dir, b.ManifestSHA, b.Launcher)
			if err == nil || !strings.HasPrefix(err.Error(), c.want) {
				t.Errorf("BEHAVIORAL_RED: %s gave %v, want %s", name, err, c.want)
			}
		})
	}
}

// The first difference in lexical order is the one reported, and the walk
// stops there: an unlisted resources/a is reported before a named pipe at
// resources/z is reached, as the entry always did.
func TestAdmitReportsFirstDifference(t *testing.T) {
	b := bundletest.Write(t, nil)
	if err := os.WriteFile(filepath.Join(b.Dir, "resources/a"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(b.Dir, "resources/z"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := bundle.Admit(b.Dir, b.ManifestSHA, b.Launcher); err == nil || err.Error() != "RESOURCE_IDENTITY: resources/a" {
		t.Errorf("BEHAVIORAL_RED: first difference %v", err)
	}
}

// Inventory's identities, written out independently of it.
func TestInventory(t *testing.T) {
	b := bundletest.Write(t, nil)
	got, err := bundle.Inventory(b.Dir)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bundle.Identity{
		"resources":                    {Kind: "directory", Mode: 0o700},
		"resources/rootfs":             {Kind: "directory", Mode: 0o755},
		"resources/rootfs/bin":         {Kind: "directory", Mode: 0o755},
		"resources/rootfs/bin/busybox": {Kind: "file", Value: "9d75f0d7c398df565d7ac04c6819b62d6d8f9560f5eb4672596ecd8f7e96ae91", Mode: 0o755},
		"resources/rootfs/usr":         {Kind: "directory", Mode: 0o755},
		"resources/rootfs/usr/bin":     {Kind: "directory", Mode: 0o755},
		"resources/rootfs/usr/bin/ls":  {Kind: "symlink", Value: "../../bin/busybox", Mode: 0o777},
		"resources/rootfs/usr/bin/sh":  {Kind: "symlink", Value: "/bin/busybox", Mode: 0o777},
		"resources/rootfs/usr/top":     {Kind: "symlink", Value: "..", Mode: 0o777},
		"resources/tofu":               {Kind: "file", Value: "cc52e9ceb39eabd803ce34abace35a582f9869d00844af61740120b0f42bdc81", Mode: 0o700},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BEHAVIORAL_RED: inventory\n got %+v\nwant %+v", got, want)
	}
	if bundle.Hash([]byte("busybox")) != want["resources/rootfs/bin/busybox"].Value {
		t.Errorf("fixture digest of busybox changed")
	}
}
