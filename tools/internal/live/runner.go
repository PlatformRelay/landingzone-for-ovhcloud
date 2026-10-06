package live

import (
	"context"
	"io"
	"os"
	"time"
)

// Run core (FR-011, research R12): one runner serves probes, chain and bootstrap. For each stack
// in order it plans to a file, passes `tofu show -json` of that file through the retained-resource
// guard (protect.go, T064) and applies exactly that file with `-json`, appending the inventory as
// resources are created. Before the first apply it registers the destroy-on-exit: on success,
// failure, SIGINT, SIGTERM and deadline expiry it destroys the ephemeral stacks whose apply
// started, in reverse order, continuing after a failed destroy; then it runs the leftover check
// and writes summary.json. Every child gets the environment of env.go with a per-run scratch HOME
// (0700, removed when the run ends; never the caller's HOME, so ~/.ovh.conf, ~/.aws/credentials
// and the like cannot reach tofu or ovhcloud), and every child stream passes through redact.go.
//
// STUB (T054): the tests pin the behaviour; T055 implements it.

// Stack is one root the run applies.
type Stack struct {
	ID        string // instance id or probe name: plan-<ID>.txt, <ID>.tfstate
	Dir       string // root module directory (tofu -chdir)
	Ephemeral bool   // destroyed on exit; a retained stack never is
}

// Runner is one live run (live.Run is the guard entry of guard.go).
type Runner struct {
	ID        string // run id, YYYYMMDDThhmmssZ-<4 hex>
	Dir       string // run record directory, .local/live/<run-id>
	Tofu      string // tofu executable
	Authority Authority
	// Creds are the authority's credential variables (and a probe's state passphrase): passed to
	// every child, and every value is redacted from every output.
	Creds map[string]string
	// Vars are further variables for every child (TF_VAR_*), not secret.
	Vars     map[string]string
	Stacks   []Stack
	Deadline time.Duration // 0: DefaultDeadline
	// Protect is the retained-resource guard, given `tofu show -json` of the saved plan; a
	// refusal stops the run before that plan is applied.
	Protect           func(s Stack, plan []byte) error
	Leftovers         LeftoverCheck
	Terminal          io.Writer
	Signals           <-chan os.Signal // nil: SIGINT and SIGTERM of this process
	StateListFallback bool             // P24 refuted: `tofu state list` after each stack
}

// Execute runs r; the error maps to the exit code through ExitCode.
func (r Runner) Execute(ctx context.Context) error { return nil }

// Probe is one `lz-live probe` run (research R12 *Probe state*): its encrypted state
// (<ID>.tfstate of its one stack) and a private per-run passphrase file (passphrase.env,
// TF_VAR_state_passphrase, 0600, written by files.go) live under ProbeDir until destroy and the
// leftover check pass, then both are deleted. Children get TF_VAR_state_path and
// TF_VAR_state_passphrase.
type Probe struct {
	Run        Runner
	ConfigRoot string // ~/.config/ovh-lz
	Account    string
}

// ProbeDir is <configRoot>/accounts/<account>/state/probes/<runID>.
func ProbeDir(configRoot, account, runID string) (string, error) { return "", nil }

// Start creates the passphrase, runs the probe and deletes its state and passphrase only after
// destroy and the leftover check passed.
func (p Probe) Start(ctx context.Context) error { return nil }

// Cleanup is `lz-live probe --cleanup <run-id>` from a fresh process: it reads the retained
// passphrase, destroys the probe from its retained state, runs the leftover check and deletes
// both files only when both pass.
func (p Probe) Cleanup(ctx context.Context) error { return nil }
