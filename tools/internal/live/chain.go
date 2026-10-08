package live

// `lz-live chain -- <instance|all>` and `lz-live destroy -- <instance>` (FR-009, FR-010, FR-011,
// SC-005, research R12, R21; tests T046 in chain_test.go, implementation T047).
//
// The chain is the plan/apply lane's apply of the selected set in run order (apply.go), holding the
// run locks of the selected set and the target's ephemeral stacks over every call. Before the first
// apply it binds the sandbox admin credential (sandbox.env) to the account through GET
// /auth/details and records the run's listings (R12) as a redacted baseline in the run directory
// (baseline.json, listings-before/<kind>.json); a baseline listing with errors ends the run there,
// blocked (exit 2), since its final check could only fail (T091). From then on a destroy-on-exit that also fires on
// an apply failure, SIGINT, SIGTERM (SIGHUP as SIGINT) and the run deadline destroys the ephemeral
// stacks whose apply started (after a complete run every ephemeral stack of the target), in reverse
// run order, continuing after a refused or failed destroy; it has its own time, and signals that
// arrive once it has started are ignored. A retained instance is never destroyed: its stage refuses
// it before anything runs (G7). Each destroy is a saved `plan -destroy` whose `show -json` passes
// protect.go before exactly that file is applied; an ephemeral root holds no retained address, so
// there protect.go refuses only what it cannot read as a complete plan and a plan marked errored,
// not a retained deletion (the stage check is the retained guard). The inventory is appended as each apply_complete event arrives. Then the leftover
// check (leftovers.go) lists every kind with the admin credential, exempts what the retained
// instances' states hold (`tofu show -json`, nested modules included) and the admin client and
// policy by recorded id, marks each leftover the baseline record listed (read back from the run
// directory), and fails on any leftover or error; the run ends with summary.json, the summary line
// and the cost reminder (contracts/checks.md `lz-live`). `destroy -- <instance>` is refused
// (consumer-applied) while a stack consuming it has a record (T091); the destroy-on-exit has no
// such check, as it destroys consumers before their producers.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/stacks"
)

// ChainOptions configures one `lz-live chain` run: the lane's options (Verb is ignored) and the
// run core's deadline, signals and leftover listings.
type ChainOptions struct {
	ApplyOptions
	// Deadline bounds the applies (0: DefaultDeadline); the destroy-on-exit has its own time.
	Deadline time.Duration
	// Signals stop the run as SIGINT/SIGTERM do (nil: those of this process, SIGHUP included).
	Signals <-chan os.Signal
	// Lister serves the leftover check's listings (tests); nil lists through API with the sandbox
	// admin credential (sandbox.env), bound to the account before the first listing.
	Lister Lister
}

// CondConsumerApplied refuses `destroy -- <producer>` while a stack consuming it has a record
// (T091): destroying the producer would orphan the consumer's resources.
const CondConsumerApplied = "consumer-applied"

// baselineFile is the run's baseline record in the run directory.
const baselineFile = "baseline.json"

// baselineRecord is what baselineFile holds: by provider type the ids listed before the first
// apply, and the listing errors of that baseline.
type baselineRecord struct {
	RunID  string              `json:"run_id"`
	Listed map[string][]string `json:"listed"`
	Errors []string            `json:"errors"`
}

// Chain runs one chain; the error maps to the exit code through ExitCode.
func Chain(ctx context.Context, o ChainOptions) error {
	a := o.ApplyOptions
	a.Verb = VerbApply
	return runLane(ctx, a, &o)
}

func (r *laneRunner) stopSignal() os.Signal {
	if s, ok := r.stop.Load().(os.Signal); ok {
		return s
	}
	return syscall.SIGINT
}

