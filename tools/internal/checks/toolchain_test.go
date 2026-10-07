package checks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These are identity inputs, not fabricated tool-output/parser fixtures. Archive
// identities are copied from the independently qualified T001 preparation and
// the official Task 3.53.1 and TFLint 0.64.0 releases.
func qualifiedInput() Prepared {
	return Prepared{
		Versions: map[string]string{"go": "1.27.1", "tofu": "1.13.0", "terramate": "0.17.3", "task": "3.53.1", "tflint": "0.64.0"},
		Artifacts: map[string]string{
			"go":        "63d339f0da5ab53635a56f2490a7984dfe12dfcff22ad749f63edaf590168445",
			"tofu":      "1f0cb37fc85dea4e7633a72aca2332b801f72650a32be911b8b9b32907f214fb",
			"terramate": "303fd597a76af00c728b3eb626493dc2a71585dcd679c5e07a338da44d24a060",
			"task":      "a54a408f6861ff921f6e87774180db31bacd8c1e7c944ca696db9fea49a82fc7",
			"tflint":    "cca9d13e2e1d7a2c627af60ff899a3c9b74212899416aeb96ec764d2ef954537",
		},
		Image: "cgr.dev/chainguard/wolfi-base@sha256:fd536778d12e19bff29cfcf73265a14f585152a49d7f7cd6739ebe48dff01e26",
		// Wolfi packages verified against the signed APKINDEX (evidence/T025.md).
		Packages: map[string]string{
			"git-2.56.0-r0.apk":         "33bc6d38714d6a65e412b87e7ef9392a27b4f2ca71f3506bd8e9eaa42d59c10d",
			"libpcre2-8-0-10.49-r1.apk": "c2e8dacd8fe2f1c3de9eabd274532f74fcbbd3e919301fc95453ab3af850c38c",
		},
	}
}

// syntheticIsolation stands in for RuntimePrepared's evidence. Only code inside
// this package can construct a non-zero Isolation.
var syntheticIsolation = Isolation{network: "net:[synthetic]"}

func captureMarkers(t *testing.T, p Prepared, isolation Isolation) (error, bool, bool) {
	t.Helper()
	root := t.TempDir()
	attempt, publication := filepath.Join(root, "capture-attempt"), filepath.Join(root, "publication")
	err := Capture(p, isolation, func() error { return os.WriteFile(attempt, []byte("synthetic invocation"), 0600) },
		func() error { return os.WriteFile(publication, []byte("synthetic publication"), 0600) })
	_, a := os.Stat(attempt)
	_, b := os.Stat(publication)
	return err, a == nil, b == nil
}

func TestToolchainValid(t *testing.T) {
	err, invoked, published := captureMarkers(t, qualifiedInput(), syntheticIsolation)
	if err != nil || !invoked || !published {
		t.Fatalf("valid qualified synthetic capture rejected: err=%v invoked=%v published=%v", err, invoked, published)
	}
}

func TestToolchainCaptureAdmission(t *testing.T) {
	for _, tool := range []string{"go", "tofu", "terramate", "task", "tflint"} {
		t.Run("wrong-"+tool, func(t *testing.T) { p := qualifiedInput(); p.Versions[tool] = "0.0.0"; assertCaptureDenied(t, p) })
		t.Run("missing-"+tool, func(t *testing.T) { p := qualifiedInput(); delete(p.Versions, tool); assertCaptureDenied(t, p) })
		t.Run("artifact-"+tool, func(t *testing.T) {
			p := qualifiedInput()
			p.Artifacts[tool] = "synthetic-wrong-digest"
			assertCaptureDenied(t, p)
		})
		t.Run("missing-artifact-"+tool, func(t *testing.T) { p := qualifiedInput(); delete(p.Artifacts, tool); assertCaptureDenied(t, p) })
	}
	t.Run("near-version", func(t *testing.T) {
		p := qualifiedInput()
		p.Versions["terramate"] = "0.17.3.1"
		assertCaptureDenied(t, p)
	})
	t.Run("extra-tool", func(t *testing.T) {
		p := qualifiedInput()
		p.Versions["synthetic"] = "1.0.0"
		assertCaptureDenied(t, p)
	})
	t.Run("image", func(t *testing.T) {
		p := qualifiedInput()
		p.Image = "synthetic-unapproved-image"
		assertCaptureDenied(t, p)
	})
	t.Run("missing-image", func(t *testing.T) { p := qualifiedInput(); p.Image = ""; assertCaptureDenied(t, p) })
	// The packages unpacked over the image are pinned like the image itself.
	for _, name := range []string{"git-2.56.0-r0.apk", "libpcre2-8-0-10.49-r1.apk"} {
		t.Run("package-"+name, func(t *testing.T) {
			p := qualifiedInput()
			p.Packages[name] = "synthetic-wrong-digest"
			assertCaptureDenied(t, p)
		})
		t.Run("missing-package-"+name, func(t *testing.T) { p := qualifiedInput(); delete(p.Packages, name); assertCaptureDenied(t, p) })
	}
	t.Run("other-package-version", func(t *testing.T) {
		p := qualifiedInput()
		p.Packages["git-2.55.0-r0.apk"] = p.Packages["git-2.56.0-r0.apk"]
		delete(p.Packages, "git-2.56.0-r0.apk")
		assertCaptureDenied(t, p)
	})
	t.Run("extra-package", func(t *testing.T) {
		p := qualifiedInput()
		p.Packages["synthetic-1.0-r0.apk"] = "33bc6d38714d6a65e412b87e7ef9392a27b4f2ca71f3506bd8e9eaa42d59c10d"
		assertCaptureDenied(t, p)
	})
	t.Run("no-packages", func(t *testing.T) { p := qualifiedInput(); p.Packages = nil; assertCaptureDenied(t, p) })
	t.Run("gate-off", func(t *testing.T) {
		err, invoked, published := captureMarkers(t, qualifiedInput(), Isolation{})
		if err == nil || invoked || published {
			t.Errorf("BEHAVIORAL_RED: capture without isolation evidence must refuse: err=%v invoked=%v published=%v", err, invoked, published)
		}
	})
}

// VerifyRuntime executes every pinned tool, git from the pinned packages
// included, and requires the pinned version in its output.
func TestRuntimeToolsCoverPins(t *testing.T) {
	want := map[string]string{"git": ""}
	for name, version := range Versions {
		want[name] = version
	}
	for name := range Packages {
		if version, ok := strings.CutPrefix(name, "git-"); ok {
			want["git"], _, _ = strings.Cut(version, "-r")
		}
	}
	got := map[string]bool{}
	for _, tool := range runtimeTools {
		version, ok := want[tool.name]
		if !ok || got[tool.name] || version == "" || !strings.Contains(tool.expected, version) {
			t.Errorf("BEHAVIORAL_RED: runtime tool %s (expects %q) is not exactly one pinned tool at its pinned version %q", tool.name, tool.expected, version)
		}
		got[tool.name] = true
	}
	for name := range want {
		if !got[name] {
			t.Errorf("BEHAVIORAL_RED: pinned tool %s is never executed by VerifyRuntime", name)
		}
	}
}

func assertCaptureDenied(t *testing.T, p Prepared) {
	t.Helper()
	err, invoked, published := captureMarkers(t, p, syntheticIsolation)
	if err == nil || invoked || published {
		t.Errorf("BEHAVIORAL_RED: capture must refuse before invocation/publication: err=%v invoked=%v published=%v", err, invoked, published)
	}
}
