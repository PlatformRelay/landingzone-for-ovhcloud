package checks

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fetchScript is tools/scripts/fetch-offline-entry.sh, the newcomer's way to
// get the offline entry that the foundation workflow fetches in CI. These
// tests run it against a fake curl: no network.
var fetchScript = filepath.Join("..", "..", "scripts", "fetch-offline-entry.sh")

// fakeCurl serves the token, the manifest and the layer from files next to
// it, and logs every URL it is asked for. A URL it does not know fails.
const fakeCurl = `#!/bin/sh
here=$(dirname "$0")
out=
url=
while [ $# -gt 0 ]; do
  case $1 in
    -o) shift; out=$1 ;;
    -H) shift ;;
    --proto|--proto-redir) shift ;;
    https://*) url=$1 ;;
  esac
  shift
done
echo "$url" >> "$here/curl.log"
case $url in
  https://ghcr.io/token\?scope=repository:*:pull) printf '{"token":"anonymous"}\n'; exit 0 ;;
  */manifests/*) src="$here/manifest.json" ;;
  */blobs/*) src="$here/layer.tgz" ;;
  *) exit 22 ;;
esac
[ -f "$src" ] || exit 22
if [ -n "$out" ]; then cat "$src" > "$out"; else cat "$src"; fi
`

// fakeSudo records that it was called; the script must never call it.
const fakeSudo = `#!/bin/sh
echo "$*" >> "$(dirname "$0")/sudo.log"
exit 1
`

type fetchFixture struct {
	repo, home, bin, bwrap string
	source                 FoundationSource
	entry, launcher        []byte
}

func sha(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func layerArchive(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var raw bytes.Buffer
	zw := gzip.NewWriter(&raw)
	tw := tar.NewWriter(zw)
	for _, dir := range []string{"host/", "runtime/"} {
		if err := tw.WriteHeader(&tar.Header{Name: dir, Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"host/bwrap", "runtime/lz-offline"} {
		if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o775, Size: int64(len(files[name]))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(files[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return raw.Bytes()
}

func manifestFor(layer string) []byte {
	return []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json",` +
		`"config":{"mediaType":"application/vnd.oci.empty.v1+json","digest":"sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a","size":2},` +
		`"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar+gzip","digest":"` + layer + `","size":1}]}`)
}

func writeFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}

// newFetchFixture lays out a candidate clone (the script and a workflow
// rendered by the real renderer), a fake registry behind a fake curl, an
// empty HOME, and a host launcher that matches the pin.
func newFetchFixture(t *testing.T) *fetchFixture {
	t.Helper()
	root := t.TempDir()
	f := &fetchFixture{
		repo: filepath.Join(root, "clone"), home: filepath.Join(root, "home"), bin: filepath.Join(root, "bin"),
		bwrap: filepath.Join(root, "usr-bin-bwrap"),
		entry: []byte("#!/bin/sh\necho entry\n"), launcher: []byte("launcher\n"),
	}
	layer := layerArchive(t, map[string][]byte{"host/bwrap": f.launcher, "runtime/lz-offline": f.entry})
	layerDigest := "sha256:" + sha(layer)
	manifest := manifestFor(layerDigest)
	f.source = FoundationSource{
		Runner: "ubuntu-24.04", TimeoutMinutes: 10,
		Manifest: "sha256:" + sha(manifest), Layer: layerDigest,
		EntrySHA256: sha(f.entry), BwrapSHA256: sha(f.launcher),
		Targets: []string{"test:a"},
	}
	script, err := os.ReadFile(fetchScript)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(f.repo, "tools", "scripts", "fetch-offline-entry.sh"), script, 0o755)
	f.writeWorkflow(t, RenderFoundationWorkflow(f.source))
	writeFile(t, filepath.Join(f.bin, "curl"), []byte(fakeCurl), 0o755)
	writeFile(t, filepath.Join(f.bin, "sudo"), []byte(fakeSudo), 0o755)
	writeFile(t, filepath.Join(f.bin, "manifest.json"), manifest, 0o644)
	writeFile(t, filepath.Join(f.bin, "layer.tgz"), layer, 0o644)
	writeFile(t, f.bwrap, f.launcher, 0o755)
	if err := os.MkdirAll(f.home, 0o755); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fetchFixture) writeWorkflow(t *testing.T, workflow string) {
	t.Helper()
	writeFile(t, filepath.Join(f.repo, filepath.FromSlash(FoundationWorkflow)), []byte(workflow), 0o644)
}

// run executes the script from the clone's root, as the README says.
func (f *fetchFixture) run(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command("sh", append([]string{"tools/scripts/fetch-offline-entry.sh"}, args...)...)
	cmd.Dir = f.repo
	cmd.Env = []string{
		"PATH=" + f.bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"HOME=" + f.home, "LZ_HOST_BWRAP=" + f.bwrap,
	}
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exit):
		code = exit.ExitCode()
	default:
		t.Fatal(err)
	}
	return out.String(), errOut.String(), code
}

func (f *fetchFixture) defaultDest() string {
	return filepath.Join(f.home, ".local", "share", "landingzone-for-ovhcloud", "runtime", f.source.EntrySHA256[:12])
}

func (f *fetchFixture) log(name string) string {
	data, err := os.ReadFile(filepath.Join(f.bin, name))
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	if err != nil {
		panic(err)
	}
	return string(data)
}

// noLeftovers fails when a refused run left anything in the install parent:
// no partial install and no download directory.
func noLeftovers(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		t.Errorf("BEHAVIORAL_RED: refused run left %s in %s", e.Name(), dir)
	}
}

