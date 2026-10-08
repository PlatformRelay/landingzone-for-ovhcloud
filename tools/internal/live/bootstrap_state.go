package live

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/planjson"
	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/stacks"
)

// Bootstrap phases state, publish and verify (spec 005 FR-004, FR-008, FR-010, FR-011, FR-012,
// research R13 phases 5–7), run as BootstrapOptions.Rest after `admin`, with the admin credential
// (never the root keys) and the bound account's passphrase.
//
//   - state: init the generated `bootstrap` root (local encrypted state under the account
//     directory), import the account bucket by name when it exists but is not in state, plan to a
//     file, read it with `show -json`, pass it through Protect (G7: the account bucket and the
//     platform S3 user are retained) and apply exactly that file; then write state.env from the
//     sensitive output through files.go.
//   - publish: the root's outputs as the account-bootstrap envelope in the account bucket, with
//     state.env's S3 keys; an unchanged envelope is not written again.
//   - verify: init and plan the generated `account-governance` root against the bucket, with its
//     resolved input from stacks.Adapt and the state lock taken (no -lock=false); never applied.
//
// Coordinator decisions of 2026-10-08 (evidence/T056.md requests, evidence/T057.md):
//  1. An account bucket that is missing while the bootstrap state or state.env records it is
//     refused (CondStateBucketLost) with the recovery steps: the remote states it held are gone,
//     and an apply would create again resources that still exist. It is created only when nothing
//     records it.
//  2. (bootstrap.go) `passphrase` refuses when encrypted bootstrap state exists without the file.
//  3. A platform S3 user this run created before the bucket's create failed on a taken name is
//     deleted again in the same run; platform S3 users no state knows (an earlier, lost bootstrap)
//     are named in the `state` detail and never deleted.
//
// The S3 backend, its lockfile and client-side encryption on OVHcloud Object Storage are premises
// P1–P3, UNVERIFIED until T010; the 409 of a taken bucket name follows P21, its text UNVERIFIED.

// Bootstrap phases after `admin`, in run order (research R13).
const (
	PhaseState   = "state"
	PhasePublish = "publish"
	PhaseVerify  = "verify"
)

// CondStateBucketLost: the account bucket is missing while a record says it existed.
const CondStateBucketLost = "state-bucket-lost"

// StateOptions configures the state, publish and verify phases of one bootstrap run.
type StateOptions struct {
	// Checkout is the owner's checkout: the generated roots stacks/account/bootstrap (`state`) and
	// stacks/account/account-governance (`verify`). Nothing is written inside it.
	Checkout string
	Manifest *stacks.Manifest // spec.org (bucket names), spec.state.project's reference
	Region   string           // Object Storage region of the account bucket as the API names it (GRA)
	Schemas  fs.FS            // schemas/outputs, for the published envelope
	Revision string           // source_revision of the published envelope (the reviewed commit)
	Tofu     string           // the tofu executable
	API      API              // the API the admin credential (BootstrapAccount.Admin) authenticates against
	// Store opens the account bucket with state.env's S3 keys (AWS_ACCESS_KEY_ID,
	// AWS_SECRET_ACCESS_KEY): S3 live, a fake offline.
	Store  func(s3 map[string]string) (ObjectStore, error)
	Stdout io.Writer // LZ-LIVE lines of these phases
}

// BootstrapState runs the state, publish and verify phases of one bootstrap run.
type BootstrapState struct {
	o  StateOptions
	rs []PhaseResult
}

// NewBootstrapState returns the phases for one run; Rest is BootstrapOptions.Rest.
func NewBootstrapState(o StateOptions) *BootstrapState { return &BootstrapState{o: o} }

// Results returns the phases' results in run order.
func (s *BootstrapState) Results() []PhaseResult { return s.rs }

// bootstrapInstance is the account bootstrap's manifest row (artefact key, bucket).
const bootstrapInstance = "account-bootstrap"

