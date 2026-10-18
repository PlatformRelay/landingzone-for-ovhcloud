package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"
)

// statementName is the statement's file in the layout, beside index.json.
const statementName = "provenance.json"

var (
	// commitID is a full git object id, SHA-1 or SHA-256, as git prints it:
	// a ref or a short id names no fixed source.
	commitID = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)
	// sha256Hex is a sha256 digest as bare lowercase hex.
	sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// provenanceInput is what a provenance statement binds (FR-017): the source
// commit, the packed manifest and layer digests, the resources.json digest,
// the entry's digest and the builder that packed them.
type provenanceInput struct{ Commit, Manifest, Layer, Resources, Entry, Builder string }

// checkCommit refuses anything but a full object id.
func checkCommit(commit string) error {
	if !commitID.MatchString(commit) {
		return errors.New("PROVENANCE: commit must be a full lowercase object id")
	}
	return nil
}

// check refuses an input missing a value the statement binds, or holding one
// in another form: manifest and layer as pack reports them ("sha256:<hex>"),
// resources.json and the entry as bare hex, the builder as lz-pack@<version>.
func (in provenanceInput) check() error {
	if err := checkCommit(in.Commit); err != nil {
		return err
	}
	for _, field := range []struct{ name, value, prefix string }{
		{"manifest", in.Manifest, "sha256:"},
		{"layer", in.Layer, "sha256:"},
		{"resources", in.Resources, ""},
		{"entry", in.Entry, ""},
	} {
		hex, ok := strings.CutPrefix(field.value, field.prefix)
		if !ok || !sha256Hex.MatchString(hex) {
			return fmt.Errorf("PROVENANCE: %s must be %s<64 lowercase hex>", field.name, field.prefix)
		}
	}
	if version, ok := strings.CutPrefix(in.Builder, "lz-pack@"); !ok || version == "" || strings.ContainsFunc(version, isSpaceOrControl) {
		return errors.New("PROVENANCE: builder must be lz-pack@<version>")
	}
	return nil
}

func isSpaceOrControl(r rune) bool { return r <= ' ' || r == 0x7f }

// sha256Set is an in-toto DigestSet holding one sha256 digest as bare hex.
func sha256Set(digest string) map[string]string {
	return map[string]string{"sha256": strings.TrimPrefix(digest, "sha256:")}
}

// provenance renders an unsigned in-toto Statement v1 with a SLSA provenance
// v1 predicate for one packed image: the subjects are its manifest and layer,
// the external parameters its source commit, the resolved dependencies
// resources.json and the entry. It holds no time, and encoding/json writes
// map keys sorted, so the same input always renders the same bytes.
func provenance(in provenanceInput) ([]byte, error) {
	if err := in.check(); err != nil {
		return nil, err
	}
	statement := map[string]any{
		"_type": "https://in-toto.io/Statement/v1",
		"subject": []any{
			map[string]any{"name": "manifest", "digest": sha256Set(in.Manifest)},
			map[string]any{"name": "layer", "digest": sha256Set(in.Layer)},
		},
		"predicateType": "https://slsa.dev/provenance/v1",
		"predicate": map[string]any{
			"buildDefinition": map[string]any{
				"buildType": source + "/tools/cmd/lz-pack/build-type/v1",
				"externalParameters": map[string]any{
					"source": map[string]any{"uri": "git+" + source, "digest": map[string]string{"gitCommit": in.Commit}},
				},
				"resolvedDependencies": []any{
					map[string]any{"name": "resources.json", "digest": sha256Set(in.Resources)},
					map[string]any{"name": "runtime/lz-offline", "digest": sha256Set(in.Entry)},
				},
			},
			"runDetails": map[string]any{"builder": map[string]any{"id": source + "/tools/cmd/" + in.Builder}},
		},
	}
	return json.Marshal(statement)
}

// builderVersion is lz-pack's version as the Go toolchain stamped it: the
// module version when there is one; otherwise "devel", followed by the
// revision lz-pack was built from (and "-modified" when its tree was not
// clean) when the build recorded one.
func builderVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "devel"
	}
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	settings := map[string]string{}
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}
	revision := settings["vcs.revision"]
	if !commitID.MatchString(revision) {
		return "devel"
	}
	if settings["vcs.modified"] == "true" {
		return "devel-" + revision + "-modified"
	}
	return "devel-" + revision
}

// writeStatement renders the statement for the packed image and writes it
// into the layout as a new private file. The entry digest is that of the
// entry the layer carries, and the resources.json digest is manifestSHA,
// which the packed bundle's admission has checked.
func writeStatement(out string, result packed, manifestSHA, commit string) error {
	data, err := provenance(provenanceInput{commit, result.Manifest, result.Layer, manifestSHA, result.Entry, "lz-pack@" + builderVersion()})
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(out, statementName), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("PROVENANCE: %w", err)
	}
	_, err = file.Write(data)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err
}
