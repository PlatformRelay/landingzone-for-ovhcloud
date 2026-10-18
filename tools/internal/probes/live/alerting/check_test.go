package alerting

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/live"
)

// T012 (spec 007 FR-007, FR-008): the budget-alert check against a fake OVHcloud API (OAuth2 token
// endpoint, GET /v1/me, the project alert listing, read and delete) that records every request.
// Shapes follow kb api/v1/cloud.json: GET /cloud/project/{serviceName}/alerting answers uuid[],
// GET and DELETE …/alerting/{id} answer cloud.alerting.Alerting (id, delay, email, emails,
// monthlyThreshold) and void. The plan is the root's: threshold 1, delay 3600, the address
// GET /me answers (data-model *Budget-alert probe check*). Baselines follow data-model *Probe
// baseline*: the record copy RunDir(<run-id>)/baseline.json, the recovery copy
// StateDir/baselines/<run-id>.json, the run's state StateDir/probes/<run-id>/budget-alert.tfstate
// (coordinator decisions 2026-10-18: Options.StateDir and Options.Project). No test calls
// OVHcloud or reads ~/.config/ovh-lz/.

const (
	tRun          = "20261018T120000Z-ab12"
	tOtherRun     = "20261017T090000Z-cd34"
	tProject      = "0123456789abcdef0123456789abcdef"
	tOtherProject = "fedcba9876543210fedcba9876543210"
	tAdminID      = "EU.budgetalertadmin"
	tAdminSK      = "Zx8cVb2nMq4wEr6t"
	tAdminTok     = "tok-alertadmin-Pq7r"
	tEmail        = "owner@example.org"
	tKind         = "ovh_cloud_project_alerting"

	alertA = "1a2b3c4d-0000-4000-8000-00000000000a" // in the baseline
	alertB = "1a2b3c4d-0000-4000-8000-00000000000b" // the run's alert
	alertC = "1a2b3c4d-0000-4000-8000-00000000000c" // set by someone after the baseline
)

// tPlanned is an alert as the root plans it.
var tPlanned = fakeAlert{threshold: 1, delay: 3600, email: tEmail}

type fakeAlert struct {
	threshold, delay int
	email            string
}

// tWorld is the fake API and the run's files.
type tWorld struct {
	t      *testing.T
	srv    *httptest.Server
	base   string
	mu     sync.Mutex
	alerts map[string]map[string]fakeAlert // project → id → alert
	sticky map[string]bool                 // ids whose DELETE answers 200 and stays listed
	reqs   []string                        // "METHOD path", the token request as "TOKEN /token"
}

func newTWorld(t *testing.T) *tWorld {
	t.Helper()
	w := &tWorld{t: t, base: t.TempDir(), sticky: map[string]bool{}, alerts: map[string]map[string]fakeAlert{
		tProject:      {alertA: tPlanned},
		tOtherProject: {},
	}}
	w.srv = httptest.NewServer(http.HandlerFunc(w.serve))
	t.Cleanup(w.srv.Close)
	return w
}

func (w *tWorld) options() Options {
	return Options{
		API:      live.API{TokenURL: w.srv.URL + "/token", BaseURL: w.srv.URL + "/v1", HTTP: w.srv.Client()},
		Admin:    live.Credential{Endpoint: "ovh-eu", ClientID: tAdminID, ClientSecret: tAdminSK},
		RunDir:   func(runID string) string { return filepath.Join(w.base, "live", runID) },
		StateDir: filepath.Join(w.base, "state"),
		Project:  tProject,
	}
}

func (w *tWorld) add(project, id string, a fakeAlert) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.alerts[project][id] = a
}

func (w *tWorld) has(project, id string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, ok := w.alerts[project][id]
	return ok
}

func (w *tWorld) seen() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.reqs)
}

// deletes are the DELETE requests sent.
func (w *tWorld) deletes() []string {
	var out []string
	for _, r := range w.seen() {
		if strings.HasPrefix(r, "DELETE ") {
			out = append(out, r)
		}
	}
	return out
}

