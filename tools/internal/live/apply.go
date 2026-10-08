package live

// `lz-live plan|apply -- <instance|all>` (FR-009, FR-010, FR-011, research R21, ADR-0007): the
// lane acts on the selected set (stacks.Select over the current code digests, the published
// artefacts and the resolved references against the records of the last applies), in run order,
// holding the run locks (stacks.HoldRun over the acted instances) from before the first plan until
// the run ends, on every path. Per stack: the authority's credentials (credentials.go), bound to
// the account through GET /auth/details before the stack's first child (binding.go), the consumed
// inputs (stacks.Adapt; a producer without an artefact blocks the stack, exit 2), init, plan to a
// file, `show -json` of that file through the retained-resource guard (protect.go), the rendered
// plan redacted to plan-<id>.txt, and for apply exactly that file applied; then the credential
// files the stack's sensitive outputs carry (account-governance, tenant-state; written by
// files.go), the envelope published and the record written. A refusal or failure stops the run:
// nothing after it is planned or applied. `destroy` refuses every retained instance (G7); the rest
// of destroy is T047's. account-bootstrap is bootstrap:account's, never the lane's (decision 1,
// 2026-10-08).
//
// Not here (recorded gaps of T059): a run deadline and the inventory of what an apply created.
// A cancelled context stops the running child as the run core does (SIGINT, then a kill after
// DefaultGrace).

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/stacks"
)

// Verbs of the lane and the target naming every selected stack.
const (
	VerbPlan    = "plan"
	VerbApply   = "apply"
	VerbDestroy = "destroy"
	TargetAll   = "all"
)

// Refusal conditions of the lane (exit 3).
const (
	CondLocked           = "locked"            // another run holds one of the run's locks
	CondProducerSelected = "producer-selected" // `-- <instance>` while a producer it consumes is selected and not applied
	CondBootstrapOwned   = "bootstrap-owned"   // account-bootstrap is bootstrap:account's, never the lane's
)

// ApplyOptions configures one `lz-live plan|apply|destroy` run.
type ApplyOptions struct {
	Verb       string           // VerbPlan, VerbApply or VerbDestroy
	Target     string           // TargetAll or one instance id
	Checkout   string           // the reviewed checkout: generated stacks and their stage/component/module closure
	Manifest   *stacks.Manifest // the checkout's decoded stacks/deployments.yaml
	ConfigRoot string           // ~/.config/ovh-lz
	Account    string           // the bound account id (accounts/<account>/)
	RunID      string           // YYYYMMDDThhmmssZ-<4 hex>
	RunDir     string           // .local/live/<run-id>: rendered plans, inputs/<id>/
	Records    string           // .local/live/records: records/<id>.json of the last applies
	Revision   string           // source_revision of the applied tree
	Tofu       string           // tofu executable
	Schemas    fs.FS            // schemas/outputs
	Locks      stacks.LockStore // DirLocks(accounts/<account>/locks) in production
	// Store opens the state buckets with one authority's S3 keys (AWS_ACCESS_KEY_ID,
	// AWS_SECRET_ACCESS_KEY): artefacts are read and published with the acting stack's keys.
	Store    func(keys map[string]string) (ObjectStore, error)
	Terminal io.Writer
	// API binds every OAuth2 credential to the account (GET /auth/details) before its first use.
	API API
}

func stageOf(m *stacks.Manifest, id string) (stacks.Instance, stacks.Stage, error) {
	in, err := m.Row(id)
	if err != nil {
		return in, stacks.Stage{}, err
	}
	st, ok := stacks.StageOf(in.Stage)
	if !ok {
		return in, st, fmt.Errorf("%s: unknown stage %s", id, in.Stage)
	}
	return in, st, nil
}

func s3Of(creds map[string]string) map[string]string {
	return map[string]string{"AWS_ACCESS_KEY_ID": creds["AWS_ACCESS_KEY_ID"], "AWS_SECRET_ACCESS_KEY": creds["AWS_SECRET_ACCESS_KEY"]}
}

type laneRunner struct {
	o       ApplyOptions
	red     *Redactor
	term    io.Writer
	home    string
	pass    string
	account stacks.BoundAccount
	binding Binding
	bound   map[string]bool // client ids bound in this run
	code    map[string]string
	passes  int // selections run so far
}

