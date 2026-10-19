// Package release renders the ADR-0022 release-train manifest (006 FR-016).
package release

import "errors"

// Manifest renders the release-train manifest of commit (a full object id) in the git repository
// at repo, for the train version (YYYY.MM.N), to bytes: JSON-compatible flow-form YAML, keys
// sorted, no timestamps, validated by schemas/release-manifest.schema.json (data-model *Release-train
// manifest*). It reads git objects only, never the working tree, the index or HEAD, so the bytes
// depend on the commit and version alone. The changelog lists the subjects of the commits reachable
// from commit but not from the newest train tag (a tag named YYYY.MM.N) reachable from it, or from
// the root when none is, grouped by commit type; a subject that does not follow
// `:gitmoji: type(scope): summary` is listed under unclassified. A malformed version is refused
// with an error starting `RELEASE_VERSION `, a version an existing tag already carries with one
// starting `RELEASE_VERSION_TAKEN `. It claims no tested tuple (`tested: []`), creates no tag and
// writes no file. Not implemented (T038).
func Manifest(repo, commit, version string) ([]byte, error) {
	return nil, errors.New("release: Manifest not implemented (T038)")
}
