package live

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"
)

// Run core (FR-011, research R12): one runner serves probes, chain and bootstrap. For each stack
// in order it initialises the root without touching its committed dependency lock file
// (`-lockfile=readonly`: a changed lock file would leave the reviewed checkout dirty, and the host
// guard would refuse the next run and the cleanup), plans to a file, passes `tofu show -json` of that file through the retained-resource
// guard (live.Protect, protect.go) and applies exactly that file with `-json`, appending the
// inventory as resources are created. Before the first apply it registers the destroy-on-exit: on
// success, failure, SIGINT, SIGTERM and deadline expiry it destroys the ephemeral stacks whose
// apply started, in reverse order, each as a saved `plan -destroy` judged by the same guard and
// applied as that file (session.destroy), continuing after a refused or failed destroy (except a
// probe root whose companion was not destroyed, which is kept: Probe); then it runs the leftover
// check, writes summary.json and, when a tofu call failed, errors.txt (each failed call's output). Every child gets the environment of env.go with a per-run scratch
// HOME (0700, removed when the run ends; never the caller's HOME, so ~/.ovh.conf,
// ~/.aws/credentials and the like cannot reach tofu) and a data directory
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
	check       *ProbeCheck // Probe.Check, on the run's one stack
	// sweepProject is the project probe.env records, for the check's Sweep (sweep.go).
	sweepProject string
	// baseline is a probe run's baseline recovery copy (spec 007 FR-006), set by Probe.run on a
	// start and a cleanup: a start lists its baseline before the first apply (session.open), and
	// a failed leftover check leaves any probe run unreconciled (session.reconciled). nil for
	// every other run.
	baseline *recoveryCopy
	// stateless: a probe cleanup of a run without state, which runs no tofu (data-model *Start-up
	// phases and run classes*; DR-T013-b).
	stateless bool
	// companionBackoff are the waits before each retry of the companion's destroy (T095); nil:
	// defaultCompanionBackoff. Tests shorten them.
	companionBackoff []time.Duration
	// deadlineAt, once closed, ends the run as its deadline does (tests: once the run reaches the
	// call the deadline is to stop); nil in production. Deadline still bounds run and destroy.
	deadlineAt <-chan struct{}
	// destroyAt, once closed, ends the destroy-on-exit as its budget running out does (tests: once
	// a destroy hangs); nil in production.
	destroyAt <-chan struct{}
	// childStarted, when set, sees the context and arguments of every child the run starts (tests:
	// the destroy's own context, spec 006 FR-002); nil in production.
	childStarted func(ctx context.Context, args []string)
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
	// started, when set, sees each child's context and arguments as it is made (tests only:
	// Runner.childStarted, ChainOptions.childStarted); nil in production.
	started func(ctx context.Context, args []string)
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
	if c.started != nil {
		c.started(ctx, arg)
	}
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
	// The failed calls' output (recordFailure), written redacted to errors.txt when the run ends.
	errMu  sync.Mutex
	errLog bytes.Buffer
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
	var stderr capped
	cmd.Stdout, cmd.Stderr = stdout, io.MultiWriter(s.term, &stderr)
	err := cmd.Run()
	killGroup(ctx, cmd)
	if err != nil {
		s.recordFailure(st, args[0], err, stderr.String())
		return &tofuFailure{err: fmt.Errorf("tofu %s %s: %w", args[0], st.ID, err), output: stderr.String()}
	}
	return nil
}

// tofuFailure is a failed tofu call with what it printed on stderr (T095): kept in memory, unredacted,
// to tell a transient API failure from a refusal (transient); its text is the call's error only.
type tofuFailure struct {
	err    error
	output string
}

func (f *tofuFailure) Error() string { return f.err.Error() }
func (f *tofuFailure) Unwrap() error { return f.err }

// errLimit bounds what one failed call keeps of its output.
const errLimit = 1 << 20

// capped keeps the first errLimit bytes written to it.
type capped struct{ bytes.Buffer }

func (c *capped) Write(p []byte) (int, error) {
	if room := errLimit - c.Len(); room > 0 {
		c.Buffer.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

// diagnosticLines keeps the `diagnostic` events of an `apply -json` stream: their detail holds the
// API's answer, which the terminal shows only as the one-line summary (T094 gap 1). Each is kept
// decoded, as plain text, so that redaction sees every secret as written, not JSON-escaped (review
// r1). A line longer than errLimit is dropped, not buffered.
type diagnosticLines struct {
	part []byte
	long bool // the current line passed errLimit: skipped up to its newline
	kept capped
}

var diagnosticToken = []byte(`"diagnostic"`)

func (d *diagnosticLines) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		chunk := p
		if i >= 0 {
			chunk = p[:i]
		}
		if !d.long {
			d.part = append(d.part, chunk...)
			if len(d.part) > errLimit {
				d.part, d.long = nil, true
			}
		}
		if i < 0 {
			break
		}
		if !d.long {
			d.keep(d.part)
		}
		d.part, d.long, p = d.part[:0], false, p[i+1:]
	}
	return n, nil
}

func (d *diagnosticLines) keep(line []byte) {
	if !bytes.Contains(line, diagnosticToken) {
		return
	}
	var e struct {
		Type       string `json:"type"`
		Diagnostic struct {
			Severity, Summary, Detail, Address string
		} `json:"diagnostic"`
	}
	if json.Unmarshal(line, &e) != nil || e.Type != "diagnostic" {
		return
	}
	g := e.Diagnostic
	fmt.Fprintf(&d.kept, "diagnostic %s: %s (%s)\n%s\n", g.Severity, g.Summary, g.Address, g.Detail)
}

// recordFailure appends a failed call's output to the run's error record (errors.txt, written
// redacted when the run ends), so a refused API call's answer can be read, and captured as a
// fixture, after the run (T094 gap 1, T095).
func (s *session) recordFailure(st Stack, sub string, err error, output string) {
	s.errMu.Lock()
	defer s.errMu.Unlock()
	fmt.Fprintf(&s.errLog, "== %s: tofu %s: %v\n%s\n", st.ID, sub, err, strings.TrimRight(output, "\n"))
}

