package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/live"
	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/probes/live/alerting"
	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/probes/live/identity"
	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/probes/live/state"
	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/probes/live/storageiam"
)

// probeCheck is a probe root's check after its apply (live.ProbeCheck), run on a start and on a
// cleanup; nil for a root without one. T096: the state-backend probe observes its S3 user's Deny
// statement and undoes its writes. T100: the storage-iam probe observes what its probe identities
// may do (P25 reads, P26 GET /me, P9 IAM write) and the companion's state object (P3), binding each
// identity like the companion's (bind), and undoes a write with the sandbox admin's credential.
// Spec 003 T030: the swift-reach probe observes the tenant users' reach through Keystone and Swift
// (its client is the S3 one: Keystone, Swift and S3 hosts) and sweeps with the sandbox admin.
// Spec 003 T032: the token-residual probe creates, deletes and sweeps its OAuth2 client and policy
// with the sandbox admin and binds nothing (its claim is about exactly one token of that client).
// Spec 003 T034: the iam-urn probe creates its two identities and policies and writes and sweeps
// its tag key with the sandbox admin, and binds nothing (each identity is the admin's own client).
// Spec 003 T036: the locked-recovery probe runs the pinned tofu on its backend root through
// recoverySeam (tofu: its run method; unused by every other root) and sweeps with the sandbox admin.
// Spec 007 T015: the budget-alert probe reads its alert back in the run's project and sweeps with
// the sandbox admin against the run's baseline, which it finds through paths (the record
// directory and the account's state directory).
func probeCheck(root string, d deps, api live.API, admin live.Credential, bind func(context.Context, live.Credential) error, tofu func(context.Context, map[string]string, bool, ...string) (state.TofuRun, error), project string, paths runPaths) *live.ProbeCheck {
	switch root {
	case "budget-alert":
		return alerting.Check(alerting.Options{API: api, Admin: admin, RunDir: paths.RunDir, StateDir: paths.StateDir, Project: project})
	case "state-backend":
		return state.Check(state.Options{HTTP: d.S3HTTP})
	case "storage-iam":
		return storageiam.Check(storageiam.Options{API: api, Admin: admin, Bind: bind, HTTP: d.S3HTTP})
	case "swift-reach":
		return storageiam.SwiftReach(storageiam.SwiftOptions{HTTP: d.S3HTTP, API: api, Admin: admin})
	case "token-residual":
		return identity.TokenResidual(identity.Options{API: api, Admin: admin})
	case "iam-urn":
		return identity.IamURN(identity.Options{API: api, Admin: admin})
	case "locked-recovery":
		return state.LockedRecovery(state.RecoveryOptions{HTTP: d.S3HTTP, Tofu: tofu, API: api, Admin: admin, Project: project})
	}
	return nil
}

// runPaths are where a probe run's files are: RunDir(<run-id>) is its record directory
// (<checkout>/.local/live/<run-id>/), StateDir the account's state directory
// (accounts/<account>/state, which holds probes/<run-id>/ and baselines/<run-id>.json).
type runPaths struct {
	RunDir   func(runID string) string
	StateDir string
}

// baselineSweepRoots are the roots whose objects only the run's baseline tells apart (FR-007):
// under --accept-lost-baseline there is no baseline to judge by and the owner has removed the
// run's alerts by hand, so their recovery deletes nothing: no Sweep and no destroy, even from
// state. Every other root is destroyed from its state and runs its Sweep (run-derived names).
var baselineSweepRoots = map[string]bool{"budget-alert": true}

// acceptedCheck is the check a --cleanup --accept-lost-baseline runs: c without its Sweep for a
// root in baselineSweepRoots, else c.
func acceptedCheck(root string, c *live.ProbeCheck) *live.ProbeCheck {
	if c == nil || !baselineSweepRoots[root] {
		return c
	}
	kept := *c
	kept.Sweep = nil
	return &kept
}

