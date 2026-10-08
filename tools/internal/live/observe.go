package live

// L7 chain observations (V010; FR-004, FR-008, FR-010, FR-011, SC-003; research R17, R20; guard
// G15): the types the collector writes to <run-dir>/observations.json, the judge that turns them
// into one assessment per assertion, the subjects the manifest requires of each assertion, the
// KD-1 canary, the run's trap and the production collector. Tests T048 (observe_test.go,
// tools/internal/probes/live/chain/, build tag live) and T062 (collect_test.go).
//
// The judge (Assess): positive assertions observe `holds` or `violated`, negative ones (a request
// that must be refused) `denied` or `allowed`; `error` or any other value is a fail (an error is
// never a pass). Every observation counts, in any order. An assertion with no observation, a
// required subject unobserved (deployer-binding: platform and tenant, P26; kd1-canary: both KD-1
// calls; and every subject the set's `expected` names, which the chain derives from the manifest,
// L7Subjects), or a name outside the L7 set is a fail. KD-1 (R20): both canary calls denied is a
// pass with a note that KD-1 did not reproduce; any allowed with shared_state_project is
// `known-deviation KD-1`, without the flag a fail; an error is a fail, also next to an allowed call.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/stacks"
)

// Observation is one observed fact of a live chain: the assertion it bears on, what was looked
// at (a bucket, a state key, an authority, a call), the observed value and a redacted detail.
// Positive assertions observe `holds` or `violated`; negative ones (a request that must be
// refused) `denied` or `allowed`; either may observe `error` when the fact could not be read.
type Observation struct {
	Assertion string `json:"assertion"`
	Subject   string `json:"subject"`
	Observed  string `json:"observed"`
	Detail    string `json:"detail,omitempty"`
}

// ObservationSet is <run-dir>/observations.json: the run, the sandbox flag the run's manifest
// carried (spec.sandbox.shared_state_project, KD-1), every observation and, by assertion, the
// subjects the manifest requires (L7Subjects; the chain fills it, the collector never does).
type ObservationSet struct {
	RunID              string              `json:"run_id"`
	SharedStateProject bool                `json:"shared_state_project"`
	Observations       []Observation       `json:"observations"`
	Expected           map[string][]string `json:"expected,omitempty"`
}

