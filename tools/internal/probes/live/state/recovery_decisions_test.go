package state

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
)

// T036's decisions on what T035 left open, pinned against T035's fakes (recovery_test.go):
//   - Undo converges (coordinator decision 2026-10-16, T038's option A): a second Undo passes and
//     deletes nothing; an Undo after a Sweep removed the bucket or the janitor (404 NoSuchBucket,
//     403 InvalidAccessKeyId) passes; any other refusal still fails it. Sweep is wired.
//   - The conditional PUT writes lock info carrying the held lock's id, so an ignored condition
//     does not turn the recovery into `blocked (lock id mismatch)`: force-unlock compares the
//     object's ID (OpenTofu 1.13.0 internal/backend/remote-state/s3/client.go:524-557), which the
//     fake now does too (Test changes, T036).
//   - A lock id is recorded only in OpenTofu's shape (a UUID); any other text in its place is
//     `blocked (lock id unrecognised)`, unbroken and unrecorded.

// TestRecoveryUndoAgain: Undo after an Undo, or after a Sweep, converges.
func TestRecoveryUndoAgain(t *testing.T) {
	if LockedRecovery(RecoveryOptions{}).Sweep == nil {
		t.Errorf("LockedRecovery has no Sweep")
	}

	t.Run("second-undo", func(t *testing.T) {
		// Exposes an Undo that fails on what an earlier Undo removed (--cleanup reruns Undo).
		w := newRcWorld(t)
		rcObserve(t, w)
		c := LockedRecovery(w.options())
		if err := c.Undo(context.Background(), rcEnv(), io.Discard); err != nil {
			t.Fatalf("first Undo = %v", err)
		}
		before := len(w.events())
		if err := c.Undo(context.Background(), rcEnv(), io.Discard); err != nil {
			t.Errorf("second Undo = %v, want nil", err)
		}
		for _, e := range w.events()[before:] {
			if e.cmd == "delete" || e.cmd == "put" || strings.HasSuffix(e.cmd, "-put") || e.tofu {
				t.Errorf("second Undo sent %v, want no change", e)
			}
		}
		if left := w.left(); len(left) != 0 {
			t.Errorf("%d versions left", len(left))
		}
	})

	for _, v := range []struct {
		name      string
		status    int
		code      string
		stateCode string // the state user's answer when it differs ("": the same)
		ok        bool
		listed    bool   // the admin's listing still shows the bucket
		project   string // "": rcProject; "none": no project
	}{
		{"bucket-swept", http.StatusNotFound, "NoSuchBucket", "", true, false, ""},
		{"both-users-swept", http.StatusForbidden, "InvalidAccessKeyId", "", true, false, ""},
		// Review r2: both keys refused proves nothing about the bucket (keys revoked by hand): the
		// admin's listing must not show it, and without a project to list Undo fails.
		{"both-keys-bucket-listed", http.StatusForbidden, "InvalidAccessKeyId", "", false, true, ""},
		{"both-keys-no-project", http.StatusForbidden, "InvalidAccessKeyId", "", false, false, "none"},
		// Review r1: an invalid janitor key beside a state key that answers proves nothing about
		// the bucket (a key not yet in effect): Undo fails and keeps the root.
		{"janitor-key-only", http.StatusForbidden, "InvalidAccessKeyId", "AccessDenied", false, false, ""},
		// Controls: a refusal that is not "gone" still fails Undo, which keeps the root.
		{"access-denied", http.StatusForbidden, "AccessDenied", "", false, false, ""},
		{"no-such-key", http.StatusNotFound, "NoSuchKey", "", false, false, ""},
	} {
		t.Run("after-sweep/"+v.name, func(t *testing.T) {
			var mu sync.Mutex
			var seen []string
			srv := httptest.NewTLSServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
				mu.Lock()
				seen = append(seen, r.Method+" "+r.URL.RawQuery)
				mu.Unlock()
				code := v.code
				if v.stateCode != "" && strings.Contains(r.Header.Get("Authorization"), "Credential="+rcStateAK+"/") {
					code = v.stateCode
				}
				rw.WriteHeader(v.status)
				fmt.Fprintf(rw, "<Error><Code>%s</Code><Message>%s %s</Message></Error>", code, rcEcho, rcJanSK)
			}))
			t.Cleanup(srv.Close)
			pool := x509.NewCertPool()
			pool.AddCert(srv.Certificate())
			addr := srv.Listener.Addr().String()
			api := newRcWorld(t)
			if v.listed {
				api.buckets["SBG"] = []string{rcBucket}
			}
			o := api.options()
			o.Project = rcProject
			if v.project == "none" {
				o.Project = ""
			}
			o.HTTP = &http.Client{Transport: &http.Transport{
				TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: "example.com"},
				DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, network, addr)
				},
			}}
			var term bytes.Buffer
			err := LockedRecovery(o).Undo(context.Background(), rcEnv(), &term)
			if v.ok && (err != nil || !strings.Contains(term.String(), "already removed")) {
				t.Errorf("Undo = %v, terminal %q; want nil and `already removed`", err, term.String())
			}
			if !v.ok && err == nil {
				t.Errorf("Undo = nil on %d %s, want an error (the root is kept)", v.status, v.code)
			}
			mu.Lock()
			defer mu.Unlock()
			for _, s := range seen {
				if strings.HasPrefix(s, http.MethodDelete) || strings.HasPrefix(s, http.MethodPut) {
					t.Errorf("Undo sent %s to a gone bucket", s)
				}
			}
			if leaks := rcLeaks(term.String(), fmt.Sprint(err)); len(leaks) > 0 || strings.Contains(term.String()+fmt.Sprint(err), rcEcho) {
				t.Errorf("terminal or error carry the server's text or %q", leaks)
			}
		})
	}
}

