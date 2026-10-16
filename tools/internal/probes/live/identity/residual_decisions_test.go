package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

// T032's decisions on what T031 left open, pinned against T031's fakes (residual_test.go):
//   - a read allowed again after a refusal is "blocked (flapping)", never a window, and Observe
//     makes Confirm reads after a refusal before the expiry to see it;
//   - a read allowed exactly at the expiry instant is within the lifetime (expires_in is whole
//     seconds), as a refusal there is;
//   - Undo is idempotent: a second Undo, and an Undo after a Sweep, pass and delete nothing.

func TestResidualWindowDecisions(t *testing.T) {
	t0 := time.Date(2026, 10, 16, 11, 20, 0, 0, time.UTC)
	exp := 10 * time.Minute
	deleted := t0.Add(time.Minute)
	untilExp := t0.Add(exp).Sub(deleted)
	ok := Poll{At: t0.Add(30 * time.Second), Status: 200}
	p := func(after time.Duration, status int) Poll { return Poll{At: deleted.Add(after), Status: status} }
	cases := []struct {
		name    string
		polls   []Poll
		dl      bool
		want    Residual
		exposes string
	}{
		{"allowed again after a refusal", []Poll{p(30*time.Second, 200), p(60*time.Second, 401), p(90*time.Second, 200), p(120*time.Second, 401)}, false,
			Residual{"blocked", "flapping", 90 * time.Second, 0, exp}, "a refusal the token outlived reported as the window's end"},
		{"allowed again after a refusal, past an outage", []Poll{p(30*time.Second, 403), p(60*time.Second, 503), p(90*time.Second, 200)}, false,
			Residual{"blocked", "flapping", 90 * time.Second, 0, exp}, "an outage taken to reset a refusal"},
		{"refused, then only refusals and outages", []Poll{p(30*time.Second, 200), p(60*time.Second, 401), p(90*time.Second, 503), p(120*time.Second, 403)}, false,
			Residual{"refused-before-expiry", "", 30 * time.Second, 60 * time.Second, exp}, "an outage after the refusal read as flapping"},
		{"allowed exactly at the expiry, refused after it", []Poll{p(untilExp-30*time.Second, 200), p(untilExp, 200), p(untilExp+30*time.Second, 401)}, false,
			Residual{"valid-until-expiry", "", untilExp, untilExp + 30*time.Second, exp}, "the expiry instant counted as outliving the lifetime"},
		{"allowed a second after the expiry", []Poll{p(untilExp-30*time.Second, 200), p(untilExp+time.Second, 200)}, false,
			Residual{Observed: "blocked", Reason: "expiry"}, "a read after the expiry tolerated"},
		{"a deadline after a refusal", []Poll{p(30*time.Second, 200), p(60*time.Second, 401)}, true,
			Residual{"refused-before-expiry", "", 30 * time.Second, 60 * time.Second, exp}, "an observed refusal discarded by a later deadline"},
	}
	for _, c := range cases {
		got := ResidualWindow(t0, deleted, exp, ok, c.polls, c.dl)
		if c.want.Reason == "expiry" {
			if got.Observed != c.want.Observed || got.Reason != c.want.Reason {
				t.Errorf("%s: got %q/%q, want blocked/expiry (exposes %s)", c.name, got.Observed, got.Reason, c.exposes)
			}
			continue
		}
		if got != c.want {
			t.Errorf("%s: got %+v, want %+v (exposes %s)", c.name, got, c.want, c.exposes)
		}
	}
}