// summary ends every chain run, whichever way it ends (runLane defers it, so the run locks are
// released first): summary.json in the run directory (data-model *Live run record*), then the
// summary line and the cost reminder. A summary.json that cannot be written fails the run.
func (r *laneRunner) summary(err error, deadline time.Duration) error {
	if deadline <= 0 {
		deadline = DefaultDeadline
	}
	outcome := "pass"
	if err != nil {
		outcome = "fail"
	}
	sum, _ := json.Marshal(map[string]any{"run_id": r.o.RunID, "outcome": outcome, "deadline": deadline.String(),
		"known_deviations": []string{}, "error": errString(err)})
	if r.o.RunDir == "" {
		err, outcome = errors.Join(err, errors.New("no run directory for summary.json")), "fail"
	} else if werr := writeRecord(filepath.Join(r.o.RunDir, "summary.json"), []byte(r.red.Redact(string(sum)))); werr != nil {
		err, outcome = errors.Join(err, werr), "fail"
	}
	fmt.Fprintf(r.term, "LZ-LIVE summary %s %s known-deviations=none\nrecord approximate cost for run %s in the PR\n", r.o.RunID, outcome, r.o.RunID)
	return err
}

// ephemeralOf returns the target's ephemeral stacks in run order.
func (r *laneRunner) ephemeralOf() ([]string, error) {
	m := r.o.Manifest
	order, err := stacks.Order(m)
	if err != nil {
		return nil, err
	}
	var eph []string
	for _, id := range order {
		_, st, err := stageOf(m, id)
		if err != nil {
			return nil, err
		}
		if st.Chain == "ephemeral" && (r.o.Target == TargetAll || r.o.Target == id) {
			eph = append(eph, id)
		}
	}
	return eph, nil
}

func (r *laneRunner) chain(ctx context.Context, c *ChainOptions, ids []string) (err error) {
	o, m := r.o, r.o.Manifest
	eph, err := r.ephemeralOf()
	if err != nil {
		return err
	}
	lockIDs := slices.Clone(ids)
	for _, id := range eph {
		if !slices.Contains(lockIDs, id) {
			lockIDs = append(lockIDs, id)
		}
	}
	release, err := stacks.HoldRun(o.Locks, m, lockIDs)
	if errors.Is(err, stacks.ErrLocked) {
		return refuse(CondLocked, "%v", err)
	}
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, release()) }()
	// Selection again under the locks, as apply does.
	again, _, err := r.acted()
	if err != nil {
		return err
	}
	if !slices.Equal(again, ids) {
		return refuse(CondLocked, "the selection changed while the run locks were taken (%v, now %v): another run applied meanwhile; run again", ids, again)
	}
	check, err := r.leftoverCheck(ctx, c)
	if err != nil {
		return err
	}
	if r.home, err = scratchHome(); err != nil {
		return err
	}
	defer removeTree(r.home)
	if r.inv, err = OpenInventory(o.RunDir, r.red); err != nil {
		return err
	}
	deadline := c.Deadline
	if deadline <= 0 {
		deadline = DefaultDeadline
	}
	// The destroy-on-exit is registered here, before the baseline and the first apply: every way
	// out of the loop below (failure, refusal, signal, deadline) reaches the destroy.
	runCtx, cancelTimeout := context.WithTimeoutCause(ctx, deadline, errDeadline)
	defer cancelTimeout()
	runCtx, cancel := context.WithCancelCause(runCtx)
	defer cancel(nil)
	sigs := c.Signals
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
				r.stop.Store(os.Signal(syscall.SIGINT))
			} else {
				r.stop.Store(sig)
			}
			cancel(fmt.Errorf("interrupted by %v", sig))
		case <-runCtx.Done():
		}
	}()

	runErr := r.baseline(runCtx, check)
	var blocked *Blocked
	if errors.As(runErr, &blocked) {
		// Nothing applied: nothing to destroy, nothing to reconcile (T091).
		return runErr
	}
	if runErr == nil {
		for _, id := range ids {
			if runCtx.Err() != nil {
				break
			}
			fmt.Fprintf(r.term, "LZ-LIVE chain %s start\n", id)
			if runErr = r.stack(runCtx, id); runErr != nil {
				fmt.Fprintf(r.term, "LZ-LIVE chain %s fail\n", id)
				break
			}
			fmt.Fprintf(r.term, "LZ-LIVE chain %s ok\n", id)
		}
	}
	if runCtx.Err() != nil {
		runErr = errors.Join(context.Cause(runCtx), runErr)
	}

	// Destroy-on-exit, with its own time: the run's context may have expired.
	dctx, dcancel := context.WithTimeout(context.WithoutCancel(ctx), deadline)
	defer dcancel()
	var destroyErr error
	for i := len(eph) - 1; i >= 0; i-- {
		id := eph[i]
		if runErr != nil && !slices.Contains(r.started, id) {
			continue
		}
		fmt.Fprintf(r.term, "LZ-LIVE destroy %s start\n", id)
		if e := r.destroyStack(dctx, id); e != nil {
			fmt.Fprintf(r.term, "LZ-LIVE destroy %s fail\n", id)
			destroyErr = errors.Join(destroyErr, e)
			continue
		}
		fmt.Fprintf(r.term, "LZ-LIVE destroy %s ok\n", id)
	}

	rep := r.leftovers(dctx, check)
	var recErr error
	for typ, items := range rep.Listings {
		raw, e := json.Marshal(items)
		if e == nil {
			e = writeRecord(filepath.Join(o.RunDir, "listings", typ+".json"), []byte(r.red.Redact(string(raw))))
		}
		recErr = errors.Join(recErr, e)
	}
	if raw, e := json.Marshal(rep); e != nil {
		recErr = errors.Join(recErr, e)
	} else {
		recErr = errors.Join(recErr, writeRecord(filepath.Join(o.RunDir, "leftovers.json"), []byte(r.red.Redact(string(raw)))))
	}
	var leftErr error
	if rep.Outcome != "pass" {
		leftErr = fmt.Errorf("leftover check: %s (%d leftovers, %d errors)", rep.Outcome, len(rep.Leftovers), len(rep.Errors))
	}
	// summary.json and the summary line follow in runLane, after the locks are released.
	return errors.Join(runErr, destroyErr, leftErr, recErr)
}