// acceptLostBaseline is FR-007's owner recovery gate: the owner, at the controlling terminal
// (never a pipe, an argument or the environment), types the run id back. No terminal, a failed
// read or another line refuses before any credential is read.
func acceptLostBaseline(ctx context.Context, d deps, runID string) error {
	refused := func(format string, a ...any) error {
		return &live.Refusal{Condition: live.CondAcceptLostBaseline, Detail: fmt.Sprintf(format, a...) + "; nothing deleted, the run's files kept"}
	}
	if d.Terminal == nil {
		return refused("--accept-lost-baseline needs the owner at a terminal")
	}
	term, err := d.Terminal(ctx)
	if err != nil {
		return refused("--accept-lost-baseline needs the owner at a terminal (%v)", err)
	}
	line, err := term.ReadLine("Owner recovery of probe run " + runID + " without a trustworthy baseline (FR-007): only after you checked the project and removed by hand what the run created that its state does not hold; for budget-alert, nothing is destroyed: remove every alert matching the root's plan created at or after the run's start, the one in its state included.\nType the run id to continue: ")
	if err != nil {
		return refused("no line read from the terminal (%v)", err)
	}
	if line != runID {
		return refused("the line typed back is not the run id: %s; expected the run id %s", readSummary(line), runID)
	}
	return nil
}

// readSummary says what a refused line was without showing it (the owner may have typed or pasted
// a secret): its length, or that it was empty, and whether it held control characters (an arrow
// key, say).
func readSummary(line string) string {
	if line == "" {
		return "read an empty line"
	}
	s := fmt.Sprintf("read %d characters", utf8.RuneCountInString(line))
	if strings.ContainsFunc(line, unicode.IsControl) {
		s += ", with control characters"
	}
	return s
}

// `lz-live probe` (spec 005 FR-011, research R12 *Probe state*, contracts/checks.md): after the
// host guard admitted the run, a probe root under tests/live/probes/ of the reviewed checkout runs
// through the run core with the sandbox admin credential (~/.config/ovh-lz/sandbox.env), bound to
// its account (GET /auth/details against accounts/<account>/account.env). Its state is local and
// encrypted (TF_VAR_state_path, TF_VAR_state_passphrase) under
// accounts/<account>/state/probes/<run-id>/; the run record is .local/live/<run-id>/ in the
// checkout. The root gets its run id (TF_VAR_run_id) and the sandbox project, LZ_PROJECT_ID_STATE
// of account.env (TF_VAR_project_id), recorded in the run's probe.env. `--cleanup <run-id>`
// finishes a probe whose destroy or leftover check failed, in the project the run recorded. A
// TMPDIR inside the checkout (the children's scratch HOME and tofu data go under it) is refused
// first.

// probePrefix names what probes create; the leftover check matches it.
const probePrefix = "lzprobe-"

// probeRoots is where probe roots live in the checkout.
var probeRoots = filepath.Join("tests", "live", "probes")

// planOnlyRoots run only with --plan-only (T008 run sheet): an apply of project-import would
// import the retained project into probe state, and every destroy would then fail on
// prevent_destroy; quota changes a project singleton whose destroy is UNVERIFIED. Matched by the
// name of the resolved root directory, so an alias or a trailing slash cannot pass.
var planOnlyRoots = map[string]bool{"project-import": true, "quota": true}

// condPlanOnly is the refusal of a plan-only root without --plan-only (exit 3).
const condPlanOnly = "plan-only"

type probeArgs struct {
	root          string
	planOnly      bool
	deadline      time.Duration
	deadlineGiven bool
	cleanup       string // run id
	// acceptLost: --accept-lost-baseline, with --cleanup only (FR-007 owner recovery).
	acceptLost bool
}

// check completes p with the positional arguments: one probe root, or --cleanup <run-id>.
func (p probeArgs) check(positional []string) (probeArgs, error) {
	switch {
	case p.deadlineGiven && p.deadline <= 0:
		return p, errors.New("--deadline must be positive")
	case p.acceptLost && p.cleanup == "":
		return p, errors.New("--accept-lost-baseline goes with --cleanup <run-id> only")
	case p.cleanup != "":
		if len(positional) != 0 || p.planOnly || !live.RunIDPattern.MatchString(p.cleanup) {
			return p, errors.New("--cleanup takes a run id and no probe root or --plan-only")
		}
	case len(positional) != 1:
		return p, errors.New("one probe root")
	default:
		p.root = positional[0]
	}
	return p, nil
}

// endpointAPI is the OVHcloud API of an endpoint: the OAuth2 token endpoints from the OVHcloud
// guide "Authenticate to the API with a service account" (kb mirror, verified 2026-10-06).
func endpointAPI(endpoint string) (live.API, error) {
	switch endpoint {
	case "ovh-eu":
		return live.API{TokenURL: "https://www.ovh.com/auth/oauth2/token", BaseURL: "https://eu.api.ovh.com/v1"}, nil
	case "ovh-ca":
		return live.API{TokenURL: "https://ca.ovh.com/auth/oauth2/token", BaseURL: "https://ca.api.ovh.com/v1"}, nil
	}
	return live.API{}, fmt.Errorf("endpoint %q is not supported (ovh-eu, ovh-ca)", endpoint)
}

