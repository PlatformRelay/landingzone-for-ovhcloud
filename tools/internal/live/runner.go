package live

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Run core (FR-011, research R12): one runner serves probes, chain and bootstrap. For each stack
// in order it initialises the root without touching its committed dependency lock file
// (`-lockfile=readonly`: a changed lock file would leave the reviewed checkout dirty, and the host
// guard would refuse the next run and the cleanup), plans to a file, passes `tofu show -json` of that file through the retained-resource
// guard (live.Protect, protect.go) and applies exactly that file with `-json`, appending the
// inventory as resources are created. Before the first apply it registers the destroy-on-exit: on
// success, failure, SIGINT, SIGTERM and deadline expiry it destroys the ephemeral stacks whose
// apply started, in reverse order, continuing after a failed destroy; then it runs the leftover
// check and writes summary.json. Every child gets the environment of env.go with a per-run scratch
// HOME (0700, removed when the run ends; never the caller's HOME, so ~/.ovh.conf,
// ~/.aws/credentials and the like cannot reach tofu or ovhcloud) and, for tofu, a data directory
// (TF_DATA_DIR) per stack inside it, and every child stream passes through redact.go.
//
// Stopping a child: SIGINT, SIGTERM or the deadline is forwarded to the running child (the
// deadline and SIGHUP, a closed terminal, as SIGINT) so tofu can stop gracefully and write its
// state; a child still running after the grace period is killed with its whole process group (its
// provider plugins). Children run in a process group of their own, so a Ctrl-C at the terminal
// reaches them once, from the runner, not a second time from the terminal (a second interrupt
// makes tofu exit at once; believed, UNVERIFIED for OpenTofu 1.13). Signals that arrive
// once the destroy-on-exit has started are ignored: the destroy runs to its own deadline, and only
// a kill stops it (then `lz-live probe --cleanup` resumes).

// DefaultGrace is how long a child may take to stop after the forwarded signal before it is
// killed (believed long enough for tofu to finish in-flight API calls and write its state).
const DefaultGrace = 2 * time.Minute

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
	// Retained is the retained set every saved plan is judged against (live.Protect).
	Retained []Retained
	// Protect replaces live.Protect as the plan check; nil (production) is live.Protect against
	// Retained. Tests inject a hook to observe the call or force a refusal; a refusal stops the
	// run before that plan is applied.
	Protect           func(s Stack, plan []byte) error
	Leftovers         LeftoverCheck
	Terminal          io.Writer
	Signals           <-chan os.Signal // nil: SIGINT and SIGTERM of this process
	StateListFallback bool             // P24 refuted: `tofu state list` after each stack
	Grace             time.Duration    // 0: DefaultGrace
	PlanOnly          bool             // plan and judge every stack; apply and destroy nothing

	cleanupOnly bool // Probe.Cleanup: destroy every ephemeral stack, plan and apply nothing
}

// childEnv is what every child of one run shares: its environment (env.go's allowlist with the
// run's scratch HOME), the grace period and the signal a stop forwards.
type childEnv struct {
	home  string
	vars  map[string]string
	grace time.Duration
	stop  atomic.Value // os.Signal; unset: SIGINT
}

func (c *childEnv) stopSignal() os.Signal {
	if s, ok := c.stop.Load().(os.Signal); ok {
		return s
	}
	return syscall.SIGINT
}

// command returns a child that, when ctx ends, receives the stop signal, then a kill after the
// grace period.
func (c *childEnv) command(ctx context.Context, name string, arg ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, arg...)
	cmd.Env = childEnviron(c.home, c.vars)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return cmd.Process.Signal(c.stopSignal()) }
	cmd.WaitDelay = c.grace
	return cmd
}

type session struct {
	r       Runner
	red     *Redactor
	term    io.Writer
	child   *childEnv
	inv     *Inventory
	started []Stack // stacks whose apply started, in order
}