// leftoverCheck is the check of this run without its states and baseline: the listings through the
// Lister seam, or through the API with the sandbox admin credential, bound to the account first
// (coordinator decision 1, T047); the account's projects; the slice prefix; the admin exemption.
func (r *laneRunner) leftoverCheck(ctx context.Context, c *ChainOptions) (LeftoverCheck, error) {
	o := r.o
	check := LeftoverCheck{Lister: c.Lister, Prefix: o.Manifest.Org + "-", RunID: o.RunID, API: o.API}
	var err error
	if check.Projects, err = r.projects(); err != nil {
		return check, err
	}
	if c.Lister == nil {
		sb, err := ReadCredentialFile(filepath.Join(o.ConfigRoot, "sandbox.env"))
		if errors.Is(err, fs.ErrNotExist) {
			return check, &Blocked{Phase: "credentials", Detail: "sandbox.env does not exist; bootstrap:account writes it"}
		}
		if err != nil {
			return check, err
		}
		check.Cred = Credential{Endpoint: sb["OVH_ENDPOINT"], ClientID: sb["OVH_CLIENT_ID"], ClientSecret: sb["OVH_CLIENT_SECRET"]}
		r.red.Add(check.Cred.ClientSecret)
		if check.Cred.Endpoint == "" || check.Cred.ClientID == "" || check.Cred.ClientSecret == "" {
			return check, refuse(CondCredentials, "sandbox.env: OVH_ENDPOINT, OVH_CLIENT_ID and OVH_CLIENT_SECRET are required")
		}
		if err := r.bind(ctx, map[string]string{"OVH_ENDPOINT": check.Cred.Endpoint, "OVH_CLIENT_ID": check.Cred.ClientID,
			"OVH_CLIENT_SECRET": check.Cred.ClientSecret}); err != nil {
			return check, err
		}
	}
	return check, nil
}