// Assessment is the judge's outcome for one assertion: pass, fail, known-deviation (with the
// deviation's id from spec *Known deviations*) or not-run (the run did not observe it), and why.
type Assessment struct {
	Assertion string `json:"assertion"`
	Outcome   string `json:"outcome"`
	Deviation string `json:"deviation,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

// The L7 assertions (tasks.md T048), sorted.
const (
	aBucketTags       = "bucket-tags"
	aDeployerBinding  = "deployer-binding"
	aKD1Canary        = "kd1-canary"
	aOutputsSchema    = "outputs-schema"
	aProjectTags      = "project-tags"
	aBucketVersioned  = "state-bucket-versioned"
	aLockContention   = "state-lock-contention"
	aObjectEncrypted  = "state-object-encrypted"
	aTenantIAMWrite   = "tenant-iam-write"
	aTenantS3Read     = "tenant-s3-read"
	kd1ID             = "KD-1"
	outcomeNotRun     = "not-run"
	outcomeKnownDev   = "known-deviation"
	canaryBulk        = "bulkDeleteObjects"
	canaryDelete      = "DELETE"
	deployerPlatform  = "platform"
	deployerTenant    = "tenant"
	observedHolds     = "holds"
	observedViolated  = "violated"
	observedDenied    = "denied"
	observedAllowed   = "allowed"
	observedError     = "error"
	versioningEnabled = "enabled"
)

var l7Assertions = []string{aBucketTags, aDeployerBinding, aKD1Canary, aOutputsSchema, aProjectTags,
	aBucketVersioned, aLockContention, aObjectEncrypted, aTenantIAMWrite, aTenantS3Read}

// negative assertions observe a request that must be refused.
var negative = map[string]bool{aTenantIAMWrite: true, aTenantS3Read: true, aKD1Canary: true}

// requiredSubjects: an assertion observed without one of these is incomplete, whatever the set
// expects (P26: both deployer classes; R20: both KD-1 calls).
var requiredSubjects = map[string][]string{aDeployerBinding: {deployerPlatform, deployerTenant}, aKD1Canary: {canaryBulk, canaryDelete}}

// Assess judges an observation set: one assessment per assertion of the L7 set, then one (a fail)
// per name the set observes that is not in it.
func Assess(set ObservationSet) []Assessment {
	by := map[string][]Observation{}
	names := slices.Clone(l7Assertions)
	for _, o := range set.Observations {
		if !slices.Contains(names, o.Assertion) {
			names = append(names, o.Assertion)
		}
		by[o.Assertion] = append(by[o.Assertion], o)
	}
	out := make([]Assessment, 0, len(names))
	for _, n := range names {
		required := slices.Clone(requiredSubjects[n])
		for _, s := range set.Expected[n] {
			if !slices.Contains(required, s) {
				required = append(required, s)
			}
		}
		out = append(out, assess(n, by[n], required, set.SharedStateProject))
	}
	return out
}

func assess(name string, obs []Observation, required []string, shared bool) Assessment {
	a := Assessment{Assertion: name, Outcome: "fail"}
	if !slices.Contains(l7Assertions, name) {
		a.Detail = "not an L7 assertion"
		return a
	}
	if len(obs) == 0 {
		a.Detail = strings.TrimSuffix("not observed: "+strings.Join(required, ", "), ": ")
		return a
	}
	var missing []string
	for _, s := range required {
		// A required subject is met by itself or a qualified one, `<subject>:<qualifier>` (the
		// tenant deployers: `tenant:<name>`).
		if !slices.ContainsFunc(obs, func(o Observation) bool { return o.Subject == s || strings.HasPrefix(o.Subject, s+":") }) {
			missing = append(missing, s)
		}
	}
	if len(missing) > 0 {
		a.Detail = "not observed: " + strings.Join(missing, ", ")
		return a
	}
	ok, gap := observedHolds, observedViolated
	if negative[name] {
		ok, gap = observedDenied, observedAllowed
	}
	var bad, gaps []string
	for _, o := range obs {
		switch o.Observed {
		case ok:
		case gap:
			gaps = append(gaps, o.Subject+": "+o.Observed)
		default:
			bad = append(bad, strings.TrimSpace(o.Subject+": "+o.Observed+" "+o.Detail))
		}
	}
	switch {
	case len(bad) > 0:
		a.Detail = strings.Join(append(bad, gaps...), "; ")
	case len(gaps) > 0 && name == aKD1Canary && shared:
		a.Outcome, a.Deviation, a.Detail = outcomeKnownDev, kd1ID, strings.Join(gaps, "; ")+" (spec.sandbox.shared_state_project)"
	case len(gaps) > 0 && name == aKD1Canary:
		a.Detail = strings.Join(gaps, "; ") + " without spec.sandbox.shared_state_project"
	case len(gaps) > 0:
		a.Detail = strings.Join(gaps, "; ")
	default:
		a.Outcome = "pass"
		if name == aKD1Canary {
			a.Detail = "KD-1 did not reproduce: the tenant deployer was denied both calls"
		}
	}
	return a
}

// KnownDeviations returns the ids of the deviations the assessments report, sorted, once each.
func KnownDeviations(as []Assessment) []string {
	var out []string
	for _, a := range as {
		if a.Outcome == outcomeKnownDev && a.Deviation != "" && !slices.Contains(out, a.Deviation) {
			out = append(out, a.Deviation)
		}
	}
	sort.Strings(out)
	return out
}

// notRun is every L7 assertion recorded as not run, for why (a partial chain, a run that ended
// before every stack was applied): never a pass.
func notRun(why string) []Assessment {
	out := make([]Assessment, 0, len(l7Assertions))
	for _, n := range l7Assertions {
		out = append(out, Assessment{Assertion: n, Outcome: outcomeNotRun, Detail: why})
	}
	return out
}

// lockRow is the instance whose state key the lock-contention assertion contends for: the first
// retained s3-backed row in run order whose state lives in a tenant bucket (where the platform and
// a tenant's deployers share a bucket), else the first retained s3-backed row. A retained row's
// state outlives the chain, and its key is not one the destroy-on-exit locks.
func lockRow(m *stacks.Manifest) (stacks.Instance, bool, error) {
	order, err := stacks.Order(m)
	if err != nil {
		return stacks.Instance{}, false, err
	}
	var first *stacks.Instance
	for _, id := range order {
		in, st, err := stageOf(m, id)
		if err != nil {
			return in, false, err
		}
		if in.Backend != "s3" || st.Chain != "retained" {
			continue
		}
		if st.Bucket == "tenant" {
			return in, true, nil
		}
		if first == nil {
			first = &in
		}
	}
	if first == nil {
		return stacks.Instance{}, false, nil
	}
	return *first, true, nil
}

// stateBuckets are the manifest's state buckets with the tenant whose bucket each is ("" for the
// account bucket): every s3 row's bucket and the bucket each row publishes to.
func stateBuckets(m *stacks.Manifest) (map[string]string, error) {
	out := map[string]string{}
	for _, in := range m.Instances {
		_, st, err := stageOf(m, in.ID)
		if err != nil {
			return nil, err
		}
		if in.Backend == "s3" {
			t := ""
			if st.Bucket == "tenant" {
				t = in.Tenant
			}
			out[in.StateBucket] = t
		}
		b, err := stacks.ArtifactBucket(m, in.ID)
		if err != nil {
			return nil, err
		}
		if _, ok := out[b]; !ok {
			out[b] = ""
		}
	}
	return out, nil
}

// deployers are the first row run by each deployer: "platform" and, by tenant, the tenant's.
func deployers(m *stacks.Manifest) (platform string, tenants map[string]string, err error) {
	tenants = map[string]string{}
	for _, in := range m.Instances {
		a, err := AuthorityOf(m, in.ID)
		if err != nil {
			return "", nil, err
		}
		switch {
		case a == AuthorityPlatform && platform == "":
			platform = in.ID
		case a == AuthorityTenant && tenants[in.Tenant] == "":
			tenants[in.Tenant] = in.ID
		}
	}
	return platform, tenants, nil
}

// L7Subjects are, by assertion, the subjects a complete observation set of the manifest names: the
// state buckets (versioning, tags), every s3 row's state object (encryption) and the lock row's
// (contention), every project row's URN (tags), every tenant with a deployer (IAM write; S3 reads
// of the account bucket and of every other tenant's bucket, as `<tenant>:<bucket>`; its deployer's
// binding, as `tenant:<tenant>`), every row's artefact (outputs schema), the platform deployer's
// binding and both KD-1 calls. Sorted.
func L7Subjects(m *stacks.Manifest, a stacks.BoundAccount) (map[string][]string, error) {
	out := map[string][]string{}
	add := func(assertion, subject string) {
		if !slices.Contains(out[assertion], subject) {
			out[assertion] = append(out[assertion], subject)
		}
	}
	buckets, err := stateBuckets(m)
	if err != nil {
		return nil, err
	}
	for b := range buckets {
		add(aBucketVersioned, b)
		add(aBucketTags, b)
	}
	for _, in := range m.Instances {
		add(aOutputsSchema, in.ID)
		if in.Backend == "s3" {
			add(aObjectEncrypted, in.StateBucket+"/"+in.StateKey)
		}
		if in.Stage == "project" {
			b, err := stacks.InstanceBinding(m, in.ID, a)
			if err != nil {
				return nil, err
			}
			add(aProjectTags, b.ProjectURN)
		}
	}
	if lock, ok, err := lockRow(m); err != nil {
		return nil, err
	} else if ok {
		add(aLockContention, lock.StateBucket+"/"+lock.StateKey)
	}
	platform, tenants, err := deployers(m)
	if err != nil {
		return nil, err
	}
	if platform != "" {
		add(aDeployerBinding, deployerPlatform)
	}
	for t := range tenants {
		add(aDeployerBinding, deployerTenant+":"+t)
		add(aTenantIAMWrite, t)
		for b, owner := range buckets {
			if owner != t {
				add(aTenantS3Read, t+":"+b)
			}
		}
	}
	add(aKD1Canary, canaryBulk)
	add(aKD1Canary, canaryDelete)
	for _, v := range out {
		sort.Strings(v)
	}
	return out, nil
}

// Trap holds the cleanups of resources a run creates outside its stacks (the KD-1 canary, the
// lock-contention lock object, a policy the tenant deployer was wrongly allowed to create); the
// chain runs it after the destroy-on-exit, before the leftover check lists.
type Trap struct {
	mu    sync.Mutex
	items []trapItem
}

type trapItem struct {
	name string
	f    func(context.Context) error
	done bool
}

// Add registers a cleanup under a name.
func (t *Trap) Add(name string, cleanup func(context.Context) error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.items = append(t.items, trapItem{name: name, f: cleanup})
}

// Names lists the registered cleanups.
func (t *Trap) Names() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]string, 0, len(t.items))
	for _, it := range t.items {
		out = append(out, it.name)
	}
	return out
}

// Run runs every registered cleanup once, the last registered first; the error names each that
// failed. A cleanup registered while Run runs waits for the next Run.
func (t *Trap) Run(ctx context.Context) error {
	t.mu.Lock()
	var todo []*trapItem
	for i := len(t.items) - 1; i >= 0; i-- {
		if !t.items[i].done {
			t.items[i].done = true
			todo = append(todo, &trapItem{name: t.items[i].name, f: t.items[i].f})
		}
	}
	t.mu.Unlock()
	var err error
	for _, it := range todo {
		if e := it.f(ctx); e != nil {
			err = errors.Join(err, fmt.Errorf("trap %s: %w", it.name, e))
		}
	}
	return err
}

// CanaryOptions configures the KD-1 canary (research R20).
type CanaryOptions struct {
	API API
	// Admin is the bootstrap authority (sandbox.env): it creates the canary and its object and
	// deletes what is left of it in the trap.
	Admin Credential
	// Tenant is the tenant deployer: the KD-1 calls (bulkDeleteObjects, DELETE) are its.
	Tenant Credential
	// Project and Region locate the state buckets (spec.state.project, resolved; the storage
	// region of the account bucket).
	Project, Region string
	Org, RunID      string
	Trap            *Trap
}

// canaryObject is the key of the canary's one object.
const canaryObject = "canary"

// post sends POST path with a JSON body and decodes the answer into out (nil: discarded).
func (b bearerClient) post(ctx context.Context, path string, in, out any) error {
	raw, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase(b.api, path)+path, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("POST %s: bad request", path)
	}
	req.Header.Set("Authorization", "Bearer "+b.tok)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	return send(b.api, req, "POST "+path, out)
}

// storageBase is the management API's storage route of a project and region.
func storageBase(project, region string) string {
	return fmt.Sprintf("/cloud/project/%s/region/%s/storage", project, region)
}

// ObserveCanary observes KD-1: a disposable canary bucket `<org>-bkt-canary-<run id, lowercase>`
// with one object (a presigned PUT: the bootstrap authority holds no S3 keys) in the state project
// and region, created by the bootstrap authority and registered for the trap once it exists and
// before the tenant's first call (never a name taken by someone else: a 409 is not ours); then the
// tenant deployer's bulkDeleteObjects and DELETE on it through the management API. Each call
// observes `denied` (403), `allowed` (2xx that did what it asked) or `error` (anything else). A
// canary not set up observes `error` twice and the tenant makes no call.
func ObserveCanary(ctx context.Context, o CanaryOptions) []Observation {
	name := strings.ToLower(o.Org + "-bkt-canary-" + o.RunID)
	base := storageBase(o.Project, o.Region)
	both := func(observed, bulk, del string) []Observation {
		return []Observation{{Assertion: aKD1Canary, Subject: canaryBulk, Observed: observed, Detail: bulk},
			{Assertion: aKD1Canary, Subject: canaryDelete, Observed: observed, Detail: del}}
	}
	notSetUp := func(why string) []Observation {
		return both(observedError, "canary not set up: "+why, "canary not set up: "+why)
	}
	admin, err := newBearer(ctx, o.API, o.Admin)
	if err != nil {
		return notSetUp(err.Error())
	}
	if err := admin.post(ctx, base, map[string]any{"name": name}, nil); err != nil {
		return notSetUp(err.Error())
	}
	o.Trap.Add(name, func(ctx context.Context) error { return deleteCanary(ctx, o, base, name) })
	var ps struct {
		URL string `json:"url"`
	}
	if err := admin.post(ctx, base+"/"+name+"/presign", map[string]any{"method": http.MethodPut, "object": canaryObject, "expire": 600}, &ps); err != nil {
		return notSetUp(err.Error())
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, ps.URL, strings.NewReader("lz canary "+o.RunID))
	if err != nil || ps.URL == "" {
		return notSetUp("presigned URL unusable")
	}
	resp, err := o.API.client().Do(req)
	if err != nil {
		return notSetUp("object upload failed")
	}
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return notSetUp(fmt.Sprintf("object upload answered %d", resp.StatusCode))
	}
	tenant, err := newBearer(ctx, o.API, o.Tenant)
	if err != nil {
		return both(observedError, err.Error(), err.Error())
	}
	var bulk struct {
		Deleted []json.RawMessage `json:"deleted"`
	}
	err = tenant.post(ctx, base+"/"+name+"/bulkDeleteObjects", map[string]any{"objects": []map[string]string{{"key": canaryObject}}}, &bulk)
	out := []Observation{refusal(aKD1Canary, canaryBulk, err, err == nil && len(bulk.Deleted) > 0)}
	err = tenant.delete(ctx, base+"/"+name)
	return append(out, refusal(aKD1Canary, canaryDelete, err, err == nil))
}

// refusal observes a request that must be refused: `denied` on 403, `allowed` when it did what it
// asked, `error` otherwise (5xx, 409, a 2xx without effect).
func refusal(assertion, subject string, err error, did bool) Observation {
	o := Observation{Assertion: assertion, Subject: subject, Observed: observedError}
	switch {
	case statusOf(err) == http.StatusForbidden:
		o.Observed, o.Detail = observedDenied, "403"
	case did:
		o.Observed = observedAllowed
	case err != nil:
		o.Detail = err.Error()
	default:
		o.Detail = "accepted without effect"
	}
	return o
}

// deleteCanary is the canary's trap: with the bootstrap authority, its objects first, then the
// bucket; content when it is gone.
func deleteCanary(ctx context.Context, o CanaryOptions, base, name string) error {
	admin, err := newBearer(ctx, o.API, o.Admin)
	if err != nil {
		return err
	}
	var c struct {
		Objects []struct {
			Key string `json:"key"`
		} `json:"objects"`
	}
	err = admin.get(ctx, base+"/"+name, &c)
	if statusOf(err) == http.StatusNotFound {
		return nil
	}
	if err != nil {
		return err
	}
	if len(c.Objects) > 0 {
		keys := make([]map[string]string, 0, len(c.Objects))
		for _, k := range c.Objects {
			keys = append(keys, map[string]string{"key": k.Key})
		}
		if err := admin.post(ctx, base+"/"+name+"/bulkDeleteObjects", map[string]any{"objects": keys}, nil); err != nil {
			return fmt.Errorf("canary %s: %w", name, err)
		}
	}
	if err := admin.delete(ctx, base+"/"+name); err != nil {
		return fmt.Errorf("canary %s: %w", name, err)
	}
	return nil
}

// ObserveRun is what the chain hands its observer, once every stack of the run is applied.
type ObserveRun struct {
	RunID, RunDir string
	Manifest      *stacks.Manifest
	Trap          *Trap
}

// Observer collects a chain's L7 observations (ChainOptions.Observer; nil: the collector).
type Observer interface {
	Observe(ctx context.Context, run ObserveRun) ([]Observation, error)
}

// objectCreator writes an object only if none exists (S3 If-None-Match: *); the lock-contention
// assertion creates its lock object through it.
type objectCreator interface {
	PutNew(bucket, key string, data []byte) error
}

// lockWriter runs the second writer of the lock-contention assertion on one instance's root.
type lockWriter interface {
	// Prepare readies the second writer (credential bound, inputs, init) before the lock exists.
	Prepare(ctx context.Context, id string) error
	// Plan is the second writer: a plan that must not wait for the lock (-lock-timeout=0s). It
	// returns the call's redacted output and its error.
	Plan(ctx context.Context, id string) (string, error)
}

// collector is the production Observer (ChainOptions.Observer nil): it reads with the bootstrap
// authority (versioning and tags of the state buckets, tags of the project URNs), each row's own
// S3 keys (its state object and artefact), and calls as each deployer (binding; the tenant
// deployer's IAM write, its S3 reads and the KD-1 canary). An unreadable fact is an `error`
// observation, never a missing one; the collector fails only when the manifest cannot be read.
type collector struct {
	API     API
	Admin   Credential          // the bootstrap authority (sandbox.env)
	Binding Binding             // the bound account (account.env)
	Account stacks.BoundAccount // its project references
	// Creds returns instance id's authority's five variables (credentials.go).
	Creds func(id string) (map[string]string, error)
	// Store opens the state buckets with one authority's S3 keys.
	Store   func(keys map[string]string) (ObjectStore, error)
	Schemas fs.FS // schemas/outputs
	Writer  lockWriter
	Now     func() time.Time // nil: time.Now
}

func oauthOf(creds map[string]string) Credential {
	return Credential{Endpoint: creds["OVH_ENDPOINT"], ClientID: creds["OVH_CLIENT_ID"], ClientSecret: creds["OVH_CLIENT_SECRET"]}
}

func errorObs(assertion, subject string, err error) Observation {
	return Observation{Assertion: assertion, Subject: subject, Observed: observedError, Detail: err.Error()}
}

// Observe collects every L7 assertion of the run's manifest.
func (c *collector) Observe(ctx context.Context, run ObserveRun) ([]Observation, error) {
	m := run.Manifest
	state, err := c.Account.Binding(m.StateProjectRef)
	if err != nil {
		return nil, err
	}
	region := strings.ToUpper(m.StateRegion)
	buckets, err := stateBuckets(m)
	if err != nil {
		return nil, err
	}
	platform, tenants, err := deployers(m)
	if err != nil {
		return nil, err
	}
	tenantNames := slices.Sorted(maps.Keys(tenants))
	var obs []Observation
	obs = append(obs, c.buckets(ctx, state.ProjectID, region, buckets)...)
	obs = append(obs, c.stateObjects(m)...)
	obs = append(obs, c.lockContention(ctx, run, state.ProjectID, region)...)
	obs = append(obs, c.projectTags(ctx, m)...)
	obs = append(obs, c.outputs(m)...)
	obs = append(obs, c.bindings(ctx, m, platform, tenants, tenantNames)...)
	for _, t := range tenantNames {
		obs = append(obs, c.tenantIAMWrite(ctx, run, m, t, tenants[t]))
		obs = append(obs, c.tenantS3Read(m, t, tenants[t], buckets)...)
	}
	canary := CanaryOptions{API: c.API, Admin: c.Admin, Project: state.ProjectID, Region: region, Org: m.Org, RunID: run.RunID, Trap: run.Trap}
	if len(tenantNames) == 0 {
		obs = append(obs, ObserveCanary(ctx, canary)...) // no tenant deployer: the canary's tenant calls fail
	} else if creds, err := c.Creds(tenants[tenantNames[0]]); err != nil {
		obs = append(obs, errorObs(aKD1Canary, canaryBulk, err), errorObs(aKD1Canary, canaryDelete, err))
	} else {
		canary.Tenant = oauthOf(creds)
		obs = append(obs, ObserveCanary(ctx, canary)...)
	}
	if ctx.Err() != nil {
		return obs, context.Cause(ctx)
	}
	return obs, nil
}

// mandatoryTags reports what the data-model *Label set* misses in tags: lz:managed-by opentofu,
// lz:managed-in, lz:instance and lz:release set, lz:tenant the tenant's name (absent at account
// scope, tenant "").
func mandatoryTags(tags map[string]string, tenant string) []string {
	var miss []string
	if tags["lz:managed-by"] != "opentofu" {
		miss = append(miss, fmt.Sprintf("lz:managed-by %q", tags["lz:managed-by"]))
	}
	for _, k := range []string{"lz:managed-in", "lz:instance", "lz:release"} {
		if tags[k] == "" {
			miss = append(miss, k+" unset")
		}
	}
	if v, ok := tags["lz:tenant"]; tenant == "" && ok {
		miss = append(miss, fmt.Sprintf("lz:tenant %q at account scope", v))
	} else if tenant != "" && v != tenant {
		miss = append(miss, fmt.Sprintf("lz:tenant %q, want %q", v, tenant))
	}
	return miss
}

// buckets observes each state bucket's versioning and tags (GET …/storage/{name}: versioning.status,
// tags; kb/api/v1/cloud.json cloud.StorageContainer) with the bootstrap authority.
func (c *collector) buckets(ctx context.Context, project, region string, buckets map[string]string) []Observation {
	var out []Observation
	admin, aerr := newBearer(ctx, c.API, c.Admin)
	for _, b := range slices.Sorted(maps.Keys(buckets)) {
		var doc struct {
			Versioning *struct {
				Status string `json:"status"`
			} `json:"versioning"`
			Tags map[string]string `json:"tags"`
		}
		err := aerr
		if err == nil {
			err = admin.get(ctx, storageBase(project, region)+"/"+b, &doc)
		}
		if err != nil {
			out = append(out, errorObs(aBucketVersioned, b, err), errorObs(aBucketTags, b, err))
			continue
		}
		v := Observation{Assertion: aBucketVersioned, Subject: b, Observed: observedHolds, Detail: "versioning.status enabled"}
		if doc.Versioning == nil || doc.Versioning.Status != versioningEnabled {
			status := "absent"
			if doc.Versioning != nil {
				status = fmt.Sprintf("%q", doc.Versioning.Status)
			}
			v.Observed, v.Detail = observedViolated, "versioning.status "+status
		}
		t := Observation{Assertion: aBucketTags, Subject: b, Observed: observedHolds}
		if miss := mandatoryTags(doc.Tags, buckets[b]); len(miss) > 0 {
			t.Observed, t.Detail = observedViolated, strings.Join(miss, "; ")
		}
		out = append(out, v, t)
	}
	return out
}

// stateObjects observes each s3 row's state object with the row's own S3 keys: an OpenTofu
// encryption envelope (`encrypted_data`, no `resources`: capture p4-encryption.txt, evidence/T007.md
// P4) holds; anything else that decodes is plaintext.
func (c *collector) stateObjects(m *stacks.Manifest) []Observation {
	var out []Observation
	for _, in := range m.Instances {
		if in.Backend != "s3" {
			continue
		}
		subject := in.StateBucket + "/" + in.StateKey
		raw, err := c.read(in.ID, in.StateBucket, in.StateKey)
		if err != nil {
			out = append(out, errorObs(aObjectEncrypted, subject, err))
			continue
		}
		var doc map[string]json.RawMessage
		var env struct {
			Data *string `json:"encrypted_data"`
		}
		o := Observation{Assertion: aObjectEncrypted, Subject: subject, Observed: observedHolds, Detail: "encryption envelope"}
		switch {
		case json.Unmarshal(raw, &doc) != nil:
			o.Observed, o.Detail = observedError, "the state object does not decode as JSON"
		case doc["resources"] != nil:
			o.Observed, o.Detail = observedViolated, "plaintext state: resources in the object"
		case json.Unmarshal(raw, &env) != nil || env.Data == nil || *env.Data == "":
			o.Observed, o.Detail = observedViolated, "no encryption envelope: encrypted_data is not a non-empty string"
		}
		out = append(out, o)
	}
	return out
}

// read reads one object with instance id's authority's S3 keys.
func (c *collector) read(id, bucket, key string) ([]byte, error) {
	creds, err := c.Creds(id)
	if err != nil {
		return nil, err
	}
	st, err := c.Store(s3Of(creds))
	if err != nil {
		return nil, err
	}
	return st.Get(bucket, key)
}

// lockInfo is the lock object OpenTofu's S3 backend writes at <key>.tflock with use_lockfile
// (the backend's lock info: ID, Operation, Info, Who, Version, Created, Path; believed from the
// OpenTofu statemgr.LockInfo type, UNVERIFIED live until T049).
type lockInfo struct {
	ID        string `json:"ID"`
	Operation string `json:"Operation"`
	Info      string `json:"Info"`
	Who       string `json:"Who"`
	Version   string `json:"Version"`
	Created   string `json:"Created"`
	Path      string `json:"Path"`
}

// lockContention observes the lock row's state key under contention: the second writer is readied,
// a holder's lock object is written beside the state with the row's S3 keys and read back (listed),
// and only then the second writer plans with -lock-timeout=0s. Refused naming the holder's lock id
// holds; a plan that ran violates; any other failure is an error (a state that does not decrypt is
// also reported under "Error acquiring the state lock", evidence/T007.md P4, so only the holder's
// id counts). The lock object is removed with the bootstrap authority (bulkDeleteObjects of exactly
// that key, only while it holds this run's lock id) right after, and by the trap if the run stops
// first; it is never another writer's.
func (c *collector) lockContention(ctx context.Context, run ObserveRun, project, region string) []Observation {
	in, ok, err := lockRow(run.Manifest)
	if err != nil || !ok {
		if err == nil {
			err = errors.New("no retained s3-backed instance")
		}
		return []Observation{errorObs(aLockContention, "", err)}
	}
	subject := in.StateBucket + "/" + in.StateKey
	fail := func(err error) []Observation { return []Observation{errorObs(aLockContention, subject, err)} }
	creds, err := c.Creds(in.ID)
	if err != nil {
		return fail(err)
	}
	st, err := c.Store(s3Of(creds))
	if err != nil {
		return fail(err)
	}
	if err := c.Writer.Prepare(ctx, in.ID); err != nil {
		return fail(fmt.Errorf("second writer not ready: %w", err))
	}
	key := in.StateKey + ".tflock"
	creator, ok := st.(objectCreator)
	if !ok {
		return fail(errors.New("the state store cannot create an object only if it is absent"))
	}
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	info := lockInfo{ID: "lz-l7-" + strings.ToLower(run.RunID), Operation: "OperationTypePlan", Info: "lz-live L7 lock contention, run " + run.RunID,
		Who: "lz-live", Created: now().UTC().Format(time.RFC3339), Path: subject}
	raw, err := json.Marshal(info)
	if err != nil {
		return fail(err)
	}
	release := func(ctx context.Context) error {
		got, err := st.Get(in.StateBucket, key)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		var held lockInfo
		if json.Unmarshal(got, &held) != nil || held.ID != info.ID {
			return nil // another writer's lock: never ours to remove
		}
		admin, err := newBearer(ctx, c.API, c.Admin)
		if err != nil {
			return err
		}
		var res struct {
			Deleted []struct {
				Key string `json:"key"`
			} `json:"deleted"`
		}
		if err := admin.post(ctx, storageBase(project, region)+"/"+in.StateBucket+"/bulkDeleteObjects",
			map[string]any{"objects": []map[string]string{{"key": key}}}, &res); err != nil {
			return fmt.Errorf("lock object %s/%s: %w", in.StateBucket, key, err)
		}
		if !slices.ContainsFunc(res.Deleted, func(d struct {
			Key string `json:"key"`
		}) bool {
			return d.Key == key
		}) {
			return fmt.Errorf("lock object %s/%s not deleted", in.StateBucket, key)
		}
		return nil
	}
	run.Trap.Add("lock "+subject, release)
	// Created only if absent (If-None-Match: *, as OpenTofu's use_lockfile does): never another
	// writer's lock overwritten, also when it locks between any check and this write.
	if err := creator.PutNew(in.StateBucket, key, raw); statusOf(err) == http.StatusPreconditionFailed {
		return fail(fmt.Errorf("%s/%s exists: another writer holds the state", in.StateBucket, key))
	} else if err != nil {
		return fail(err)
	}
	if got, err := st.Get(in.StateBucket, key); err != nil || !bytes.Equal(got, raw) {
		return fail(fmt.Errorf("the lock object %s/%s is not listed as written", in.StateBucket, key))
	}
	text, perr := c.Writer.Plan(ctx, in.ID)
	o := Observation{Assertion: aLockContention, Subject: subject}
	switch {
	case perr == nil:
		o.Observed, o.Detail = observedViolated, "the second writer planned while the lock object was held"
	case strings.Contains(text, "Error acquiring the state lock") && strings.Contains(text, info.ID):
		o.Observed, o.Detail = observedHolds, "second writer refused: the state is locked by "+info.ID
	default:
		o.Observed, o.Detail = observedError, "second writer failed without naming the held lock: "+lastLine(text)
	}
	if err := release(ctx); err != nil {
		o.Detail += "; release: " + err.Error() + " (the trap retries)"
	}
	return []Observation{o}
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// projectTags observes each project row's URN tags (GET /v2/iam/resource/{urn}, kb/api/v2/iam.json
// iam.resource.Resource.tags) with the bootstrap authority.
func (c *collector) projectTags(ctx context.Context, m *stacks.Manifest) []Observation {
	var out []Observation
	admin, aerr := newBearer(ctx, c.API, c.Admin)
	for _, in := range m.Instances {
		if in.Stage != "project" {
			continue
		}
		b, err := stacks.InstanceBinding(m, in.ID, c.Account)
		if err != nil {
			out = append(out, errorObs(aProjectTags, in.ID, err))
			continue
		}
		var doc struct {
			Tags map[string]string `json:"tags"`
		}
		if err = aerr; err == nil {
			err = admin.get(ctx, "/iam/resource/"+b.ProjectURN, &doc)
		}
		if err != nil {
			out = append(out, errorObs(aProjectTags, b.ProjectURN, err))
			continue
		}
		o := Observation{Assertion: aProjectTags, Subject: b.ProjectURN, Observed: observedHolds}
		if miss := mandatoryTags(doc.Tags, in.Tenant); len(miss) > 0 {
			o.Observed, o.Detail = observedViolated, strings.Join(miss, "; ")
		}
		out = append(out, o)
	}
	return out
}

// outputs observes each row's published artefact, read with the row's own S3 keys and validated as
// its consumers validate it (stacks.ValidateEnvelope against its expectation).
func (c *collector) outputs(m *stacks.Manifest) []Observation {
	var out []Observation
	for _, in := range m.Instances {
		want, err := stacks.ExpectationOf(m, in.ID, c.Account)
		var raw []byte
		if err == nil {
			var bucket string
			if bucket, err = stacks.ArtifactBucket(m, in.ID); err == nil {
				raw, err = c.read(in.ID, bucket, stacks.ArtifactKey(in.ID))
			}
		}
		if err != nil {
			out = append(out, errorObs(aOutputsSchema, in.ID, err))
			continue
		}
		o := Observation{Assertion: aOutputsSchema, Subject: in.ID, Observed: observedHolds}
		if _, err := stacks.ValidateEnvelope(c.Schemas, raw, want); err != nil {
			o.Observed, o.Detail = observedViolated, err.Error()
		}
		out = append(out, o)
	}
	return out
}

// bindings observes the platform and each tenant deployer binding to the bound account through
// GET /auth/details (P26): bound holds, a refusal (another account, endpoint or org) violates.
func (c *collector) bindings(ctx context.Context, m *stacks.Manifest, platform string, tenants map[string]string, names []string) []Observation {
	type who struct{ subject, id, detail string }
	var ws []who
	if platform != "" {
		ws = append(ws, who{deployerPlatform, platform, "platform deployer"})
	}
	for _, t := range names {
		ws = append(ws, who{deployerTenant + ":" + t, tenants[t], "tenant " + t + " deployer"})
	}
	var out []Observation
	for _, w := range ws {
		creds, err := c.Creds(w.id)
		if err != nil {
			out = append(out, errorObs(aDeployerBinding, w.subject, err))
			continue
		}
		o := Observation{Assertion: aDeployerBinding, Subject: w.subject, Observed: observedHolds, Detail: w.detail + " bound to " + c.Binding.AccountID}
		var refusal *Refusal
		switch err := Bind(ctx, c.API, oauthOf(creds), c.Binding, m.Org); {
		case errors.As(err, &refusal):
			o.Observed, o.Detail = observedViolated, w.detail+": "+err.Error()
		case err != nil:
			o.Observed, o.Detail = observedError, w.detail+": "+err.Error()
		}
		out = append(out, o)
	}
	return out
}

// tenantIAMWrite observes tenant t's deployer creating an IAM policy (POST /v2/iam/policy,
// account:apiovh:iam/policy/create, which its policy does not grant): 403 denied; a created policy
// is allowed, and the trap deletes it with the bootstrap authority.
func (c *collector) tenantIAMWrite(ctx context.Context, run ObserveRun, m *stacks.Manifest, t, id string) Observation {
	creds, err := c.Creds(id)
	if err != nil {
		return errorObs(aTenantIAMWrite, t, err)
	}
	b, err := stacks.InstanceBinding(m, id, c.Account)
	if err != nil {
		return errorObs(aTenantIAMWrite, t, err)
	}
	tenant, err := newBearer(ctx, c.API, oauthOf(creds))
	if err != nil {
		return errorObs(aTenantIAMWrite, t, err)
	}
	body := map[string]any{
		"name":        strings.ToLower(m.Org + "-l7-iam-write-" + run.RunID),
		"description": "lz-live L7 negative: a tenant deployer must not write IAM (run " + run.RunID + ")",
		"identities":  []string{},
		"resources":   []map[string]string{{"urn": b.ProjectURN}},
		"permissions": map[string]any{"allow": []map[string]string{{"action": "publicCloudProject:apiovh:region/storage/get"}}},
	}
	var created struct {
		ID string `json:"id"`
	}
	name := body["name"].(string)
	// The cleanup is registered before the request (review r2: a policy created behind an answer
	// that does not decode, or a dropped connection, must not stay): the trap deletes it with the
	// bootstrap authority, by id, or by its name when no id came back; nothing found, nothing done.
	run.Trap.Add("policy "+name, func(ctx context.Context) error {
		admin, err := newBearer(ctx, c.API, c.Admin)
		if err != nil {
			return err
		}
		ids := []string{created.ID}
		if created.ID == "" {
			all, err := getAll[struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}](ctx, admin, "/iam/policy")
			if err != nil {
				return err
			}
			ids = nil
			for _, p := range all {
				if p.Name == name {
					ids = append(ids, p.ID)
				}
			}
		}
		for _, id := range ids {
			if err := admin.delete(ctx, "/iam/policy/"+id); err != nil && statusOf(err) != http.StatusNotFound {
				return err
			}
		}
		return nil
	})
	err = tenant.post(ctx, "/iam/policy", body, &created)
	o := refusal(aTenantIAMWrite, t, err, err == nil)
	if o.Observed == observedAllowed {
		o.Detail = "POST /v2/iam/policy created policy " + name
	}
	return o
}

// tenantS3Read observes tenant t's S3 keys reading the account bucket (the account-bootstrap
// artefact) and every other tenant's bucket (an object the tenant's own rows hold there): a 403
// is denied, a read allowed, anything else (a 404 included: the read was not refused) an error.
func (c *collector) tenantS3Read(m *stacks.Manifest, t, id string, buckets map[string]string) []Observation {
	targets := map[string]string{} // bucket -> key
	for _, in := range m.Instances {
		bucket, err := stacks.ArtifactBucket(m, in.ID)
		if err != nil {
			continue
		}
		if owner, ok := buckets[bucket]; ok && owner != t {
			if _, seen := targets[bucket]; !seen {
				targets[bucket] = stacks.ArtifactKey(in.ID)
			}
		}
	}
	var out []Observation
	for _, b := range slices.Sorted(maps.Keys(buckets)) {
		if buckets[b] == t {
			continue
		}
		subject := t + ":" + b
		key, ok := targets[b]
		if !ok {
			out = append(out, errorObs(aTenantS3Read, subject, errors.New("no object of the bucket to read")))
			continue
		}
		_, err := c.read(id, b, key)
		o := refusal(aTenantS3Read, subject, err, err == nil)
		if o.Observed == observedAllowed {
			o.Detail = "read " + b + "/" + key
		}
		out = append(out, o)
	}
	return out
}