// A verified fetch installs the layer outside the clone, prints only the
// entry's path on stdout and the command to run a target on stderr, and a
// second run reuses the verified install without fetching again.
func TestFoundationFetchInstalls(t *testing.T) {
	f := newFetchFixture(t)
	stdout, stderr, code := f.run(t)
	if code != 0 {
		t.Fatalf("BEHAVIORAL_RED: verified fetch exited %d: %s", code, stderr)
	}
	dest := f.defaultDest()
	entry := filepath.Join(dest, "runtime", "lz-offline")
	if stdout != entry+"\n" {
		t.Errorf("BEHAVIORAL_RED: stdout %q, want the entry path %q", stdout, entry)
	}
	if got, err := os.ReadFile(entry); err != nil || !bytes.Equal(got, f.entry) {
		t.Errorf("BEHAVIORAL_RED: installed entry %q, %v", got, err)
	}
	if info, err := os.Stat(entry); err != nil || info.Mode().Perm() != 0o775 {
		t.Errorf("BEHAVIORAL_RED: entry mode not preserved: %v %v", info, err)
	}
	if want := "'" + entry + `' --candidate "$PWD" -- task test:slice`;!strings.Contains(stderr, want) {
		t.Errorf("BEHAVIORAL_RED: stderr lacks the run command %q:\n%s", want, stderr)
	}
	want := []string{
		"https://ghcr.io/token?scope=repository:" + RuntimeImage + ":pull",
		"https://ghcr.io/v2/" + RuntimeImage + "/manifests/" + f.source.Manifest,
		"https://ghcr.io/v2/" + RuntimeImage + "/blobs/" + f.source.Layer,
	}
	if got := f.log("curl.log"); got != strings.Join(want, "\n")+"\n" {
		t.Errorf("BEHAVIORAL_RED: fetched %q, want %q", got, want)
	}
	if entries, err := os.ReadDir(filepath.Dir(dest)); err != nil || len(entries) != 1 {
		t.Errorf("BEHAVIORAL_RED: install parent holds more than the install: %v %v", entries, err)
	}

	if err := os.Remove(filepath.Join(f.bin, "curl.log")); err != nil {
		t.Fatal(err)
	}
	again, stderr, code := f.run(t)
	if code != 0 || again != stdout {
		t.Fatalf("BEHAVIORAL_RED: second run exited %d with %q: %s", code, again, stderr)
	}
	if got := f.log("curl.log"); got != "" {
		t.Errorf("BEHAVIORAL_RED: second run fetched again: %q", got)
	}
}

// --dest overrides the default; a clean absolute path outside the clone is
// used as given.
func TestFoundationFetchDestOverride(t *testing.T) {
	f := newFetchFixture(t)
	dest := filepath.Join(f.home, "elsewhere", "entry")
	stdout, stderr, code := f.run(t, "--dest", dest)
	if code != 0 || stdout != filepath.Join(dest, "runtime", "lz-offline")+"\n" {
		t.Fatalf("BEHAVIORAL_RED: --dest run exited %d with %q: %s", code, stdout, stderr)
	}
}