// baseline lists every kind before the first apply (R12) and persists it, redacted, in the run
// directory: the raw listings under listings-before/ and the listed ids with the listing errors
// in baseline.json, which the final reconciliation reads back. A record that cannot be written
// stops the run before any apply; a listing with errors (an HTTP error, an unparseable listing)
// blocks it there once the record is written (exit 2, T091): the final check would fail on it, so
// the run could only fail.
func (r *laneRunner) baseline(ctx context.Context, check LeftoverCheck) error {
	o := r.o
	rep := check.Check(ctx, nil)
	for typ, items := range rep.Listings {
		raw, err := json.Marshal(items)
		if err != nil {
			return err
		}
		if err := writeRecord(filepath.Join(o.RunDir, "listings-before", typ+".json"), []byte(r.red.Redact(string(raw)))); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(baselineRecord{RunID: o.RunID, Listed: rep.Listed, Errors: rep.Errors})
	if err != nil {
		return err
	}
	if err := writeRecord(filepath.Join(o.RunDir, baselineFile), []byte(r.red.Redact(string(raw)))); err != nil {
		return err
	}
	// A listing cut short by a signal or the deadline is the run's interruption, not a blocked run:
	// the loop sees the cause and nothing is applied either way.
	if len(rep.Errors) > 0 && ctx.Err() == nil {
		return &Blocked{Phase: "baseline", Detail: fmt.Sprintf("the baseline listing before the first apply failed (%d errors, see %s); nothing applied",
			len(rep.Errors), baselineFile)}
	}
	return nil
}

// readBaseline reads the run's baseline record back.
func readBaseline(runDir, runID string) (baselineRecord, error) {
	var b baselineRecord
	raw, err := os.ReadFile(filepath.Join(runDir, baselineFile))
	if err != nil {
		return b, err
	}
	if err := json.Unmarshal(raw, &b); err != nil {
		return b, errors.New("does not decode")
	}
	if b.RunID != runID || b.Listed == nil {
		return b, fmt.Errorf("is not this run's (run %q)", b.RunID)
	}
	return b, nil
}

// destroyOne is `destroy -- <ephemeral instance>`: the run locks of that instance and of every
// stack consuming it (a data or an authority edge), then, refused while any such consumer has a
// record (T091: its resources would be orphaned; destroy it first), destroyStack. The chain's
// destroy-on-exit calls destroyStack directly: it destroys consumers before their producers.
func (r *laneRunner) destroyOne(ctx context.Context) (err error) {
	o, m := r.o, r.o.Manifest
	var consumers []string
	for _, in := range m.Instances {
		if slices.ContainsFunc(in.Edges, func(e stacks.Edge) bool { return e.Producer == o.Target }) {
			consumers = append(consumers, in.ID)
		}
	}
	release, err := stacks.HoldRun(o.Locks, m, append([]string{o.Target}, consumers...))
	if errors.Is(err, stacks.ErrLocked) {
		return refuse(CondLocked, "%v", err)
	}
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, release()) }()
	records, err := stacks.ReadRecords(o.Records, m)
	if err != nil {
		return err
	}
	var applied []string
	for _, id := range consumers {
		if _, ok := records[id]; ok {
			applied = append(applied, id)
		}
	}
	if len(applied) > 0 {
		return refuse(CondConsumerApplied, "destroy -- %s would orphan its applied consumers %s; destroy them first", o.Target, strings.Join(applied, ", "))
	}
	if r.home, err = scratchHome(); err != nil {
		return err
	}
	defer removeTree(r.home)
	fmt.Fprintf(r.term, "LZ-LIVE destroy %s start\n", o.Target)
	if err := r.destroyStack(ctx, o.Target); err != nil {
		fmt.Fprintf(r.term, "LZ-LIVE destroy %s fail\n", o.Target)
		return err
	}
	fmt.Fprintf(r.term, "LZ-LIVE destroy %s ok\n", o.Target)
	return nil
}

