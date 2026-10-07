package live

import (
	"context"
	"io"
)

// Account bootstrap, phases guard, identify, passphrase, admin and revoke (spec 005 FR-010,
// FR-012, research R13, R19; the state, publish and verify phases are T056/T057).
//
// Permissive stub written with the T042 tests: it reports every phase `unchanged` and does
// nothing. T043 replaces the body (bootstrap.go, rootkeys.go, ovhapi.go).

// Bootstrap phases, in run order (research R13). `revoke` runs only with FreshAccount.
const (
	PhaseGuard      = "guard"
	PhaseIdentify   = "identify"
	PhasePassphrase = "passphrase"
	PhaseAdmin      = "admin"
	PhaseRevoke     = "revoke"
)

// Phase statuses (research R13).
const (
	StatusRan       = "ran"
	StatusUnchanged = "unchanged"
	StatusBlocked   = "blocked"
	StatusFail      = "fail"
)

// PhaseResult is one phase's outcome; Detail never holds a secret.
type PhaseResult struct {
	Phase  string
	Status string
	Detail string
}

// Terminal is the operator's terminal. Production reads /dev/tty; tests inject a fake.
type Terminal interface {
	// ReadSecret prints prompt and reads one line with echo off (root AK/AS/CK).
	ReadSecret(prompt string) (string, error)
	// ReadLine prints prompt and reads one line with echo on (project references; not secret).
	ReadLine(prompt string) (string, error)
}

// BootstrapAccount is what the phases after `admin` run with.
type BootstrapAccount struct {
	ID    string     // the account id GET /auth/details returned
	Dir   string     // <ConfigRoot>/accounts/<ID>
	Admin Credential // the sandbox.env credential (never the root keys)
}

// BootstrapOptions configures one `lz-live bootstrap` run.
type BootstrapOptions struct {
	ConfigRoot   string   // ~/.config/ovh-lz (live.env, sandbox.env, accounts/)
	Checkout     string   // the owner's checkout; no credential file may lie inside it
	Endpoint     string   // API endpoint name written to sandbox.env and account.env, e.g. ovh-eu
	Org          string   // the manifest's spec.org
	ProjectRefs  []string // the manifest's project references; account.env holds LZ_PROJECT_ID_<REF> for each
	FreshAccount bool     // --fresh-account: root keys at the prompt, admin created, root credential revoked
	API          API      // OAuth2 token endpoint and API base; root-key requests are signed in process
	Terminal     Terminal
	Stdout       io.Writer // LZ-LIVE lines
	// Guard is the host guard (Host.Check); a refusal ends the run before any credential is read.
	Guard func() error
	// Rest runs the phases after `admin` (state, publish, verify; T056/T057); nil skips them.
	Rest func(ctx context.Context, a BootstrapAccount) error
}

// Bootstrap runs the phases in order and returns each phase's result. A refusal is a *Refusal
// (exit 3); a blocked phase exits 2, a failed one 1. Once root keys were entered, `revoke` runs
// on every exit path.
func Bootstrap(ctx context.Context, o BootstrapOptions) ([]PhaseResult, error) {
	return []PhaseResult{
		{Phase: PhaseGuard, Status: StatusUnchanged},
		{Phase: PhaseIdentify, Status: StatusUnchanged},
		{Phase: PhasePassphrase, Status: StatusUnchanged},
		{Phase: PhaseAdmin, Status: StatusUnchanged},
	}, nil
}
