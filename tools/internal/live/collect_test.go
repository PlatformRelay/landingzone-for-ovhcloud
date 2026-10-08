package live

// T062: the L7 collector and the chain's judgement of a complete set (spec 005 FR-004, FR-008,
// FR-010, FR-011, SC-003; contracts/checks.md V010; research R17, R20; coordinator decisions
// 2026-10-08):
//
//  1. `chain -- <instance>` does not observe: every assertion is recorded `not-run`, never `pass`.
//  2. Every one of the ten assertions is collected by the full chain, and an assertion whose
//     required subjects are not all observed is `fail`: the chain judges the set against the
//     subjects the manifest requires (L7Subjects), whichever observer collected it.
//
// The collector (TestCollect*) runs against the canary fake of observe_test.go (the storage routes
// of kb/api/v1/cloud.json, each route's IAM action enforced on the fake credentials' policies),
// extended with GET /auth/details (P26), GET /v2/iam/resource/{urn} (iam.resource.Resource.tags)
// and POST/DELETE /v2/iam/policy (account:apiovh:iam/policy/create, …/delete), a fake S3 store
// over the same buckets that admits each bucket's own keys only (403 otherwise), and a fake second
// writer that is refused exactly while a lock object is held, naming its id (as OpenTofu's S3
// backend with use_lockfile; UNVERIFIED live until T049).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/stacks"
)

func sandboxManifest(t *testing.T) *stacks.Manifest {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(laneRepoRoot, stacks.ManifestPath))
	if err != nil {
		t.Fatal(err)
	}
	m, err := stacks.DecodeManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// sandboxAccount binds STATE and DEMO_DEV to one project (KD-1).
func sandboxAccount(project string) stacks.BoundAccount {
	return stacks.BoundAccount{Endpoint: "ovh-eu", ProjectIDs: map[string]string{"STATE": project, "DEMO_DEV": project}}
}

// TestL7Subjects pins what a complete set of the sandbox manifest names: both state buckets, the
// five s3 rows' state objects, the project row's state key for contention, the project URN, the
// demo tenant (IAM write; S3 read of the account bucket), the six rows' artefacts, both deployer
// classes and both KD-1 calls.
func TestL7Subjects(t *testing.T) {
	got, err := L7Subjects(sandboxManifest(t), sandboxAccount(laneProject))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"state-bucket-versioned": {"lz-bkt-state", "lz-demo-bkt-state"},
		"bucket-tags":            {"lz-bkt-state", "lz-demo-bkt-state"},
		"state-object-encrypted": {"lz-bkt-state/account/account-governance/terraform.tfstate", "lz-bkt-state/account/tenant-state/demo/terraform.tfstate",
			"lz-demo-bkt-state/tenants/demo/dev/gra11/project-network/terraform.tfstate", "lz-demo-bkt-state/tenants/demo/dev/gra11/runtime/terraform.tfstate",
			"lz-demo-bkt-state/tenants/demo/dev/project/terraform.tfstate"},
		"state-lock-contention": {"lz-demo-bkt-state/tenants/demo/dev/project/terraform.tfstate"},
		"project-tags":          {"urn:v1:eu:resource:publicCloudProject:" + laneProject},
		"tenant-iam-write":      {"demo"},
		"tenant-s3-read":        {"demo:lz-bkt-state"},
		"outputs-schema":        {"account-bootstrap", "account-governance", "demo-dev-gra11-network", "demo-dev-gra11-runtime", "demo-dev-project", "demo-state"},
		"deployer-binding":      {"platform", "tenant:demo"},
		"kd1-canary":            {"DELETE", "bulkDeleteObjects"},
	}
	if len(got) != len(want) {
		t.Errorf("subjects for %d assertions, want %d: %v", len(got), len(want), got)
	}
	for a, subs := range want {
		if !slices.Equal(got[a], subs) {
			t.Errorf("%s: subjects %v, want %v", a, got[a], subs)
		}
	}
}

// completeSet observes every expected subject with the value that passes.
func completeSet(exp map[string][]string) []Observation {
	var out []Observation
	for _, a := range slices.Sorted(mapKeys(exp)) {
		ok := "holds"
		if a == "tenant-iam-write" || a == "tenant-s3-read" || a == "kd1-canary" {
			ok = "denied"
		}
		for _, s := range exp[a] {
			out = append(out, Observation{Assertion: a, Subject: s, Observed: ok})
		}
	}
	return out
}

// Decision 2 at the judge: with the manifest's subjects as the set's `expected`, an assertion
// missing the observation of any one of them fails (naming it), and only that assertion; an
// expected subject observed `error` fails too.
func TestAssessExpected(t *testing.T) {
	exp, err := L7Subjects(sandboxManifest(t), sandboxAccount(laneProject))
	if err != nil || len(exp) != 10 {
		t.Fatalf("L7Subjects: %v, %d assertions, want the ten", err, len(exp))
	}
	base := ObservationSet{RunID: canaryRunID, SharedStateProject: true, Observations: completeSet(exp), Expected: exp}
	for _, a := range Assess(base) {
		if a.Outcome != "pass" {
			t.Errorf("complete set: %s %s (%s), want pass", a.Assertion, a.Outcome, a.Detail)
		}
	}
	for _, assertion := range slices.Sorted(mapKeys(exp)) {
		for _, subject := range exp[assertion] {
			t.Run(assertion+"/"+subject, func(t *testing.T) {
				set := base
				set.Observations = slices.DeleteFunc(slices.Clone(base.Observations), func(o Observation) bool {
					return o.Assertion == assertion && o.Subject == subject
				})
				for _, a := range Assess(set) {
					switch {
					case a.Assertion == assertion && (a.Outcome != "fail" || !strings.Contains(a.Detail, subject)):
						t.Errorf("%s without %s observed: %s (%s), want fail naming it", assertion, subject, a.Outcome, a.Detail)
					case a.Assertion != assertion && a.Outcome != "pass":
						t.Errorf("%s: %s (%s), want pass (only %s lost a subject)", a.Assertion, a.Outcome, a.Detail, assertion)
					}
				}
				set.Observations = slices.Clone(base.Observations)
				for i, o := range set.Observations {
					if o.Assertion == assertion && o.Subject == subject {
						set.Observations[i].Observed = "error"
					}
				}
				for _, a := range Assess(set) {
					if a.Assertion == assertion && a.Outcome != "fail" {
						t.Errorf("%s with %s observed error: %s, want fail", assertion, subject, a.Outcome)
					}
				}
			})
		}
	}
}