// accountFile reads one of the bound account's own files, which bootstrap:account writes: missing,
// the run is blocked (exit 2) naming it.
func accountFile(dir, name string) (map[string]string, error) {
	v, err := ReadCredentialFile(filepath.Join(dir, name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, &Blocked{Phase: "credentials", Detail: fmt.Sprintf("%s of the account does not exist; bootstrap:account writes it", name)}
	}
	return v, err
}

// Apply runs one plan, apply or destroy; the error maps to the exit code through ExitCode.
func Apply(ctx context.Context, o ApplyOptions) (err error) {
	red := NewRedactor()
	terminal := o.Terminal
	if terminal == nil {
		terminal = io.Discard
	}
	term := red.Writer(&quietWriter{w: terminal})
	defer term.Close()
	defer func() {
		if err != nil {
			err = redactedError{err, red}
		}
	}()
	m := o.Manifest
	if o.Verb == VerbDestroy {
		// Before any file is read or child started (G7).
		ids := []string{o.Target}
		if o.Target == TargetAll {
			ids = nil
			for _, in := range m.Instances {
				ids = append(ids, in.ID)
			}
		}
		for _, id := range ids {
			_, st, err := stageOf(m, id)
			if err != nil {
				return err
			}
			if st.Chain == "retained" {
				return refuse(CondRetained, "destroy refuses retained instance %s", id)
			}
		}
		return errors.New("destroy of ephemeral instances arrives with lz-live destroy (T047)")
	}
	if o.Verb != VerbPlan && o.Verb != VerbApply {
		return fmt.Errorf("unknown verb %q", o.Verb)
	}
	if o.Target != TargetAll {
		in, _, err := stageOf(m, o.Target)
		if err != nil {
			return err
		}
		if in.Stage == "bootstrap" {
			return refuse(CondBootstrapOwned, "%s is applied by bootstrap:account only", o.Target)
		}
	}
	acctDir, err := AccountDir(o.ConfigRoot, o.Account)
	if err != nil {
		return err
	}
	r := &laneRunner{o: o, red: red, term: term, bound: map[string]bool{}, code: map[string]string{}}
	pf, err := accountFile(acctDir, "state-passphrase.env")
	if err != nil {
		return err
	}
	if r.pass = pf["TF_VAR_state_passphrase"]; r.pass == "" {
		return refuse(CondCredentials, "state-passphrase.env has no TF_VAR_state_passphrase")
	}
	red.Add(r.pass)
	acc, err := accountFile(acctDir, "account.env")
	if err != nil {
		return err
	}
	// Every credential is bound against this. The binding is the run's account: an account.env
	// naming another account is refused here, since credentials of that other account copied
	// beside it would pass the binding (review r1).
	r.binding = Binding{AccountID: acc["LZ_ACCOUNT_ID"], Endpoint: acc["OVH_ENDPOINT"], Org: acc["LZ_ORG"]}
	if r.binding.AccountID != o.Account {
		return refuse(CondAccount, "account.env of %s names account %q", o.Account, r.binding.AccountID)
	}
	if r.account, err = BoundAccountFromEnv(acc); err != nil {
		return err
	}
	ids, err := r.acted()
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		fmt.Fprintf(term, "LZ-LIVE %s nothing selected\n", o.Verb)
		return nil
	}
	release, err := stacks.HoldRun(o.Locks, m, ids)
	if errors.Is(err, stacks.ErrLocked) {
		return refuse(CondLocked, "%v", err)
	}
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, release()) }()
	// Selection again under the locks (review r1): another run that held them between the first
	// selection and HoldRun may have applied, so the set the locks cover may be stale.
	again, err := r.acted()
	if err != nil {
		return err
	}
	if !slices.Equal(again, ids) {
		return refuse(CondLocked, "the selection changed while the run locks were taken (%v, now %v): another run applied meanwhile; run again", ids, again)
	}
	if r.home, err = scratchHome(); err != nil {
		return err
	}
	defer removeTree(r.home)
	for _, id := range ids {
		fmt.Fprintf(term, "LZ-LIVE %s %s start\n", o.Verb, id)
		if err := r.stack(ctx, id); err != nil {
			fmt.Fprintf(term, "LZ-LIVE %s %s fail\n", o.Verb, id)
			return err
		}
		fmt.Fprintf(term, "LZ-LIVE %s %s ok\n", o.Verb, id)
	}
	return nil
}