// destroyStack destroys one ephemeral stack with its authority's credential, bound first: init,
// `plan -destroy` to a file in the scratch HOME (it holds the root's variable values) with the
// stack's consumed inputs, `show -json` of that file through protect.go, exactly that file applied;
// then its record is removed, so the next selection picks it again. A retained instance is refused
// before anything runs; that refusal, not protect.go (no retained set applies to an ephemeral
// root), is what keeps a retained instance from being destroyed.
func (r *laneRunner) destroyStack(ctx context.Context, id string) (err error) {
	o, m := r.o, r.o.Manifest
	in, st, err := stageOf(m, id)
	if err != nil {
		return err
	}
	if st.Chain != "ephemeral" {
		return refuse(CondRetained, "destroy refuses retained instance %s", id)
	}
	child, dir, store, err := r.prepare(ctx, in)
	if err != nil {
		return err
	}
	inputs, err := stacks.Adapt(stacks.AdaptOptions{Manifest: m, Consumer: id, Store: store, Schemas: o.Schemas, Account: r.account,
		Dir: filepath.Join(o.RunDir, "inputs-destroy", id)})
	if errors.Is(err, stacks.ErrBlocked) {
		return &Blocked{Phase: "inputs", Detail: fmt.Sprintf("%s: %v", id, err)}
	}
	if err != nil {
		return err
	}
	if err := r.tofu(ctx, child, id, dir, r.term, initArgs...); err != nil {
		return err
	}
	plan := filepath.Join(r.home, "destroy-"+id+".tfplan")
	defer func() { err = errors.Join(err, removeFiles(plan)) }()
	args := []string{"plan", "-destroy", "-input=false", "-out=" + plan}
	for _, f := range inputs.Files {
		args = append(args, "-var-file="+f)
	}
	if err := r.tofu(ctx, child, id, dir, r.term, args...); err != nil {
		return err
	}
	var js bytes.Buffer
	if err := r.tofu(ctx, child, id, dir, &js, "show", "-json", plan); err != nil {
		return err
	}
	if err := Protect(js.Bytes(), nil); err != nil {
		return err
	}
	applied := &dropOutputs{w: r.term}
	err = r.tofu(ctx, child, id, dir, applied, "apply", "-json", "-input=false", plan)
	applied.flush()
	if err != nil {
		return err
	}
	return RemoveRecord(o.Records, id)
}

// prepare loads and binds a stack's credential and returns its child environment, root directory
// and state store.
func (r *laneRunner) prepare(ctx context.Context, in stacks.Instance) (*childEnv, string, ObjectStore, error) {
	o := r.o
	_, creds, err := LoadCredentials(o.ConfigRoot, o.Account, o.Manifest, in.ID)
	if err != nil {
		return nil, "", nil, err
	}
	r.red.Add(creds["OVH_CLIENT_SECRET"], creds["AWS_SECRET_ACCESS_KEY"])
	if err := r.bind(ctx, creds); err != nil {
		return nil, "", nil, err
	}
	store, err := o.Store(s3Of(creds))
	if err != nil {
		return nil, "", nil, err
	}
	vars := map[string]string{"TF_VAR_state_passphrase": r.pass}
	for k, v := range creds {
		vars[k] = v
	}
	if in.Stage == "bootstrap" {
		// account-bootstrap's local backend lives in the bound account's directory
		// (stacks/account/bootstrap/_lz_backend.tf; as bootstrap_state.go passes it).
		dir, err := AccountDir(o.ConfigRoot, o.Account)
		if err != nil {
			return nil, "", nil, err
		}
		vars["TF_VAR_lz_account_dir"] = dir
	}
	return &childEnv{home: r.home, vars: vars, grace: DefaultGrace}, filepath.Join(o.Checkout, filepath.FromSlash(in.Path)), store, nil
}

// stateIDAttr is, per provider type, the state attribute holding the id the listing reports
// (provider docs, kb mirror terraform-provider-ovh/docs/resources; evidence/T046.md reading 5).
// ovh_iam_resource_tags holds the listed tag keys in `tags`.
var stateIDAttr = map[string]string{tStorage: "name", tUser: "id", tCred: "access_key_id", tS3Pol: "user_id", tClient: "client_id",
	tPolicy: "id", tGroup: "name", tAlert: "id", tNetwork: "id", tSubnet: "id"}