func tAlertPath(project, id string) string {
	p := "/v1/cloud/project/" + project + "/alerting"
	if id != "" {
		p += "/" + id
	}
	return p
}

func (w *tWorld) serve(rw http.ResponseWriter, r *http.Request) {
	io.Copy(io.Discard, r.Body)
	w.mu.Lock()
	defer w.mu.Unlock()
	rw.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/token" {
		w.reqs = append(w.reqs, "TOKEN /token")
		fmt.Fprintf(rw, `{"access_token":%q,"token_type":"Bearer","expires_in":3599,"scope":"all"}`, tAdminTok)
		return
	}
	w.reqs = append(w.reqs, r.Method+" "+r.URL.Path)
	if r.Header.Get("Authorization") != "Bearer "+tAdminTok {
		rw.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(rw, `{"message":"Invalid token"}`)
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == "/v1/me" {
		fmt.Fprintf(rw, `{"nichandle":"xx1111-ovh","email":%q}`, tEmail)
		return
	}
	w.alerting(rw, r)
}

// alerting answers the alert routes of a known project; anything else is 404.
func (w *tWorld) alerting(rw http.ResponseWriter, r *http.Request) {
	rest, ok := strings.CutPrefix(r.URL.Path, "/v1/cloud/project/")
	project, sub, _ := strings.Cut(rest, "/")
	alerts, known := w.alerts[project]
	id, isOne := strings.CutPrefix(sub, "alerting/")
	a, exists := alerts[id]
	switch {
	case !ok || !known:
	case sub == "alerting" && r.Method == http.MethodGet:
		ids := make([]string, 0, len(alerts))
		for id := range alerts {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		json.NewEncoder(rw).Encode(ids)
		return
	case isOne && r.Method == http.MethodGet && exists:
		fmt.Fprintf(rw, `{"id":%q,"delay":%d,"email":%q,"emails":[%q],"monthlyThreshold":%d,"name":"","service":"all"}`, id, a.delay, a.email, a.email, a.threshold)
		return
	case isOne && r.Method == http.MethodDelete && exists:
		if !w.sticky[id] {
			delete(alerts, id)
		}
		fmt.Fprint(rw, "null")
		return
	}
	rw.WriteHeader(http.StatusNotFound)
	fmt.Fprint(rw, `{"message":"The requested object does not exist"}`)
}

func tDigest(id string) string {
	s := sha256.Sum256([]byte(id))
	return hex.EncodeToString(s[:])
}

// tBaseline is a valid baseline of the run holding alert A, edited by edit.
func tBaseline(t *testing.T, edit func(m map[string]any)) []byte {
	t.Helper()
	m := map[string]any{
		"run_id":         tRun,
		"project_sha256": tDigest(tProject),
		"listed":         map[string]any{tKind: []string{alertA}},
		"complete":       true,
		"errors":         []string{},
	}
	if edit != nil {
		edit(m)
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func tWritePrivate(t *testing.T, path string, raw []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// tFiles is a run's files on disk; nil leaves a file absent.
type tFiles struct {
	record, recovery []byte
	state            bool
	inventory        bool
}

func (w *tWorld) write(f tFiles) {
	o := w.options()
	if f.record != nil {
		tWritePrivate(w.t, filepath.Join(o.RunDir(tRun), "baseline.json"), f.record)
	}
	if f.recovery != nil {
		tWritePrivate(w.t, filepath.Join(o.StateDir, "baselines", tRun+".json"), f.recovery)
	}
	if f.state {
		tWritePrivate(w.t, filepath.Join(o.StateDir, "probes", tRun, "budget-alert.tfstate"), []byte(`{"version":4,"resources":[]}`))
	}
	if f.inventory {
		line, _ := json.Marshal(live.InventoryEntry{Stack: "budget-alert", Address: "module.project.ovh_cloud_project_alerting.this[0]", Type: tKind, ID: alertB})
		tWritePrivate(w.t, filepath.Join(o.RunDir(tRun), "inventory.jsonl"), append(line, '\n'))
	}
}

func tCtx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return c
}

// sweep runs the check's Sweep on the run's project, the argument the runner hands it from
// probe.env; Options.Project names the other project, so a sweep working in it is seen.
func (w *tWorld) sweep() (string, error) {
	var term bytes.Buffer
	o := w.options()
	o.Project = tOtherProject
	err := Check(o).Sweep(tCtx(w.t), tRun, tProject, &term)
	return term.String(), err
}

// tWantOnlyDelete: exactly one DELETE, of id in the run's project, followed by a listing.
func tWantOnlyDelete(t *testing.T, w *tWorld, id string) {
	t.Helper()
	del := "DELETE " + tAlertPath(tProject, id)
	if got := w.deletes(); !slices.Equal(got, []string{del}) {
		t.Fatalf("DELETEs = %v, want only %s", got, del)
	}
	reqs := w.seen()
	at := slices.Index(reqs, del)
	if !slices.Contains(reqs[at+1:], "GET "+tAlertPath(tProject, "")) {
		t.Errorf("no alert listing after the DELETE; requests %v", reqs)
	}
}

// The run's alert B (not in the baseline, matching the plan) is deleted, A is not, the sweep
// lists again and passes once B is gone.
func TestSweepDeletesOnlyNewAlert(t *testing.T) {
	w := newTWorld(t)
	w.add(tProject, alertB, tPlanned)
	w.write(tFiles{record: tBaseline(t, nil), state: true})
	term, err := w.sweep()
	if err != nil {
		t.Fatalf("Sweep: %v (terminal %q)", err, term)
	}
	tWantOnlyDelete(t, w, alertB)
	if w.has(tProject, alertB) || !w.has(tProject, alertA) {
		t.Errorf("after the sweep B listed %v, A listed %v; want B gone, A kept", w.has(tProject, alertB), w.has(tProject, alertA))
	}
}

// A DELETE answered 200 whose alert is still listed afterwards fails the sweep, naming it.
func TestSweepFailsWhenStillListed(t *testing.T) {
	w := newTWorld(t)
	w.add(tProject, alertB, tPlanned)
	w.sticky[alertB] = true
	w.write(tFiles{record: tBaseline(t, nil), state: true})
	term, err := w.sweep()
	if err == nil {
		t.Fatalf("Sweep passed with %s still listed after its DELETE; requests %v", alertB, w.seen())
	}
	if !strings.Contains(err.Error()+term, alertB) {
		t.Errorf("Sweep error %q and terminal %q do not name %s", err, term, alertB)
	}
	tWantOnlyDelete(t, w, alertB)
}

// G27: a listing holding only baseline ids sends no DELETE and passes.
func TestSweepBaselineOnly(t *testing.T) {
	w := newTWorld(t)
	w.write(tFiles{record: tBaseline(t, nil), state: true})
	term, err := w.sweep()
	if err != nil {
		t.Fatalf("Sweep: %v (terminal %q)", err, term)
	}
	if got := w.deletes(); len(got) != 0 {
		t.Errorf("DELETEs = %v, want none (A is in the baseline)", got)
	}
	if !slices.Contains(w.seen(), "GET "+tAlertPath(tProject, "")) {
		t.Errorf("the sweep never listed the run's project's alerts; requests %v", w.seen())
	}
}

// A non-baseline alert that does not match the plan (one the owner set later) is reported and
// fails the sweep, and is never deleted; the plan-matching row is the acceptance control.
func TestSweepLeavesForeignAlert(t *testing.T) {
	cases := []struct {
		name  string
		c     fakeAlert
		owned bool
	}{
		{"threshold-50", fakeAlert{threshold: 50, delay: 3600, email: tEmail}, false},
		{"delay-86400", fakeAlert{threshold: 1, delay: 86400, email: tEmail}, false},
		{"other-address", fakeAlert{threshold: 1, delay: 3600, email: "someone@example.net"}, false},
		{"matches-plan", tPlanned, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newTWorld(t)
			w.add(tProject, alertC, tc.c)
			w.write(tFiles{record: tBaseline(t, nil), state: true})
			term, err := w.sweep()
			if tc.owned {
				if err != nil {
					t.Fatalf("Sweep: %v (terminal %q)", err, term)
				}
				tWantOnlyDelete(t, w, alertC)
				return
			}
			tWantReported(t, w, term, err)
		})
	}
}

// The run's alert B beside a foreign alert (threshold 50), listed before or after it: B is still
// deleted and the deletion checked by a listing, the foreign one is reported and kept, and the
// sweep fails (review round 2).
func TestSweepDeletesBesideForeign(t *testing.T) {
	for _, foreign := range []string{"1a2b3c4d-0000-4000-8000-000000000009", alertC} {
		t.Run(foreign, func(t *testing.T) {
			w := newTWorld(t)
			w.add(tProject, alertB, tPlanned)
			w.add(tProject, foreign, fakeAlert{threshold: 50, delay: 3600, email: tEmail})
			w.write(tFiles{record: tBaseline(t, nil), state: true})
			term, err := w.sweep()
			tWantOnlyDelete(t, w, alertB)
			if err == nil || !strings.Contains(err.Error()+term, foreign) {
				t.Errorf("Sweep error %v, terminal %q: want a failure reporting %s", err, term, foreign)
			}
			if !w.has(tProject, foreign) {
				t.Errorf("the foreign alert %s is gone, want it kept", foreign)
			}
		})
	}
}

// tWantReported: C was read, not deleted, and the sweep failed reporting it.
func tWantReported(t *testing.T, w *tWorld, term string, err error) {
	t.Helper()
	if got := w.deletes(); len(got) != 0 {
		t.Fatalf("DELETEs = %v, want none: C does not match the plan", got)
	}
	if !slices.Contains(w.seen(), "GET "+tAlertPath(tProject, alertC)) {
		t.Fatalf("C was never read; requests %v", w.seen())
	}
	if err == nil || !strings.Contains(err.Error()+term, alertC) {
		t.Errorf("Sweep error %v, terminal %q: want a failure reporting %s", err, term, alertC)
	}
}

// An untrusted run refuses with BASELINE_INVALID or BASELINE_MISSING and deletes nothing,
// though B (matching the plan, outside any baseline) is listed.
func TestSweepUntrusted(t *testing.T) {
	cases := []struct {
		name string
		f    tFiles
		want string
	}{
		{"unreadable", tFiles{record: []byte(`{"run_id":`), state: true}, "BASELINE_INVALID"},
		{"other-run", tFiles{record: tBaseline(t, func(m map[string]any) { m["run_id"] = tOtherRun }), state: true}, "BASELINE_INVALID"},
		{"other-project", tFiles{record: tBaseline(t, func(m map[string]any) { m["project_sha256"] = tDigest(tOtherProject) }), state: true}, "BASELINE_INVALID"},
		{"no-alert-key", tFiles{record: tBaseline(t, func(m map[string]any) { m["listed"] = map[string]any{} }), state: true}, "BASELINE_INVALID"},
		{"copies-differ", tFiles{record: tBaseline(t, nil), recovery: tBaseline(t, func(m map[string]any) {
			m["listed"] = map[string]any{tKind: []string{alertA, alertB}}
		}), state: true}, "BASELINE_INVALID"},
		{"missing-beside-state", tFiles{state: true}, "BASELINE_MISSING"},
		{"missing-beside-inventory", tFiles{inventory: true}, "BASELINE_MISSING"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newTWorld(t)
			w.add(tProject, alertB, tPlanned)
			w.write(tc.f)
			term, err := w.sweep()
			if got := w.deletes(); len(got) != 0 {
				t.Errorf("DELETEs = %v, want none on an untrusted run", got)
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Sweep error %v (terminal %q), want one carrying %s", err, term, tc.want)
			}
		})
	}
}

// The recovery copy stands in for a record copy that is missing or unreadable (data-model
// LoadBaseline): the run is baseline-backed and B is deleted.
func TestSweepRecoveryCopy(t *testing.T) {
	cases := []struct {
		name string
		f    tFiles
	}{
		{"record-missing", tFiles{recovery: tBaseline(t, nil), state: true}},
		{"record-unreadable", tFiles{record: []byte(`{"run_id":`), recovery: tBaseline(t, nil), state: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newTWorld(t)
			w.add(tProject, alertB, tPlanned)
			w.write(tc.f)
			if term, err := w.sweep(); err != nil {
				t.Fatalf("Sweep: %v (terminal %q)", err, term)
			}
			tWantOnlyDelete(t, w, alertB)
		})
	}
}

// A lost create (FR-007): a valid baseline, in either place, with no state and no inventory is
// baseline-backed, not pre-create, so B is deleted.
func TestSweepLostCreate(t *testing.T) {
	cases := []struct {
		name string
		f    tFiles
	}{
		{"record-copy", tFiles{record: tBaseline(t, nil)}},
		{"recovery-copy", tFiles{recovery: tBaseline(t, nil)}},
		{"both-copies", tFiles{record: tBaseline(t, nil), recovery: tBaseline(t, nil)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newTWorld(t)
			w.add(tProject, alertB, tPlanned)
			w.write(tc.f)
			if term, err := w.sweep(); err != nil {
				t.Fatalf("Sweep: %v (terminal %q)", err, term)
			}
			tWantOnlyDelete(t, w, alertB)
		})
	}
}

// A pre-create run (no state, no inventory; no baseline, or a failed one) passes the sweep and
// sends no request at all.
func TestSweepPreCreate(t *testing.T) {
	failed := tBaseline(t, func(m map[string]any) {
		m["complete"] = false
		m["errors"] = []string{"GET /cloud/project/<p>/alerting: status 500 (attempt 3 of 3)"}
		m["listed"] = map[string]any{}
	})
	cases := []struct {
		name string
		f    tFiles
	}{
		{"no-baseline", tFiles{}},
		{"failed-baseline", tFiles{record: failed, recovery: failed}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newTWorld(t)
			w.add(tProject, alertB, tPlanned)
			w.write(tc.f)
			if term, err := w.sweep(); err != nil {
				t.Fatalf("Sweep of a pre-create run: %v (terminal %q)", err, term)
			}
			if got := w.seen(); len(got) != 0 {
				t.Errorf("requests %v, want none for a pre-create run", got)
			}
		})
	}
}