// tofuCmd is tofu on st's root with st's data directory (TF_DATA_DIR: backend configuration,
// module manifest, providers) in the run's scratch HOME, one per stack and run, never in the root:
// a probe root's backend path changes with each run id, so a data directory kept from another run
// fails init ("Backend configuration changed"); stacks of one run differ in backend and modules,
// so a shared one would leave a stack's destroy with another root's data; and the reviewed
// checkout stays untouched. A cleanup is a fresh run and initialises its own.
func (s *session) tofuCmd(ctx context.Context, st Stack, args ...string) *exec.Cmd {
	cmd := s.child.command(ctx, s.r.Tofu, append([]string{"-chdir=" + st.Dir}, args...)...)
	cmd.Env = append(cmd.Env, "TF_DATA_DIR="+filepath.Join(s.child.home, "tofu-data", st.ID))
	return cmd
}

func (s *session) tofu(ctx context.Context, st Stack, stdout io.Writer, args ...string) error {
	cmd := s.tofuCmd(ctx, st, args...)
	cmd.Stdout, cmd.Stderr = stdout, s.term
	err := cmd.Run()
	killGroup(ctx, cmd)
	if err != nil {
		return fmt.Errorf("tofu %s %s: %w", args[0], st.ID, err)
	}
	return nil
}

// killGroup kills what is left of a stopped child's process group (tofu's provider plugins): Go's
// kill after the grace period reaches the child's own pid only.
func killGroup(ctx context.Context, cmd *exec.Cmd) {
	if ctx.Err() != nil && cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

// initArgs initialise a root without writing its dependency lock file.
var initArgs = []string{"init", "-input=false", "-lockfile=readonly"}

func (s *session) protect(st Stack, plan []byte) error {
	if s.r.Protect != nil {
		return s.r.Protect(st, plan)
	}
	return Protect(plan, s.r.Retained)
}

// stack plans st to a file, records the rendered plan, judges the saved plan and applies exactly
// that file (unless the run is plan-only). The saved plan holds root variable values (a probe's
// state passphrase among them): it is removed when the stack is done.
func (s *session) stack(ctx context.Context, st Stack) (err error) {
	plan := filepath.Join(s.r.Dir, "plan-"+st.ID+".tfplan")
	defer func() { err = errors.Join(err, removeFiles(plan)) }()
	if err := s.tofu(ctx, st, s.term, initArgs...); err != nil {
		return err
	}
	if err := s.tofu(ctx, st, s.term, "plan", "-input=false", "-out="+plan); err != nil {
		return err
	}
	var js, txt bytes.Buffer
	if err := s.tofu(ctx, st, &js, "show", "-json", plan); err != nil {
		return err
	}
	if err := s.tofu(ctx, st, &txt, "show", "-no-color", plan); err != nil {
		return err
	}
	if err := writeRecord(filepath.Join(s.r.Dir, "plan-"+st.ID+".txt"), []byte(s.red.Redact(txt.String()))); err != nil {
		return err
	}
	if err := s.protect(st, js.Bytes()); err != nil {
		return err
	}
	if s.r.PlanOnly {
		return nil
	}
	s.started = append(s.started, st)
	cmd := s.tofuCmd(ctx, st, "apply", "-json", "-input=false", plan)
	cmd.Stderr = s.term
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("tofu apply %s: %w", st.ID, err)
	}
	cerr := ConsumeApply(out, st.ID, s.inv, s.term)
	werr := cmd.Wait()
	killGroup(ctx, cmd)
	// P24 fallback, also (above all) after a failed apply: the state holds what the stream missed.
	var ferr error
	if s.r.StateListFallback {
		var list bytes.Buffer
		// Also after a stop: bounded by the grace period, so a hanging `state list` cannot hold
		// back the destroy-on-exit.
		lctx, lcancel := context.WithTimeout(context.WithoutCancel(ctx), s.child.grace)
		defer lcancel()
		if ferr = s.tofu(lctx, st, &list, "state", "list"); ferr == nil {
			ferr = s.inv.AppendStateList(st.ID, &list)
		}
	}
	if werr != nil {
		return errors.Join(fmt.Errorf("tofu apply %s: %w", st.ID, werr), ferr)
	}
	return errors.Join(cerr, ferr)
}

