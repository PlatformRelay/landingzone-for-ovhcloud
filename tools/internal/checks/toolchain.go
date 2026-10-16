package checks

import (
	"fmt"
	"maps"
)

const Image = "cgr.dev/chainguard/wolfi-base@sha256:fd536778d12e19bff29cfcf73265a14f585152a49d7f7cd6739ebe48dff01e26"

// Versions and Artifacts pin each tool's release and the SHA256 of its official
// release archive: go, tofu and terramate as the qualified toolchain holds
// them; task from
// https://github.com/go-task/task/releases/download/v3.53.1/task_linux_amd64.tar.gz
// as that release's task_checksums.txt publishes it; tflint from
// https://github.com/terraform-linters/tflint/releases/download/v0.64.0/tflint_linux_amd64.zip
// as that release's checksums.txt publishes it, whose keyless signature was
// verified against the release workflow identity before use.
var Versions = map[string]string{"go": "1.27.1", "tofu": "1.13.0", "terramate": "0.17.3", "task": "3.53.1", "tflint": "0.64.0"}
var Artifacts = map[string]string{
	"go":        "63d339f0da5ab53635a56f2490a7984dfe12dfcff22ad749f63edaf590168445",
	"tofu":      "1f0cb37fc85dea4e7633a72aca2332b801f72650a32be911b8b9b32907f214fb",
	"terramate": "303fd597a76af00c728b3eb626493dc2a71585dcd679c5e07a338da44d24a060",
	"task":      "a54a408f6861ff921f6e87774180db31bacd8c1e7c944ca696db9fea49a82fc7",
	"tflint":    "cca9d13e2e1d7a2c627af60ff899a3c9b74212899416aeb96ec764d2ef954537",
}

// The kinds of artefact Packages pins.
const (
	// KindAPK is a Wolfi package unpacked into the image's root on top of Image.
	KindAPK = "apk"
	// KindProvider is a provider package OpenTofu installs from the runtime's
	// filesystem mirror.
	KindProvider = "provider"
)

// Package is one artefact the runtime preparation fetches besides the tools'
// release archives: its version, where it is fetched from, the SHA-256 of the
// fetched file and its kind.
type Package struct {
	Version, URL, SHA256, Kind string
}

// Packages pins, by file name, every artefact the runtime preparation fetches
// besides the tools' release archives (Artifacts) and the image (Image). The
// Wolfi packages' provenance (signed APKINDEX, control checksum, data hash) was
// verified when they were pinned (specs/001-offline-foundation/evidence/T025.md).
// git's HTTP helpers need libcurl, which is deliberately absent: the runtime
// has no network. A bundle records each row's digest (PackagePins) and
// AdmitPins accepts only this exact set, so a bundle prepared before a row was
// added or changed is refused until it is prepared again.
var Packages = map[string]Package{
	"git-2.56.0-r0.apk": {
		Version: "2.56.0-r0", Kind: KindAPK,
		URL:    "https://packages.wolfi.dev/os/x86_64/git-2.56.0-r0.apk",
		SHA256: "33bc6d38714d6a65e412b87e7ef9392a27b4f2ca71f3506bd8e9eaa42d59c10d",
	},
	"libpcre2-8-0-10.49-r1.apk": {
		Version: "10.49-r1", Kind: KindAPK,
		URL:    "https://packages.wolfi.dev/os/x86_64/libpcre2-8-0-10.49-r1.apk",
		SHA256: "c2e8dacd8fe2f1c3de9eabd274532f74fcbbd3e919301fc95453ab3af850c38c",
	},
	"terraform-provider-ovh_2.21.0_linux_amd64.zip": {
		Version: "2.21.0", Kind: KindProvider,
		URL:    "https://github.com/ovh/terraform-provider-ovh/releases/download/v2.21.0/terraform-provider-ovh_2.21.0_linux_amd64.zip",
		SHA256: "54dc88d0e863d9f87e9a9c63407f9d9e6a73c0f29d16b226f9bafc6fd2507712",
	},
}

// PackagePins is Packages as a bundle records it: each file name with its
// SHA-256.
func PackagePins() map[string]string {
	pins := make(map[string]string, len(Packages))
	for name, row := range Packages {
		pins[name] = row.SHA256
	}
	return pins
}

// Pinned is what a bundle prepared from the pin tables records.
func Pinned() Prepared {
	return Prepared{Versions: Versions, Artifacts: Artifacts, Image: Image, Packages: PackagePins()}
}

// Prepared carries the tool identities recorded when the bundle was prepared.
type Prepared struct {
	Versions  map[string]string
	Artifacts map[string]string
	Image     string
	Packages  map[string]string
}

// Isolation is evidence that the caller runs inside the admitted sandbox. Its
// zero value refuses capture, and only RuntimePrepared creates another one, so
// code outside this package cannot assert isolation.
type Isolation struct{ network string }

// AdmitPins accepts exactly the pinned tools, archives, image and packages.
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
	if !maps.Equal(p.Packages, PackagePins()) {
		return fmt.Errorf("PIN_PACKAGE: exact pinned package set required")
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