// Transient API failures (T095) are retried in the companion's destroy: an HTTP 5xx (go-ovh's
// "status code 503", net/http's "500 Internal Server Error"), an OAuth2 token fetch failure
// (golang.org/x/oauth2 "cannot fetch token: <status>", or a transport error on the token endpoint)
// or a connection reset. A 4xx status wins: it is never retried (a refused call, a bad credential,
// an unknown resource do not heal by waiting). A status counts only in those contexts — after
// "status code", after "cannot fetch token: ", or before a standard reason phrase — so a number in
// a response body ("400 Test Network") is no status (review r1).
var (
	statusCodeRE  = regexp.MustCompile(`(?i)\bstatus[ _]?code\D{0,3}([1-5][0-9]{2})\b`)
	tokenStatusRE = regexp.MustCompile(`oauth2: cannot fetch token: ([1-5][0-9]{2})\b`)
	statusTextRE  = regexp.MustCompile(`(?i)\b([45][0-9]{2}) (bad request|unauthorized|payment required|forbidden|not found|method not allowed|not acceptable|proxy authentication required|request timeout|conflict|gone|length required|precondition failed|request entity too large|content too large|request-uri too long|uri too long|unsupported media type|requested range not satisfiable|range not satisfiable|expectation failed|misdirected request|unprocessable entity|unprocessable content|locked|failed dependency|too early|upgrade required|precondition required|too many requests|request header fields too large|unavailable for legal reasons|internal server error|not implemented|bad gateway|service unavailable|gateway timeout|http version not supported|insufficient storage|network authentication required)\b`)
	// transientMarks need no status: a transport failure on the token endpoint, a reset connection,
	// a connection that timed out (T110: the state read of T049 run 3).
	transientMarks = []string{`/oauth2/token": `, "connection reset by peer", "connection timed out"}
)