// TestRecoveryConditionIgnoredKeepsLock: with the condition ignored, the check's own PUT must not
// break the recovery it observes. Exposes a control that replaces the held lock's id.
func TestRecoveryConditionIgnoredKeepsLock(t *testing.T) {
	w := newRcWorld(t)
	w.condIgnored = true
	raw, _, err := rcObserve(t, w)
	evs := w.events()
	var lock struct{ ID, Path string }
	conds := rcS3(evs, "cond-put")
	if len(conds) != 1 || json.Unmarshal(conds[0].body, &lock) != nil || lock.ID != w.lastLock || lock.Path != rcBucket+"/"+rcStateKey {
		t.Errorf("conditional PUT body %q, want lock info with the held lock's ID %s and Path %s/%s", firstBody(conds), w.lastLock, rcBucket, rcStateKey)
	}
	rcWantFlow(t, evs, []string{"apply", "plan", "force-unlock", "plan"}, w.lastLock)
	rec := rcParse(t, raw, err)
	m := rcByStep(t, rec)
	rcWant(t, m["lock-conditional-put"], "condition-ignored", 200, "", "")
	rcWant(t, m["force-unlock"], "allowed", -1, "", "")
	rcWant(t, m["writer-b-plan-again"], "allowed", -1, "", "")
	if rec.Observed != "recovered" || !rcFinding(rec, "lock-conditional-put") || err != nil {
		t.Errorf("record observed %q findings %q, Observe = %v; want recovered, the condition finding, nil", rec.Observed, rec.Findings, err)
	}
}

func firstBody(evs []rcEvent) string {
	if len(evs) == 0 {
		return ""
	}
	return string(evs[0].body)
}

// TestRecoveryLockIDShape: B's refusal names a lock id that is not OpenTofu's UUID (a server's
// text, or a secret, in its place): blocked, no conditional PUT, no unlock, nothing recorded.
func TestRecoveryLockIDShape(t *testing.T) {
	for name, id := range map[string]string{"text": "not-a-lock-id", "secret": rcStateSK, "upper-case": "5F1C0A2E-7B3D-4C8E-9A61-000000000001"} {
		t.Run(name, func(t *testing.T) {
			w := newRcWorld(t)
			w.apply = func() (TofuRun, error) {
				w.lastLock = id // the diagnostic B's plan prints names this id
				return TofuRun{Exit: -1, Signaled: true}, nil
			}
			raw, _, err := rcObserve(t, w)
			evs := w.events()
			rcWantFlow(t, evs, []string{"apply", "plan"}, "")
			if len(rcS3(evs, "cond-put")) != 0 {
				t.Errorf("a conditional PUT ran: %v", evs)
			}
			rec := rcParse(t, raw, err)
			m := rcByStep(t, rec)
			rcWant(t, m["writer-b-plan"], "blocked", -1, "", "lock id unrecognised")
			if strings.Contains(string(raw), id) || rec.Observed != "blocked" || err != nil {
				t.Errorf("record %s, Observe = %v; want blocked without the id, nil", raw, err)
			}
		})
	}
}

// TestRecoveryPlanClassifier (review r1): OpenTofu prints the lock info it finds after any failed
// lock PUT (client.go:336-345), so B's refusal is `refused (locked)` only beside the conditional
// PUT's own 412; the same diagnostic with a 5xx is an outage. Exposes an outage read as a lock.
func TestRecoveryPlanClassifier(t *testing.T) {
	fx := rcLoadFixtures(t)
	id := "5f1c0a2e-7b3d-4c8e-9a61-0000000000aa"
	held := strings.NewReplacer("@LOCK_ID@", id, "@PATH@", rcBucket+"/"+rcStateKey).Replace(fx["lock-held"].text)
	if o, r, got := recPlan(TofuRun{Exit: 1, Output: []byte(held)}, nil, rcBucket); o != refused || r != "locked" || got != id {
		t.Fatalf("lock-held = %s %s %s, want refused locked %s", o, r, got, id)
	}
	for name, text := range map[string]string{
		"503": strings.NewReplacer("StatusCode: 412", "StatusCode: 503", "PreconditionFailed", "SlowDown").Replace(held),
		"500": strings.NewReplacer("StatusCode: 412", "StatusCode: 500", "PreconditionFailed", "InternalError").Replace(held),
		"403": strings.NewReplacer("StatusCode: 412", "StatusCode: 403", "PreconditionFailed", "AccessDenied").Replace(held),
	} {
		if o, _, got := recPlan(TofuRun{Exit: 1, Output: []byte(text)}, nil, rcBucket); o != blocked || got != "" {
			t.Errorf("%s beside lock info = %s (lock id %q), want blocked", name, o, got)
		}
	}
}