// Per-tenant deployer bindings (review r1): with two tenants expected, a set missing one tenant's
// binding fails although the other tenant's is observed; a qualified subject `tenant:<name>`
// meets the fixed requirement `tenant`.
func TestAssessTenantBindings(t *testing.T) {
	exp := map[string][]string{"deployer-binding": {"platform", "tenant:a", "tenant:b"}}
	obs := []Observation{{Assertion: "deployer-binding", Subject: "platform", Observed: "holds"},
		{Assertion: "deployer-binding", Subject: "tenant:a", Observed: "holds"}, {Assertion: "deployer-binding", Subject: "tenant:b", Observed: "holds"}}
	judge := func(obs []Observation) Assessment {
		for _, a := range Assess(ObservationSet{Observations: obs, Expected: exp}) {
			if a.Assertion == "deployer-binding" {
				return a
			}
		}
		return Assessment{}
	}
	if a := judge(obs); a.Outcome != "pass" {
		t.Errorf("both tenants bound: %s (%s), want pass", a.Outcome, a.Detail)
	}
	if a := judge(obs[:2]); a.Outcome != "fail" || !strings.Contains(a.Detail, "tenant:b") {
		t.Errorf("tenant b unobserved: %s (%s), want fail naming tenant:b", a.Outcome, a.Detail)
	}
	if a := judge([]Observation{obs[0]}); a.Outcome != "fail" {
		t.Errorf("no tenant observed: %s, want fail", a.Outcome)
	}
}

// chainAssertions reads summary.json's assertions.
func chainAssertions(t *testing.T, runDir string) map[string]Assessment {
	t.Helper()
	var s struct {
		Assertions []Assessment `json:"assertions"`
	}
	raw, err := os.ReadFile(filepath.Join(runDir, "summary.json"))
	if err != nil || json.Unmarshal(raw, &s) != nil {
		t.Errorf("summary.json unreadable (%v)", err)
	}
	out := map[string]Assessment{}
	for _, a := range s.Assertions {
		if _, dup := out[a.Assertion]; dup {
			t.Errorf("summary.json lists %s twice", a.Assertion)
		}
		out[a.Assertion] = a
	}
	if len(out) != len(l7Names) {
		t.Errorf("summary.json lists %d assertions, want the ten: %v", len(out), slices.Sorted(mapKeys(out)))
	}
	return out
}

var l7Names = []string{"bucket-tags", "deployer-binding", "kd1-canary", "outputs-schema", "project-tags", "state-bucket-versioned",
	"state-lock-contention", "state-object-encrypted", "tenant-iam-write", "tenant-s3-read"}

// Decision 1: `chain -- <instance>` applies and destroys as a chain does but does not observe:
// the observer is never called, no observations.json is written, every assertion is recorded
// `not-run` with the reason, and the exit code is the chain's.
func TestObserveChainPartial(t *testing.T) {
	h := newChainHarness(t)
	h.steady()
	h.touch("demo-dev-gra11-runtime")
	o := &chainObserver{obs: chainPassing()}
	r := h.chain(chainExec{target: "demo-dev-gra11-runtime", observer: o})
	laneExit(t, r.laneRun, 0)
	if got := r.applies(false); !slices.Equal(got, []string{"demo-dev-gra11-runtime"}) {
		t.Fatalf("applied %v, want the runtime only (a partial chain)", got)
	}
	if len(o.runs) != 0 || len(r.events("observe")) != 0 {
		t.Errorf("observer called %d times under -- <instance>: a partial chain does not observe", len(o.runs))
	}
	if _, err := os.Stat(filepath.Join(r.runDir, "observations.json")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("observations.json written by a partial chain (%v)", err)
	}
	got := chainAssertions(t, r.runDir)
	for _, name := range l7Names {
		a, ok := got[name]
		if !ok || a.Outcome != "not-run" || !strings.Contains(a.Detail, "-- demo-dev-gra11-runtime") {
			t.Errorf("%s: %+v, want not-run naming the partial chain", name, a)
		}
	}
	chainSummaryLine(t, h, r.runID, "pass", "none")
}

// summary.json records every assertion the chain judged: a full chain's outcomes; a chain that
// ended before observing (an apply failed), every assertion not-run, never pass.
func TestObserveChainAssertions(t *testing.T) {
	t.Run("judged", func(t *testing.T) {
		h := newChainHarness(t)
		r := h.chain(chainExec{observer: &chainObserver{obs: chainWith(func(o Observation) *Observation {
			if o.Assertion == "kd1-canary" {
				o.Observed = "allowed"
			}
			return &o
		})}})
		laneExit(t, r.laneRun, 0)
		got := chainAssertions(t, r.runDir)
		for _, name := range l7Names {
			want := "pass"
			if name == "kd1-canary" {
				want = "known-deviation"
			}
			if got[name].Outcome != want {
				t.Errorf("%s: %+v, want %s", name, got[name], want)
			}
		}
	})
	for _, tc := range []struct {
		name, reason string
		obs          *chainObserver
		sig          bool
	}{
		{name: "collector-failed", reason: "collector failed", obs: &chainObserver{err: errors.New("collector: boom")}},
		{name: "interrupted", reason: "interrupt", obs: &chainObserver{}, sig: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newChainHarness(t)
			c := chainExec{observer: tc.obs}
			if tc.sig {
				c.signalAtObserve = syscall.SIGINT
			}
			r := h.chain(c)
			laneExit(t, r.laneRun, 1)
			for name, a := range chainAssertions(t, r.runDir) {
				if a.Outcome != "not-run" || !strings.Contains(a.Detail, tc.reason) {
					t.Errorf("%s: %+v, want not-run naming %q", name, a, tc.reason)
				}
			}
		})
	}
	t.Run("apply-failed", func(t *testing.T) {
		h := newChainHarness(t)
		h.setApplyExit("demo-dev-gra11-runtime", 1)
		r := h.chain(chainExec{})
		laneExit(t, r.laneRun, 1)
		for name, a := range chainAssertions(t, r.runDir) {
			if a.Outcome != "not-run" || a.Detail == "" {
				t.Errorf("%s: %+v after a failed apply, want not-run with a reason", name, a)
			}
		}
	})
}