func TestResidualObserveDecisions(t *testing.T) {
	t.Run("confirms a refusal", func(t *testing.T) {
		// Exposes a check that stops at the first refusal and so cannot see flapping.
		w := newRRWorld(t)
		w.after = func(n int, at, expiry time.Time) int { return []int{0, 200, 401}[min(n, 2)] }
		raw, _, err := rrObserve(t, w, w.options())
		tr := w.trace()
		if len(tr.polls) != 2+Confirm {
			t.Errorf("%d polls, want the allowed one, the refusal and %d confirmations", len(tr.polls), Confirm)
		}
		rec := rrParse(t, raw, err)
		if rec.Observed != "refused-before-expiry" || err != nil {
			t.Errorf("observed %q, err %v; want refused-before-expiry and nil", rec.Observed, err)
		}
		wantRecorded(t, rec, tr)
	})
	t.Run("flapping", func(t *testing.T) {
		// Exposes a refusal the token outlived reported as the end of the window.
		w := newRRWorld(t)
		w.after = func(n int, at, expiry time.Time) int { return []int{0, 200, 401, 503, 200, 401}[min(n, 5)] }
		raw, _, err := rrObserve(t, w, w.options())
		tr := w.trace()
		if len(tr.polls) != 4 {
			t.Errorf("%d polls, want a stop at the read allowed again (4)", len(tr.polls))
		}
		rec := rrParse(t, raw, err)
		if lower := secs(tr.polls[3].at.Sub(tr.delAt)); rec.Observed != "blocked" || rec.Reason != "flapping" || rec.Lower != lower || rec.Upper != 0 || err == nil {
			t.Errorf("observed %q reason %q lower %d upper %d err %v; want blocked, flapping, %d, 0 and an error", rec.Observed, rec.Reason, rec.Lower, rec.Upper, err, lower)
		}
		wantRecorded(t, rec, tr)
	})
}

func TestResidualUndoAgain(t *testing.T) {
	deletes := func(w *rrWorld, from int) int {
		n := 0
		for _, r := range w.seen()[from:] {
			if r.method == http.MethodDelete {
				n++
			}
		}
		return n
	}
	t.Run("second undo", func(t *testing.T) {
		// Exposes an Undo that fails on what an earlier Undo removed (--cleanup reruns Undo).
		w := newRRWorld(t)
		w.foreign()
		w.after = func(n int, at, expiry time.Time) int { return []int{0, 200, 401}[min(n, 2)] }
		rrObserve(t, w, w.options())
		c := TokenResidual(w.options())
		if err := c.Undo(context.Background(), rrEnv(), io.Discard); err != nil {
			t.Fatalf("first Undo = %v", err)
		}
		before := len(w.seen())
		var term bytes.Buffer
		if err := c.Undo(context.Background(), rrEnv(), &term); err != nil {
			t.Errorf("second Undo = %v, want nil", err)
		}
		if n := deletes(w, before); n != 0 {
			t.Errorf("second Undo sent %d deletes, want none", n)
		}
		relisted(t, w.seen()[before:])
	})
	t.Run("undo after sweep", func(t *testing.T) {
		w := newRRWorld(t)
		cl := w.addClient(rrName)
		w.addPolicy(rrName, cl.identity)
		if err := w.options().sweep(context.Background(), rrRun, rrProject, io.Discard); err != nil {
			t.Fatalf("sweep = %v", err)
		}
		if err := TokenResidual(w.options()).Undo(context.Background(), rrEnv(), io.Discard); err != nil {
			t.Errorf("Undo after the sweep = %v, want nil", err)
		}
	})
	t.Run("delete answering 404", func(t *testing.T) {
		// Exposes a cleanup that counts a delete of something already gone as a failure: the fake
		// lists a client and policy that a delete no longer finds.
		w := newRRWorld(t)
		cl := w.addClient(rrName)
		w.addPolicy(rrName, cl.identity)
		o := w.options()
		inner := o.API.HTTP.Transport
		if inner == nil {
			inner = http.DefaultTransport
		}
		o.API.HTTP = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			if r.Method == http.MethodDelete {
				w.mu.Lock()
				w.clients, w.policies = nil, nil
				w.mu.Unlock()
			}
			return inner.RoundTrip(r)
		})}
		err := TokenResidual(o).Undo(context.Background(), rrEnv(), io.Discard)
		if err != nil {
			t.Errorf("Undo = %v, want nil: a 404 to a delete is gone already", err)
		}
		if n := deletes(w, 0); n == 0 {
			t.Errorf("no delete was sent, so the 404 path was not exercised")
		}
	})
	t.Run("delete refused", func(t *testing.T) {
		// Control: a delete refused with 403 is a failure naming the run's client.
		w := newRRWorld(t)
		cl := w.addClient(rrName)
		w.addPolicy(rrName, cl.identity)
		w.deleteFail = http.StatusForbidden
		w.keep = true
		err := TokenResidual(w.options()).Undo(context.Background(), rrEnv(), io.Discard)
		if err == nil || !strings.Contains(err.Error(), rrName) || !strings.Contains(err.Error(), "403") {
			t.Errorf("Undo = %v, want an error naming %s and the 403", err, rrName)
		}
	})
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// slow wraps the world's client: after an answer that match picks, the fake clock advances by d
// (the server saw the request at the earlier instant, the check gets the answer at the later one).
func slow(w *rrWorld, o Options, d time.Duration, match func(r *http.Request, n int) bool) Options {
	inner := w.srv.Client().Transport
	n := 0
	o.API.HTTP = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		resp, err := inner.RoundTrip(r)
		w.mu.Lock()
		n++
		if match(r, n) {
			w.clock = w.clock.Add(d)
		}
		w.mu.Unlock()
		return resp, err
	})}
	return o
}