// A listed id that is not one path segment, or a project that is not a project id, is refused
// before any request; each row's control, the same operation with a uuid id and the project, is
// sent.
func TestAlertCallsGated(t *testing.T) {
	w := newTWorld(t)
	o := w.options()
	c, err := live.NewAPIChecker(tCtx(t), o.API, o.Admin)
	if err != nil {
		t.Fatal(err)
	}
	list := live.APICall{Op: live.APIListAlerts, Project: tProject}
	get := live.APICall{Op: live.APIGetAlert, Project: tProject, ID: alertA}
	del := live.APICall{Op: live.APIDeleteAlert, Project: tProject, ID: alertA}
	cases := []struct {
		name          string
		refused, sent live.APICall
		want          string // the control's request
	}{
		{"get-dotdot", live.APICall{Op: live.APIGetAlert, Project: tProject, ID: "../x"}, get, "GET " + tAlertPath(tProject, alertA)},
		{"get-no-id", live.APICall{Op: live.APIGetAlert, Project: tProject}, get, "GET " + tAlertPath(tProject, alertA)},
		{"delete-dotdot", live.APICall{Op: live.APIDeleteAlert, Project: tProject, ID: "../x"}, del, "DELETE " + tAlertPath(tProject, alertA)},
		{"delete-slash", live.APICall{Op: live.APIDeleteAlert, Project: tProject, ID: alertA + "/x"}, del, "DELETE " + tAlertPath(tProject, alertA)},
		{"delete-no-id", live.APICall{Op: live.APIDeleteAlert, Project: tProject}, del, "DELETE " + tAlertPath(tProject, alertA)},
		{"list-bad-project", live.APICall{Op: live.APIListAlerts, Project: "../" + tProject}, list, "GET " + tAlertPath(tProject, "")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w.add(tProject, alertA, tPlanned)
			before := len(w.seen())
			_, err := c.Do(tCtx(t), tc.refused)
			if sent := w.seen()[before:]; err == nil || len(sent) != 0 {
				t.Errorf("Do(%+v) = %v, sent %v; want refused before any request", tc.refused, err, sent)
			}
			before = len(w.seen())
			_, err = c.Do(tCtx(t), tc.sent)
			if sent := w.seen()[before:]; err != nil || !slices.Equal(sent, []string{tc.want}) {
				t.Errorf("control Do(%+v) = %v, sent %v; want %s", tc.sent, err, sent, tc.want)
			}
		})
	}
}