// Decision 2 at the chain: an observer that drops one subject's observation fails the run naming
// the assertion, and observations.json records the subjects the manifest requires.
func TestObserveChainExpectedSubjects(t *testing.T) {
	cases := []struct{ assertion, subject string }{
		{"state-bucket-versioned", "lz-demo-bkt-state"},
		{"bucket-tags", "lz-bkt-state"},
		{"state-object-encrypted", "lz-demo-bkt-state/tenants/demo/dev/gra11/runtime/terraform.tfstate"},
		{"outputs-schema", "account-bootstrap"},
		{"deployer-binding", "platform"},
		{"kd1-canary", "DELETE"},
	}
	for _, tc := range cases {
		t.Run(tc.assertion, func(t *testing.T) {
			h := newChainHarness(t)
			o := &chainObserver{obs: chainWith(func(o Observation) *Observation {
				if o.Assertion == tc.assertion && o.Subject == tc.subject {
					return nil
				}
				return &o
			})}
			r := h.chain(chainExec{observer: o})
			laneExit(t, r.laneRun, 1)
			if r.err == nil || !strings.Contains(r.err.Error(), tc.assertion) {
				t.Errorf("err %v, want one naming %s", r.err, tc.assertion)
			}
			var set ObservationSet
			raw, err := os.ReadFile(filepath.Join(r.runDir, "observations.json"))
			if err != nil || json.Unmarshal(raw, &set) != nil {
				t.Fatalf("observations.json unreadable (%v)", err)
			}
			want, _ := L7Subjects(h.m, h.boundAccount())
			if len(set.Expected) != 10 || !slices.Equal(set.Expected[tc.assertion], want[tc.assertion]) {
				t.Errorf("observations.json expected %v, want the manifest's subjects %v", set.Expected, want)
			}
			if a := chainAssertions(t, r.runDir)[tc.assertion]; a.Outcome != "fail" {
				t.Errorf("%s: %+v, want fail", tc.assertion, a)
			}
		})
	}
}

// ---------------------------------------------------------------- the production collector

// collectWorld is the collector's fake account: the canary fake's API and buckets, the IAM routes,
// the S3 store, the credentials of each authority and the second writer.
type collectWorld struct {
	t             *testing.T
	f             *canaryAPI
	m             *stacks.Manifest
	account       stacks.BoundAccount
	admit         map[string][]string             // bucket -> access keys the bucket's policy admits
	creds         map[Authority]map[string]string // the five variables of each authority
	resources     map[string]map[string]string    // URN -> tags (GET /v2/iam/resource/{urn})
	policies      map[string]string               // policy id -> name (POST /v2/iam/policy)
	policyNoID    bool                            // POST /v2/iam/policy answers 200 without an id
	policyGarbled bool                            // POST /v2/iam/policy creates, then answers 200 with a body that does not decode
	writer        *fakeLockWriter
	log           []string         // store, writer and IAM events in order
	credErr       map[string]error // instance id -> Creds error
	admin         Credential       // the collector's bootstrap authority (sandbox.env)
}

// fakeLockWriter is the second writer: refused, naming the held lock's id, exactly while
// <key>.tflock exists (mode "tofu"); "ignore" plans anyway; "decrypt" fails as a state that does
// not decrypt fails, under the same heading but without a lock id (evidence/T007.md P4).
type fakeLockWriter struct {
	w          *collectWorld
	mode       string
	prepareErr error
	prepared   []string
	planned    []string
}

func (l *fakeLockWriter) Prepare(_ context.Context, id string) error {
	l.w.log = append(l.w.log, "prepare "+id)
	l.prepared = append(l.prepared, id)
	return l.prepareErr
}

func (l *fakeLockWriter) Plan(_ context.Context, id string) (string, error) {
	l.w.log = append(l.w.log, "plan "+id)
	l.planned = append(l.planned, id)
	in, err := l.w.m.Row(id)
	if err != nil {
		return "", err
	}
	if !slices.Contains(l.prepared, id) {
		return "Error: Backend initialization required", errors.New("exit status 1")
	}
	l.w.f.mu.Lock()
	held, locked := l.w.f.buckets[in.StateBucket][in.StateKey+".tflock"]
	l.w.f.mu.Unlock()
	switch l.mode {
	case "ignore":
		return "Plan: 0 to add", nil
	case "decrypt":
		return "Error: Error acquiring the state lock\n\nError message: decryption failed: cipher: message authentication failed", errors.New("exit status 1")
	}
	if !locked {
		return "No changes.", nil
	}
	var info struct {
		ID string `json:"ID"`
	}
	_ = json.Unmarshal([]byte(held), &info)
	return fmt.Sprintf("Error: Error acquiring the state lock\n\nLock Info:\n  ID:        %s\n", info.ID), errors.New("exit status 1")
}

// collectBucket is the fake S3 store opened with one access key.
type collectBucket struct {
	w  *collectWorld
	ak string
}

func (b collectBucket) admitted(bucket string) bool { return slices.Contains(b.w.admit[bucket], b.ak) }

func (b collectBucket) Get(bucket, key string) ([]byte, error) {
	b.w.f.mu.Lock()
	defer b.w.f.mu.Unlock()
	b.w.log = append(b.w.log, "get "+b.ak+" "+bucket+"/"+key)
	if !b.admitted(bucket) {
		return nil, &apiStatus{what: "GET " + bucket + "/" + key, code: http.StatusForbidden}
	}
	v, ok := b.w.f.buckets[bucket][key]
	if !ok {
		return nil, fmt.Errorf("GET %s/%s: %w", bucket, key, fs.ErrNotExist)
	}
	return []byte(v), nil
}

// PutNew is Put if absent: an existing object answers 412 (If-None-Match: *).
func (b collectBucket) PutNew(bucket, key string, data []byte) error {
	b.w.f.mu.Lock()
	_, exists := b.w.f.buckets[bucket][key]
	b.w.f.mu.Unlock()
	if exists && b.admitted(bucket) {
		b.w.log = append(b.w.log, "put-new-refused "+b.ak+" "+bucket+"/"+key)
		return &apiStatus{what: "PUT " + bucket + "/" + key, code: http.StatusPreconditionFailed}
	}
	return b.Put(bucket, key, data)
}

func (b collectBucket) Put(bucket, key string, data []byte) error {
	b.w.f.mu.Lock()
	defer b.w.f.mu.Unlock()
	b.w.log = append(b.w.log, "put "+b.ak+" "+bucket+"/"+key)
	if !b.admitted(bucket) {
		return &apiStatus{what: "PUT " + bucket + "/" + key, code: http.StatusForbidden}
	}
	objs, ok := b.w.f.buckets[bucket]
	if !ok {
		return &apiStatus{what: "PUT " + bucket + "/" + key, code: http.StatusNotFound}
	}
	objs[key] = string(data)
	return nil
}

// collectLabels is the mandatory label set of an instance (data-model *Label set*).
func collectLabels(instance, tenant string) map[string]any {
	l := map[string]any{"lz:managed-by": "opentofu", "lz:managed-in": "github.com/platformrelay/landingzone-for-ovhcloud//stacks", "lz:instance": instance, "lz:release": "unreleased"}
	if tenant != "" {
		l["lz:tenant"] = tenant
	}
	return l
}

