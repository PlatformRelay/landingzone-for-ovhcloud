package checks

import "fmt"

const Image = "cgr.dev/chainguard/wolfi-base@sha256:fd536778d12e19bff29cfcf73265a14f585152a49d7f7cd6739ebe48dff01e26"

// Versions and Artifacts pin each tool's release and the SHA256 of its official
// release archive.
var Versions = map[string]string{"go": "1.27.1", "tofu": "1.13.0", "terramate": "0.17.3", "task": "3.53.1", "tflint": "0.64.0"}
var Artifacts = map[string]string{
	"go":        "63d339f0da5ab53635a56f2490a7984dfe12dfcff22ad749f63edaf590168445",
	"tofu":      "1f0cb37fc85dea4e7633a72aca2332b801f72650a32be911b8b9b32907f214fb",
	"terramate": "303fd597a76af00c728b3eb626493dc2a71585dcd679c5e07a338da44d24a060",
	"task":      "a54a408f6861ff921f6e87774180db31bacd8c1e7c944ca696db9fea49a82fc7",
	"tflint":    "cca9d13e2e1d7a2c627af60ff899a3c9b74212899416aeb96ec764d2ef954537",
}

// Prepared carries the tool identities recorded when the bundle was prepared.
type Prepared struct {
	Versions  map[string]string
	Artifacts map[string]string
	Image     string
}

// Isolation is evidence that the caller runs inside the admitted sandbox. Its
// zero value refuses capture, and only RuntimePrepared creates another one, so
// code outside this package cannot assert isolation.
type Isolation struct{ network string }

// AdmitPins accepts exactly the pinned tools, archives and image.
func AdmitPins(p Prepared) error {
	if len(p.Versions) != len(Versions) || len(p.Artifacts) != len(Artifacts) {
		return fmt.Errorf("PIN_SET: exact pinned tool set required")
	}
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
	return nil
}

// AdmitCapture requires the pins and the sandbox's isolation evidence.
func AdmitCapture(p Prepared, isolation Isolation) error {
	if err := AdmitPins(p); err != nil {
		return err
	}
	if isolation.network == "" {
		return fmt.Errorf("ISOLATION_GATE: runtime isolation evidence required")
	}
	return nil
}

// Capture exposes observable pre-invocation and publication ordering to tests.
func Capture(p Prepared, isolation Isolation, invoke, publish func() error) error {
	if err := AdmitCapture(p, isolation); err != nil {
		return err
	}
	if err := invoke(); err != nil {
		return err
	}
	return publish()
}