// retainedIDs reads a retained instance's state (`tofu show -json`, nested modules included) into
// provider type -> listing ids.
func (r *laneRunner) retainedIDs(ctx context.Context, in stacks.Instance, into map[string][]string) error {
	child, dir, _, err := r.prepare(ctx, in)
	if err != nil {
		return err
	}
	if err := r.tofu(ctx, child, in.ID, dir, r.term, initArgs...); err != nil {
		return err
	}
	var js bytes.Buffer
	if err := r.tofu(ctx, child, in.ID, dir, &js, "show", "-json"); err != nil {
		return err
	}
	type res struct {
		Type   string         `json:"type"`
		Values map[string]any `json:"values"`
	}
	type mod struct {
		Resources []res  `json:"resources"`
		Children  []*mod `json:"child_modules"`
	}
	var doc struct {
		Values *struct {
			Root mod `json:"root_module"`
		} `json:"values"`
	}
	if err := json.Unmarshal(js.Bytes(), &doc); err != nil {
		return fmt.Errorf("state of %s does not decode", in.ID)
	}
	if doc.Values == nil {
		return nil // an empty state
	}
	var walk func(m *mod)
	walk = func(m *mod) {
		for _, rs := range m.Resources {
			if rs.Type == tTags {
				if tags, ok := rs.Values["tags"].(map[string]any); ok {
					for k := range tags {
						into[tTags] = append(into[tTags], k)
					}
				}
				continue
			}
			if a, ok := stateIDAttr[rs.Type]; ok {
				if v, ok := rs.Values[a].(string); ok {
					into[rs.Type] = append(into[rs.Type], v)
				}
			}
		}
		for _, c := range m.Children {
			walk(c)
		}
	}
	walk(&doc.Values.Root)
	return nil
}

// leftovers is the final reconciliation: the retained instances' states, the admin exemption, the
// inventory and the baseline record read back from the run directory. A state, exemption,
// inventory or baseline that cannot be read, or a baseline listing that failed, is an error of the
// report (fail closed).
func (r *laneRunner) leftovers(ctx context.Context, check LeftoverCheck) Report {
	o, m := r.o, r.o.Manifest
	check.Retained = map[string][]string{}
	var errs []string
	for _, in := range m.Instances {
		_, st, err := stageOf(m, in.ID)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		if st.Chain != "retained" {
			continue
		}
		if err := r.retainedIDs(ctx, in, check.Retained); err != nil {
			errs = append(errs, fmt.Sprintf("state of %s: %s", in.ID, r.red.Redact(err.Error())))
		}
	}
	var err error
	if check.Exempt, err = LoadExemption(o.ConfigRoot, o.Account); err != nil {
		errs = append(errs, fmt.Sprintf("admin exemption: %v", err))
	}
	entries, err := ReadInventory(o.RunDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		errs = append(errs, fmt.Sprintf("inventory: %v", err))
	}
	rep := check.Check(ctx, entries)
	// The final listing against the baseline record as the run directory holds it now.
	b, err := readBaseline(o.RunDir, o.RunID)
	if err != nil {
		errs = append(errs, fmt.Sprintf("baseline record %s %v", baselineFile, err))
	} else if len(b.Errors) > 0 {
		errs = append(errs, fmt.Sprintf("baseline listing before the first apply failed (%d errors, see %s)", len(b.Errors), baselineFile))
	}
	for i, l := range rep.Leftovers {
		rep.Leftovers[i].Before = slices.Contains(b.Listed[l.Type], l.ID)
	}
	if len(errs) > 0 {
		rep.Errors = append(rep.Errors, errs...)
		rep.Outcome = "fail"
	}
	return rep
}

// projects are the account's distinct projects (account.env LZ_PROJECT_ID_*) with their URNs.
func (r *laneRunner) projects() ([]Project, error) {
	refs := make([]string, 0, len(r.account.ProjectIDs))
	for ref := range r.account.ProjectIDs {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	var out []Project
	for _, ref := range refs {
		b, err := r.account.Binding(ref)
		if err != nil {
			return nil, err
		}
		if !slices.ContainsFunc(out, func(p Project) bool { return p.ID == b.ProjectID }) {
			out = append(out, Project{ID: b.ProjectID, URN: b.ProjectURN})
		}
	}
	return out, nil
}