// projectURN is the IAM URN of a public cloud project (terraform-provider-ovh docs,
// iam_resource_tags: urn:v1:eu:resource:publicCloudProject:<id>; the ca form from the OVHcloud
// guide on service accounts for OpenStack).
func projectURN(endpoint, id string) string {
	return "urn:v1:" + strings.TrimPrefix(endpoint, "ovh-") + ":resource:publicCloudProject:" + id
}

// probeRoot resolves root inside <checkout>/tests/live/probes/ (symlinks resolved), or refuses:
// a root elsewhere would run code the guard's reviewed-SHA check does not cover.
func probeRoot(checkout, root string) (string, error) {
	if !filepath.IsAbs(root) {
		root = filepath.Join(checkout, root)
	}
	base, err := filepath.EvalSymlinks(filepath.Join(checkout, probeRoots))
	if err != nil {
		return "", &live.Refusal{Condition: live.CondCheckout, Detail: "probe root: no " + probeRoots + " in the checkout"}
	}
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", &live.Refusal{Condition: live.CondCheckout, Detail: fmt.Sprintf("probe root %s does not exist", root)}
	}
	rel, err := filepath.Rel(base, real)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || strings.Contains(rel, string(filepath.Separator)) {
		return "", &live.Refusal{Condition: live.CondCheckout, Detail: fmt.Sprintf("probe root %s is not a directory directly under %s", root, probeRoots)}
	}
	return real, nil
}

// beforeCredentials runs a probe's refusals that need no credential, before any credential is
// read, any file written or any child started: a TMPDIR in the checkout, a start's root outside
// the probe roots or plan-only without --plan-only (it returns the resolved root), and the
// owner's confirmation of --accept-lost-baseline.
func beforeCredentials(ctx context.Context, d deps, checkout string, p probeArgs) (root string, err error) {
	// The children's scratch HOME and tofu data directories go under TMPDIR, which must not lie in
	// the reviewed checkout (.local/ included).
	if err := live.ScratchOutside(checkout); err != nil {
		return "", err
	}
	if p.cleanup != "" {
		if p.acceptLost {
			return "", acceptLostBaseline(ctx, d, p.cleanup)
		}
		return "", nil
	}
	if root, err = probeRoot(checkout, p.root); err != nil {
		return "", err
	}
	if planOnlyRoots[filepath.Base(root)] && !p.planOnly {
		return "", &live.Refusal{Condition: condPlanOnly, Detail: fmt.Sprintf("probe root %s runs only with --plan-only", filepath.Base(root))}
	}
	return root, nil
}

