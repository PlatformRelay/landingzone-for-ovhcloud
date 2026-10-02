package checks

import (
	"os"
	"path/filepath"
	"testing"
)

// These are identity inputs, not fabricated tool-output/parser fixtures. Archive
// identities are copied from the independently qualified T001 preparation.
func qualifiedInput() Prepared {
	return Prepared{
		Versions: map[string]string{"go":"1.27.1", "tofu":"1.13.0", "terramate":"0.17.3"},
		Artifacts: map[string]string{
			"go":"63d339f0da5ab53635a56f2490a7984dfe12dfcff22ad749f63edaf590168445",
			"tofu":"1f0cb37fc85dea4e7633a72aca2332b801f72650a32be911b8b9b32907f214fb",
			"terramate":"303fd597a76af00c728b3eb626493dc2a71585dcd679c5e07a338da44d24a060",
		},
		Image:"cgr.dev/chainguard/wolfi-base@sha256:fd536778d12e19bff29cfcf73265a14f585152a49d7f7cd6739ebe48dff01e26",
		IsolationProof:"synthetic-qualified-control",
	}
}

func captureMarkers(t *testing.T, p Prepared) (error, bool, bool) {
	t.Helper()
	root := t.TempDir()
	attempt, publication := filepath.Join(root,"capture-attempt"), filepath.Join(root,"publication")
	err := Capture(p, func() error { return os.WriteFile(attempt, []byte("synthetic invocation"),0600) },
		func() error { return os.WriteFile(publication, []byte("synthetic publication"),0600) })
	_, a := os.Stat(attempt)
	_, b := os.Stat(publication)
	return err, a == nil, b == nil
}

func TestToolchainValid(t *testing.T) {
	err, invoked, published := captureMarkers(t,qualifiedInput())
	if err != nil || !invoked || !published { t.Fatalf("valid qualified synthetic capture rejected: err=%v invoked=%v published=%v",err,invoked,published) }
}

func TestToolchainCaptureAdmission(t *testing.T) {
	for _, tool := range []string{"go","tofu","terramate"} {
		t.Run("wrong-"+tool,func(t *testing.T) { p:=qualifiedInput(); p.Versions[tool]="0.0.0"; assertCaptureDenied(t,p) })
		t.Run("missing-"+tool,func(t *testing.T) { p:=qualifiedInput(); delete(p.Versions,tool); assertCaptureDenied(t,p) })
		t.Run("artifact-"+tool,func(t *testing.T) { p:=qualifiedInput(); p.Artifacts[tool]="synthetic-wrong-digest"; assertCaptureDenied(t,p) })
		t.Run("missing-artifact-"+tool,func(t *testing.T) { p:=qualifiedInput(); delete(p.Artifacts,tool); assertCaptureDenied(t,p) })
	}
	t.Run("near-version",func(t *testing.T) { p:=qualifiedInput(); p.Versions["terramate"]="0.17.3.1"; assertCaptureDenied(t,p) })
	t.Run("image",func(t *testing.T) { p:=qualifiedInput(); p.Image="synthetic-unapproved-image"; assertCaptureDenied(t,p) })
	t.Run("missing-image",func(t *testing.T) { p:=qualifiedInput(); p.Image=""; assertCaptureDenied(t,p) })
	t.Run("gate-off",func(t *testing.T) { p:=qualifiedInput(); p.IsolationProof=""; assertCaptureDenied(t,p) })
}

func assertCaptureDenied(t *testing.T, p Prepared) {
	t.Helper()
	err, invoked, published := captureMarkers(t,p)
	if err == nil || invoked || published { t.Errorf("BEHAVIORAL_RED: capture must refuse before invocation/publication: err=%v invoked=%v published=%v",err,invoked,published) }
}