var errDeadline = errors.New("deadline exceeded")

// Execute runs r; the error maps to the exit code through ExitCode.
func (r Runner) Execute(ctx context.Context) error {
	deadline := r.Deadline
	if deadline <= 0 {
		deadline = DefaultDeadline
	}
	grace := r.Grace
	if grace <= 0 {
		grace = DefaultGrace
	}
	secrets := make([]string, 0, len(r.Creds))
	for _, v := range r.Creds {
		secrets = append(secrets, v)
	}
	red := NewRedactor(secrets...)
	terminal := r.Terminal
	if terminal == nil {
		terminal = io.Discard
	}
	term := red.Writer(&quietWriter{w: terminal})
	defer term.Close()
	if err := ensureDir(r.Dir); err != nil {
		return err
	}
	home, err := scratchHome()
	if err != nil {
		return err
	}
	defer removeTree(home)
	vars := maps.Clone(r.Vars)
	if vars == nil {
		vars = map[string]string{}
	}
	maps.Copy(vars, r.Creds)
	delete(vars, "TF_DATA_DIR") // set per stack by tofuCmd; a caller cannot move it
	s := &session{r: r, red: red, term: term, child: &childEnv{home: home, vars: vars, grace: grace}}
	if s.inv, err = OpenInventory(r.Dir, red); err != nil {
		return err
	}
	defer s.inv.Close()

	// The destroy-on-exit is registered before the first apply: from here on every way out of
	// the stack loop (failure, refusal, signal, deadline) reaches the destroy below.
	runCtx, cancelTimeout := context.WithTimeoutCause(ctx, deadline, errDeadline)
	defer cancelTimeout()
	runCtx, cancel := context.WithCancelCause(runCtx)
	defer cancel(nil)
	sigs := r.Signals
	if sigs == nil {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
		defer signal.Stop(ch)
		sigs = ch
	}
	go func() {
		select {
		case sig := <-sigs:
			if sig == syscall.SIGHUP {
				s.child.stop.Store(os.Signal(syscall.SIGINT))
			} else {
				s.child.stop.Store(sig)
			}
			cancel(fmt.Errorf("interrupted by %v", sig))
		case <-runCtx.Done():
		}
	}()

	var runErr error
	if r.cleanupOnly {
		s.started = r.Stacks
	} else {
		for _, st := range r.Stacks {
			if runCtx.Err() != nil {
				break
			}
			if runErr = s.stack(runCtx, st); runErr != nil {
				break
			}
		}
	}
	if runCtx.Err() != nil {
		runErr = errors.Join(context.Cause(runCtx), runErr)
	}

	// Destroy-on-exit, with its own time: the run's context may have expired.
	dctx, dcancel := context.WithTimeout(context.WithoutCancel(ctx), deadline)
	defer dcancel()
	var destroyErr error
	for i := len(s.started) - 1; i >= 0; i-- {
		st := s.started[i]
		if !st.Ephemeral {
			continue
		}
		if r.cleanupOnly {
			// A fresh process: the root may never have been initialised in this checkout.
			if e := s.tofu(dctx, st, term, initArgs...); e != nil {
				destroyErr = errors.Join(destroyErr, e)
				continue
			}
		}
		if e := s.tofu(dctx, st, term, "destroy", "-auto-approve", "-input=false"); e != nil {
			destroyErr = errors.Join(destroyErr, e)
		}
	}

	entries, invErr := ReadInventory(r.Dir)
	if errors.Is(invErr, os.ErrNotExist) {
		invErr = nil
	}
	check := r.Leftovers
	check.child = s.child
	rep := check.Check(dctx, entries)
	var recErr error
	for typ, items := range rep.Listings {
		raw, e := json.Marshal(items)
		if e == nil {
			e = writeRecord(filepath.Join(r.Dir, "listings", typ+".json"), []byte(red.Redact(string(raw))))
		}
		recErr = errors.Join(recErr, e)
	}
	if raw, e := json.Marshal(rep); e != nil {
		recErr = errors.Join(recErr, e)
	} else {
		recErr = errors.Join(recErr, writeRecord(filepath.Join(r.Dir, "leftovers.json"), []byte(red.Redact(string(raw)))))
	}
	var leftErr error
	if rep.Outcome != "pass" {
		leftErr = fmt.Errorf("leftover check: %s (%d leftovers, %d errors)", rep.Outcome, len(rep.Leftovers), len(rep.Errors))
	}
	err = errors.Join(runErr, destroyErr, invErr, leftErr, recErr)
	outcome := "pass"
	if err != nil {
		outcome = "fail"
	}
	sum, _ := json.Marshal(map[string]any{"run_id": r.ID, "outcome": outcome, "deadline": deadline.String(),
		"plan_only": r.PlanOnly, "known_deviations": []string{}, "error": errString(err)})
	err = errors.Join(err, writeRecord(filepath.Join(r.Dir, "summary.json"), []byte(red.Redact(string(sum)))))
	fmt.Fprintf(term, "LZ-LIVE summary %s %s known-deviations=none\nrecord approximate cost for run %s in the PR\n", r.ID, outcome, r.ID)
	if err != nil {
		return redactedError{err, red}
	}
	return nil
}

