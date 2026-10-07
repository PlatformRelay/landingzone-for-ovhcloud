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

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/live"
)

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
}

// check completes p with the positional arguments: one probe root, or --cleanup <run-id>.
func (p probeArgs) check(positional []string) (probeArgs, error) {
	switch {
	case p.deadlineGiven && p.deadline <= 0:
		return p, errors.New("--deadline must be positive")
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

// probe runs or cleans up one probe after the guard admitted the run.
func probe(ctx context.Context, d deps, checkout, cfg string, p probeArgs) error {
	var root string
	var err error
	// Before any credential, file or child: the children's scratch HOME and tofu data directories
	// go under TMPDIR, which must not lie in the reviewed checkout (.local/ included).
	if err := live.ScratchOutside(checkout); err != nil {
		return err
	}
	if p.cleanup == "" {
		if root, err = probeRoot(checkout, p.root); err != nil {
			return err
		}
		// Before any credential is read, any file written or any child started.
		if planOnlyRoots[filepath.Base(root)] && !p.planOnly {
			return &live.Refusal{Condition: condPlanOnly, Detail: fmt.Sprintf("probe root %s runs only with --plan-only", filepath.Base(root))}
		}
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
	tofu, err := d.LookPath("tofu")
	if err != nil {
		return fmt.Errorf("tofu: %w", err)
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
	rel, _ := filepath.Rel(checkout, root)
	if p.cleanup != "" {
		fmt.Fprintf(d.Stdout, "LZ-LIVE run %s start cleanup %s\n", runID, rel)
		return pr.Cleanup(ctx)
	}
	fmt.Fprintf(d.Stdout, "LZ-LIVE run %s start probe %s\n", runID, rel)
	return pr.Start(ctx)
}