// The pins are read from the committed, rendered workflow: the script asks
// the registry for exactly the image and manifest of the approved source.
func TestFoundationFetchReadsCommittedPins(t *testing.T) {
	f := newFetchFixture(t)
	data, err := os.ReadFile(filepath.Join("..", "..", "..", FoundationSourcePath))
	if err != nil {
		t.Fatal(err)
	}
	source, err := ParseFoundationSource(data)
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := os.ReadFile(filepath.Join("..", "..", "..", FoundationWorkflow))
	if err != nil {
		t.Fatal(err)
	}
	f.writeWorkflow(t, string(workflow))
	_, stderr, code := f.run(t)
	if code == 0 {
		t.Fatal("BEHAVIORAL_RED: fixture layer accepted under the committed pins")
	}
	if !strings.Contains(stderr, "DIGEST_MISMATCH") {
		t.Errorf("BEHAVIORAL_RED: committed pins refused for another reason: %s", stderr)
	}
	if want := "https://ghcr.io/v2/" + RuntimeImage + "/manifests/" + source.Manifest + "\n"; !strings.Contains(f.log("curl.log"), want) {
		t.Errorf("BEHAVIORAL_RED: committed manifest not requested; log %q", f.log("curl.log"))
	}
}

// A workflow without exactly one well-formed value for each pin is refused
// before anything is fetched.
func TestFoundationFetchRefusesBadPins(t *testing.T) {
	for name, edit := range map[string]func(string) string{
		"missing entry pin": func(w string) string {
			return strings.Replace(w, "      LZ_ENTRY_SHA256:", "      LZ_ENTRY_SHA:", 1)
		},
		"repeated layer pin": func(w string) string {
			i := strings.Index(w, "      LZ_LAYER:")
			j := strings.Index(w[i:], "\n") + i + 1
			return w[:j] + w[i:j] + w[j:]
		},
		"malformed manifest": func(w string) string {
			return strings.Replace(w, "LZ_MANIFEST: sha256:", "LZ_MANIFEST: sha512:", 1)
		},
		"short bwrap pin": func(w string) string {
			return strings.Replace(w, "LZ_BWRAP_SHA256: ", "LZ_BWRAP_SHA256: x", 1)
		},
		"image with a path escape": func(w string) string {
			return strings.Replace(w, "LZ_IMAGE: "+RuntimeImage, "LZ_IMAGE: ../other", 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFetchFixture(t)
			f.writeWorkflow(t, edit(RenderFoundationWorkflow(f.source)))
			stdout, stderr, code := f.run(t)
			if code != 1 || stdout != "" || !strings.Contains(stderr, "PIN_INVALID") {
				t.Errorf("BEHAVIORAL_RED: bad pins gave %d %q: %s", code, stdout, stderr)
			}
			if got := f.log("curl.log"); got != "" {
				t.Errorf("BEHAVIORAL_RED: fetched despite bad pins: %q", got)
			}
		})
	}
}

// Any digest mismatch fails closed: non-zero, nothing on stdout, nothing
// installed and no download left behind.
func TestFoundationFetchRefusesMismatch(t *testing.T) {
	for name, tamper := range map[string]func(f *fetchFixture){
		"manifest bytes": func(f *fetchFixture) {
			writeFile(t, filepath.Join(f.bin, "manifest.json"), append(manifestFor(f.source.Layer), ' '), 0o644)
		},
		"manifest names another layer": func(f *fetchFixture) {
			other := manifestFor("sha256:" + strings.Repeat("0", 64))
			writeFile(t, filepath.Join(f.bin, "manifest.json"), other, 0o644)
			f.source.Manifest = "sha256:" + sha(other)
		},
		"layer bytes": func(f *fetchFixture) {
			writeFile(t, filepath.Join(f.bin, "layer.tgz"), layerArchive(t, map[string][]byte{"host/bwrap": f.launcher, "runtime/lz-offline": []byte("evil")}), 0o644)
		},
		"entry pin": func(f *fetchFixture) { f.source.EntrySHA256 = strings.Repeat("e", 64) },
		"bwrap pin": func(f *fetchFixture) { f.source.BwrapSHA256 = strings.Repeat("b", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFetchFixture(t)
			tamper(f)
			f.writeWorkflow(t, RenderFoundationWorkflow(f.source))
			stdout, stderr, code := f.run(t)
			if code != 1 || stdout != "" || !strings.Contains(stderr, "DIGEST_MISMATCH") {
				t.Errorf("BEHAVIORAL_RED: mismatch gave %d %q: %s", code, stdout, stderr)
			}
			noLeftovers(t, filepath.Dir(f.defaultDest()))
		})
	}
}

