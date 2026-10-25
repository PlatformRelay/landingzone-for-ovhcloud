package configure

import "errors"

// This file is the message catalogue (FR-004, contracts/checks.md *Message catalogue*): every
// refusal prints one line `configure: <CODE>: <detail>. <plain sentence> Next: <action>`. The
// entry is chosen by (source, code), because the answers decoder and the manifest decoder share
// code strings: a derivation defect must not blame the answers.

// Catalogue sources. SourceConfigure: the answers file, the current manifest and the staging
// write. SourceGenerated: a stacks Code constant returned for the generated manifest.
const (
	SourceConfigure = "configure"
	SourceGenerated = "generated"
)

// Refusal codes the command raises (data-model.md *Refusal message*), under SourceConfigure.
const (
	CodeCurrentManifestInvalid = "CURRENT_MANIFEST_INVALID" // today's stacks/deployments.yaml does not decode
	CodeStagingSymlink         = "STAGING_SYMLINK"          // .local, .local/configure or the staging file is a symlink
	CodeStagingWrite           = "STAGING_WRITE"            // the staging file was not written; the previous one is unchanged
	CodeStagingNotDurable      = "STAGING_NOT_DURABLE"      // renamed into place, but the directory sync failed
)

type catalogueKey struct{ source, code string }

type catalogueEntry struct{ sentence, next string }

// catalogue: stub until the green commit of T018.
var catalogue = map[catalogueKey]catalogueEntry{}

// Explain is the refusal line of code from source (stub: not implemented).
func Explain(source, code, detail string) (string, error) {
	return "", errors.New("not implemented")
}

// ExplainError is the refusal line of err (stub: not implemented).
func ExplainError(err error) (string, error) {
	return "", errors.New("not implemented")
}
