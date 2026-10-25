package configure

import "errors"

// Export derives the manifest of a decoded answers file (FR-002, data-model.md *Derivation*) and
// passes it through stacks.DecodeManifest (FR-003). It returns the bytes only when the decoder
// accepts them; on a refusal it returns nil and the decoder's *stacks.ManifestError.
func Export(a Answers) ([]byte, error) {
	return nil, errors.New("not implemented")
}