// probe runs or cleans up one probe after the guard admitted the run, with the resolved pinned
// OpenTofu (T093).
func probe(ctx context.Context, d deps, checkout, cfg, tofu string, p probeArgs) error {
	root, err := beforeCredentials(ctx, d, checkout, p)
	if err != nil {
		return err
	}
	sandbox, err := live.ReadCredentialFile(filepath.Join(cfg, "sandbox.env"))
	if err != nil {
		return fmt.Errorf("sandbox.env: %w", err)
	}
	cred := live.Credential{Endpoint: sandbox["OVH_ENDPOINT"], ClientID: sandbox["OVH_CLIENT_ID"], ClientSecret: sandbox["OVH_CLIENT_SECRET"]}
	if cred.Endpoint == "" || cred.ClientID == "" || cred.ClientSecret == "" {
		return errors.New("sandbox.env: OVH_ENDPOINT, OVH_CLIENT_ID and OVH_CLIENT_SECRET are required")
	}
	api, err := d.API(cred.Endpoint)
	if err != nil {
		return err
	}
	account, err := api.Account(ctx, cred)
	if err != nil {
		return err
	}
	accDir, err := live.AccountDir(cfg, account)
	if err != nil {
		return err
	}
	acc, err := live.ReadCredentialFile(filepath.Join(accDir, "account.env"))
	if err != nil {
		return fmt.Errorf("account.env: %w", err)
	}
	b := live.Binding{AccountID: acc["LZ_ACCOUNT_ID"], Endpoint: acc["OVH_ENDPOINT"], Org: acc["LZ_ORG"]}
	// A probe reads no manifest: the org clause compares the binding with itself.
	if err := live.Bind(ctx, api, cred, b, b.Org); err != nil {
		return err
	}
	exempt, err := live.LoadExemption(cfg, account)
	if err != nil {
		return err
	}
	var projects []live.Project
	keys := make([]string, 0, len(acc))
	for k := range acc {
		if strings.HasPrefix(k, "LZ_PROJECT_ID_") && acc[k] != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		projects = append(projects, live.Project{ID: acc[k], URN: projectURN(cred.Endpoint, acc[k])})
	}

	// The probes' project at start; a cleanup uses the one its run recorded (below).
	projectID := acc["LZ_PROJECT_ID_STATE"]
	if projectID == "" && p.cleanup == "" {
		return errors.New("account.env: LZ_PROJECT_ID_STATE (the probes' project) is required")
	}

	runID := p.cleanup
	if runID == "" {
		if runID, err = live.NewRunID(d.Now()); err != nil {
			return err
		}
	} else {
		recorded, err := live.ProbeRoot(cfg, account, runID)
		if err != nil {
			return err
		}
		if root, err = probeRoot(checkout, recorded); err != nil {
			return err
		}
		// After the recorded root is confined (an outside root is the first refusal): the project
		// the run ran against, not the one account.env names now; its leftovers are listed too.
		if projectID, err = live.ProbeProject(cfg, account, runID); err != nil {
			return err
		}
		if !slices.ContainsFunc(projects, func(q live.Project) bool { return q.ID == projectID }) {
			projects = append(projects, live.Project{ID: projectID, URN: projectURN(cred.Endpoint, projectID)})
		}
	}
	r := live.Runner{
		ID:        runID,
		Dir:       filepath.Join(checkout, ".local", "live", runID),
		Tofu:      tofu,
		Authority: live.AuthorityBootstrap,
		Creds:     map[string]string{"OVH_ENDPOINT": cred.Endpoint, "OVH_CLIENT_ID": cred.ClientID, "OVH_CLIENT_SECRET": cred.ClientSecret},
		Vars:      map[string]string{"TF_VAR_project_id": projectID},
		Stacks:    []live.Stack{{ID: filepath.Base(root), Dir: root, Ephemeral: true}},
		Deadline:  p.deadline,
		// The leftover check lists through lz-live's own read-only API client with the bound sandbox
		// credential (T084; ovhcloud 0.15.0 has no `api` command, P18).
		Leftovers: live.LeftoverCheck{API: api, Cred: cred, Projects: projects, Prefix: probePrefix, RunID: runID, Exempt: exempt},
		Terminal:  d.Stdout,
		PlanOnly:  p.planOnly,
	}
	// P26 tenant half (T075/T076): every probe identity a root publishes for its companion (the
	// P25 identity included) is bound like the sandbox credential before the companion runs, on a
	// start and on a cleanup.
	pr := live.Probe{Run: r, ConfigRoot: cfg, Account: account, Bind: func(ctx context.Context, c live.Credential) error {
		return live.Bind(ctx, api, c, b, b.Org)
	}}
	// The locked-recovery check's tofu seam (T036): its scratch HOME lives as long as the run.
	seam := newRecoverySeam(tofu, root)
	defer seam.close()
	paths := runPaths{RunDir: func(id string) string { return filepath.Join(checkout, ".local", "live", id) }, StateDir: filepath.Join(accDir, "state")}
	pr.Check = probeCheck(filepath.Base(root), d, api, cred, pr.Bind, seam.run, projectID, paths)
	rel, _ := filepath.Rel(checkout, root)
	if p.cleanup != "" {
		if pr.AcceptLostBaseline = p.acceptLost; p.acceptLost {
			pr.Check = acceptedCheck(filepath.Base(root), pr.Check)
			pr.AcceptDeletesNothing = baselineSweepRoots[filepath.Base(root)]
			rel += " (accept-lost-baseline)"
		}
		fmt.Fprintf(d.Stdout, "LZ-LIVE run %s start cleanup %s\n", runID, rel)
		return pr.Cleanup(ctx)
	}
	fmt.Fprintf(d.Stdout, "LZ-LIVE run %s start probe %s\n", runID, rel)
	return pr.Start(ctx)
}