// acted returns the stacks the run acts on, in run order: the selected set without
// account-bootstrap for `all`; for `-- <instance>` that instance, refused while a producer it
// consumes is selected.
func (r *laneRunner) acted() ([]string, error) {
	m := r.o.Manifest
	sel, err := r.selected()
	if err != nil {
		return nil, err
	}
	if r.o.Target != TargetAll {
		in, _ := m.Row(r.o.Target)
		for _, e := range in.Edges {
			if e.Kind == stacks.EdgeData && slices.ContainsFunc(sel, func(s stacks.Selected) bool { return s.ID == e.Producer }) {
				return nil, refuse(CondProducerSelected, "%s consumes %s, which is selected and not applied", r.o.Target, e.Producer)
			}
		}
		return []string{r.o.Target}, nil
	}
	var ids []string
	for _, s := range sel {
		if in, _ := m.Row(s.ID); in.Stage != "bootstrap" {
			ids = append(ids, s.ID)
		}
	}
	return ids, nil
}

// selected runs stacks.Select over the current code digests, the resolved-reference digests and
// the published artefacts (read with each producer's own S3 keys; a producer whose keys do not
// exist yet has published nothing) against the records.
func (r *laneRunner) selected() ([]stacks.Selected, error) {
	o, m := r.o, r.o.Manifest
	r.passes++ // each selection adapts into its own fresh directories
	records, err := stacks.ReadRecords(o.Records, m)
	if err != nil {
		return nil, err
	}
	resolved := map[string]string{}
	artifacts := map[string]string{}
	for _, in := range m.Instances {
		if r.code[in.ID], err = stacks.CodeDigest(o.Checkout, in); err != nil {
			return nil, err
		}
		data := false
		for _, e := range in.Edges {
			if e.Kind != stacks.EdgeData {
				continue
			}
			data = true
			if _, done := artifacts[e.Producer]; done {
				continue
			}
			_, creds, err := LoadCredentials(o.ConfigRoot, o.Account, m, e.Producer)
			var blocked *Blocked
			if errors.As(err, &blocked) {
				continue // the producer's keys are not written yet: it has published nothing
			}
			if err != nil {
				return nil, err
			}
			r.red.Add(creds["OVH_CLIENT_SECRET"], creds["AWS_SECRET_ACCESS_KEY"])
			store, err := o.Store(s3Of(creds))
			if err != nil {
				return nil, err
			}
			bucket, err := stacks.ArtifactBucket(m, e.Producer)
			if err != nil {
				return nil, err
			}
			b, err := store.Get(bucket, stacks.ArtifactKey(e.Producer))
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			artifacts[e.Producer] = digestHex(b)
		}
		// A stage that consumes no artefact needs no store to adapt: its resolved-reference digest
		// comes from account.env alone. (No stage of the table takes both today; a consumer taking
		// resolved references would need stacks to expose that digest on its own.)
		if !data {
			ins, err := stacks.Adapt(stacks.AdaptOptions{Manifest: m, Consumer: in.ID, Schemas: o.Schemas, Account: r.account,
				Dir: filepath.Join(o.RunDir, "select", strconv.Itoa(r.passes), in.ID)})
			if err != nil {
				return nil, err
			}
			resolved[in.ID] = ins.Resolved
		}
	}
	return stacks.Select(stacks.SelectOptions{Manifest: m, Code: r.code, Resolved: resolved, Artifacts: artifacts, Records: records})
}

// bind checks the stack's OAuth2 credential against the bound account and the manifest's org
// (FR-010, G13), once per credential and run, before the stack's first child.
func (r *laneRunner) bind(ctx context.Context, creds map[string]string) error {
	c := Credential{Endpoint: creds["OVH_ENDPOINT"], ClientID: creds["OVH_CLIENT_ID"], ClientSecret: creds["OVH_CLIENT_SECRET"]}
	if r.bound[c.ClientID] {
		return nil
	}
	if err := Bind(ctx, r.o.API, c, r.binding, r.o.Manifest.Org); err != nil {
		return err
	}
	r.bound[c.ClientID] = true
	return nil
}

// tofu runs one tofu call on a stack's root as the run core does: allowlisted environment, its own
// process group, SIGINT when ctx ends and a kill after DefaultGrace (runner.go childEnv).
func (r *laneRunner) tofu(ctx context.Context, child *childEnv, id, dir string, stdout io.Writer, args ...string) error {
	cmd := child.command(ctx, r.o.Tofu, append([]string{"-chdir=" + dir}, args...)...)
	cmd.Env = append(cmd.Env, "TF_DATA_DIR="+filepath.Join(r.home, "tofu-data", id))
	cmd.Stdout, cmd.Stderr = stdout, r.term
	err := cmd.Run()
	killGroup(ctx, cmd)
	if err != nil {
		return fmt.Errorf("tofu %s %s: %w", args[0], id, err)
	}
	return nil
}