type tObservation struct {
	AlertID       string `json:"alert_id"`
	ProjectSHA256 string `json:"project_sha256"`
	Threshold     *int   `json:"monthly_threshold"`
	Delay         *int   `json:"delay"`
	MatchesPlan   *bool  `json:"matches_plan"`
}

func (w *tWorld) observe() ([]byte, string, error) {
	var term bytes.Buffer
	rec, err := Check(w.options()).Observe(tCtx(w.t), map[string]string{"alert_id": alertB}, &term)
	return rec, term.String(), err
}

func tDecode(t *testing.T, rec []byte) tObservation {
	t.Helper()
	var o tObservation
	if err := json.Unmarshal(rec, &o); err != nil {
		t.Fatalf("record %q is not an observation: %v", rec, err)
	}
	if bytes.Contains(rec, []byte(tProject)) {
		t.Errorf("record %s holds the project id, want its digest only", rec)
	}
	return o
}

func tWantRead(t *testing.T, w *tWorld) {
	t.Helper()
	if !slices.Contains(w.seen(), "GET "+tAlertPath(tProject, alertB)) {
		t.Fatalf("the alert was never read in the run's project; requests %v", w.seen())
	}
}

// G28: Observe records what the API reads back on the run's project.
func TestObserveReadsBack(t *testing.T) {
	w := newTWorld(t)
	w.add(tProject, alertB, tPlanned)
	rec, term, err := w.observe()
	if err != nil {
		t.Fatalf("Observe: %v (terminal %q)", err, term)
	}
	tWantRead(t, w)
	o := tDecode(t, rec)
	if o.AlertID != alertB || o.ProjectSHA256 != tDigest(tProject) || o.Threshold == nil || *o.Threshold != 1 ||
		o.Delay == nil || *o.Delay != 3600 || o.MatchesPlan == nil || !*o.MatchesPlan {
		t.Errorf("record %s, want alert %s, the project's digest, threshold 1, delay 3600, matches_plan true", rec, alertB)
	}
}