func probeRead(r *http.Request, _ int) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/v1"+rrReadPath
}

// slowBefore is slow with the delay before the server sees the request (in flight on the way
// there): the server's instant is later than the check's send stamp.
func slowBefore(w *rrWorld, o Options, d time.Duration, match func(r *http.Request) bool) Options {
	inner := w.srv.Client().Transport
	o.API.HTTP = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if match(r) {
			w.mu.Lock()
			w.clock = w.clock.Add(d)
			w.mu.Unlock()
		}
		return inner.RoundTrip(r)
	})}
	return o
}

// TestResidualSlowReads (review r2): reads slower than the interval, and a refusal processed
// after it was sent.
func TestResidualSlowReads(t *testing.T) {
	t.Run("control slower than the interval", func(t *testing.T) {
		// Exposes a control sent past Settle when the schedule falls behind: with 45 s reads the
		// sends are 0, 45, 90 s, and the next is due at 120 s but could go out only at 135 s.
		w := newRRWorld(t)
		w.control = slices.Repeat([]int{403}, 1000)
		rrObserve(t, w, slow(w, w.options(), 45*time.Second, probeRead))
		tr := w.trace()
		if n := len(tr.controls); n < 2 || tr.controls[n-1].at.Sub(tr.controls[0].at) > rrSettle {
			t.Errorf("controls %v: want the last within Settle %s of the first", tr.controls, rrSettle)
		}
	})
	t.Run("poll slower than the interval", func(t *testing.T) {
		// Exposes a poll past the expiry plus one interval: with 46 s reads a poll goes out 11 s
		// before the expiry, and the next could go out only 35 s after it.
		w := newRRWorld(t)
		rrObserve(t, w, slow(w, w.options(), 46*time.Second, probeRead))
		tr := w.trace()
		if len(tr.polls) == 0 || !slices.ContainsFunc(tr.polls, func(p rrReq) bool { return p.at.After(w.expiry.Add(-15*time.Second)) && !p.at.After(w.expiry) }) {
			t.Fatalf("the case did not put a poll in the last 15 s before the expiry: %v", tr.polls)
		}
		wantBounded(t, w, tr)
	})
	t.Run("refusal processed after it was sent", func(t *testing.T) {
		// Exposes an Upper taken from the refusal's send: the server refused 10 s later, so
		// "refused by" the send instant understates the window.
		w := newRRWorld(t)
		w.after = func(n int, at, expiry time.Time) int { return []int{0, 200, 401}[min(n, 2)] }
		raw, _, err := rrObserve(t, w, slowBefore(w, w.options(), 10*time.Second, func(r *http.Request) bool { return probeRead(r, 0) }))
		tr := w.trace()
		f := slices.IndexFunc(tr.polls, func(p rrReq) bool { return p.status == http.StatusUnauthorized })
		if f < 0 {
			t.Fatalf("no refusal: %v", tr.polls)
		}
		var rec rrRecord
		if raw == nil || json.Unmarshal(raw, &rec) != nil {
			t.Fatalf("no record (err %v)", err)
		}
		if upper := secs(tr.polls[f].at.Sub(tr.delAt)); rec.Observed != "refused-before-expiry" || rec.Upper < upper {
			t.Errorf("observed %q upper %d, want refused-before-expiry with upper ≥ %d (the server's refusal)", rec.Observed, rec.Upper, upper)
		}
	})
}

