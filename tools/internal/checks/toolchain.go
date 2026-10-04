package checks

import "fmt"

const Image = "cgr.dev/chainguard/wolfi-base@sha256:fd536778d12e19bff29cfcf73265a14f585152a49d7f7cd6739ebe48dff01e26"

var Versions = map[string]string{"go": "1.27.1", "tofu": "1.13.0", "terramate": "0.17.3"}
var Artifacts = map[string]string{
	"go":        "63d339f0da5ab53635a56f2490a7984dfe12dfcff22ad749f63edaf590168445",
	"tofu":      "1f0cb37fc85dea4e7633a72aca2332b801f72650a32be911b8b9b32907f214fb",
	"terramate": "303fd597a76af00c728b3eb626493dc2a71585dcd679c5e07a338da44d24a060",
}

// Prepared carries capture identities. Synthetic inputs qualify only the pure
// admission oracle; the external entry separately proves the actual namespace,
// mount and prepared-byte boundary before permitting a real tool invocation.
type Prepared struct {
	Versions       map[string]string
	Artifacts      map[string]string
	Image          string
	IsolationProof string
}

func AdmitCapture(p Prepared) error {
	for name, version := range Versions {
		if p.Versions[name] != version {
			return fmt.Errorf("PIN_VERSION: %s", name)
		}
		if p.Artifacts[name] != Artifacts[name] {
			return fmt.Errorf("PIN_ARTIFACT: %s", name)
		}
	}
	if p.Image != Image {
		return fmt.Errorf("PIN_IMAGE")
	}
	if p.IsolationProof != "synthetic-qualified-control" && p.IsolationProof != "runtime-qualified" {
		return fmt.Errorf("ISOLATION_GATE")
	}
	return nil
}

// Capture exposes observable pre-invocation and publication ordering to tests.
func Capture(p Prepared, invoke, publish func() error) error {
	if err := AdmitCapture(p); err != nil {
		return err
	}
	if err := invoke(); err != nil {
		return err
	}
	return publish()
}