// A read-back that differs from the plan, or a missing alert, is an error naming it; the record
// holds the API's values with matches_plan false.
func TestObserveMismatch(t *testing.T) {
	cases := []struct {
		name    string
		project string // where B exists
		b       fakeAlert
		names   []string // the error names one of these (lower case)
	}{
		{"threshold-2", tProject, fakeAlert{threshold: 2, delay: 3600, email: tEmail}, []string{"threshold"}},
		{"delay-7200", tProject, fakeAlert{threshold: 1, delay: 7200, email: tEmail}, []string{"delay"}},
		{"404-other-project", tOtherProject, tPlanned, []string{"404", "not found", "missing"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newTWorld(t)
			w.add(tc.project, alertB, tc.b)
			rec, term, err := w.observe()
			tWantRead(t, w)
			if err == nil || !slices.ContainsFunc(tc.names, func(n string) bool { return strings.Contains(strings.ToLower(err.Error()), n) }) {
				t.Fatalf("Observe error %v (terminal %q), want one naming %s", err, term, tc.names)
			}
			if tc.project != tProject {
				return
			}
			o := tDecode(t, rec)
			if o.Threshold == nil || *o.Threshold != tc.b.threshold || o.Delay == nil || *o.Delay != tc.b.delay || o.MatchesPlan == nil || *o.MatchesPlan {
				t.Errorf("record %s, want the API's threshold %d and delay %d with matches_plan false", rec, tc.b.threshold, tc.b.delay)
			}
		})
	}
}