// TestResidualLatency (review r1): the bounds hold when the requests themselves take time.
func TestResidualLatency(t *testing.T) {
	t.Run("control within Settle", func(t *testing.T) {
		// Exposes retries spaced by the interval plus each read's latency, running past Settle:
		// with 15 s reads they go out at 0, 45, 90 and 135 s against a Settle of 120 s.
		w := newRRWorld(t)
		w.control = slices.Repeat([]int{403}, 1000)
		raw, _, err := rrObserve(t, w, slow(w, w.options(), 15*time.Second, probeRead))
		tr := w.trace()
		n := len(tr.controls)
		if n < 2 {
			t.Fatalf("%d control reads", n)
		}
		if span := tr.controls[n-1].at.Sub(tr.controls[0].at); span > rrSettle || span < rrSettle-rrInterval {
			t.Errorf("control retries spanned %s with 15 s reads, want Settle %s used up and not exceeded", span, rrSettle)
		}
		if rec := rrParse(t, raw, err); rec.Reason != "control" {
			t.Errorf("reason %q, want control", rec.Reason)
		}
	})
	t.Run("polls within the expiry plus one interval", func(t *testing.T) {
		// Exposes a poll after the bound: with 21 s reads, polls spaced 51 s put one 43 s past
		// the expiry.
		w := newRRWorld(t)
		raw, _, err := rrObserve(t, w, slow(w, w.options(), 21*time.Second, probeRead))
		tr := w.trace()
		wantBounded(t, w, tr)
		if rec := rrParse(t, raw, err); rec.Observed != "valid-until-expiry" {
			t.Errorf("observed %q, want valid-until-expiry", rec.Observed)
		}
	})
	t.Run("refusal straddling the expiry", func(t *testing.T) {
		// Exposes a token that simply expired reported as revoked early: the token answer took
		// 29.5 s, so the expiry computed from its arrival is 29.5 s later than the server's, and a
		// refusal at the server's expiry falls before it.
		w := newRRWorld(t)
		// The probe token is the second token request (the admin's is the first).
		o := slow(w, w.options(), 29500*time.Millisecond, func(r *http.Request, n int) bool { return r.URL.Path == "/token" && n > 1 })
		raw, _, err := rrObserve(t, w, o)
		tr := w.trace()
		f := slices.IndexFunc(tr.polls, func(p rrReq) bool { return p.status == http.StatusUnauthorized })
		if f < 1 || tr.polls[f].at.Before(w.expiry) || tr.polls[f].at.After(w.expiry.Add(time.Second)) || tr.polls[f-1].status != http.StatusOK {
			t.Fatalf("the case did not put the first refusal at the server's expiry after an allowed poll: %v", tr.polls)
		}
		rec := rrParse(t, raw, err)
		if upper := secs(tr.polls[f].at.Sub(tr.delAt)); rec.Observed != "valid-until-expiry" || rec.Upper < upper {
			t.Errorf("observed %q lower %d upper %d, want valid-until-expiry with upper ≥ %d: the refusal came at the server's expiry", rec.Observed, rec.Lower, rec.Upper, upper)
		}
	})
}

// TestResidualSetupFails: a policy create refused after the client exists stops before any
// delete, records blocked (setup), and Undo removes the client by name.
func TestResidualSetupFails(t *testing.T) {
	w := newRRWorld(t)
	o := w.options()
	inner := w.srv.Client().Transport
	o.API.HTTP = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost && r.URL.Path == "/v2/iam/policy" {
			return &http.Response{StatusCode: http.StatusForbidden, Body: io.NopCloser(strings.NewReader(`{"message":"refused"}`)), Header: http.Header{}, Request: r}, nil
		}
		return inner.RoundTrip(r)
	})}
	raw, _, err := rrObserve(t, w, o)
	tr := w.trace()
	if err == nil || tr.del >= 0 || len(tr.tokenReqs) != 0 {
		t.Errorf("Observe = %v, delete at %d, %d probe tokens; want an error, no delete, no token", err, tr.del, len(tr.tokenReqs))
	}
	if rec := rrParse(t, raw, err); rec.Observed != "blocked" || rec.Reason != "setup" {
		t.Errorf("observed %q reason %q, want blocked and setup", rec.Observed, rec.Reason)
	}
	if cl, _ := w.names(); len(cl) != 1 {
		t.Fatalf("precondition: clients %q, want the one Observe created", cl)
	}
	if err := TokenResidual(w.options()).Undo(context.Background(), rrEnv(), io.Discard); err != nil {
		t.Errorf("Undo = %v", err)
	}
	if cl, pol := w.names(); len(cl) != 0 || len(pol) != 0 {
		t.Errorf("after Undo clients %q policies %q", cl, pol)
	}
}