func transient(output string) bool {
	fiveXX := false
	for _, re := range []*regexp.Regexp{statusCodeRE, tokenStatusRE, statusTextRE} {
		for _, m := range re.FindAllStringSubmatch(output, -1) {
			switch m[1][0] {
			case '4':
				return false
			case '5':
				fiveXX = true
			}
		}
	}
	return fiveXX || slices.ContainsFunc(transientMarks, func(m string) bool { return strings.Contains(output, m) })
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
	var stderr capped
	cmd.Stderr = io.MultiWriter(s.term, &stderr)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("tofu apply %s: %w", st.ID, err)
	}
	diags := &diagnosticLines{}
	reader := io.TeeReader(out, diags)
	var second chan error
	fired := false
	if c := s.r.companion; c != nil && st.ID == c.stack.ID {
		second = make(chan error, 1)
		reader = &applyStartWatch{r: reader, fire: func() {
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
		s.recordFailure(st, "apply", werr, stderr.String()+diags.kept.String())
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

// s3Keys are the published S3 credential's names: no OVHcloud identity, passed on unbound (T076).
var s3Keys = []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"}

// credentialSuffixes end, in any case, the name of a published key that is a credential but no half
// of an identity (T080, the narrow reading): such a key is refused, never passed on unbound.
var credentialSuffixes = []string{"key", "token", "password", "secret"}

// identities returns the identities in a published environment: every <prefix>client_id with the
// <prefix>client_secret of the same prefix, the suffixes in any case (T080), sorted by prefix. The
// default one, OVH_CLIENT_ID/OVH_CLIENT_SECRET, is required. Half an identity, an empty half, two
// ids or two secrets of one prefix, and a credential-shaped key (credentialSuffixes) other than the
// S3 keys are refused; any other key (e.g. an identity URN holding "client") is no credential. The
// errors name keys, never values.
func identities(env map[string]string, endpoint string) ([]Credential, error) {
	const idSfx, secretSfx = "client_id", "client_secret"
	type pair struct{ id, secret string }
	pairs := map[string]*pair{}
	for _, k := range slices.Sorted(maps.Keys(env)) {
		l := strings.ToLower(k)
		var prefix string
		isID := false
		switch {
		case strings.HasSuffix(l, idSfx):
			prefix, isID = k[:len(k)-len(idSfx)], true
		case strings.HasSuffix(l, secretSfx):
			prefix = k[:len(k)-len(secretSfx)]
		case slices.Contains(s3Keys, k):
			continue
		case slices.ContainsFunc(credentialSuffixes, func(sfx string) bool { return strings.HasSuffix(l, sfx) }):
			return nil, fmt.Errorf("output %s publishes %s, which looks like a credential but pairs with no identity", companionOutput, k)
		default:
			continue
		}
		p := pairs[prefix]
		if p == nil {
			p = &pair{}
			pairs[prefix] = p
		}
		half := &p.secret
		if isID {
			half = &p.id
		}
		if *half != "" {
			return nil, fmt.Errorf("output %s publishes %s and %s for one identity", companionOutput, *half, k)
		}
		*half = k
	}
	var out []Credential
	for _, prefix := range slices.Sorted(maps.Keys(pairs)) {
		p := pairs[prefix]
		if p.id == "" || p.secret == "" {
			return nil, fmt.Errorf("output %s publishes half an identity (%s%s)", companionOutput, p.id, p.secret)
		}
		if env[p.id] == "" || env[p.secret] == "" {
			return nil, fmt.Errorf("output %s publishes %s with an empty half", companionOutput, p.id)
		}
		out = append(out, Credential{Endpoint: endpoint, ClientID: env[p.id], ClientSecret: env[p.secret]})
	}
	// The provider reads only these names: a default pair in another case leaves the companion
	// without its credential (review r1).
	if env["OVH_CLIENT_ID"] == "" || env["OVH_CLIENT_SECRET"] == "" {
		return nil, fmt.Errorf("output %s holds no OVH_CLIENT_ID and OVH_CLIENT_SECRET", companionOutput)
	}
	return out, nil
}

// companionPairs are the identities a root may hand its companion (FR-006), by exact name: the
// OVHcloud OAuth2 client and the S3 key. Each pair is published complete or not at all.
var companionPairs = [][2]string{{"OVH_CLIENT_ID", "OVH_CLIENT_SECRET"}, {"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"}}

// companionVarName is the root's own Terraform variables (FR-006), matched case-sensitively: tofu
// reads TF_VAR_ only in that case.
var companionVarName = regexp.MustCompile(`^TF_VAR_[A-Za-z0-9_]+$`)

// companionRunVars are the run's own variables for the companion (T081): a root may not publish
// them, in any case, or it would override the run's state, run id and project.
var companionRunVars = []string{"TF_VAR_state_passphrase", "TF_VAR_state_path", "TF_VAR_run_id", "TF_VAR_project_id"}

// admitCompanionName judges one published name against the companion allow-list (FR-006, D115): nil
// for a TF_VAR_ name other than the run's own or a half of companionPairs, else
// COMPANION_NAME_REFUSED <name>. Anything else (the endpoint, HOME, PATH, tofu's own TF_* settings,
// another provider's credential, a name in another case) is refused, including names nobody has
// thought of yet. Pair completeness is companionEnv's.
func admitCompanionName(name string) error {
	if companionVarName.MatchString(name) && !slices.ContainsFunc(companionRunVars, func(r string) bool { return strings.EqualFold(name, r) }) {
		return nil
	}
	if slices.ContainsFunc(companionPairs, func(p [2]string) bool { return name == p[0] || name == p[1] }) {
		return nil
	}
	// A name with a line break, an escape or invalid UTF-8 is quoted, so it cannot forge lines in
	// lz-live's output (review r2).
	if !utf8.ValidString(name) || strings.ContainsFunc(name, func(r rune) bool { return !strconv.IsPrint(r) }) {
		name = strconv.Quote(name)
	}
	return fmt.Errorf("COMPANION_NAME_REFUSED %s", name)
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
			return false, publishedRefusal{fmt.Errorf("tofu output -json of %s is not a map of outputs", c.stage.ID)}
		}
		o, ok := all[companionOutput]
		if !ok {
			return false, nil
		}
		if err := json.Unmarshal(o.Value, &env); err != nil {
			return false, publishedRefusal{fmt.Errorf("output %s of %s is not a map of strings", companionOutput, c.stage.ID)}
		}
	} else {
		if err := s.tofu(ctx, c.stage, &out, "output", "-json", companionOutput); err != nil {
			return false, err
		}
		if err := json.Unmarshal(out.Bytes(), &env); err != nil {
			return false, publishedRefusal{fmt.Errorf("output %s of %s is not a map of strings", companionOutput, c.stage.ID)}
		}
	}
	secrets := make([]string, 0, len(env))
	for _, v := range env {
		secrets = append(secrets, v)
	}
	s.red.Add(secrets...)
	// A name off the allow-list, or half a pair, is refused before anything is written, bound or
	// run (FR-006), so a name that is no variable name at all is refused by name too, and on a
	// cleanup such a name keeps nothing (review r1); the error names the key, never its value.
	for _, k := range slices.Sorted(maps.Keys(env)) {
		if err := admitCompanionName(k); err != nil {
			return false, publishedRefusal{fmt.Errorf("output %s publishes a name off the allow-list: %w", companionOutput, err)}
		}
	}
	for _, p := range companionPairs {
		_, id := env[p[0]]
		_, secret := env[p[1]]
		if id != secret {
			return false, publishedRefusal{fmt.Errorf("output %s publishes half the pair %s/%s", companionOutput, p[0], p[1])}
		}
	}
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
		return false, publishedRefusal{err}
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
// retained state and bound again, the companion initialised afresh and destroyed under it, before
// the stage. keep reports a companion that may still hold resources (T095): the stage, which holds
// its identities and its state, must then stay. A refused published environment keeps nothing: the
// run refused it too, before the companion ran.
func (s *session) cleanupCompanion(ctx context.Context) (keep bool, err error) {
	ok, err := s.companionEnv(ctx, true)
	if err != nil {
		// Only what depends on the published values alone: a binding or file refusal can arise at
		// cleanup only (account.env edited, file mode changed) after a companion that ran (review r1).
		var pub publishedRefusal
		return !errors.As(err, &pub), err
	}
	if !ok {
		return false, nil
	}
	c := s.r.companion.stack
	if err := s.tofu(ctx, c, s.term, initArgs...); err != nil {
		return true, err
	}
	if err := s.destroyCompanion(ctx, c); err != nil {
		return true, err
	}
	return false, nil
}

// publishedRefusal is companionEnv's refusal of what a stage published (an output of the wrong shape,
// a name off the allow-list or half a pair, a credential that pairs with no identity): it depends on the published values only, so the run refused it too,
// before the companion ran (T095).
type publishedRefusal struct{ error }

func (p publishedRefusal) Unwrap() error { return p.error }

// checkEnv reads the variables the root published for its check (T096): every output, from
// `tofu output -json` into memory, then the check's one, a map of strings. Every value whose key
// is not public is added to the redactor before anyone sees it. A root that publishes no such
// output has nothing for a check: nil, nil. A refusal that depends on the published values alone
// is a publishedRefusal.
func (s *session) checkEnv(ctx context.Context, st Stack) (map[string]string, error) {
	c := s.r.check
	var out bytes.Buffer
	if err := s.tofu(ctx, st, &out, "output", "-json"); err != nil {
		return nil, err
	}
	var all map[string]struct {
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(out.Bytes(), &all); err != nil {
		return nil, publishedRefusal{fmt.Errorf("tofu output -json of %s is not a map of outputs", st.ID)}
	}
	o, ok := all[c.Output]
	if !ok {
		return nil, nil
	}
	var env map[string]string
	if err := json.Unmarshal(o.Value, &env); err != nil {
		return nil, publishedRefusal{fmt.Errorf("output %s of %s is not a map of strings", c.Output, st.ID)}
	}
	for k, v := range env {
		if !slices.Contains(c.Public, k) {
			s.red.Add(v)
		}
	}
	if c.Validate != nil {
		if err := c.Validate(env); err != nil {
			return nil, publishedRefusal{err}
		}
	}
	return env, nil
}

// observe runs the root's check once after its apply and writes its record, redacted, to
// observations.json, also when it fails.
func (s *session) observe(ctx context.Context) error {
	st := s.r.Stacks[0]
	env, err := s.checkEnv(ctx, st)
	if err != nil {
		return err
	}
	if env == nil {
		return fmt.Errorf("%s publishes no output %s for its check", st.ID, s.r.check.Output)
	}
	rec, err := s.r.check.Observe(ctx, env, s.term)
	if rec != nil {
		err = errors.Join(err, writeRecord(filepath.Join(s.r.Dir, "observations.json"), []byte(s.red.Redact(string(rec)))))
	}
	return err
}

// undo removes what the root's check may have written, before the root is destroyed. keep reports
// that writes may remain: the root, which holds the check's credentials, must then stay for
// `--cleanup`. A root that published nothing for its check, or published what the check refuses,
// was never observed: nothing to undo, nothing to keep.
func (s *session) undo(ctx context.Context, st Stack) (keep bool, err error) {
	env, err := s.checkEnv(ctx, st)
	if err != nil {
		var pub publishedRefusal
		return !errors.As(err, &pub), err
	}
	if env == nil {
		return false, nil
	}
	// A cleanup knows what the run's Observe wrote only from its record (T113).
	if s.r.cleanupOnly && s.r.check.Recorded != nil {
		rec, err := os.ReadFile(filepath.Join(s.r.Dir, "observations.json"))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			rec = nil
		case err != nil:
			return true, fmt.Errorf("undo of %s not run: observations.json unreadable: %w", st.ID, err)
		}
		if err := s.r.check.Recorded(rec, s.term); err != nil {
			return true, fmt.Errorf("undo of %s not run: %w", st.ID, err)
		}
	}
	if err := s.r.check.Undo(ctx, env, s.term); err != nil {
		return true, err
	}
	return false, nil
}

// defaultCompanionBackoff are the waits before the second and third attempt of the companion's
// destroy (T095): three attempts within about 80 s, well inside the default deadline.
var defaultCompanionBackoff = []time.Duration{20 * time.Second, 60 * time.Second}

// defaultStateReadBackoff are the waits before the second and third attempt of the leftover
// check's read of a retained instance's state (T110, chain.go readRetained), each admitted only by
// the check's one retry budget (spec 006 FR-008). The probe's leftover check reads no retained
// state, so it has no retry budget.
var defaultStateReadBackoff = []time.Duration{10 * time.Second, 30 * time.Second}

// destroyCompanion destroys the companion (session.destroy: plan, judge, apply that plan), retrying
// the whole destroy — a saved plan is stale once its apply failed — after a transient failure, at
// most len(backoff) times, each after its wait; a wait that would end after ctx's deadline is not
// started (T095). Each failed attempt's output is in the run's error record.
func (s *session) destroyCompanion(ctx context.Context, st Stack) error {
	backoff := s.r.companionBackoff
	if backoff == nil {
		backoff = defaultCompanionBackoff
	}
	for attempt := 0; ; attempt++ {
		err := s.destroy(ctx, st)
		if err == nil {
			return nil
		}
		err = fmt.Errorf("destroy of %s failed (attempt %d of %d): %w", st.ID, attempt+1, len(backoff)+1, err)
		var f *tofuFailure
		if attempt >= len(backoff) || ctx.Err() != nil || !errors.As(err, &f) || !transient(f.output) {
			return err
		}
		wait := backoff[attempt]
		if dl, ok := ctx.Deadline(); ok && time.Until(dl) < wait {
			return fmt.Errorf("%w; no retry: the deadline leaves less than %v", err, wait)
		}
		fmt.Fprintf(s.term, "LZ-LIVE retry destroy %s in %v: transient API failure (attempt %d of %d)\n", st.ID, wait, attempt+2, len(backoff)+1)
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return errors.Join(err, context.Cause(ctx))
		}
	}
}

// destroy destroys st as every destroy of the live lane does (T047, coordinator decision 2: one
// destroy discipline with the chain): `plan -destroy` to a file, `show -json` of that file through
// the retained-resource guard, then exactly that file applied (a saved plan is applied without a
// prompt); never `tofu destroy -auto-approve`. The saved plan holds root variable values (a probe's
// state passphrase among them): it is removed whatever happened.
func (s *session) destroy(ctx context.Context, st Stack) (err error) {
	plan := filepath.Join(s.r.Dir, "destroy-"+st.ID+".tfplan")
	defer func() { err = errors.Join(err, removeFiles(plan)) }()
	if err := s.tofu(ctx, st, s.term, "plan", "-destroy", "-input=false", "-out="+plan); err != nil {
		return err
	}
	var js bytes.Buffer
	if err := s.tofu(ctx, st, &js, "show", "-json", plan); err != nil {
		return err
	}
	if err := s.protect(st, js.Bytes()); err != nil {
		return err
	}
	return s.tofu(ctx, st, s.term, "apply", "-input=false", plan)
}

var errDeadline = errors.New("deadline exceeded")

// recordDir creates the run's record directory; on `--cleanup` it first archives the record an
// earlier run or cleanup left there (keepRecord, spec 007 FR-002), before this cleanup writes
// anything of its own.
func (r Runner) recordDir() error {
	if err := ensureDir(r.Dir); err != nil {
		return err
	}
	if !r.cleanupOnly {
		return nil
	}
	_, err := keepRecord(r.Dir)
	return err
}

// open opens the run's inventory and, on a probe start that applies, lists and writes the
// probe's baseline before any child runs (spec 007 FR-006 phase 3), under runCtx (the run's
// deadline and signals): its retries use the leftover check's backoff and clock and a retry
// budget ending at that deadline. A failed baseline ends the run here: nothing was created.
func (s *session) open(runCtx context.Context) error {
	inv, err := OpenInventory(s.r.Dir, s.red)
	if err != nil {
		return err
	}
	s.inv = inv
	if s.r.baseline == nil || s.r.cleanupOnly || s.r.PlanOnly {
		return nil
	}
	check := s.r.Leftovers
	check.Red, check.Term = s.red, s.term
	end, _ := runCtx.Deadline()
	check.Budget = newRetryBudget(end, minRetryReserve)
	if err := s.probeBaseline(runCtx, check); err != nil {
		if runCtx.Err() != nil {
			err = errors.Join(context.Cause(runCtx), err)
		}
		inv.Close()
		return redactedError{err, s.red}
	}
	return nil
}

// cleanupStacks are the stacks a cleanup destroys: every stack, or none for a stateless probe
// cleanup, which runs no tofu.
func (r Runner) cleanupStacks() []Stack {
	if r.stateless {
		return nil
	}
	return r.Stacks
}

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
	if err := r.recordDir(); err != nil {
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
	s := &session{r: r, red: red, term: term, child: &childEnv{home: home, vars: vars, grace: grace, started: r.childStarted}, stackVars: map[string]map[string]string{}}
	// The destroy-on-exit is registered before the first apply: from here on every way out of
	// the stack loop (failure, refusal, signal, deadline) reaches the destroy below. The run's
	// deadline and signals already bound the probe's baseline (session.open).
	runCtx, cancelTimeout := withDeadline(ctx, deadline, r.deadlineAt)
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
	if err = s.open(runCtx); err != nil {
		return err
	}
	defer s.inv.Close()

	var runErr error
	if r.cleanupOnly {
		s.started = r.cleanupStacks()
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
		if runErr == nil && runCtx.Err() == nil && r.check != nil && !r.PlanOnly {
			runErr = s.observe(runCtx)
		}
	}
	if runCtx.Err() != nil {
		runErr = errors.Join(context.Cause(runCtx), runErr)
	}

	// Destroy-on-exit, with its own time: the run's context may have expired. One context serves the
	// destroys and the leftover check, with at least DefaultDeadline (FR-002).
	budget := destroyBudget(deadline)
	dctx, dcancel := destroyContext(ctx, budget, r.destroyAt)
	defer dcancel()
	var destroyErr error
	// keepStage: the companion was not destroyed, so its stage — the identities it runs under, the
	// bucket of its state, its S3 credential — is not destroyed either, and `--cleanup` can reach
	// the companion later (T095; before, the stage's destroy removed the identity and left the
	// companion's resources out of every cleanup's reach).
	keepStage := false
	rootKept := false // the probe root stays for --cleanup: the check's Sweep waits for it (sweep.go)
	for i := len(s.started) - 1; i >= 0; i-- {
		st := s.started[i]
		if !st.Ephemeral {
			continue
		}
		c := r.companion
		if r.cleanupOnly {
			// A fresh process: the root may never have been initialised in this checkout.
			if e := s.tofu(dctx, st, term, initArgs...); e != nil {
				destroyErr = errors.Join(destroyErr, e)
				rootKept = rootKept || st.ID == r.Stacks[0].ID
				continue
			}
			// The companion first, under the identity the root still holds.
			if c != nil && st.ID == c.stage.ID {
				keep, e := s.cleanupCompanion(dctx)
				destroyErr, keepStage = errors.Join(destroyErr, e), keep
			}
		}
		if c != nil && st.ID == c.stack.ID {
			if e := s.destroyCompanion(dctx, st); e != nil {
				destroyErr, keepStage = errors.Join(destroyErr, e), true
			}
			continue
		}
		// The check's writes go before the root that holds its credentials (T096), also when the
		// root is kept for its companion (T100 review r1: an IAM write OVHcloud accepted must not
		// wait for the cleanup).
		keepCheck := false
		if r.check != nil && st.ID == r.Stacks[0].ID {
			var e error
			keepCheck, e = s.undo(dctx, st)
			destroyErr = errors.Join(destroyErr, e)
			if keepCheck {
				destroyErr = errors.Join(destroyErr, fmt.Errorf("%s kept: its check's writes may remain; undo and destroy with --cleanup %s", st.ID, r.ID))
			}
		}
		if keepStage && c != nil && st.ID == c.stage.ID {
			destroyErr = errors.Join(destroyErr, fmt.Errorf("%s kept: its companion %s was not destroyed and needs the identities %s holds; destroy both with --cleanup %s", st.ID, c.stack.ID, st.ID, r.ID))
			rootKept = true
			continue
		}
		if keepCheck {
			rootKept = true
			continue
		}
		if e := s.destroy(dctx, st); e != nil {
			destroyErr = errors.Join(destroyErr, e)
		}
	}
	// The probe credential goes once the companion is done with (a kept stage still holds it in its
	// state, where `--cleanup` reads it again).
	if s.credFile != "" {
		destroyErr = errors.Join(destroyErr, removeFiles(s.credFile))
	}
	// The token the check obtains goes to this run's redactor before its first listing (FR-004):
	// the one every record below and the terminal go through, whatever Red the caller set. Its
	// listing retries wait in a budget ending at the destroy context's deadline and print to the
	// run's terminal (spec 007 FR-001). The Sweep's reads are lent that same retry (T027): one
	// budget, one clock, and their attempts in leftovers.json. The Sweep spends first, so its
	// waits leave the check's listings less to retry with (fail closed: a refused wait fails).
	check := s.r.Leftovers // with the baseline's Before (probeBaseline)
	check.Red, check.Term = red, term
	end, _ := dctx.Deadline()
	check.Budget = newRetryBudget(end, minRetryReserve)
	check.lent = check.retry()
	destroyErr = errors.Join(destroyErr, s.sweep(context.WithValue(dctx, readRetryKey{}, check.lent), rootKept))

	entries, invErr := ReadInventory(r.Dir)
	if errors.Is(invErr, os.ErrNotExist) {
		invErr = nil
	}
	rep := check.Check(dctx, entries)
	// A call that ignores its context may still answer after the budget ran out: the context's own
	// state fails the run too.
	destroyErr = errors.Join(destroyErr, destroyBudgetSpent(dctx, budget))
	var recErr error
	for typ, items := range rep.Listings {
		raw, e := json.Marshal(items)
		if e == nil {
			e = writeRecord(filepath.Join(r.Dir, "listings", typ+".json"), []byte(red.Redact(string(raw))))
		}
		recErr = errors.Join(recErr, e)
	}
	s.errMu.Lock()
	errText := s.errLog.String()
	s.errMu.Unlock()
	if errText != "" {
		recErr = errors.Join(recErr, writeRecord(filepath.Join(r.Dir, "errors.txt"), []byte(red.Redact(errText))))
	}
	if raw, e := json.Marshal(rep); e != nil {
		recErr = errors.Join(recErr, e)
	} else {
		recErr = errors.Join(recErr, writeRecord(filepath.Join(r.Dir, "leftovers.json"), []byte(red.Redact(string(raw)))))
	}
	var leftErr error
	if rep.Outcome != "pass" {
		leftErr = s.reconciled(fmt.Errorf("leftover check: %s (%d leftovers, %d errors)", rep.Outcome, len(rep.Leftovers), len(rep.Errors)))
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
// to clean up, so its files go whatever the outcome, unless its check's Sweep or the leftover
// check failed (sweep.go).
// Children get TF_VAR_state_path, TF_VAR_state_passphrase and TF_VAR_run_id (the probe roots name
// their resources with it).
//
// Second stage (T075/T076, tests/live/probes/README.md *Two-stage runs*): a root with a
// `companion/` directory publishes the companion's variables as output companion_env. After the
// root's apply (never in a plan-only run) they are written 0600 to companion.env in the run's
// probe directory, every published identity is bound (Bind), and the companion runs with them in
// place of the admin credential, while a second writer tries its state lock. On every way out the
// companion is destroyed before the root and companion.env is removed; `--cleanup` re-reads the
// output from the root's retained state and does the same. The companion's destroy is retried after
// a transient API failure (destroyCompanion); a companion that still cannot be destroyed keeps the
// root, its identities and the companion's state bucket, for `--cleanup` (T095).
type Probe struct {
	Run        Runner
	ConfigRoot string // ~/.config/ovh-lz
	Account    string
	// Bind binds a probe identity published for a companion to the run's account (P26) before
	// the companion runs; nil binds nothing.
	Bind func(context.Context, Credential) error
	// Check is the root's check after its apply (T096); nil: none.
	Check *ProbeCheck
	// AcceptLostBaseline is FR-007's owner recovery (`--cleanup <run-id> --accept-lost-baseline`,
	// after the owner typed the run id back on the terminal): Cleanup finishes an untrusted run
	// without a baseline (destroy from its state if it has one, the check's Sweep, the leftover
	// check with no Before) and retires it only when all passed; it refuses a run of any other
	// class (CondAcceptLostBaseline). A Sweep that judges by the baseline is the caller's to drop.
	AcceptLostBaseline bool
	// AcceptDeletesNothing (with AcceptLostBaseline): no destroy either, even from state (FR-007:
	// the owner recovery of a root whose objects only a baseline could tell apart deletes nothing;
	// the owner removed them by hand). The run's state file is retired with its other files.
	AcceptDeletesNothing bool
}

// CondAcceptLostBaseline refuses `--accept-lost-baseline`: no terminal, another line typed back,
// or a run that is not untrusted.
const CondAcceptLostBaseline = "accept-lost-baseline"

// ProbeCheck is a probe root's check after its apply (T096: the state-backend probe observes its
// S3 user's Deny statement). The root publishes the check's variables, credentials included, as
// the sensitive output named Output (a map of strings); lz-live reads it from `tofu output -json`
// into memory only — no file, no argv, no child environment — and adds every value whose key is
// not in Public to the run's redactor before the check sees it. One child is the exception (spec
// 003 T036): the locked-recovery check's tofu seam (cmd/lz-live recovery_tofu.go), the pinned
// OpenTofu on that probe's disposable backend root, receives the state user's key pair
// (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY) in its environment, and nothing else the root
// published; the seam refuses any other key. Observe runs once, after the
// root's apply (never in a plan-only run); the record it returns is written, redacted, to
// observations.json in the run record, also when it fails. Undo runs on every way out before the
// root is destroyed, and on `--cleanup` from the root's retained state; it must remove whatever
// Observe may have written, whether or not Observe ran or finished. A root whose Undo fails is
// kept, not destroyed, for `--cleanup`. Both get the run's redacted terminal.
type ProbeCheck struct {
	Output string
	Public []string
	// Validate refuses published variables the check cannot use; it depends on them alone, so a
	// refused root was never observed and is destroyed, not kept (review r1). nil: none.
	Validate func(env map[string]string) error
	Observe  func(ctx context.Context, env map[string]string, term io.Writer) (record []byte, err error)
	Undo     func(ctx context.Context, env map[string]string, term io.Writer) error
	// Recorded (optional) hands a `--cleanup` — a fresh process — what the run's Observe recorded,
	// before Undo: the run directory's observations.json as the run wrote it (redacted), or nil
	// when there is none (the run never observed, or the cleanup runs in another checkout). An
	// unreadable file, or an error from Recorded, fails the undo step without Undo, so the root is
	// kept (T113, T101 gap 2). A run hands Undo what its own Observe saw and never calls Recorded.
	Recorded func(record []byte, term io.Writer) error
	// Sweep (optional) deletes, by listing exact run-derived names, every resource the root
	// or the check may have created, whether or not an output or a state entry exists; it
	// never reads a published output. nil: none (spec 005's roots). The runner calls it with the
	// run id, the project probe.env records and the run's redacted terminal, after destroy and
	// before the leftover check, on every way out and on `--cleanup`, except while the root is
	// kept and in a plan-only run; a failure keeps the run's files for `--cleanup` (sweep.go).
	Sweep func(ctx context.Context, runID, projectID string, term io.Writer) error
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

// CondUnreconciledRun names the refusal of a probe start while another probe run is unreconciled
// (its directory exists under state/probes/; spec 007 FR-006).
const CondUnreconciledRun = "unreconciled-run"

// Baseline is a probe run's baseline.json (spec 007 FR-006, data-model *Probe baseline*): the ids
// of the namelessKinds listed before the run's first apply.
type Baseline struct {
	RunID         string              `json:"run_id"`
	ProjectSHA256 string              `json:"project_sha256"`
	Listed        map[string][]string `json:"listed"`
	Complete      bool                `json:"complete"`
	Errors        []string            `json:"errors"`
}

// RunClass is what a `--cleanup` may do with a probe run (data-model *Start-up phases and run
// classes*).
type RunClass string

const (
	RunPreCreate      RunClass = "pre-create"
	RunBaselineBacked RunClass = "baseline-backed"
	RunUntrusted      RunClass = "untrusted"
)

// The reasons of LoadBaseline's errors that ClassifyRun reads (baselineError).
const (
	baselineMissing    = "missing"
	baselineIncomplete = "incomplete"
)

// baselineError is LoadBaseline's error: BASELINE_MISSING, or BASELINE_INVALID <reason>.
type baselineError struct{ reason, detail string }

func (e *baselineError) Error() string {
	if e.reason == baselineMissing {
		return "BASELINE_MISSING: " + e.detail
	}
	return "BASELINE_INVALID " + e.reason + ": " + e.detail
}

// LoadBaseline reads a run's baseline and validates it against runID and projectID (data-model
// *Probe baseline*, *Fixed signatures*): recordDir's baseline.json first; if that copy is missing
// or unreadable, the recovery copy at recoveryPath; two readable copies that differ are
// BASELINE_INVALID copies-differ. Its error text starts BASELINE_MISSING or BASELINE_INVALID
// <reason>; it never names the project id.
func LoadBaseline(recordDir, recoveryPath, runID, projectID string) (Baseline, error) {
	b, err := chooseBaseline(filepath.Join(recordDir, baselineFile), recoveryPath)
	if err != nil {
		return Baseline{}, err
	}
	return b, b.check(runID, projectID)
}

// baselineRead is one copy of a baseline as read: its bytes and their decoding.
type baselineRead struct {
	raw []byte
	b   Baseline
	err error // fs.ErrNotExist, a read error or a decoding error
}

func readBaselineCopy(path string) baselineRead {
	raw, err := os.ReadFile(path)
	if err != nil {
		return baselineRead{err: err}
	}
	var b Baseline
	if err := json.Unmarshal(raw, &b); err != nil {
		return baselineRead{err: err}
	}
	return baselineRead{raw: raw, b: b}
}

// chooseBaseline is the copy LoadBaseline validates: the record copy, else the recovery copy.
func chooseBaseline(record, recovery string) (Baseline, error) {
	rc, rv := readBaselineCopy(record), readBaselineCopy(recovery)
	switch {
	case rc.err == nil && rv.err == nil && !bytes.Equal(rc.raw, rv.raw):
		return Baseline{}, &baselineError{"copies-differ", fmt.Sprintf("%s and %s are both readable and differ", record, recovery)}
	case rc.err == nil:
		return rc.b, nil
	case rv.err == nil:
		return rv.b, nil
	case errors.Is(rc.err, fs.ErrNotExist) && errors.Is(rv.err, fs.ErrNotExist):
		return Baseline{}, &baselineError{baselineMissing, fmt.Sprintf("neither %s nor %s exists", record, recovery)}
	}
	return Baseline{}, &baselineError{"unreadable", fmt.Sprintf("%s: %v; %s: %v", record, rc.err, recovery, rv.err)}
}

// check validates a baseline read for runID and projectID: its run, the project's digest, a
// complete listing without errors, and the alert kind listed (an empty list, never null).
func (b Baseline) check(runID, projectID string) error {
	alerts, listed := b.Listed[tAlert]
	switch {
	case b.RunID != runID:
		return &baselineError{"another-run", fmt.Sprintf("it names run %q, not %s", b.RunID, runID)}
	case b.ProjectSHA256 != projectDigest(projectID):
		return &baselineError{"another-project", "its project digest is not the one probe.env records"}
	case !b.Complete:
		return &baselineError{baselineIncomplete, fmt.Sprintf("its listing failed (%d errors): nothing was created after it", len(b.Errors))}
	case len(b.Errors) > 0:
		return &baselineError{"errors", fmt.Sprintf("it records %d errors", len(b.Errors))}
	case !listed || alerts == nil:
		return &baselineError{"no-alert-key", "it lists no " + tAlert}
	}
	return nil
}

// projectDigest is the hex SHA-256 of a project id, which a baseline records instead of the id.
func projectDigest(id string) string {
	h := sha256.Sum256([]byte(id))
	return hex.EncodeToString(h[:])
}

// ClassifyRun classes a probe run from whether its state exists, whether its inventory holds an
// entry, and its baseline as LoadBaseline returned it (data-model *Start-up phases and run
// classes*): a valid baseline is baseline-backed; with no state and no inventory entry, a missing
// baseline or one of this run and project whose listing failed is pre-create; everything else is
// untrusted. Missing state alone never makes a run pre-create.
func ClassifyRun(stateExists, inventoryHasEntry bool, b Baseline, loadErr error) RunClass {
	if loadErr == nil {
		return RunBaselineBacked
	}
	var be *baselineError
	if stateExists || inventoryHasEntry || !errors.As(loadErr, &be) {
		return RunUntrusted
	}
	if be.reason == baselineMissing || be.reason == baselineIncomplete {
		return RunPreCreate
	}
	return RunUntrusted
}

// unreconciledRuns returns the run ids of the entries under probesDir other than self, sorted:
// every directory, and every symlink (fail closed); a regular file is no run, and a missing
// probesDir holds none.
func unreconciledRuns(probesDir, self string) ([]string, error) {
	des, err := os.ReadDir(probesDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, de := range des {
		if de.Name() != self && (de.IsDir() || de.Type()&fs.ModeSymlink != 0) {
			out = append(out, de.Name())
		}
	}
	slices.Sort(out)
	return out, nil
}

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

// recoveryCopy is a probe run's baseline recovery copy, <root>/<rel> with rel
// accounts/<account>/state/baselines/<run-id>.json (data-model *Probe baseline*).
type recoveryCopy struct{ root, rel string }

func (c *recoveryCopy) path() string { return filepath.Join(c.root, c.rel) }

// recovery is the run's baseline recovery copy.
func (p Probe) recovery() (*recoveryCopy, error) {
	if !RunIDPattern.MatchString(p.Run.ID) {
		return nil, fmt.Errorf("run id %q is not YYYYMMDDThhmmssZ-<4 hex>", p.Run.ID)
	}
	if _, err := AccountDir(p.ConfigRoot, p.Account); err != nil {
		return nil, err
	}
	return &recoveryCopy{root: p.ConfigRoot, rel: filepath.Join("accounts", p.Account, "state", "baselines", p.Run.ID+".json")}, nil
}

// runner is the probe's Runner for one run or cleanup: the passphrase (none for a stateless
// cleanup) in its credentials, the state path and run id in its variables, the check, the
// project its Sweep gets, the companion stage and the baseline's recovery copy.
func (p Probe) runner(phrase, state string, cleanup bool) (Runner, error) {
	_, _, _, root, _ := p.files()
	r := p.Run
	r.Creds = maps.Clone(r.Creds)
	if r.Creds == nil {
		r.Creds = map[string]string{}
	}
	if phrase != "" {
		r.Creds["TF_VAR_state_passphrase"] = phrase // a secret: redacted, environment only
	}
	r.Vars = maps.Clone(r.Vars)
	if r.Vars == nil {
		r.Vars = map[string]string{}
	}
	r.Vars["TF_VAR_state_path"] = state
	r.Vars["TF_VAR_run_id"] = r.ID
	r.cleanupOnly = cleanup
	r.check = p.Check
	var err error
	if r.baseline, err = p.recovery(); err != nil {
		return Runner{}, err
	}
	if p.Check != nil && p.Check.Sweep != nil {
		// Sweep gets the project probe.env records, never the caller's.
		if r.sweepProject, err = recordedProject(root, r.ID); err != nil {
			return Runner{}, err
		}
	}
	r.companion, err = p.companion()
	return r, err
}

// companion is the probe root's companion stage, when the root has a `companion/` directory: it
// runs after a full apply of the root (never in a plan-only run: Execute), and is destroyed first
// on exit and on `--cleanup`.
func (p Probe) companion() (*companionStage, error) {
	cdir := filepath.Join(p.Run.Stacks[0].Dir, "companion")
	if fi, err := os.Stat(cdir); err != nil || !fi.IsDir() {
		return nil, nil
	}
	dir, _, _, _, _ := p.files()
	rel, err := filepath.Rel(p.ConfigRoot, dir)
	if err != nil {
		return nil, err
	}
	st := p.Run.Stacks[0]
	return &companionStage{stage: st, stack: Stack{ID: st.ID + "-companion", Dir: cdir, Ephemeral: true},
		configRoot: p.ConfigRoot, rel: filepath.Join(rel, "companion.env"), bind: p.Bind}, nil
}

func (p Probe) run(ctx context.Context, phrase string, cleanup bool) error {
	dir, state, pass, root, err := p.files()
	if err != nil {
		return err
	}
	r, err := p.runner(phrase, state, cleanup)
	if err != nil {
		return err
	}
	if err := r.Execute(ctx); err != nil {
		if _, serr := os.Lstat(state); errors.Is(serr, os.ErrNotExist) && !isUnreconciled(err) && !cleanup {
			// A start that wrote no state and whose sweep and leftover check passed (or that
			// ended at or before its baseline, before any create): nothing to destroy, so no
			// cleanup can use the passphrase; the run's files and the baseline's recovery copy
			// go whatever the outcome. A failed Sweep or leftover check keeps them, also without
			// state (sweep.go, spec 007 FR-007). A failed cleanup always keeps them: it may have
			// ended before its sweep and leftover check ran.
			_ = retire(dir, root, pass, r.baseline.path())
		}
		return err
	}
	return retire(dir, root, state, state+".backup", pass, r.baseline.path())
}

// retire removes a reconciled run's files, probe.env (root, which a cleanup starts from) last, and
// then its directory: a crash part-way leaves probe.env, so `--cleanup` can still finish the run.
// The run's own `.write-*` temporaries (a kill inside WriteCredentialFile) go with its files; any
// other entry refuses before anything is removed, so the run never leaves a directory that blocks
// every start and that `--cleanup` cannot reach (spec 007 T027).
func retire(dir, root string, files ...string) error {
	temps, stray, err := runDirEntries(dir, append([]string{root}, files...)...)
	if err != nil {
		return err
	}
	if len(stray) > 0 {
		return fmt.Errorf("run directory %s holds %s, which the run did not write: not retired, probe.env kept; check and remove it by hand, then run the cleanup again",
			dir, strings.Join(stray, ", "))
	}
	if err := removeFiles(slices.Concat(files, temps)...); err != nil {
		return err
	}
	if err := removeFiles(root); err != nil {
		return err
	}
	return removeEmptyDir(dir)
}

// runDirEntries reads a run directory (spec 007 T027): temps are its WriteCredentialFile
// temporaries (`.write-*` regular files, full paths), stray the names of its other entries that
// are none of known (paths; those outside dir are ignored). A missing directory holds nothing.
func runDirEntries(dir string, known ...string) (temps, stray []string, err error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	names := map[string]bool{}
	for _, k := range known {
		if filepath.Dir(k) == filepath.Clean(dir) {
			names[filepath.Base(k)] = true
		}
	}
	for _, e := range entries {
		switch {
		case names[e.Name()]:
		case strings.HasPrefix(e.Name(), ".write-") && e.Type().IsRegular():
			temps = append(temps, filepath.Join(dir, e.Name()))
		default:
			stray = append(stray, e.Name())
		}
	}
	return temps, stray, nil
}

// Start refuses to run beside another unreconciled probe run, then runs the start-up phases in
// order, each written and synced before the next (spec 007 FR-006): probe.env (the root and the
// project), the passphrase, the baseline (Execute, before the first apply); it runs the probe and
// deletes the probe's files and the baseline's recovery copy only after destroy, the sweep and the
// leftover check passed.
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
	rel, err := filepath.Rel(p.ConfigRoot, dir)
	if err != nil {
		return err
	}
	// An earlier run still unreconciled may have left a nameless resource this run's baseline
	// would take in as the project's own (FR-006): refused before any file of this run. The run's
	// own directory is claimed first (created exclusively, synced), so of two starts at once each
	// sees the other and both are refused; a refused start removes its empty directory again.
	if err := claimDir(p.ConfigRoot, rel); err != nil {
		return err
	}
	if err := refuseUnreconciled(filepath.Dir(dir), p.Run.ID); err != nil {
		_ = removeEmptyDir(dir)
		return err
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	phrase := hex.EncodeToString(b)
	if err := WriteCredentialFile(p.ConfigRoot, filepath.Join(rel, "probe.env"), map[string]string{probeRootKey: p.Run.Stacks[0].Dir, probeProjectKey: p.Run.Vars["TF_VAR_project_id"]}); err != nil {
		return err
	}
	if err := WriteCredentialFile(p.ConfigRoot, filepath.Join(rel, "passphrase.env"), map[string]string{"TF_VAR_state_passphrase": phrase}); err != nil {
		return err
	}
	return p.run(ctx, phrase, false)
}

// refuseUnreconciled refuses a probe start while another run's directory exists under probesDir
// (CondUnreconciledRun), naming each such run and the cleanup that finishes the first.
func refuseUnreconciled(probesDir, self string) error {
	others, err := unreconciledRuns(probesDir, self)
	if err != nil {
		return err
	}
	if len(others) > 0 {
		return refuse(CondUnreconciledRun, "probe run %s is unreconciled (its directory under state/probes/ exists); finish it first: task live:probe -- --reviewed-sha <sha> --cleanup %s (a directory without probe.env cannot be cleaned up: the owner checks the project and removes it by hand)",
			strings.Join(others, ", "), others[0])
	}
	return nil
}

// Cleanup is `lz-live probe --cleanup <run-id>` from a fresh process. It classes the run
// (ClassifyRun, data-model *Start-up phases and run classes*): an untrusted run is refused before
// anything is read or deleted, and its files stay, unless the owner accepted its lost baseline
// (AcceptLostBaseline); a run without state (pre-create, or
// baseline-backed whose create left no state) runs no tofu and needs no passphrase; otherwise it
// reads the retained passphrase and destroys the probe from its retained state (planning and
// applying nothing). Then the check's Sweep and the leftover check run, with a baseline-backed
// run's baseline as Before; the probe's files and the recovery copy go only when all passed.
func (p Probe) Cleanup(ctx context.Context) error {
	_, state, pass, record, err := p.files()
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
	if err := p.classify(state, project); err != nil {
		return err
	}
	if p.Run.stateless {
		return p.run(ctx, "", true)
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

// classify classes the run a cleanup finishes: untrusted is refused (nothing deleted, the files
// kept: FR-007's owner recovery) unless its lost baseline was accepted (admit); a baseline-backed run's leftover check gets its baseline as
// Before; a run without state is stateless. A state file that cannot be examined counts as state,
// and an unreadable inventory refuses the cleanup.
func (p *Probe) classify(state, project string) error {
	rc, err := p.recovery()
	if err != nil {
		return err
	}
	b, loadErr := LoadBaseline(p.Run.Dir, rc.path(), p.Run.ID, project)
	_, serr := os.Lstat(state)
	hasState := !errors.Is(serr, fs.ErrNotExist)
	inv, err := ReadInventory(p.Run.Dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	class := ClassifyRun(hasState, len(inv) > 0, b, loadErr)
	if err := p.admit(class, loadErr); err != nil {
		return err
	}
	if class == RunBaselineBacked {
		p.Run.Leftovers.Before, p.Run.Leftovers.beforeProject = b.Listed, project
	}
	p.Run.stateless = !hasState || p.AcceptLostBaseline && p.AcceptDeletesNothing
	return nil
}

// admit refuses an untrusted run, unless the owner accepted its lost baseline; that acceptance is
// refused for any other class, whose plain --cleanup finishes it with its baseline.
func (p *Probe) admit(class RunClass, loadErr error) error {
	switch {
	case p.AcceptLostBaseline && class != RunUntrusted:
		return refuse(CondAcceptLostBaseline, "run %s is %s, not untrusted: nothing deleted; finish it with --cleanup %s alone", p.Run.ID, class, p.Run.ID)
	case class == RunUntrusted && !p.AcceptLostBaseline:
		return refuse(string(RunUntrusted), "cleanup of run %s: %v; nothing deleted and its files kept: owner recovery, then --cleanup %s --accept-lost-baseline",
			p.Run.ID, loadErr, p.Run.ID)
	}
	return nil
}
