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
	"slices"
	"strings"
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
	companion   *companionStage
}

// companionStage is a probe root's second stage (T075/T076): after the root's apply, the
// credential the root publishes (output companion_env) is written by files.go to rel (0600, below
// configRoot), every identity in it is bound, and the companion root runs with it in place of the
// admin credential.
type companionStage struct {
	stage      Stack
	stack      Stack
	configRoot string
	rel        string
	bind       func(context.Context, Credential) error
}

// companionOutput is the root output that publishes the companion's variables.
const companionOutput = "companion_env"

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
	// stackVars replaces the run's child variables for a stack (the companion's identity).
	stackVars map[string]map[string]string
	credFile  string // the companion credential file, once written
	// The second writer's log and init outcome (prepareSecondWriter, secondWriter).
	writerLog      bytes.Buffer
	writerInit     error
	writerRecorded bool
}

// tofuCmd is tofu on st's root with st's data directory (TF_DATA_DIR: backend configuration,
// module manifest, providers) in the run's scratch HOME, one per stack and run, never in the root:
// a probe root's backend path changes with each run id, so a data directory kept from another run
// fails init ("Backend configuration changed"); stacks of one run differ in backend and modules,
// so a shared one would leave a stack's destroy with another root's data; and the reviewed
// checkout stays untouched. A cleanup is a fresh run and initialises its own.
func (s *session) tofuCmd(ctx context.Context, st Stack, args ...string) *exec.Cmd {
	cmd := s.child.command(ctx, s.r.Tofu, append([]string{"-chdir=" + st.Dir}, args...)...)
	if v, ok := s.stackVars[st.ID]; ok {
		cmd.Env = childEnviron(s.child.home, v)
	}
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
	var reader io.Reader = out
	var second chan error
	fired := false
	if c := s.r.companion; c != nil && st.ID == c.stack.ID {
		second = make(chan error, 1)
		reader = &applyStartWatch{r: out, fire: func() {
			fired = true
			go func() { second <- s.secondWriter(ctx, st) }()
		}}
	}
	cerr := ConsumeApply(reader, st.ID, s.inv, s.term)
	werr := cmd.Wait()
	killGroup(ctx, cmd)
	if fired {
		cerr = errors.Join(cerr, <-second)
	} else if second != nil {
		// No resource operation, so no lock to contend for: the record says so (T010 reads it).
		fmt.Fprintf(&s.writerLog, "\nsecond writer never started: the apply reported no resource operation\n")
		cerr = errors.Join(cerr, s.writerRecord(st))
	}
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

// applyStartWatch calls fire once, when the apply stream reports its first resource operation:
// tofu holds the state lock from before that line until the apply ends. The event name may fall
// across two reads (review r1), so the end of each read is kept for the next.
type applyStartWatch struct {
	r    io.Reader
	fire func()
	done bool
	tail []byte
}

var applyStartToken = []byte(`"apply_start"`)

func (a *applyStartWatch) Read(p []byte) (int, error) {
	n, err := a.r.Read(p)
	if !a.done && n > 0 {
		seen := append(a.tail, p[:n]...)
		if bytes.Contains(seen, applyStartToken) {
			a.done, a.tail = true, nil
			a.fire()
		} else {
			a.tail = bytes.Clone(seen[max(0, len(seen)-len(applyStartToken)+1):])
		}
	}
	return n, err
}

// secondWriterStack is the second writer of st's state: st's root with a data directory of its own.
func secondWriterStack(st Stack) Stack { return Stack{ID: st.ID + "-second-writer", Dir: st.Dir} }

// prepareSecondWriter initialises the second writer's data directory before st's apply starts, so
// that live the writer reaches its plan while a short apply still holds the lock (T076). Its
// output and an init failure go to the writer's record, not to the run's outcome.
func (s *session) prepareSecondWriter(ctx context.Context, st Stack) {
	cmd := s.tofuCmd(ctx, secondWriterStack(st), initArgs...)
	cmd.Stdout, cmd.Stderr = &s.writerLog, &s.writerLog
	s.writerInit = cmd.Run()
	killGroup(ctx, cmd)
}

// writerRecord writes the second writer's log to the run record (redacted), once per run.
func (s *session) writerRecord(st Stack) error {
	s.writerRecorded = true
	return writeRecord(filepath.Join(s.r.Dir, "second-writer-"+st.ID+".txt"), []byte(s.red.Redact(s.writerLog.String())))
}

// secondWriter is a second writer of st's state while st's apply holds the lock (P1–P3): a plan
// that must not wait for the lock, from the data directory prepareSecondWriter initialised. Its
// outcome is recorded in the run record; a refusal is the expected observation, not an error of
// the run.
func (s *session) secondWriter(ctx context.Context, st Stack) error {
	buf := &s.writerLog
	err := s.writerInit
	if err == nil {
		cmd := s.tofuCmd(ctx, secondWriterStack(st), "plan", "-input=false", "-lock-timeout=0s")
		cmd.Stdout, cmd.Stderr = buf, buf
		err = cmd.Run()
		killGroup(ctx, cmd)
	}
	fmt.Fprintf(buf, "\nsecond writer exit: %v\n", err)
	return s.writerRecord(st)
}

// identities returns the identities in a published environment: every <prefix>CLIENT_ID (or
// <prefix>client_id) with its <prefix>CLIENT_SECRET (client_secret), sorted by variable name. The
// default one, OVH_CLIENT_ID/OVH_CLIENT_SECRET, is required; half an identity is refused.
func identities(env map[string]string, endpoint string) ([]Credential, error) {
	pair := func(k string) (string, bool) {
		for _, sfx := range [][2]string{{"CLIENT_ID", "CLIENT_SECRET"}, {"client_id", "client_secret"}, {"CLIENT_SECRET", "CLIENT_ID"}, {"client_secret", "client_id"}} {
			if p, ok := strings.CutSuffix(k, sfx[0]); ok {
				return p + sfx[1], true
			}
		}
		return "", false
	}
	keys := slices.Sorted(maps.Keys(env))
	var out []Credential
	for _, k := range keys {
		other, ok := pair(k)
		if !ok {
			continue
		}
		if env[k] == "" || env[other] == "" {
			return nil, fmt.Errorf("output %s publishes %s without %s", companionOutput, k, other)
		}
		if strings.HasSuffix(strings.ToUpper(k), "CLIENT_ID") {
			out = append(out, Credential{Endpoint: endpoint, ClientID: env[k], ClientSecret: env[other]})
		}
	}
	if env["OVH_CLIENT_ID"] == "" {
		return nil, fmt.Errorf("output %s holds no OVH_CLIENT_ID and OVH_CLIENT_SECRET", companionOutput)
	}
	return out, nil
}

// companionEnv reads the variables the stage published (under the admin credential), has
// files.go write them to the run's probe directory, binds every published identity to the run's
// account (P26) and makes them the companion's environment in place of the admin credential. On a
// cleanup (optional) a stage that published nothing — its apply failed before — has no companion:
// false, nil.
func (s *session) companionEnv(ctx context.Context, optional bool) (bool, error) {
	c := s.r.companion
	var out bytes.Buffer
	var env map[string]string
	if optional {
		if err := s.tofu(ctx, c.stage, &out, "output", "-json"); err != nil {
			return false, err
		}
		var all map[string]struct {
			Value json.RawMessage `json:"value"`
		}
		if err := json.Unmarshal(out.Bytes(), &all); err != nil {
			return false, fmt.Errorf("tofu output -json of %s is not a map of outputs", c.stage.ID)
		}
		o, ok := all[companionOutput]
		if !ok {
			return false, nil
		}
		if err := json.Unmarshal(o.Value, &env); err != nil {
			return false, fmt.Errorf("output %s of %s is not a map of strings", companionOutput, c.stage.ID)
		}
	} else {
		if err := s.tofu(ctx, c.stage, &out, "output", "-json", companionOutput); err != nil {
			return false, err
		}
		if err := json.Unmarshal(out.Bytes(), &env); err != nil {
			return false, fmt.Errorf("output %s of %s is not a map of strings", companionOutput, c.stage.ID)
		}
	}
	secrets := make([]string, 0, len(env))
	for _, v := range env {
		secrets = append(secrets, v)
	}
	s.red.Add(secrets...)
	if err := WriteCredentialFile(c.configRoot, c.rel, env); err != nil {
		return false, err
	}
	s.credFile = filepath.Join(c.configRoot, c.rel)
	cred, err := ReadCredentialFile(s.credFile)
	if err != nil {
		return false, err
	}
	// Bound as the companion gets them: from the file.
	ids, err := identities(cred, s.r.Creds["OVH_ENDPOINT"])
	if err != nil {
		return false, err
	}
	if c.bind != nil {
		for _, id := range ids {
			if err := c.bind(ctx, id); err != nil {
				return false, err
			}
		}
	}
	vars := maps.Clone(s.r.Vars)
	if vars == nil {
		vars = map[string]string{}
	}
	delete(vars, "TF_VAR_state_path") // the companion's state is its own backend's
	delete(vars, "TF_DATA_DIR")
	vars["OVH_ENDPOINT"] = s.r.Creds["OVH_ENDPOINT"]
	vars["TF_VAR_state_passphrase"] = s.r.Creds["TF_VAR_state_passphrase"]
	maps.Copy(vars, cred)
	s.stackVars[c.stack.ID] = vars
	s.stackVars[secondWriterStack(c.stack).ID] = vars
	return true, nil
}

// companion runs the companion root after the stage's apply, under the published identity, with
// the second writer's data directory initialised before the companion's apply.
func (s *session) companion(ctx context.Context) (err error) {
	// A companion that stops before its apply starts no writer: the record says so (review r2).
	defer func() {
		if !s.writerRecorded {
			fmt.Fprintf(&s.writerLog, "\nsecond writer never started: the companion stopped before its apply: %v\n", err)
			err = errors.Join(err, s.writerRecord(s.r.companion.stack))
		}
	}()
	if _, err := s.companionEnv(ctx, false); err != nil {
		return err
	}
	s.prepareSecondWriter(ctx, s.r.companion.stack)
	return s.stack(ctx, s.r.companion.stack)
}

// cleanupCompanion is the companion's part of `--cleanup`: the identity re-read from the stage's
// retained state, the companion initialised afresh and destroyed under it, before the stage.
func (s *session) cleanupCompanion(ctx context.Context) error {
	ok, err := s.companionEnv(ctx, true)
	if err != nil || !ok {
		return err
	}
	c := s.r.companion.stack
	if err := s.tofu(ctx, c, s.term, initArgs...); err != nil {
		return err
	}
	return s.tofu(ctx, c, s.term, "destroy", "-auto-approve", "-input=false")
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
	if err := r.scratchOutside(); err != nil {
		return err
	}
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
	s := &session{r: r, red: red, term: term, child: &childEnv{home: home, vars: vars, grace: grace}, stackVars: map[string]map[string]string{}}
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
		if runErr == nil && runCtx.Err() == nil && r.companion != nil && !r.PlanOnly {
			runErr = s.companion(runCtx)
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
			// The companion first, under the identity the root still holds; the root follows
			// whatever the companion's outcome, so the identity does not outlive the cleanup.
			if c := r.companion; c != nil && st.ID == c.stage.ID {
				destroyErr = errors.Join(destroyErr, s.cleanupCompanion(dctx))
			}
		}
		if e := s.tofu(dctx, st, term, "destroy", "-auto-approve", "-input=false"); e != nil {
			destroyErr = errors.Join(destroyErr, e)
		}
	}
	// The probe credential goes once the companion is destroyed (the root's destroy above has
	// removed the identity it names).
	if s.credFile != "" {
		destroyErr = errors.Join(destroyErr, removeFiles(s.credFile))
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
// leftover check pass, then both are deleted, with the run record (probe.env: the probe root and
// the project the run ran against) that lets `--cleanup` find the root and its project again. A
// run that wrote no state (it failed before any apply wrote one, or was plan-only) leaves nothing
// to clean up, so its files go whatever the outcome.
// Children get TF_VAR_state_path, TF_VAR_state_passphrase and TF_VAR_run_id (the probe roots name
// their resources with it).
//
// Second stage (T075/T076, tests/live/probes/README.md *Two-stage runs*): a root with a
// `companion/` directory publishes the companion's variables as output companion_env. After the
// root's apply (never in a plan-only run) they are written 0600 to companion.env in the run's
// probe directory, every published identity is bound (Bind), and the companion runs with them in
// place of the admin credential, while a second writer tries its state lock. On every way out the
// companion is destroyed before the root and companion.env is removed; `--cleanup` re-reads the
// output from the root's retained state and does the same.
type Probe struct {
	Run        Runner
	ConfigRoot string // ~/.config/ovh-lz
	Account    string
	// Bind binds a probe identity published for a companion to the run's account (P26) before
	// the companion runs; nil binds nothing.
	Bind func(context.Context, Credential) error
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

// probeProjectKey names the project the run ran against (its TF_VAR_project_id) in probe.env.
const probeProjectKey = "LZ_PROBE_PROJECT_ID"

// CondRecord names the refusal of a cleanup whose run record (probe.env) names no project id.
const CondRecord = "record"

// ProbeProject reads the project a run recorded (probe.env), for `--cleanup`: a cleanup destroys
// and checks leftovers in that project, not the one account.env names now (T077/T078).
func ProbeProject(configRoot, account, runID string) (string, error) {
	dir, err := ProbeDir(configRoot, account, runID)
	if err != nil {
		return "", err
	}
	return recordedProject(filepath.Join(dir, "probe.env"), runID)
}

func recordedProject(path, runID string) (string, error) {
	v, err := ReadCredentialFile(path)
	if err != nil {
		return "", err
	}
	if v[probeProjectKey] == "" {
		return "", refuse(CondRecord, "probe.env of run %s records no project id; cleanup refused", runID)
	}
	return v[probeProjectKey], nil
}

// scratchOutside: the runner knows the stack roots and the run records' directory (the parent of
// Dir, .local/live/ in the checkout); lz-live refuses the rest of the checkout.
func (r Runner) scratchOutside() error {
	dirs := []string{filepath.Dir(r.Dir)}
	for _, s := range r.Stacks {
		dirs = append(dirs, s.Dir)
	}
	return ScratchOutside(dirs...)
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
	// A companion root (its `companion/` directory) runs after a full apply of the root (never in
	// a plan-only run: Execute), and is destroyed first on exit and on `--cleanup`.
	cdir := filepath.Join(r.Stacks[0].Dir, "companion")
	if fi, err := os.Stat(cdir); err == nil && fi.IsDir() {
		dir, _, _, _, _ := p.files()
		rel, err := filepath.Rel(p.ConfigRoot, dir)
		if err != nil {
			return err
		}
		r.companion = &companionStage{stage: r.Stacks[0], stack: Stack{ID: r.Stacks[0].ID + "-companion", Dir: cdir, Ephemeral: true},
			configRoot: p.ConfigRoot, rel: filepath.Join(rel, "companion.env"), bind: p.Bind}
	}
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
	// Before the probe's files are written (Execute checks again for every other run).
	if err := p.Run.scratchOutside(); err != nil {
		return err
	}
	// A run whose record names no project could not be cleaned up (review r1).
	if p.Run.Vars["TF_VAR_project_id"] == "" {
		return refuse(CondRecord, "probe start without a project id (TF_VAR_project_id): its cleanup would be refused")
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
	if err := WriteCredentialFile(p.ConfigRoot, filepath.Join(rel, "probe.env"), map[string]string{probeRootKey: p.Run.Stacks[0].Dir, probeProjectKey: p.Run.Vars["TF_VAR_project_id"]}); err != nil {
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
	_, _, pass, record, err := p.files()
	if err != nil {
		return err
	}
	// Before the passphrase is read.
	if err := p.Run.scratchOutside(); err != nil {
		return err
	}
	// The project the run ran against, whatever the caller passes now.
	project, err := recordedProject(record, p.Run.ID)
	if err != nil {
		return err
	}
	p.Run.Vars = maps.Clone(p.Run.Vars)
	if p.Run.Vars == nil {
		p.Run.Vars = map[string]string{}
	}
	p.Run.Vars["TF_VAR_project_id"] = project
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