// quietWriter drops everything after the terminal's first write error (a hung-up terminal answers
// EIO): an error would make os/exec close the child's pipe, and a child writing to a closed pipe
// dies of SIGPIPE — the destroy-on-exit among them.
type quietWriter struct {
	mu   sync.Mutex
	w    io.Writer
	dead bool
}

func (q *quietWriter) Write(p []byte) (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.dead {
		if _, err := q.w.Write(p); err != nil {
			q.dead = true
		}
	}
	return len(p), nil
}

// redactedError is a run's error as lz-live prints it: its text redacted, its chain (a *Refusal
// keeps exit 3) intact.
type redactedError struct {
	err error
	red *Redactor
}

func (e redactedError) Error() string { return e.red.Redact(e.err.Error()) }
func (e redactedError) Unwrap() error { return e.err }

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// Probe is one `lz-live probe` run (research R12 *Probe state*): its encrypted state
// (<ID>.tfstate of its one stack) and a private per-run passphrase file (passphrase.env,
// TF_VAR_state_passphrase, 0600, written by files.go) live under ProbeDir until destroy and the
// leftover check pass, then both are deleted, with the probe-root record (probe.env) that lets
// `--cleanup` find the root again. A run that wrote no state (it failed before any apply wrote
// one, or was plan-only) leaves nothing to clean up, so its files go whatever the outcome.
// Children get TF_VAR_state_path, TF_VAR_state_passphrase and TF_VAR_run_id (the probe roots name
// their resources with it).
type Probe struct {
	Run        Runner
	ConfigRoot string // ~/.config/ovh-lz
	Account    string
}