// TestRecoveryConditionalPutOtherRefusal (review r1): a conditional PUT refused for another reason
// than the condition (403) is not as expected and carries a reason, but it is no finding: it
// refutes nothing. Exposes a self-contradictory finding "expected refused, observed refused".
func TestRecoveryConditionalPutOtherRefusal(t *testing.T) {
	w := newRcWorld(t)
	w.s3Script["cond-put"] = []rcReply{{status: 403, code: "AccessDenied"}}
	raw, _, err := rcObserve(t, w)
	m := rcByStep(t, rcParse(t, raw, err))
	c := m["lock-conditional-put"]
	rcWant(t, c, "refused", 403, "AccessDenied", "")
	rec := rcParse(t, raw, err)
	if c.AsExpected || c.Reason == "" || rcFinding(rec, "lock-conditional-put") || err != nil {
		t.Errorf("observation %+v findings %q, Observe = %v; want not as expected, a reason, no finding, nil", c, rec.Findings, err)
	}
	if rec.Observed != "recovered" {
		t.Errorf("record observed %q, want recovered (the refusal changed nothing)", rec.Observed)
	}
}

// TestRecoverySweepKeepsJanitor (review r1): when the bucket's delete fails (retained versions),
// Sweep keeps the janitor, the only identity that can empty it, and fails; the state user goes.
// Exposes a Sweep that deletes the credential a later Undo needs.
func TestRecoverySweepKeepsJanitor(t *testing.T) {
	w := newRcWorld(t)
	w.buckets["GRA"] = []string{rcBucket}
	w.users = []rcAPIUser{{ID: 401, Description: rcUsers(rcRun)[0]}, {ID: 402, Description: rcUsers(rcRun)[1]}}
	o := w.options()
	base := http.DefaultTransport
	o.API.HTTP = &http.Client{Transport: rcTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/storage/") {
			return &http.Response{StatusCode: http.StatusConflict, Body: io.NopCloser(strings.NewReader(`{"message":"bucket not empty"}`)), Header: http.Header{}, Request: r}, nil
		}
		return base.RoundTrip(r)
	})}
	err := o.sweep(context.Background(), rcRun, rcProject, io.Discard)
	var deleted []string
	for _, e := range w.events() {
		if strings.HasPrefix(e.cmd, "api DELETE ") && strings.Contains(e.cmd, "/user/") {
			deleted = append(deleted, e.cmd)
		}
	}
	if err == nil || len(deleted) != 1 || !strings.HasSuffix(deleted[0], "/user/401") {
		t.Errorf("sweep = %v, deleted users %q; want an error and only the state user (401) deleted", err, deleted)
	}
}

type rcTrip func(*http.Request) (*http.Response, error)

func (f rcTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestRecoveryWriterALockNotTaken (review r2): writer A exits before its prompt because its lock
// PUT failed: `blocked (lock not taken …)`, not "owner alive"; nothing is broken. Exposes a lock
// never taken reported as an owner that may still hold it.
func TestRecoveryWriterALockNotTaken(t *testing.T) {
	fx := rcLoadFixtures(t)
	text := strings.NewReplacer("@LOCK_ID@", "", "@PATH@", "", "StatusCode: 412", "StatusCode: 400", "PreconditionFailed", "InvalidRequest").Replace(fx["lock-held"].text)
	w := newRcWorld(t)
	w.noLock = true
	w.apply = func() (TofuRun, error) { return TofuRun{Exit: 1, Output: []byte(text)}, nil }
	raw, _, err := rcObserve(t, w)
	if slices.ContainsFunc(w.events(), func(e rcEvent) bool { return e.tofu && e.cmd == "force-unlock" }) {
		t.Errorf("force-unlock ran")
	}
	rec := rcParse(t, raw, err)
	m := rcByStep(t, rec)
	if a := m["writer-a-apply"]; a.Observed != "blocked" || !strings.HasPrefix(a.Reason, "lock not taken") || rec.Reason != a.Reason || err != nil {
		t.Errorf("writer-a-apply %+v, record reason %q, Observe = %v; want blocked (lock not taken …), nil", a, rec.Reason, err)
	}
}

// TestRecoveryRestoreWithoutVersionID (review r2): a restore PUT answered 2xx without a version id
// leaves the restore unshown: that PUT is not as expected and says why. Exposes a run reported
// clean although C005.8 was never shown.
func TestRecoveryRestoreWithoutVersionID(t *testing.T) {
	w := newRcWorld(t)
	w.s3Script["put"] = []rcReply{{status: 200}}
	raw, _, err := rcObserve(t, w)
	m := rcByStep(t, rcParse(t, raw, err))
	if p := m["restore-put-v1"]; p.Observed != "allowed" || p.AsExpected || p.Reason == "" {
		t.Errorf("restore-put-v1 %+v, want allowed, not as expected, a reason", p)
	}
	rcWant(t, m["restore-copy"], "not-run", -1, "", "")
}