// accountBootstrapRetained: the account bucket and the platform S3 user (user, credential, policy) of the
// generated root, which calls the stage as module "bootstrap".
var accountBootstrapRetained = []Retained{{Instance: bootstrapInstance, Addresses: []string{
	"module.bootstrap.module.state_backend.module.bucket",
	`module.bootstrap.module.state_backend.module.s3_user["platform"]`,
}}}

const (
	bootstrapBucketAddr = "module.bootstrap.module.state_backend.module.bucket.ovh_cloud_project_storage.this"
	// bootstrapUserModule holds the platform S3 user, its credential and its policy.
	bootstrapUserModule = `module.bootstrap.module.state_backend.module.s3_user["platform"]`
	bootstrapUserAddr   = bootstrapUserModule + ".ovh_cloud_project_user.this"
	// bootstrapStateFile is the generated root's local state under accounts/<account>/state/
	// (stacks/account/bootstrap/_lz_backend.tf).
	bootstrapStateFile = "account-bootstrap.tfstate"
)

// platformS3UserDescription is the description modules/naming gives the account scope's platform
// S3 user (kind s3_user, role state-platform): lz-s3u-state-platform for org lz in the captured
// stage plan (tests/fixtures/outputs/captures/bootstrap-plan.json), which the tests' fake carries.
func platformS3UserDescription(org string) string { return org + "-s3u-state-platform" }

type stateRun struct {
	s       *BootstrapState
	a       BootstrapAccount
	red     *Redactor
	out     io.WriteCloser
	home    string            // scratch HOME and data directories of the run's children
	vars    map[string]string // the bootstrap authority: admin credential, passphrase, account inputs
	api     bearerClient      // the admin's token
	binding map[string]string // account.env
	project string            // the bound state project
	bucket  string            // the account bucket
	output  []byte            // `tofu output -json` of the bootstrap root
}

// Rest runs state, publish and verify for the bound account with its admin credential. A refusal
// is a *Refusal (exit 3), a failure any other error (exit 1); every phase that ran reports one
// result, and a phase after a failed one does not run.
func (s *BootstrapState) Rest(ctx context.Context, a BootstrapAccount) (err error) {
	r := &stateRun{s: s, a: a, red: NewRedactor(a.Admin.ClientSecret)}
	stdout := s.o.Stdout
	if stdout == nil {
		stdout = io.Discard
	}
	r.out = r.red.Writer(stdout)
	defer r.out.Close()
	defer func() {
		if err != nil {
			err = redactedError{err: err, red: r.red}
		}
	}()
	// G3: the children's scratch HOME, plan files and data directories never go inside the checkout
	// (review r2).
	if err = ScratchOutside(s.o.Checkout); err != nil {
		var ref *Refusal
		if errors.As(err, &ref) {
			return r.refuse(PhaseState, ref.Condition, ref.Detail)
		}
		return r.fail(PhaseState, err.Error())
	}
	if r.home, err = scratchHome(); err != nil {
		return r.fail(PhaseState, "scratch: "+err.Error())
	}
	defer removeTree(r.home)
	if err = r.setup(ctx); err != nil {
		return err
	}
	keys, stateChanged, err := r.state(ctx)
	if err != nil {
		return err
	}
	pubChanged, err := r.publish(keys)
	if err != nil {
		return err
	}
	return r.verify(ctx, keys, stateChanged || pubChanged)
}