// collectEnvelope is a valid artefact of row id: the stage's envelope fixture bound to the fake's
// project, checked as consumers check it.
func collectEnvelope(t *testing.T, m *stacks.Manifest, a stacks.BoundAccount, id string) string {
	t.Helper()
	in, err := m.Row(id)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(laneRepoRoot, "tests", "fixtures", "outputs", "envelopes", in.Stage+".json"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.NewReplacer(laneFixtureProject, canaryProject, "0123456789abcdef0123456789abcdef", canaryProject,
		"lz-demo-dev-gra11-bkt-runtime-blue", "lz-demo-dev-gra11-bkt-runtime", "demo-dev-gra11-runtime-blue", "demo-dev-gra11-runtime").Replace(string(raw))
	var env map[string]any
	if err := json.Unmarshal([]byte(text), &env); err != nil {
		t.Fatal(err)
	}
	env["instance_id"] = id
	if v, ok := env["values"].(map[string]any); ok {
		delete(v, "slot")
	}
	out, _ := json.Marshal(env)
	want, err := stacks.ExpectationOf(m, id, a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stacks.ValidateEnvelope(laneSchemas(), out, want); err != nil {
		t.Fatalf("fixture envelope of %s: %v", id, err)
	}
	return string(out)
}

// newCollectWorld is the account after a complete chain: both state buckets versioned and
// labelled, every s3 row's state encrypted, every artefact valid, the project URN labelled; the
// tenant deployer holds the R6 allowlist (storage delete and bulkDeleteObjects included: KD-1)
// and no IAM write; each bucket admits its own S3 users only.
func newCollectWorld(t *testing.T) *collectWorld {
	t.Helper()
	w := &collectWorld{t: t, f: newCanaryAPI(t, nil), m: sandboxManifest(t), account: sandboxAccount(canaryProject),
		admit:     map[string][]string{"lz-bkt-state": {"AK-bootstrap"}, "lz-demo-bkt-state": {"AK-platform", "AK-tenant"}},
		resources: map[string]map[string]string{}, policies: map[string]string{}, credErr: map[string]error{}}
	w.writer = &fakeLockWriter{w: w, mode: "tofu"}
	w.admin = w.f.cred("admin")
	cred := func(name, ak string) map[string]string {
		c := w.f.cred(name)
		return map[string]string{"OVH_ENDPOINT": c.Endpoint, "OVH_CLIENT_ID": c.ClientID, "OVH_CLIENT_SECRET": c.ClientSecret,
			"AWS_ACCESS_KEY_ID": ak, "AWS_SECRET_ACCESS_KEY": "SK-" + ak}
	}
	w.creds = map[Authority]map[string]string{AuthorityBootstrap: cred("admin", "AK-bootstrap"), AuthorityPlatform: cred("platform-deployer", "AK-platform"),
		AuthorityTenant: cred("tenant-deployer", "AK-tenant")}
	f := w.f
	f.buckets["lz-bkt-state"] = map[string]string{}
	f.buckets["lz-demo-bkt-state"] = map[string]string{}
	f.meta["lz-bkt-state"] = map[string]any{"versioning": map[string]string{"status": "enabled"}, "tags": collectLabels("account-bootstrap", "")}
	f.meta["lz-demo-bkt-state"] = map[string]any{"versioning": map[string]string{"status": "enabled"}, "tags": collectLabels("demo-state", "demo")}
	for _, in := range w.m.Instances {
		if in.Backend == "s3" {
			f.buckets[in.StateBucket][in.StateKey] = `{"serial":3,"lineage":"l7","meta":{"key_provider.pbkdf2.main":"e30="},"encrypted_data":"c2VhbGVk","encryption_version":"v0"}`
		}
		bucket, err := stacks.ArtifactBucket(w.m, in.ID)
		if err != nil {
			t.Fatal(err)
		}
		f.buckets[bucket][stacks.ArtifactKey(in.ID)] = collectEnvelope(t, w.m, w.account, in.ID)
		if in.Stage == "project" {
			b, _ := stacks.InstanceBinding(w.m, in.ID, w.account)
			tags := map[string]string{}
			for k, v := range collectLabels(in.ID, in.Tenant) {
				tags[k] = v.(string)
			}
			w.resources[b.ProjectURN] = tags
		}
	}
	f.mux.HandleFunc("/v1/auth/details", func(rw http.ResponseWriter, r *http.Request) {
		c, ok := f.tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		if !ok {
			http.Error(rw, `{"message":"Invalid credentials"}`, http.StatusUnauthorized)
			return
		}
		json.NewEncoder(rw).Encode(map[string]string{"account": c.Account})
	})
	f.mux.HandleFunc("/v2/", w.iam)
	return w
}

// iam serves GET /v2/iam/resource/{urn}, POST /v2/iam/policy and DELETE /v2/iam/policy/{id}.
func (w *collectWorld) iam(rw http.ResponseWriter, r *http.Request) {
	f := w.f
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
	if !ok {
		http.Error(rw, `{"message":"Invalid credentials"}`, http.StatusUnauthorized)
		return
	}
	reply := func(code int, v any) {
		w.log = append(w.log, fmt.Sprintf("%s %s %s %d", c.Name, r.Method, r.URL.Path, code))
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(code)
		if v != nil {
			json.NewEncoder(rw).Encode(v)
		}
	}
	switch urn, isRes := strings.CutPrefix(r.URL.Path, "/v2/iam/resource/"); {
	case isRes && r.Method == http.MethodGet:
		tags, ok := w.resources[urn]
		if !ok {
			reply(http.StatusNotFound, map[string]string{"message": "no such resource"})
			return
		}
		reply(http.StatusOK, map[string]any{"urn": urn, "type": "publicCloudProject", "tags": tags})
	case r.URL.Path == "/v2/iam/policy" && r.Method == http.MethodPost:
		if !allows(c.Policy, "account:apiovh:iam/policy/create") {
			reply(http.StatusForbidden, map[string]string{"message": "This call has not been granted"})
			return
		}
		var in struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		id := fmt.Sprintf("00000000-0000-4000-8000-%012d", len(w.policies)+1)
		w.policies[id] = in.Name
		if w.policyGarbled {
			w.log = append(w.log, fmt.Sprintf("%s %s %s 200 garbled", c.Name, r.Method, r.URL.Path))
			rw.WriteHeader(http.StatusOK)
			fmt.Fprint(rw, "<html>gateway</html>")
			return
		}
		if w.policyNoID {
			reply(http.StatusOK, map[string]string{"name": in.Name})
			return
		}
		reply(http.StatusOK, map[string]string{"id": id, "name": in.Name})
	case r.URL.Path == "/v2/iam/policy" && r.Method == http.MethodGet:
		if !allows(c.Policy, "account:apiovh:iam/policy/get") {
			reply(http.StatusForbidden, map[string]string{"message": "This call has not been granted"})
			return
		}
		list := []map[string]string{}
		for _, id := range slices.Sorted(mapKeys(w.policies)) {
			list = append(list, map[string]string{"id": id, "name": w.policies[id]})
		}
		reply(http.StatusOK, list)
	case strings.HasPrefix(r.URL.Path, "/v2/iam/policy/") && r.Method == http.MethodDelete:
		id := strings.TrimPrefix(r.URL.Path, "/v2/iam/policy/")
		if !allows(c.Policy, "account:apiovh:iam/policy/delete") {
			reply(http.StatusForbidden, map[string]string{"message": "This call has not been granted"})
			return
		}
		if _, ok := w.policies[id]; !ok {
			reply(http.StatusNotFound, map[string]string{"message": "no such policy"})
			return
		}
		delete(w.policies, id)
		reply(http.StatusNoContent, nil)
	default:
		reply(http.StatusNotFound, map[string]string{"message": "not modelled by the fake"})
	}
}

func (w *collectWorld) collector() *collector {
	return &collector{API: w.f.options().API, Admin: w.admin, Binding: Binding{AccountID: "zz00000-ovh", Endpoint: "ovh-eu", Org: "lz"},
		Account: w.account,
		Creds: func(id string) (map[string]string, error) {
			if err := w.credErr[id]; err != nil {
				return nil, err
			}
			a, err := AuthorityOf(w.m, id)
			if err != nil {
				return nil, err
			}
			return w.creds[a], nil
		},
		Store: func(keys map[string]string) (ObjectStore, error) {
			return collectBucket{w: w, ak: keys["AWS_ACCESS_KEY_ID"]}, nil
		},
		Schemas: laneSchemas(), Writer: w.writer, Now: func() time.Time { return time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC) }}
}

func (w *collectWorld) observe() ([]Observation, error) {
	return w.collector().Observe(context.Background(), ObserveRun{RunID: canaryRunID, RunDir: w.t.TempDir(), Manifest: w.m, Trap: w.f.trap})
}

// collectComplete checks the collector's own contract on every run: each subject the manifest
// requires is observed exactly once (an unreadable fact is an `error` observation, never a missing
// one), and no client or S3 secret is in an observation.
func collectComplete(t *testing.T, w *collectWorld, obs []Observation) map[string]map[string]Observation {
	t.Helper()
	exp, err := L7Subjects(w.m, w.account)
	if err != nil || len(exp) != 10 {
		t.Fatalf("L7Subjects: %v (%d assertions)", err, len(exp))
	}
	got := map[string]map[string]Observation{}
	for _, o := range obs {
		if got[o.Assertion] == nil {
			got[o.Assertion] = map[string]Observation{}
		}
		if _, dup := got[o.Assertion][o.Subject]; dup {
			t.Errorf("%s %s observed twice", o.Assertion, o.Subject)
		}
		got[o.Assertion][o.Subject] = o
	}
	for a, subs := range exp {
		for _, s := range subs {
			if _, ok := got[a][s]; !ok {
				t.Errorf("%s %s not observed: the full chain collects every subject the manifest requires", a, s)
			}
		}
	}
	raw, _ := json.Marshal(obs)
	for _, c := range w.creds {
		for _, k := range []string{"OVH_CLIENT_SECRET", "AWS_SECRET_ACCESS_KEY"} {
			if strings.Contains(string(raw), c[k]) {
				t.Errorf("a secret (%s) in the observations", k)
			}
		}
	}
	return got
}

// TestCollect: each mechanism against one changed fact of the fake account; every row also checks
// completeness (collectComplete).
func TestCollect(t *testing.T) {
	type want map[string]map[string]string // assertion -> subject -> observed
	tenantRuntime := "lz-demo-bkt-state/tenants/demo/dev/gra11/runtime/terraform.tfstate"
	lockSubject := "lz-demo-bkt-state/tenants/demo/dev/project/terraform.tfstate"
	urn := "urn:v1:eu:resource:publicCloudProject:" + canaryProject
	cases := []struct {
		name  string
		setup func(w *collectWorld)
		want  want
	}{
		{name: "complete", want: want{
			"state-bucket-versioned": {"lz-bkt-state": "holds", "lz-demo-bkt-state": "holds"},
			"bucket-tags":            {"lz-bkt-state": "holds", "lz-demo-bkt-state": "holds"},
			"state-object-encrypted": {tenantRuntime: "holds", "lz-bkt-state/account/account-governance/terraform.tfstate": "holds"},
			"state-lock-contention":  {lockSubject: "holds"},
			"project-tags":           {urn: "holds"},
			"tenant-iam-write":       {"demo": "denied"},
			"tenant-s3-read":         {"demo:lz-bkt-state": "denied"},
			"outputs-schema":         {"account-bootstrap": "holds", "demo-dev-gra11-runtime": "holds"},
			"deployer-binding":       {"platform": "holds", "tenant:demo": "holds"},
			"kd1-canary":             {"bulkDeleteObjects": "allowed", "DELETE": "allowed"},
		}},
		{name: "versioning-suspended", setup: func(w *collectWorld) {
			w.f.meta["lz-demo-bkt-state"]["versioning"] = map[string]string{"status": "suspended"}
		}, want: want{"state-bucket-versioned": {"lz-demo-bkt-state": "violated", "lz-bkt-state": "holds"}}},
		{name: "versioning-absent", setup: func(w *collectWorld) { delete(w.f.meta["lz-bkt-state"], "versioning") },
			want: want{"state-bucket-versioned": {"lz-bkt-state": "violated", "lz-demo-bkt-state": "holds"}}},
		{name: "bucket-tag-missing", setup: func(w *collectWorld) { delete(w.f.meta["lz-demo-bkt-state"]["tags"].(map[string]any), "lz:release") },
			want: want{"bucket-tags": {"lz-demo-bkt-state": "violated", "lz-bkt-state": "holds"}}},
		{name: "account-bucket-tenant-tag", setup: func(w *collectWorld) { w.f.meta["lz-bkt-state"]["tags"].(map[string]any)["lz:tenant"] = "demo" },
			want: want{"bucket-tags": {"lz-bkt-state": "violated"}}},
		{name: "tenant-bucket-wrong-tenant", setup: func(w *collectWorld) { w.f.meta["lz-demo-bkt-state"]["tags"].(map[string]any)["lz:tenant"] = "other" },
			want: want{"bucket-tags": {"lz-demo-bkt-state": "violated"}}},
		{name: "bucket-unreadable", setup: func(w *collectWorld) { w.f.answer["admin GET get"] = http.StatusInternalServerError },
			want: want{"state-bucket-versioned": {"lz-bkt-state": "error", "lz-demo-bkt-state": "error"}, "bucket-tags": {"lz-bkt-state": "error"}}},
		{name: "state-plaintext", setup: func(w *collectWorld) {
			w.f.buckets["lz-demo-bkt-state"]["tenants/demo/dev/gra11/runtime/terraform.tfstate"] = `{"version":4,"serial":3,"lineage":"l7","resources":[]}`
		}, want: want{"state-object-encrypted": {tenantRuntime: "violated", "lz-bkt-state/account/account-governance/terraform.tfstate": "holds"}}},
		{name: "state-envelope-null", setup: func(w *collectWorld) {
			w.f.buckets["lz-demo-bkt-state"]["tenants/demo/dev/gra11/runtime/terraform.tfstate"] = `{"serial":3,"encrypted_data":null,"encryption_version":"v0"}`
		}, want: want{"state-object-encrypted": {tenantRuntime: "violated"}}},
		{name: "state-envelope-object", setup: func(w *collectWorld) {
			w.f.buckets["lz-demo-bkt-state"]["tenants/demo/dev/gra11/runtime/terraform.tfstate"] = `{"serial":3,"encrypted_data":{"resources":[]},"encryption_version":"v0"}`
		}, want: want{"state-object-encrypted": {tenantRuntime: "violated"}}},
		{name: "state-envelope-empty", setup: func(w *collectWorld) {
			w.f.buckets["lz-demo-bkt-state"]["tenants/demo/dev/gra11/runtime/terraform.tfstate"] = `{"serial":3,"encrypted_data":"","encryption_version":"v0"}`
		}, want: want{"state-object-encrypted": {tenantRuntime: "violated"}}},
		{name: "state-missing", setup: func(w *collectWorld) {
			delete(w.f.buckets["lz-bkt-state"], "account/tenant-state/demo/terraform.tfstate")
		}, want: want{"state-object-encrypted": {"lz-bkt-state/account/tenant-state/demo/terraform.tfstate": "error"}}},
		{name: "lock-not-refused", setup: func(w *collectWorld) { w.writer.mode = "ignore" }, want: want{"state-lock-contention": {lockSubject: "violated"}}},
		{name: "lock-error-without-holder", setup: func(w *collectWorld) { w.writer.mode = "decrypt" }, want: want{"state-lock-contention": {lockSubject: "error"}}},
		{name: "lock-not-ready", setup: func(w *collectWorld) { w.writer.prepareErr = errors.New("init failed") }, want: want{"state-lock-contention": {lockSubject: "error"}}},
		{name: "project-tags-missing", setup: func(w *collectWorld) { delete(w.resources[urn], "lz:tenant") }, want: want{"project-tags": {urn: "violated"}}},
		{name: "project-unreadable", setup: func(w *collectWorld) { delete(w.resources, urn) }, want: want{"project-tags": {urn: "error"}}},
		{name: "outputs-invalid", setup: func(w *collectWorld) {
			w.f.buckets["lz-demo-bkt-state"][stacks.ArtifactKey("demo-dev-gra11-runtime")] = `{"apiVersion":"lz.platformrelay.dev/v1alpha1","kind":"StageOutputs","instance_id":"demo-dev-gra11-runtime","stage":"runtime","values":{}}`
		}, want: want{"outputs-schema": {"demo-dev-gra11-runtime": "violated", "account-bootstrap": "holds"}}},
		{name: "outputs-missing", setup: func(w *collectWorld) { delete(w.f.buckets["lz-bkt-state"], stacks.ArtifactKey("account-bootstrap")) },
			want: want{"outputs-schema": {"account-bootstrap": "error"}}},
		{name: "binding-foreign", setup: func(w *collectWorld) {
			c := w.f.cred("other-account-admin")
			w.creds[AuthorityPlatform]["OVH_CLIENT_ID"], w.creds[AuthorityPlatform]["OVH_CLIENT_SECRET"] = c.ClientID, c.ClientSecret
		}, want: want{"deployer-binding": {"platform": "violated", "tenant:demo": "holds"}}},
		{name: "binding-unreadable", setup: func(w *collectWorld) { w.credErr["demo-dev-gra11-network"] = errors.New("deployer.env missing") },
			want: want{"deployer-binding": {"tenant:demo": "error", "platform": "holds"}, "tenant-iam-write": {"demo": "error"}, "tenant-s3-read": {"demo:lz-bkt-state": "error"},
				"kd1-canary": {"bulkDeleteObjects": "error", "DELETE": "error"}}},
		{name: "s3-read-allowed", setup: func(w *collectWorld) { w.admit["lz-bkt-state"] = append(w.admit["lz-bkt-state"], "AK-tenant") },
			want: want{"tenant-s3-read": {"demo:lz-bkt-state": "allowed"}}},
		{name: "kd1-denied", setup: func(w *collectWorld) {
			tc := w.f.creds[w.f.cred("tenant-deployer").ClientID]
			tc.Policy = without("publicCloudProject:apiovh:region/storage/delete", "publicCloudProject:apiovh:region/storage/bulkDeleteObjects")(slices.Clone(tc.Policy))
			w.f.creds[tc.ClientID] = tc
			w.f.tokens["fake-token-"+tc.Name] = tc
		}, want: want{"kd1-canary": {"bulkDeleteObjects": "denied", "DELETE": "denied"}}},
		{name: "admin-rejected", setup: func(w *collectWorld) { w.admin.ClientSecret = "rotated" }, want: want{"state-bucket-versioned": {"lz-bkt-state": "error"}, "bucket-tags": {"lz-demo-bkt-state": "error"}, "project-tags": {urn: "error"},
			"kd1-canary": {"bulkDeleteObjects": "error", "DELETE": "error"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newCollectWorld(t)
			if tc.setup != nil {
				tc.setup(w)
			}
			obs, err := w.observe()
			if err != nil {
				t.Fatalf("Observe: %v (a fact that cannot be read is an error observation, not a collector failure)", err)
			}
			got := collectComplete(t, w, obs)
			for a, subs := range tc.want {
				for s, v := range subs {
					if o := got[a][s]; o.Observed != v {
						t.Errorf("%s %s observed %q (%s), want %q", a, s, o.Observed, o.Detail, v)
					}
				}
			}
			if tc.name != "complete" {
				return
			}
			// The complete account passes, KD-1 reproduced (the R6 allowlist) as known-deviation.
			exp, _ := L7Subjects(w.m, w.account)
			for _, a := range Assess(ObservationSet{RunID: canaryRunID, SharedStateProject: true, Observations: obs, Expected: exp}) {
				if want := map[bool]string{true: "known-deviation", false: "pass"}[a.Assertion == "kd1-canary"]; a.Outcome != want {
					t.Errorf("%s: %s (%s), want %s", a.Assertion, a.Outcome, a.Detail, want)
				}
			}
		})
	}
}

// The lock-contention order (T048 task text: the second writer is started after the lock object
// is listed): the second writer is readied first, the lock object is written with the lock row's
// own S3 keys and read back, then the plan runs; the object is gone afterwards (removed with the
// bootstrap authority: bulkDeleteObjects of that key only) and the trap holds its removal, which
// is content when nothing is left. A lock object already there is another writer's: an error,
// left untouched, and no plan runs. A lock object of another writer found by the trap is left.
func TestCollectLock(t *testing.T) {
	key := "tenants/demo/dev/project/terraform.tfstate.tflock"
	t.Run("order", func(t *testing.T) {
		w := newCollectWorld(t)
		if _, err := w.observe(); err != nil {
			t.Fatal(err)
		}
		idx := func(ev string) int { return slices.Index(w.log, ev) }
		prep, put, get, plan := idx("prepare demo-dev-project"), idx("put AK-platform lz-demo-bkt-state/"+key), -1, idx("plan demo-dev-project")
		for i := put + 1; put >= 0 && i < len(w.log); i++ {
			if w.log[i] == "get AK-platform lz-demo-bkt-state/"+key {
				get = i
				break
			}
		}
		if prep < 0 || put < 0 || get < 0 || plan < 0 || !(prep < put && put < get && get < plan) {
			t.Errorf("events %v: want the second writer readied, the lock object put and listed with the project's S3 keys, then the plan", w.log)
		}
		if _, ok := w.f.buckets["lz-demo-bkt-state"][key]; ok {
			t.Errorf("lock object %s left after the observation", key)
		}
		var removed []canaryCall
		for _, c := range w.f.Calls() {
			if c.Bucket == "lz-demo-bkt-state" && c.Method != http.MethodGet {
				removed = append(removed, c)
			}
		}
		if len(removed) != 1 || removed[0].Cred != "admin" || removed[0].Op != "bulk" {
			t.Errorf("writes on the state bucket through the API %+v: want one admin bulkDeleteObjects (the lock object only)", removed)
		}
		if !slices.Contains(w.f.trap.Names(), "lock lz-demo-bkt-state/tenants/demo/dev/project/terraform.tfstate") {
			t.Errorf("trap %v: the lock object's removal is registered", w.f.trap.Names())
		}
		state := w.f.buckets["lz-demo-bkt-state"]["tenants/demo/dev/project/terraform.tfstate"]
		// A writer that locked the state since: the trap leaves its lock object.
		later := `{"ID":"a-later-writer","Operation":"OperationTypeApply"}`
		w.f.buckets["lz-demo-bkt-state"][key] = later
		if err := w.f.trap.Run(context.Background()); err != nil {
			t.Errorf("trap: %v", err)
		}
		if w.f.buckets["lz-demo-bkt-state"]["tenants/demo/dev/project/terraform.tfstate"] != state {
			t.Error("the trap touched the state object")
		}
		if w.f.buckets["lz-demo-bkt-state"][key] != later {
			t.Error("the trap removed a lock object that is not this run's")
		}
	})
	t.Run("held-by-another", func(t *testing.T) {
		w := newCollectWorld(t)
		other := `{"ID":"another-writer","Operation":"OperationTypeApply"}`
		w.f.buckets["lz-demo-bkt-state"][key] = other
		obs, err := w.observe()
		if err != nil {
			t.Fatal(err)
		}
		if o := collectComplete(t, w, obs)["state-lock-contention"]["lz-demo-bkt-state/tenants/demo/dev/project/terraform.tfstate"]; o.Observed != "error" {
			t.Errorf("observed %q, want error (another writer holds the state)", o.Observed)
		}
		if len(w.writer.planned) != 0 {
			t.Error("the second writer planned although the lock object was not this run's")
		}
		_ = w.f.trap.Run(context.Background())
		if w.f.buckets["lz-demo-bkt-state"][key] != other {
			t.Error("another writer's lock object was changed or removed")
		}
	})
	t.Run("trap-after-interrupt", func(t *testing.T) {
		// The release after the plan fails (the API refuses the bulk delete); the trap retries
		// once the API answers, and only this run's lock object goes.
		w := newCollectWorld(t)
		w.f.answer["admin POST bulk"] = http.StatusServiceUnavailable
		if _, err := w.observe(); err != nil {
			t.Fatal(err)
		}
		if _, ok := w.f.buckets["lz-demo-bkt-state"][key]; !ok {
			t.Fatal("fake: the lock object went although the release was refused")
		}
		delete(w.f.answer, "admin POST bulk")
		if err := w.f.trap.Run(context.Background()); err != nil {
			t.Errorf("trap: %v", err)
		}
		if _, ok := w.f.buckets["lz-demo-bkt-state"][key]; ok {
			t.Error("the trap left this run's lock object")
		}
	})
}

// A tenant deployer wrongly allowed to write IAM leaves a policy: the trap deletes it with the
// bootstrap authority, by its id, or by its name when the answer carried no id (review r1).
func TestCollectIAMWriteTrap(t *testing.T) {
	for _, mode := range []string{"with-id", "without-id", "garbled"} {
		t.Run(mode, func(t *testing.T) { collectIAMWriteTrap(t, mode) })
	}
}

// garbled (review r2): the policy is created but the answer does not decode: observed `error`
// (never a pass), and the trap still deletes the policy by its name.
func collectIAMWriteTrap(t *testing.T, mode string) {
	w := newCollectWorld(t)
	w.policyNoID, w.policyGarbled = mode == "without-id", mode == "garbled"
	tc := w.f.creds[w.f.cred("tenant-deployer").ClientID]
	tc.Policy = append(slices.Clone(tc.Policy), "account:apiovh:iam/policy/create")
	w.f.creds[tc.ClientID] = tc
	w.f.tokens["fake-token-"+tc.Name] = tc
	obs, err := w.observe()
	if err != nil {
		t.Fatal(err)
	}
	want := map[bool]string{false: "allowed", true: "error"}[mode == "garbled"]
	if o := collectComplete(t, w, obs)["tenant-iam-write"]["demo"]; o.Observed != want {
		t.Errorf("observed %q, want %s", o.Observed, want)
	}
	if len(w.policies) != 1 {
		t.Fatalf("policies %v, want the one the tenant created", w.policies)
	}
	for _, name := range w.policies {
		if !strings.HasPrefix(name, "lz-") || !strings.Contains(name, strings.ToLower(canaryRunID)) {
			t.Errorf("policy name %q: want the org prefix and the run id (the leftover check finds it)", name)
		}
		if !slices.Contains(w.f.trap.Names(), "policy "+name) {
			t.Errorf("trap %v: want the created policy registered", w.f.trap.Names())
		}
	}
	if err := w.f.trap.Run(context.Background()); err != nil {
		t.Errorf("trap: %v", err)
	}
	if len(w.policies) != 0 {
		t.Errorf("policies %v after the trap", w.policies)
	}
	if !slices.ContainsFunc(w.log, func(e string) bool { return strings.HasPrefix(e, "admin DELETE /v2/iam/policy/") }) {
		t.Errorf("events %v: the trap deletes with the bootstrap authority", w.log)
	}
}

// The full chain without an Observer runs the production collector with the lane's own
// credentials, store and tofu: every subject the manifest requires is observed (the fake API
// answers only GET /auth/details, so most facts are errors and the run fails); the deployer
// bindings and the artefacts the chain published hold; the second writer is a plan of the lock
// row with -lock-timeout=0s under its own authority, run after the lock object was written and
// refused naming it; the harness judges the run as every lane run (secrets, authorities, binding,
// locks, saved plans).
func TestObserveChainProductionCollector(t *testing.T) {
	h := newChainHarness(t)
	runID := h.nextRunID()
	h.setStack("demo-dev-project", func(s *laneStack) { s.LockHeld = "lz-l7-" + strings.ToLower(runID) })
	r := h.chain(chainExec{productionObserver: true})
	laneExit(t, r.laneRun, 1)
	chainDestroyedAfter(t, h, r, chainDestroyOrder, "fail")
	var set ObservationSet
	raw, err := os.ReadFile(filepath.Join(r.runDir, "observations.json"))
	if err != nil || json.Unmarshal(raw, &set) != nil {
		t.Fatalf("observations.json unreadable (%v)", err)
	}
	exp, _ := L7Subjects(h.m, h.boundAccount())
	got := map[string]map[string]string{}
	for _, o := range set.Observations {
		if got[o.Assertion] == nil {
			got[o.Assertion] = map[string]string{}
		}
		got[o.Assertion][o.Subject] = o.Observed
	}
	for a, subs := range exp {
		for _, s := range subs {
			if _, ok := got[a][s]; !ok {
				t.Errorf("%s %s not observed by the production collector", a, s)
			}
		}
	}
	for a, subs := range map[string]map[string]string{
		"deployer-binding":      {"platform": "holds", "tenant:demo": "holds"},
		"state-lock-contention": {"lz-demo-bkt-state/tenants/demo/dev/project/terraform.tfstate": "holds"},
		"outputs-schema":        {"account-governance": "holds", "demo-state": "holds", "demo-dev-project": "holds", "demo-dev-gra11-network": "holds", "demo-dev-gra11-runtime": "holds"},
	} {
		for s, v := range subs {
			if got[a][s] != v {
				t.Errorf("%s %s observed %q, want %q", a, s, got[a][s], v)
			}
		}
	}
	put := r.index(false, func(c laneCall) bool {
		return c.Cmd == "put" && c.Stack == "lz-demo-bkt-state/tenants/demo/dev/project/terraform.tfstate.tflock"
	})
	plan := r.index(false, func(c laneCall) bool {
		return c.Cmd == "plan" && c.Stack == "demo-dev-project" && slices.Contains(c.Args, "-lock-timeout=0s")
	})
	if put < 0 || plan < 0 || plan < put || r.calls[plan].Exit == 0 {
		t.Errorf("lock object put at %d, second writer plan at %d (exit %d): want the plan after the put, refused; events %v", put, plan, r.calls[max(plan, 0)].Exit, laneEvents(r.calls))
	}
	if put >= 0 && r.calls[put].Key != lanePlatformAK {
		t.Errorf("lock object written with access key %s, want the project's own (platform) keys", r.calls[put].Key)
	}
}

// PutNew (review r1): the lock object is created only if absent — the request carries
// `If-None-Match: *`, still signed as every request; an existing object answers 412, typed.
func TestS3StorePutNew(t *testing.T) {
	f := newFakeS3(t)
	var cond []string
	f.fault = func(rw http.ResponseWriter, r *http.Request) bool {
		if r.Method != http.MethodPut {
			return false
		}
		f.mu.Lock()
		cond = append(cond, r.Header.Get("If-None-Match"))
		_, exists := f.objects[strings.TrimPrefix(r.URL.Path, "/")]
		f.mu.Unlock()
		if r.Header.Get("If-None-Match") == "*" && exists {
			rw.WriteHeader(http.StatusPreconditionFailed)
			return true
		}
		return false
	}
	s := newTestS3Store(t, f)
	if err := s.PutNew(s3TestBucket, "k.tflock", []byte("one")); err != nil {
		t.Fatalf("first PutNew: %v", err)
	}
	err := s.PutNew(s3TestBucket, "k.tflock", []byte("two"))
	if statusOf(err) != http.StatusPreconditionFailed {
		t.Errorf("second PutNew: %v, want a typed 412", err)
	}
	if got := f.objects[s3TestBucket+"/k.tflock"]; string(got) != "one" {
		t.Errorf("object %q after a refused PutNew, want the first writer's", got)
	}
	if !slices.Equal(cond, []string{"*", "*"}) {
		t.Errorf("If-None-Match %q, want * on each PutNew", cond)
	}
	for _, r := range f.requests() {
		if !r.SignedOK {
			t.Errorf("%s %s not signed correctly", r.Method, r.Path)
		}
	}
	if err := s.Put(s3TestBucket, "k.tflock", []byte("three")); err != nil || len(cond) != 3 || cond[2] != "" {
		t.Errorf("Put: %v, conditions %q: Put stays unconditional", err, cond)
	}
}

// The S3 store's refusal is typed (T062): a 403 is told apart from any other failure (the tenant's
// S3 read of a bucket that is not its own observes `denied` only on a 403), with the error text
// unchanged; a 404 stays fs.ErrNotExist.
func TestS3StoreRefusalTyped(t *testing.T) {
	f := newFakeS3(t)
	f.get = func(ak, bucket, key string) ([]byte, int) {
		if key == "missing" {
			return nil, http.StatusNotFound
		}
		return nil, http.StatusForbidden
	}
	s := newTestS3Store(t, f)
	_, err := s.Get(s3TestBucket, "k")
	if statusOf(err) != http.StatusForbidden || err.Error() != "GET "+s3TestBucket+"/k answered 403" {
		t.Errorf("GET refused: %v (status %d), want a typed 403 with the same text", err, statusOf(err))
	}
	if o := refusal("tenant-s3-read", "demo:"+s3TestBucket, err, err == nil); o.Observed != "denied" {
		t.Errorf("a 403 observed %q, want denied", o.Observed)
	}
	_, err = s.Get(s3TestBucket, "missing")
	if !errors.Is(err, fs.ErrNotExist) || statusOf(err) == http.StatusForbidden {
		t.Errorf("GET missing: %v, want fs.ErrNotExist", err)
	}
	if o := refusal("tenant-s3-read", "demo:"+s3TestBucket, err, err == nil); o.Observed != "error" {
		t.Errorf("a 404 observed %q, want error (the read was not refused)", o.Observed)
	}
}