// RunIDPattern is the shape of a run id: YYYYMMDDThhmmssZ-<4 hex>.
var RunIDPattern = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-[0-9a-f]{4}$`)

// NewRunID returns a run id for now (UTC).
func NewRunID(now time.Time) (string, error) {
	b := make([]byte, 2)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return now.UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(b), nil
}

// ProbeDir is <configRoot>/accounts/<account>/state/probes/<runID>.
func ProbeDir(configRoot, account, runID string) (string, error) {
	if !RunIDPattern.MatchString(runID) {
		return "", fmt.Errorf("run id %q is not YYYYMMDDThhmmssZ-<4 hex>", runID)
	}
	dir, err := AccountDir(configRoot, account)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "state", "probes", runID), nil
}

// probeRootKey names the probe root directory in probe.env.
const probeRootKey = "LZ_PROBE_ROOT"

// ProbeRoot reads the probe root directory a run recorded (probe.env), for `--cleanup`.
func ProbeRoot(configRoot, account, runID string) (string, error) {
	dir, err := ProbeDir(configRoot, account, runID)
	if err != nil {
		return "", err
	}
	v, err := ReadCredentialFile(filepath.Join(dir, "probe.env"))
	if err != nil {
		return "", err
	}
	if v[probeRootKey] == "" {
		return "", fmt.Errorf("probe.env of run %s names no probe root", runID)
	}
	return v[probeRootKey], nil
}

func (p Probe) files() (dir, state, pass, root string, err error) {
	if len(p.Run.Stacks) != 1 {
		return "", "", "", "", errors.New("a probe has exactly one root")
	}
	dir, err = ProbeDir(p.ConfigRoot, p.Account, p.Run.ID)
	if err != nil {
		return "", "", "", "", err
	}
	return dir, filepath.Join(dir, p.Run.Stacks[0].ID+".tfstate"), filepath.Join(dir, "passphrase.env"), filepath.Join(dir, "probe.env"), nil
}

func (p Probe) run(ctx context.Context, phrase string, cleanup bool) error {
	_, state, pass, root, err := p.files()
	if err != nil {
		return err
	}
	r := p.Run
	r.Creds = maps.Clone(r.Creds)
	if r.Creds == nil {
		r.Creds = map[string]string{}
	}
	r.Creds["TF_VAR_state_passphrase"] = phrase // a secret: redacted, environment only
	r.Vars = maps.Clone(r.Vars)
	if r.Vars == nil {
		r.Vars = map[string]string{}
	}
	r.Vars["TF_VAR_state_path"] = state
	r.Vars["TF_VAR_run_id"] = r.ID
	r.cleanupOnly = cleanup
	if err := r.Execute(ctx); err != nil {
		if _, serr := os.Lstat(state); errors.Is(serr, os.ErrNotExist) {
			// No state was ever written: nothing to destroy, so no cleanup can use the
			// passphrase; it goes whatever the outcome.
			_ = removeFiles(pass, root)
			dir, _, _, _, _ := p.files()
			_ = removeEmptyDir(dir)
		}
		return err
	}
	if err := removeFiles(state, state+".backup", pass, root); err != nil {
		return err
	}
	dir, _, _, _, _ := p.files()
	return removeEmptyDir(dir)
}

// Start creates the passphrase, records the probe root, runs the probe and deletes the probe's
// files only after destroy and the leftover check passed.
func (p Probe) Start(ctx context.Context) error {
	dir, _, _, _, err := p.files()
	if err != nil {
		return err
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	phrase := hex.EncodeToString(b)
	rel, err := filepath.Rel(p.ConfigRoot, dir)
	if err != nil {
		return err
	}
	if err := WriteCredentialFile(p.ConfigRoot, filepath.Join(rel, "probe.env"), map[string]string{probeRootKey: p.Run.Stacks[0].Dir}); err != nil {
		return err
	}
	if err := WriteCredentialFile(p.ConfigRoot, filepath.Join(rel, "passphrase.env"), map[string]string{"TF_VAR_state_passphrase": phrase}); err != nil {
		return err
	}
	return p.run(ctx, phrase, false)
}

// Cleanup is `lz-live probe --cleanup <run-id>` from a fresh process: it reads the retained
// passphrase, destroys the probe from its retained state (planning and applying nothing), runs the
// leftover check and deletes the probe's files only when both pass. Without the passphrase it
// destroys nothing.
func (p Probe) Cleanup(ctx context.Context) error {
	_, _, pass, _, err := p.files()
	if err != nil {
		return err
	}
	v, err := ReadCredentialFile(pass)
	if err != nil {
		return err
	}
	phrase := v["TF_VAR_state_passphrase"]
	if len(phrase) < 16 {
		return errors.New("probe passphrase missing or shorter than 16 characters")
	}
	return p.run(ctx, phrase, true)
}