// setup reads the passphrase and the binding and takes the admin's token: what every child and
// API call of the phases needs. A failure is the state phase's.
func (r *stateRun) setup(ctx context.Context) error {
	o := r.s.o
	pass, err := ReadCredentialFile(filepath.Join(r.a.Dir, "state-passphrase.env"))
	if err != nil {
		return r.fail(PhaseState, "state-passphrase.env: "+err.Error())
	}
	if pass["TF_VAR_state_passphrase"] == "" {
		return r.fail(PhaseState, "state-passphrase.env holds no TF_VAR_state_passphrase")
	}
	r.red.Add(pass["TF_VAR_state_passphrase"])
	if r.binding, err = ReadEnvFile(filepath.Join(r.a.Dir, "account.env")); err != nil {
		return r.fail(PhaseState, "account.env: "+err.Error())
	}
	ref := "LZ_PROJECT_ID_" + o.Manifest.StateProjectRef
	if r.project = r.binding[ref]; r.project == "" {
		return r.fail(PhaseState, "account.env has no "+ref)
	}
	if o.Region == "" {
		return r.fail(PhaseState, "no Object Storage region for the account bucket")
	}
	if r.bucket, err = stacks.ArtifactBucket(o.Manifest, bootstrapInstance); err != nil {
		return r.fail(PhaseState, err.Error())
	}
	r.vars = map[string]string{
		"OVH_ENDPOINT": r.a.Admin.Endpoint, "OVH_CLIENT_ID": r.a.Admin.ClientID, "OVH_CLIENT_SECRET": r.a.Admin.ClientSecret,
		"TF_VAR_state_passphrase": pass["TF_VAR_state_passphrase"], "TF_VAR_lz_account_dir": r.a.Dir, "TF_VAR_state_project_id": r.project,
	}
	if r.api, err = newBearer(ctx, o.API, r.a.Admin); err != nil {
		return r.fail(PhaseState, err.Error())
	}
	return nil
}

func (r *stateRun) report(phase, status, detail string) {
	detail = r.red.Redact(detail)
	r.s.rs = append(r.s.rs, PhaseResult{Phase: phase, Status: status, Detail: detail})
	fmt.Fprintln(r.out, strings.TrimSpace("LZ-LIVE "+phase+" bootstrap "+status+" "+detail))
}

func (r *stateRun) fail(phase, detail string) error {
	r.report(phase, StatusFail, detail)
	return fmt.Errorf("%s: %s", phase, r.red.Redact(detail))
}

// refuse reports phase as failed with the refusal and returns it (exit 3).
func (r *stateRun) refuse(phase, cond, detail string) error {
	detail = r.red.Redact(detail)
	r.report(phase, StatusFail, "refused ("+cond+"): "+detail)
	return refuse(cond, "%s", detail)
}

