package main

import "errors"

// provenanceInput is what a provenance statement binds (FR-017): the source
// commit, the packed manifest and layer digests, the resources.json digest,
// the entry's digest and the builder that packed them.
type provenanceInput struct{ Commit, Manifest, Layer, Resources, Entry, Builder string }

// provenance renders an unsigned in-toto statement with a SLSA provenance v1
// predicate for one packed image. Stub until T034.
func provenance(in provenanceInput) ([]byte, error) {
	return nil, errors.New("PROVENANCE: not implemented")
}