func (r *laneRunner) stack(ctx context.Context, id string) (err error) {
	o, m := r.o, r.o.Manifest
	in, st, err := stageOf(m, id)
	if err != nil {
		return err
	}
	_, creds, err := LoadCredentials(o.ConfigRoot, o.Account, m, id)
	if err != nil {
		return err
	}
	r.red.Add(creds["OVH_CLIENT_SECRET"], creds["AWS_SECRET_ACCESS_KEY"])
	if err := r.bind(ctx, creds); err != nil {
		return err
	}
	store, err := o.Store(s3Of(creds))
	if err != nil {
		return err
	}
	inputs, err := stacks.Adapt(stacks.AdaptOptions{Manifest: m, Consumer: id, Store: store, Schemas: o.Schemas, Account: r.account,
		Dir: filepath.Join(o.RunDir, "inputs", id)})
	if errors.Is(err, stacks.ErrBlocked) {
		return &Blocked{Phase: "inputs", Detail: fmt.Sprintf("%s: %v", id, err)}
	}
	if err != nil {
		return err
	}
	vars := map[string]string{"TF_VAR_state_passphrase": r.pass}
	for k, v := range creds {
		vars[k] = v
	}
	child := &childEnv{home: r.home, vars: vars, grace: DefaultGrace}
	dir := filepath.Join(o.Checkout, filepath.FromSlash(in.Path))
	if err := r.tofu(ctx, child, id, dir, r.term, initArgs...); err != nil {
		return err
	}
	// The saved plan holds the root's variable values (the passphrase among them): it lives in the
	// run's scratch HOME outside the checkout (review r1) and is removed when the stack is done,
	// whatever happened.
	plan := filepath.Join(r.home, "plan-"+id+".tfplan")
	defer func() { err = errors.Join(err, removeFiles(plan)) }()
	args := []string{"plan", "-input=false", "-out=" + plan}
	for _, f := range inputs.Files {
		args = append(args, "-var-file="+f)
	}
	if err := r.tofu(ctx, child, id, dir, r.term, args...); err != nil {
		return err
	}
	// The plan document stays in memory: it carries the passphrase too.
	var js, txt bytes.Buffer
	if err := r.tofu(ctx, child, id, dir, &js, "show", "-json", plan); err != nil {
		return err
	}
	if err := r.tofu(ctx, child, id, dir, &txt, "show", "-no-color", plan); err != nil {
		return err
	}
	if err := writeRecord(filepath.Join(o.RunDir, "plan-"+id+".txt"), []byte(r.red.Redact(txt.String()))); err != nil {
		return err
	}
	// Every resource of a retained instance is retained: the generated root calls its stage as one
	// module, module.<stage with - as _> (stacks/*/_lz_main.tf).
	var retained []Retained
	if st.Chain == "retained" {
		retained = []Retained{{Instance: id, Addresses: []string{"module." + strings.ReplaceAll(in.Stage, "-", "_")}}}
	}
	if err := Protect(js.Bytes(), retained); err != nil {
		return err
	}
	if o.Verb == VerbPlan {
		return nil
	}
	if err := r.tofu(ctx, child, id, dir, r.term, "apply", "-json", "-input=false", plan); err != nil {
		return err
	}
	var out bytes.Buffer
	if err := r.tofu(ctx, child, id, dir, &out, "output", "-json"); err != nil {
		return err
	}
	if err := r.credentialFiles(in, out.Bytes()); err != nil {
		return err
	}
	if _, err := Publish(PublishOptions{Manifest: m, Instance: id, Revision: o.Revision, TofuOutput: out.Bytes(), Schemas: o.Schemas, Account: r.account, Store: store}); err != nil {
		return err
	}
	return WriteRecord(o.Records, id, Record{AppliedAt: time.Now().UTC().Format(time.RFC3339), SourceRevision: o.Revision,
		CodeDigest: r.code[id], Consumed: inputs.Consumed, Resolved: inputs.Resolved})
}

type sensitiveOut struct {
	Sensitive bool            `json:"sensitive"`
	Value     json.RawMessage `json:"value"`
}