// tofu runs tofu in the generated root stacks/account/<root> of the checkout, with the run's
// allowlisted environment (env.go) plus extra, its data directory under the scratch HOME, and the
// run core's stop handling (SIGINT, then a kill after the grace period). It returns stdout; stderr
// goes, redacted, only into the error.
func (r *stateRun) tofu(ctx context.Context, root string, extra map[string]string, args ...string) ([]byte, error) {
	dir := filepath.Join(r.s.o.Checkout, "stacks", "account", root)
	vars := maps.Clone(r.vars)
	maps.Copy(vars, extra)
	vars["TF_DATA_DIR"] = filepath.Join(r.home, "data-"+root)
	child := &childEnv{home: r.home, vars: vars, grace: DefaultGrace}
	cmd := child.command(ctx, r.s.o.Tofu, append([]string{"-chdir=" + dir}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	// After a stop, what is left of the child's process group (provider plugins) is killed, as the
	// run core does (review r1).
	killGroup(ctx, cmd)
	if err != nil {
		return stdout.Bytes(), fmt.Errorf("tofu %s (%s): %v: %s", args[0], root, err, r.red.Redact(strings.TrimSpace(stderr.String())))
	}
	return stdout.Bytes(), nil
}

// stateList lists the bootstrap root's state addresses. Before the first apply the local state file
// does not exist and `tofu state list` fails ("No state file was found!", exit 1; observed with
// tofu 1.10.3 on the host, review r1): no file is an empty list.
func (r *stateRun) stateList(ctx context.Context) ([]string, error) {
	if _, err := os.Lstat(filepath.Join(r.a.Dir, "state", bootstrapStateFile)); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	list, err := r.tofu(ctx, "bootstrap", nil, "state", "list")
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(list)), nil
}

// bucketExists reads the account bucket in the bound project (GET
// /cloud/project/{serviceName}/region/{regionName}/storage/{name}, kb/api/v1/cloud.json).
func (r *stateRun) bucketExists(ctx context.Context) (bool, error) {
	var c struct {
		Name string `json:"name"`
	}
	err := r.api.get(ctx, fmt.Sprintf("/cloud/project/%s/region/%s/storage/%s", r.project, r.s.o.Region, r.bucket), &c)
	switch {
	case err == nil:
		return true, nil
	case statusOf(err) == 404:
		return false, nil
	}
	return false, err
}

// platformUsers lists the ids of the bound project's platform S3 users (GET
// /cloud/project/{serviceName}/user, kb/api/v1/cloud.json: cloud.user.User, id long), by the
// description the stage gives them; users being deleted are left out.
func (r *stateRun) platformUsers(ctx context.Context) ([]string, error) {
	var us []struct {
		ID          int64  `json:"id"`
		Description string `json:"description"`
		Status      string `json:"status"`
	}
	if err := r.api.get(ctx, "/cloud/project/"+r.project+"/user", &us); err != nil {
		return nil, err
	}
	want := platformS3UserDescription(r.s.o.Manifest.Org)
	var ids []string
	for _, u := range us {
		if u.Description == want && u.Status != "deleted" && u.Status != "deleting" {
			ids = append(ids, strconv.FormatInt(u.ID, 10))
		}
	}
	slices.Sort(ids)
	return ids, nil
}

// state is phase 5: it returns state.env's keys and whether it changed anything.
func (r *stateRun) state(ctx context.Context) (map[string]string, bool, error) {
	changed := false
	if _, err := r.tofu(ctx, "bootstrap", nil, "init", "-input=false", "-lockfile=readonly"); err != nil {
		return nil, false, r.fail(PhaseState, err.Error())
	}
	addrs, err := r.stateList(ctx)
	if err != nil {
		return nil, false, r.fail(PhaseState, err.Error())
	}
	inState := slices.Contains(addrs, bootstrapBucketAddr)
	stateEnv := filepath.Join(r.a.Dir, "state.env")
	_, serr := os.Lstat(stateEnv)
	if serr != nil && !errors.Is(serr, fs.ErrNotExist) {
		return nil, false, r.fail(PhaseState, "state.env: "+serr.Error())
	}
	exists, err := r.bucketExists(ctx)
	if err != nil {
		return nil, false, r.fail(PhaseState, err.Error())
	}
	switch {
	case !exists && (inState || serr == nil):
		return nil, false, r.lost(inState, serr == nil)
	case exists && !inState:
		// The bucket exists but the local state lost it: adopt it by name
		// (cloud_project_storage.md Import: <service_name>/<region_name>/<name>).
		id := r.project + "/" + r.s.o.Region + "/" + r.bucket
		if _, err := r.tofu(ctx, "bootstrap", nil, "import", "-input=false", bootstrapBucketAddr, id); err != nil {
			return nil, false, r.fail(PhaseState, err.Error())
		}
		changed = true
	}
	before, err := r.platformUsers(ctx)
	if err != nil {
		return nil, false, r.fail(PhaseState, err.Error())
	}

	// G7: plan to a file, judge that file's document, apply that file and nothing else.
	planFile := filepath.Join(r.home, "bootstrap.tfplan")
	if _, err := r.tofu(ctx, "bootstrap", nil, "plan", "-input=false", "-out="+planFile); err != nil {
		return nil, false, r.fail(PhaseState, err.Error())
	}
	doc, err := r.tofu(ctx, "bootstrap", nil, "show", "-json", planFile)
	if err != nil {
		return nil, false, r.fail(PhaseState, err.Error())
	}
	if err := Protect(doc, accountBootstrapRetained); err != nil {
		r.report(PhaseState, StatusFail, err.Error())
		return nil, false, err
	}
	applies, creates, err := planApplies(doc)
	if err != nil {
		return nil, false, r.fail(PhaseState, err.Error())
	}
	if applies {
		if _, err := r.tofu(ctx, "bootstrap", nil, "apply", "-input=false", planFile); err != nil {
			if slices.Contains(creates, bootstrapBucketAddr) && r.bucketTaken(ctx, err) {
				// This run's user: its own plan created it, and the state did not hold it before.
				ours := slices.Contains(creates, bootstrapUserAddr) && !slices.Contains(addrs, bootstrapUserAddr)
				return nil, false, r.taken(ctx, before, ours)
			}
			return nil, false, r.fail(PhaseState, err.Error())
		}
		changed = true
	}

	raw, err := r.tofu(ctx, "bootstrap", nil, "output", "-json")
	if err != nil {
		return nil, false, r.fail(PhaseState, err.Error())
	}
	var outs struct {
		UserID struct {
			Value string `json:"value"`
		} `json:"platform_s3_user_id"`
		S3 struct {
			Value struct {
				AK string `json:"access_key_id"`
				SK string `json:"secret_access_key"`
			} `json:"value"`
		} `json:"platform_s3"`
	}
	if err := json.Unmarshal(raw, &outs); err != nil || outs.S3.Value.AK == "" || outs.S3.Value.SK == "" || outs.UserID.Value == "" {
		return nil, false, r.fail(PhaseState, "the bootstrap root has no platform_s3 or platform_s3_user_id output")
	}
	r.red.Add(outs.S3.Value.SK)
	r.output = raw
	keys := map[string]string{"AWS_ACCESS_KEY_ID": outs.S3.Value.AK, "AWS_SECRET_ACCESS_KEY": outs.S3.Value.SK}
	if cur, err := ReadCredentialFile(stateEnv); err != nil || !maps.Equal(cur, keys) {
		// a.Dir is <config root>/accounts/<account>.
		root := filepath.Dir(filepath.Dir(r.a.Dir))
		if err := WriteCredentialFile(root, filepath.Join("accounts", filepath.Base(r.a.Dir), "state.env"), keys); err != nil {
			return nil, false, r.fail(PhaseState, err.Error())
		}
		changed = true
	}

	// Decision 3: platform S3 users no state knows are the owner's to delete; they are named.
	users, err := r.platformUsers(ctx)
	if err != nil {
		return nil, false, r.fail(PhaseState, err.Error())
	}
	detail := ""
	if left := slices.DeleteFunc(users, func(id string) bool { return id == outs.UserID.Value }); len(left) > 0 {
		detail = fmt.Sprintf("leftover platform S3 users not in the bootstrap state: %s (their keys may still reach %s; delete them by hand, Control Panel or DELETE /cloud/project/%s/user/<id>, once nothing uses them)",
			strings.Join(left, ", "), r.bucket, r.project)
	}
	status := StatusUnchanged
	if changed {
		status = StatusRan
	}
	r.report(PhaseState, status, detail)
	return keys, changed, nil
}

// lost refuses a missing account bucket that a record says existed (decision 1).
func (r *stateRun) lost(inState, stateEnv bool) error {
	var records []string
	if inState {
		records = append(records, "the bootstrap state (state/account-bootstrap.tfstate)")
	}
	if stateEnv {
		records = append(records, "state.env")
	}
	return r.refuse(PhaseState, CondStateBucketLost, fmt.Sprintf(
		"the account bucket %s is not in project %s (region %s), but %s records it: the remote states it held are lost, and an apply now would create again resources that still exist. "+
			"Recover: restore the bucket under the same name with its objects (from a copy or backup) and re-run; "+
			"or, to start the account's state over deliberately, delete the resources those states managed (Control Panel), "+
			"then remove accounts/%s/state.env and accounts/%s/state/account-bootstrap.tfstate and re-run. "+
			"If spec.org changed since the bucket was created, this is a rename, which is not supported: restore the bound org (LZ_ORG in account.env)",
		r.bucket, r.project, r.s.o.Region, strings.Join(records, " and "), r.a.ID, r.a.ID))
}

// planApplies reads a `show -json` plan: whether applying it changes anything (a resource action
// other than no-op/read, or an output change: `tofu output` reads only applied outputs, review r1)
// and the addresses it creates.
func planApplies(doc []byte) (applies bool, creates []string, err error) {
	plan, err := planjson.Decode(doc)
	if err != nil {
		return false, nil, err
	}
	for _, c := range plan.Changes {
		if slices.ContainsFunc(c.Actions, func(a string) bool { return a != "no-op" && a != "read" }) {
			applies = true
			if slices.Contains(c.Actions, "create") {
				creates = append(creates, c.Address)
			}
		}
	}
	var outs struct {
		OutputChanges map[string]struct {
			Actions []string `json:"actions"`
		} `json:"output_changes"`
	}
	if err := json.Unmarshal(doc, &outs); err != nil {
		return false, nil, errors.New("plan output_changes unreadable")
	}
	for _, o := range outs.OutputChanges {
		applies = applies || slices.ContainsFunc(o.Actions, func(a string) bool { return a != "no-op" })
	}
	return applies, creates, nil
}

// bucketTaken: one diagnostic of the failed apply is a 409 on the storage create (P21: names are
// global; the error text is the provider's and UNVERIFIED until T044/T045), and the bucket is not
// in the bound project afterwards. Each diagnostic is judged on its own, whitespace collapsed (tofu
// wraps a diagnostic's detail) and case folded; a 409 of another resource is a plain failure
// (review r1, r2).
func (r *stateRun) bucketTaken(ctx context.Context, applyErr error) bool {
	storage := "/region/" + strings.ToLower(r.s.o.Region) + "/storage"
	found := false
	for _, d := range strings.Split(applyErr.Error(), "Error:") {
		d = strings.ToLower(strings.Join(strings.Fields(d), " "))
		found = found || strings.Contains(d, "status code 409") && strings.Contains(d, storage)
	}
	if !found {
		return false
	}
	exists, err := r.bucketExists(ctx)
	return err == nil && !exists
}

// taken fails a bucket create refused on a taken name and compensates (decision 3): the platform
// S3 user this run created is deleted again and removed from the bootstrap state. It is this run's
// only when the evidence agrees: this run's plan created the user's address, which the state did
// not hold before (ours), the address is in the state now, and exactly one platform user appeared
// in the project; otherwise nothing is deleted and the new users are named (review r1, r2).
// Platform users that existed before the run are named, never deleted.
func (r *stateRun) taken(ctx context.Context, before []string, ours bool) error {
	msg := fmt.Sprintf("the account bucket name %s is taken by another account (bucket names are global): set spec.org in %s and LZ_ORG in accounts/%s/account.env to the same new value (identify refuses a manifest org that differs from the bound one) and re-run",
		r.bucket, stacks.ManifestPath, r.a.ID)
	if len(before) > 0 {
		msg += "; platform S3 users that existed before this run, not deleted: " + strings.Join(before, ", ")
	}
	after, err := r.platformUsers(ctx)
	if err != nil {
		return r.fail(PhaseState, msg+"; the platform S3 users of project "+r.project+" could not be listed: delete the one this run created by hand")
	}
	fresh := slices.DeleteFunc(after, func(id string) bool { return slices.Contains(before, id) })
	addrs, err := r.stateList(ctx)
	if err != nil {
		return r.fail(PhaseState, msg+"; the bootstrap state could not be read: delete the platform S3 user this run created by hand ("+strings.Join(fresh, ", ")+")")
	}
	byHand := fmt.Sprintf("delete by hand once nothing uses it: DELETE /cloud/project/%s/user/<id>", r.project)
	switch {
	case len(fresh) == 0:
	case len(fresh) == 1 && ours && slices.Contains(addrs, bootstrapUserAddr):
		if err := r.api.delete(ctx, "/cloud/project/"+r.project+"/user/"+fresh[0]); err != nil {
			msg += "; could not delete the platform S3 user this run created: " + fresh[0] + " (" + byHand + ")"
			break
		}
		msg += "; deleted the platform S3 user this run created: " + fresh[0]
		if _, err := r.tofu(ctx, "bootstrap", nil, "state", "rm", bootstrapUserModule); err != nil {
			msg += fmt.Sprintf("; remove it from the bootstrap state by hand: tofu state rm '%s'", bootstrapUserModule)
		}
	default:
		msg += "; new platform S3 users that cannot be attributed to this run, not deleted: " + strings.Join(fresh, ", ") + " (" + byHand + ")"
	}
	return r.fail(PhaseState, msg)
}

// publish is phase 6: the bootstrap envelope into the account bucket with state.env's keys.
func (r *stateRun) publish(keys map[string]string) (bool, error) {
	store, err := r.s.o.Store(keys)
	if err != nil {
		return false, r.fail(PhasePublish, err.Error())
	}
	acct, err := BoundAccountFromEnv(r.binding)
	if err != nil {
		return false, r.fail(PhasePublish, err.Error())
	}
	old, err := store.Get(r.bucket, stacks.ArtifactKey(bootstrapInstance))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, r.fail(PhasePublish, err.Error())
	}
	same := &unchangedStore{ObjectStore: store, old: old, had: err == nil}
	if _, err := Publish(PublishOptions{Manifest: r.s.o.Manifest, Instance: bootstrapInstance, Revision: r.s.o.Revision,
		TofuOutput: r.output, Schemas: r.s.o.Schemas, Account: acct, Store: same}); err != nil {
		return false, r.fail(PhasePublish, err.Error())
	}
	if !same.wrote {
		r.report(PhasePublish, StatusUnchanged, "")
		return false, nil
	}
	r.report(PhasePublish, StatusRan, "")
	return true, nil
}