// The entry refuses to run from inside the candidate, so the script refuses
// to install there, and refuses a relative or an existing unverified target.
func TestFoundationFetchRefusesLocation(t *testing.T) {
	for name, dest := range map[string]func(f *fetchFixture) string{
		"inside the clone":   func(f *fetchFixture) string { return filepath.Join(f.repo, "runtime") },
		"the clone itself":   func(f *fetchFixture) string { return f.repo },
		"inside via ..":      func(f *fetchFixture) string { return f.home + "/../clone/runtime" },
		"relative":           func(f *fetchFixture) string { return "runtime" },
		"existing directory": func(f *fetchFixture) string { return f.home },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFetchFixture(t)
			stdout, stderr, code := f.run(t, "--dest", dest(f))
			if code != 1 || stdout != "" || !strings.Contains(stderr, "INSTALL_LOCATION") {
				t.Errorf("BEHAVIORAL_RED: %s gave %d %q: %s", name, code, stdout, stderr)
			}
			if got := f.log("curl.log"); got != "" {
				t.Errorf("BEHAVIORAL_RED: fetched before refusing the location: %q", got)
			}
			if _, err := os.Stat(filepath.Join(f.repo, "runtime")); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("BEHAVIORAL_RED: created a directory in the clone: %v", err)
			}
		})
	}
}

// A host launcher that is missing or not the pinned build leaves the
// verified install in place, prints the one sudo command that installs the
// pinned build, exits non-zero, and never runs sudo itself.
func TestFoundationFetchBwrapMessage(t *testing.T) {
	for name, prepare := range map[string]func(f *fetchFixture){
		"missing":   func(f *fetchFixture) { _ = os.Remove(f.bwrap) },
		"different": func(f *fetchFixture) { writeFile(t, f.bwrap, []byte("distro build\n"), 0o755) },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFetchFixture(t)
			prepare(f)
			stdout, stderr, code := f.run(t)
			if code != 3 || stdout != "" {
				t.Errorf("BEHAVIORAL_RED: unpinned launcher gave %d %q, want 3 and no path", code, stdout)
			}
			want := fmt.Sprintf("sudo install -m 0755 '%s' '%s'", filepath.Join(f.defaultDest(), "host", "bwrap"), f.bwrap)
			if !strings.Contains(stderr, "BWRAP_UNPINNED") || strings.Count(stderr, "sudo ") != 1 || !strings.Contains(stderr, want) {
				t.Errorf("BEHAVIORAL_RED: stderr lacks the one fix %q:\n%s", want, stderr)
			}
			if got := f.log("sudo.log"); got != "" {
				t.Errorf("BEHAVIORAL_RED: the script ran sudo: %q", got)
			}
			if _, err := os.Stat(filepath.Join(f.defaultDest(), "host", "bwrap")); err != nil {
				t.Errorf("BEHAVIORAL_RED: verified install removed: %v", err)
			}
		})
	}
}

// An existing install is reused only when it is the verified install of the
// pinned layer with an executable entry; anything else is refused, not
// repaired or overwritten.
func TestFoundationFetchRefusesTamperedInstall(t *testing.T) {
	for name, tamper := range map[string]func(dest string){
		"replaced entry": func(dest string) {
			writeFile(t, filepath.Join(dest, "runtime", "lz-offline"), []byte("evil"), 0o775)
		},
		"stale layer marker": func(dest string) {
			writeFile(t, filepath.Join(dest, ".layer-digest"), []byte("sha256:"+strings.Repeat("0", 64)+"\n"), 0o644)
		},
		"entry not executable": func(dest string) {
			if err := os.Chmod(filepath.Join(dest, "runtime", "lz-offline"), 0o644); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFetchFixture(t)
			if _, stderr, code := f.run(t); code != 0 {
				t.Fatalf("first install exited %d: %s", code, stderr)
			}
			tamper(f.defaultDest())
			stdout, stderr, code := f.run(t)
			if code != 1 || stdout != "" || !strings.Contains(stderr, "INSTALL_LOCATION") {
				t.Errorf("BEHAVIORAL_RED: tampered install gave %d %q: %s", code, stdout, stderr)
			}
		})
	}
}

// Usage errors exit 2 before anything is read or fetched.
func TestFoundationFetchUsage(t *testing.T) {
	for name, args := range map[string][]string{
		"unknown argument": {"--force"},
		"--dest without a value": {"--dest"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFetchFixture(t)
			stdout, stderr, code := f.run(t, args...)
			if code != 2 || stdout != "" || !strings.Contains(stderr, "USAGE") {
				t.Errorf("BEHAVIORAL_RED: %v gave %d %q: %s", args, code, stdout, stderr)
			}
			if got := f.log("curl.log"); got != "" {
				t.Errorf("BEHAVIORAL_RED: fetched on a usage error: %q", got)
			}
		})
	}
}