// sensitiveOutputs carry the secrets the lane writes: each must be a sensitive output, or Publish
// (which drops only sensitive outputs) would publish it (review r1).
var sensitiveOutputs = map[string][]string{
	"account-governance": {"platform_deployer_secret", "tenant_deployer_secrets"},
	"tenant-state":       {"tenant_s3", "platform_s3"},
}

// credentialFiles writes the credential files a stack's outputs carry (data-model *Account binding
// and local files*): after account-governance the platform deployer's and each manifest tenant's
// deployer client, after a tenant-state instance its tenant's and the platform's S3 keys. Every
// value is checked before the first file is written; the tenants are exactly the manifest's.
func (r *laneRunner) credentialFiles(in stacks.Instance, out []byte) error {
	var outs map[string]sensitiveOut
	if err := json.Unmarshal(out, &outs); err != nil {
		return errors.New("tofu output -json does not decode")
	}
	for _, name := range sensitiveOutputs[in.Stage] {
		if o, ok := outs[name]; ok && !o.Sensitive {
			return fmt.Errorf("%s output %s is not sensitive: it would be published", in.Stage, name)
		}
	}
	acct := filepath.Join("accounts", r.o.Account)
	files := map[string]map[string]string{}
	switch in.Stage {
	case "account-governance":
		var pd struct {
			ClientID string `json:"client_id"`
		}
		var ps string
		var tenants map[string]struct {
			ClientID string `json:"deployer_client_id"`
		}
		var ts map[string]string
		if json.Unmarshal(outs["platform_deployer"].Value, &pd) != nil || json.Unmarshal(outs["platform_deployer_secret"].Value, &ps) != nil ||
			json.Unmarshal(outs["tenants"].Value, &tenants) != nil || json.Unmarshal(outs["tenant_deployer_secrets"].Value, &ts) != nil {
			return errors.New("account-governance outputs lack the deployer clients")
		}
		r.red.Add(ps)
		for _, s := range ts {
			r.red.Add(s)
		}
		if pd.ClientID == "" || ps == "" {
			return errors.New("account-governance outputs: the platform deployer has no client id or secret")
		}
		files[filepath.Join(acct, "platform-deployer.env")] = map[string]string{"OVH_ENDPOINT": r.account.Endpoint, "OVH_CLIENT_ID": pd.ClientID, "OVH_CLIENT_SECRET": ps}
		var names []string
		for _, t := range r.o.Manifest.Tenants {
			names = append(names, t.Name)
		}
		for t := range tenants {
			if !slices.Contains(names, t) {
				return fmt.Errorf("account-governance outputs: tenant %q is not in the manifest", t)
			}
		}
		for t := range ts {
			if !slices.Contains(names, t) {
				return fmt.Errorf("account-governance outputs: tenant %q is not in the manifest", t)
			}
		}
		for _, t := range names {
			if tenants[t].ClientID == "" || ts[t] == "" {
				return fmt.Errorf("account-governance outputs: tenant %s has no deployer client id or secret", t)
			}
			files[filepath.Join(acct, "tenants", t, "deployer.env")] = map[string]string{"OVH_ENDPOINT": r.account.Endpoint, "OVH_CLIENT_ID": tenants[t].ClientID, "OVH_CLIENT_SECRET": ts[t]}
		}
	case "tenant-state":
		type s3 struct {
			AK string `json:"access_key_id"`
			SK string `json:"secret_access_key"`
		}
		var ten, plat s3
		if json.Unmarshal(outs["tenant_s3"].Value, &ten) != nil || json.Unmarshal(outs["platform_s3"].Value, &plat) != nil {
			return errors.New("tenant-state outputs lack the S3 credentials")
		}
		r.red.Add(ten.SK, plat.SK)
		for name, k := range map[string]s3{"state.env": ten, "platform-state.env": plat} {
			if k.AK == "" || k.SK == "" {
				return fmt.Errorf("tenant-state outputs: no S3 key for %s", name)
			}
			files[filepath.Join(acct, "tenants", in.Tenant, name)] = map[string]string{"AWS_ACCESS_KEY_ID": k.AK, "AWS_SECRET_ACCESS_KEY": k.SK}
		}
	}
	for rel, values := range files {
		if err := checkCredentialValues(values); err != nil {
			return fmt.Errorf("%s outputs for %s: %w", in.Stage, rel, err)
		}
	}
	for _, rel := range slices.Sorted(maps.Keys(files)) {
		if err := WriteCredentialFile(r.o.ConfigRoot, rel, files[rel]); err != nil {
			return err
		}
	}
	return nil
}

func digestHex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