// unchangedStore skips a Put of the bytes the object already holds, so a re-run adds no object
// version to the versioned account bucket.
type unchangedStore struct {
	ObjectStore
	old   []byte
	had   bool
	wrote bool
}

func (s *unchangedStore) Put(bucket, key string, data []byte) error {
	if s.had && bytes.Equal(s.old, data) {
		return nil
	}
	s.wrote = true
	return s.ObjectStore.Put(bucket, key, data)
}

// verify is phase 7: init and plan account-governance against the account bucket, the state lock
// taken (P1 UNVERIFIED); a held lock fails the phase and is neither broken nor bypassed.
func (r *stateRun) verify(ctx context.Context, keys map[string]string, changed bool) error {
	if _, err := r.tofu(ctx, "account-governance", keys, "init", "-input=false", "-lockfile=readonly"); err != nil {
		return r.fail(PhaseVerify, err.Error())
	}
	store, err := r.s.o.Store(keys)
	if err != nil {
		return r.fail(PhaseVerify, err.Error())
	}
	acct, err := BoundAccountFromEnv(r.binding)
	if err != nil {
		return r.fail(PhaseVerify, err.Error())
	}
	in, err := stacks.Adapt(stacks.AdaptOptions{Manifest: r.s.o.Manifest, Consumer: "account-governance", Store: store,
		Schemas: r.s.o.Schemas, Account: acct, Dir: filepath.Join(r.home, "inputs", "account-governance")})
	if err != nil {
		return r.fail(PhaseVerify, err.Error())
	}
	args := []string{"plan", "-input=false", "-lock-timeout=0s"}
	for _, f := range in.Files {
		args = append(args, "-var-file="+f)
	}
	if _, err := r.tofu(ctx, "account-governance", keys, args...); err != nil {
		return r.fail(PhaseVerify, err.Error())
	}
	status := StatusUnchanged
	if changed {
		status = StatusRan
	}
	r.report(PhaseVerify, status, "")
	return nil
}
