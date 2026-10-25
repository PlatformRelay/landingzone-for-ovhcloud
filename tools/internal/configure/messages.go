package configure

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/stacks"
)

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

// toolDefect is the one entry of every generated code the derivation cannot reach.
var toolDefect = catalogueEntry{
	"Unreachable: the tool built a manifest the decoder refuses. This is a defect of the tool, not of your answers.",
	"Report it with the answers file; meanwhile edit `stacks/deployments.yaml` by hand.",
}

// catalogue is contracts/checks.md *Message catalogue*, verbatim. Every key is listed by name:
// there is no default entry, so a new code without one fails the completeness test.
var catalogue = map[catalogueKey]catalogueEntry{
	{SourceConfigure, CodeAnswersSyntax}: {
		"The file is not one JSON-compatible document; only whole-line `#` comments are allowed.",
		"Fix the bracket, comma or quote near the place named, or start again from the sandbox answers file."},
	{SourceConfigure, CodeAnswersTooLarge}: {
		"The answers file is larger than 64 KiB.",
		"Remove what is not an answer; a real answers file is a few hundred bytes."},
	{SourceConfigure, CodeAnswersNotRegular}: {
		"The answers path is not a regular file (a symlink, a directory or missing).",
		"Give the path of the file itself."},
	{SourceConfigure, CodeDuplicateKey}: {
		"A key appears twice in one object; only one could be used.",
		"Keep one of the two keys."},
	{SourceConfigure, CodeUnknownField}: {
		"This key is not part of the answers file, or its case differs.",
		"Check the spelling against the how-to; keys are lower case with `_`."},
	{SourceConfigure, CodeMissingField}: {
		"A required answer is missing or `null`.",
		"Add it; the how-to lists the required answers."},
	{SourceConfigure, CodeUnsupportedVersion}: {
		"`apiVersion` or `kind` is not the one this tool reads.",
		"Use `lz.platformrelay.dev/v1alpha1` and `DeploymentAnswers`."},
	{SourceConfigure, CodeInvalidValue}: {
		"A value does not have the allowed form.",
		"Use the form the how-to gives for that field."},
	{SourceConfigure, CodeCurrentManifestInvalid}: {
		"Today's `stacks/deployments.yaml` does not decode, so no diff can be shown.",
		"Fix it first; `task stacks:check` names the problem."},
	{SourceConfigure, CodeStagingSymlink}: {
		"`.local`, `.local/configure` or the staging file is a symlink; nothing was written.",
		"Remove the symlink and run the export again."},
	{SourceConfigure, CodeStagingWrite}: {
		"The staging file could not be written; the previous one is unchanged.",
		"Check free space and permissions of `.local/configure/`."},
	{SourceConfigure, CodeStagingNotDurable}: {
		"The staging file was written, but its directory could not be synced; after a crash the previous file may come back.",
		"Check `.local/configure/deployments.yaml` before you copy it, or run the export again."},

	{SourceGenerated, stacks.CodeInvalidValue}: {
		"A name built from your names is longer than 63 characters.",
		"Shorten the tenant, environment or region name; if they are short, report it with the answers file."},
	{SourceGenerated, stacks.CodeDuplicateName}: {
		"A tenant, an environment of one tenant or a region of one environment is named twice, or two environments use one project ref.",
		"Rename one of them, or give each environment its own `project_ref`."},
	{SourceGenerated, stacks.CodeDuplicateVLAN}: {
		"Two regions of one environment share a VLAN id.",
		"Give one region its own `vlan_id` (1 to 4000)."},
	{SourceGenerated, stacks.CodeSharedStateProject}: {
		"An environment uses the state project, which is allowed only in the sandbox (KD-1).",
		"Give the environment its own `project_ref`, or set `shared_state_project` if this is the sandbox."},
	{SourceGenerated, stacks.CodeManifestSyntax}:      toolDefect,
	{SourceGenerated, stacks.CodeDuplicateKey}:        toolDefect,
	{SourceGenerated, stacks.CodeUnknownField}:        toolDefect,
	{SourceGenerated, stacks.CodeMissingField}:        toolDefect,
	{SourceGenerated, stacks.CodeUnsupportedVersion}:  toolDefect,
	{SourceGenerated, stacks.CodeStageNotImplemented}: toolDefect,
	{SourceGenerated, stacks.CodeSlotNotAllowed}:      toolDefect,
	{SourceGenerated, stacks.CodeScopeViolation}:      toolDefect,
	{SourceGenerated, stacks.CodeDuplicateID}:         toolDefect,
	{SourceGenerated, stacks.CodeDuplicateScope}:      toolDefect,
	{SourceGenerated, stacks.CodePlatformRows}:        toolDefect,
	{SourceGenerated, stacks.CodeMissingTenantState}:  toolDefect,
	{SourceGenerated, stacks.CodeTenantScopeAccount}:  toolDefect,
	{SourceGenerated, stacks.CodeUnresolvedProducer}:  toolDefect,
}

// Explain is the refusal line of code from source, without a trailing newline. A (source, code)
// pair without an entry is an error, never a line without its sentence (FR-004).
func Explain(source, code, detail string) (string, error) {
	entry, ok := catalogue[catalogueKey{source, code}]
	if !ok {
		return "", fmt.Errorf("no catalogue entry for %s %s", source, code)
	}
	if strings.ContainsFunc(detail, unicode.IsControl) {
		detail = strconv.Quote(detail) // a path with a newline must not break the one line
	}
	return "configure: " + code + ": " + detail + ". " + entry.sentence + " Next: " + entry.next, nil
}

// ExplainError is the refusal line of err, its source taken from its own type: *AnswersError is
// SourceConfigure, *stacks.ManifestError SourceGenerated (Export's refusal of the generated
// manifest only; a current manifest that does not decode is CodeCurrentManifestInvalid through
// Explain). A wrapped or other error has no source and is an error, never printed raw.
func ExplainError(err error) (string, error) {
	switch e := err.(type) {
	case *AnswersError:
		return Explain(SourceConfigure, e.Code, e.Detail)
	case *stacks.ManifestError:
		return Explain(SourceGenerated, e.Code, e.Detail)
	default:
		return "", fmt.Errorf("no catalogue source for %T: %v", err, err)
	}
}
