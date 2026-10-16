package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

// T034's decisions on what T033 left open, pinned against T033's fakes (urn_test.go):
//   - Undo is idempotent: a second Undo, and an Undo after a Sweep, pass and change nothing;
//   - Sweep also deletes the probe bucket a lost create answer left outside the state
//     (ProbeCheck.Sweep: every resource the root or the check may have created), never another;
//   - the actions a refusal names are filtered against every identity's token and secret, not
//     only the caller's;
//   - each attempt keeps its send and answer instants; policy_age_seconds runs to the send;
//   - a blocked result fails Observe; the record says how the bucket listing went and which URNs
//     it showed under the bucket's name;
//   - a setup failure records it, makes no probe call, and Undo removes what was created.

// uuRecordT034 reads what T034 adds to the record.
type uuRecordT034 struct {
	BucketListing string   `json:"bucket_listing"`
	BucketURNs    []string `json:"bucket_urns_listed"`
	Setup         string   `json:"setup"`
	Attempts      []struct {
		At         time.Time `json:"at"`
		AnsweredAt time.Time `json:"answered_at"`
		PolicyAge  int64     `json:"policy_age_seconds"`
		Identity   string    `json:"identity"`
		Actions    []string  `json:"actions"`
	} `json:"attempts"`
}

func uuDecode(t *testing.T, raw []byte, err error) uuRecordT034 {
	t.Helper()
	var rec uuRecordT034
	if raw == nil || json.Unmarshal(raw, &rec) != nil {
		t.Fatalf("no readable record (err %v)", err)
	}
	return rec
}

type uuTrip func(*http.Request) (*http.Response, error)

func (f uuTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestURNUndoAgain(t *testing.T) {
	mutations := func(reqs []uuReq) int {
		n := 0
		for _, r := range reqs {
			if uuMutation(r) {
				n++
			}
		}
		return n
	}
	t.Run("second undo", func(t *testing.T) {
		// Exposes an Undo that fails on what an earlier Undo removed (--cleanup reruns Undo).
		w := newUUWorld(t)
		w.foreign()
		uuObserve(t, w)
		c := IamURN(w.options())
		if err := c.Undo(context.Background(), uuEnv(), io.Discard); err != nil {
			t.Fatalf("first Undo = %v", err)
		}
		before := len(w.seen())
		if err := c.Undo(context.Background(), uuEnv(), io.Discard); err != nil {
			t.Errorf("second Undo = %v, want nil", err)
		}
		again := w.seen()[before:]
		if n := mutations(again); n != 0 {
			t.Errorf("second Undo sent %d changes, want none", n)
		}
		uuRelisted(t, again)
		uuClean(t, w)
	})
	t.Run("undo after sweep", func(t *testing.T) {
		w := newUUWorld(t)
		c := w.addClient(uuNameA)
		w.addPolicy(w.grantFor("account", c))
		w.tagBucket()
		if err := w.options().sweepURN(context.Background(), uuRun, uuProject, io.Discard); err != nil {
			t.Fatalf("sweep = %v", err)
		}
		before := len(w.seen())
		if err := IamURN(w.options()).Undo(context.Background(), uuEnv(), io.Discard); err != nil {
			t.Errorf("Undo after the sweep = %v, want nil", err)
		}
		if n := mutations(w.seen()[before:]); n != 0 {
			t.Errorf("Undo after the sweep sent %d changes, want none", n)
		}
	})
	t.Run("delete answering 404", func(t *testing.T) {
		// Exposes a cleanup that counts a delete of something already gone as a failure: the
		// fake lists a client, a policy and the tag key that a delete no longer finds (another
		// cleanup ran in between).
		w := newUUWorld(t)
		c := w.addClient(uuNameP)
		w.addPolicy(w.grantFor("project", c))
		w.tagBucket()
		o := w.options()
		inner := w.srv.Client().Transport
		o.API.HTTP = &http.Client{Transport: uuTrip(func(r *http.Request) (*http.Response, error) {
			if r.Method == http.MethodDelete {
				w.mu.Lock()
				w.clients, w.policies = nil, nil
				delete(w.resources[0].tags, uuTagKey)
				w.mu.Unlock()
			}
			return inner.RoundTrip(r)
		})}
		before := len(w.seen())
		if err := IamURN(o).Undo(context.Background(), uuEnv(), io.Discard); err != nil {
			t.Errorf("Undo = %v, want nil: a 404 to a delete is gone already", err)
		}
		if n := mutations(w.seen()[before:]); n < 3 {
			t.Errorf("%d deletes sent, want the client's, the policy's and the tag key's (the 404 path exercised)", n)
		}
	})
	t.Run("untags only the run's bucket", func(t *testing.T) {
		// Exposes a cleanup that removes the run's key from any resource listed under the
		// bucket's name: one whose URN does not end in the bucket keeps its tags.
		w := newUUWorld(t)
		elsewhere := "urn:v1:eu:resource:storageS3:" + uuProject + "/elsewhere"
		w.resources = append(w.resources, uuRes{elsewhere, uuBucket, map[string]string{uuTagKey: uuRun}, true})
		w.tagBucket()
		if err := IamURN(w.options()).Undo(context.Background(), uuEnv(), io.Discard); err != nil {
			t.Errorf("Undo = %v", err)
		}
		if got := w.tags(elsewhere); got[uuTagKey] != uuRun {
			t.Errorf("tags of %s = %v, want the key kept (not the run's bucket)", elsewhere, got)
		}
		if got := w.tags(uuBucketURN); got[uuTagKey] != "" {
			t.Errorf("the run's bucket still carries %s: %v", uuTagKey, got)
		}
	})
}

func TestURNSweepBucket(t *testing.T) {
	others := []string{uuOtherBkt, uuBucket + "-x", "lzprobe-iam-urn-" + uuRun}
	t.Run("lost create", func(t *testing.T) {
		// Exposes a probe bucket a lost create answer left outside the state and the destroy.
		w := newUUWorld(t)
		w.foreign()
		w.buckets = append([]string{uuBucket}, others...)
		var term bytes.Buffer
		before := len(w.seen())
		err := w.options().sweepURN(context.Background(), uuRun, uuProject, &term)
		reqs := w.seen()[before:]
		if err != nil {
			t.Errorf("sweep = %v, want nil", err)
		}
		if !slices.Equal(w.buckets, others) {
			t.Errorf("buckets %q left, want only %q", w.buckets, others)
		}
		del := slices.IndexFunc(reqs, func(r uuReq) bool { return r.method == http.MethodDelete && strings.Contains(r.path, "/storage/") })
		relist := slices.IndexFunc(reqs[del+1:], func(r uuReq) bool {
			return r.method == http.MethodGet && strings.HasSuffix(r.path, "/region/GRA/storage")
		})
		if del < 0 || relist < 0 {
			t.Errorf("want the bucket deleted and the buckets listed again after: %v", reqs)
		}
		uuAdminOnly(t, reqs)
		uuClean(t, w)
	})
	t.Run("still listed", func(t *testing.T) {
		w := newUUWorld(t)
		w.buckets = []string{uuBucket}
		w.keep = true
		err := w.options().sweepURN(context.Background(), uuRun, uuProject, io.Discard)
		if err == nil || !strings.Contains(err.Error(), uuBucket) {
			t.Errorf("sweep = %v, want an error naming the bucket left (%s)", err, uuBucket)
		}
	})
	t.Run("undo leaves the bucket to the destroy", func(t *testing.T) {
		// Exposes an Undo that deletes the root's bucket before tofu destroys it.
		w := newUUWorld(t)
		w.buckets = []string{uuBucket}
		if err := IamURN(w.options()).Undo(context.Background(), uuEnv(), io.Discard); err != nil {
			t.Errorf("Undo = %v", err)
		}
		if !slices.Equal(w.buckets, []string{uuBucket}) {
			t.Errorf("buckets %q after Undo, want the root's bucket kept for the destroy", w.buckets)
		}
	})
}

func TestURNActionsFiltered(t *testing.T) {
	// Exposes an answer's actions filtered against the caller's token only: the account
	// identity's refusal names the admin's token and the project identity's token and secret as
	// action-shaped words, and the action itself.
	w := newUUWorld(t)
	o := w.options()
	inner := w.srv.Client().Transport
	o.API.HTTP = &http.Client{Transport: uuTrip(func(r *http.Request) (*http.Response, error) {
		w.mu.Lock()
		id, isProbe := w.tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		var words []string
		if isProbe && r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v2/iam/policy/") {
			for _, c := range w.clients {
				if c.id == id && c.name == uuNameA {
					words = append(words, "account:apiovh:"+uuAdminTok, "account:apiovh:x"+uuAdminSK)
					for tok, other := range w.tokens {
						if other != id {
							words = append(words, "account:apiovh:"+tok)
						}
					}
					for _, s := range w.secrets {
						words = append(words, "account:apiovh:"+s)
					}
				}
			}
		}
		w.mu.Unlock()
		if words == nil {
			return inner.RoundTrip(r)
		}
		body := fmt.Sprintf(`{"message":"refused","details":{"unauthorizedActionsByIAM":%q}}`, strings.Join(append(words, uuRead), ","))
		return &http.Response{StatusCode: http.StatusForbidden, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: r}, nil
	})}
	var term bytes.Buffer
	raw, err := IamURN(o).Observe(context.Background(), uuEnv(), &term)
	if leaks := w.leaks(string(raw), term.String(), fmt.Sprint(err)); len(leaks) > 0 {
		t.Errorf("record, terminal or error carry %q", leaks)
	}
	rec := uuDecode(t, raw, err)
	found := false
	for _, a := range rec.Attempts {
		if a.Identity == "account" && slices.Equal(a.Actions, []string{uuRead}) {
			found = true
		}
	}
	if !found {
		t.Errorf("no account attempt kept exactly the named action %s: %+v", uuRead, rec.Attempts)
	}
}

func TestURNBothInstants(t *testing.T) {
	// Exposes an attempt timed from one edge only: every probe identity's answer arrives 7 s
	// after the server saw the request.
	w := newUUWorld(t)
	o := w.options()
	inner := w.srv.Client().Transport
	o.API.HTTP = &http.Client{Transport: uuTrip(func(r *http.Request) (*http.Response, error) {
		resp, err := inner.RoundTrip(r)
		w.mu.Lock()
		if _, ok := w.tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]; ok {
			w.clock = w.clock.Add(7 * time.Second)
		}
		w.mu.Unlock()
		return resp, err
	})}
	raw, err := IamURN(o).Observe(context.Background(), uuEnv(), io.Discard)
	rec := uuDecode(t, raw, err)
	seen := w.probes()
	if len(rec.Attempts) != len(seen) || len(seen) == 0 {
		t.Fatalf("%d attempts, %d calls", len(rec.Attempts), len(seen))
	}
	created := w.created
	for i, a := range rec.Attempts {
		if !a.At.Equal(seen[i].at) || !a.AnsweredAt.Equal(seen[i].at.Add(7*time.Second)) {
			t.Errorf("attempt %d at %s answered %s, want %s and 7 s later", i, a.At, a.AnsweredAt, seen[i].at)
		}
		if want := secs(seen[i].at.Sub(created[uuIdent[a.Identity].name])); a.PolicyAge != want {
			t.Errorf("attempt %d policy age %d, want %d (to the send)", i, a.PolicyAge, want)
		}
	}
}

func TestURNObserveError(t *testing.T) {
	for _, tc := range []struct {
		name   string
		script func(w *uuWorld)
		want   []string // in the error; none: Observe succeeds
	}{
		{"verdicts", func(w *uuWorld) { w.answer["project write"] = uuRefuse(uuWrite) }, nil},
		{"both refused", func(w *uuWorld) {
			w.answer["project read"], w.answer["account read"] = uuRefuse(uuRead), uuRefuse(uuRead)
		}, []string{uuRead, "refused"}},
		{"no owned target", func(w *uuWorld) { w.resources[0].listed = false }, []string{uuWrite, "no owned target"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Exposes a blocked result that passes as an observation.
			w := newUUWorld(t)
			tc.script(w)
			_, _, err := uuObserve(t, w)
			if tc.want == nil && err != nil {
				t.Errorf("Observe = %v, want nil", err)
			}
			for _, s := range tc.want {
				if err == nil || !strings.Contains(err.Error(), s) {
					t.Errorf("Observe = %v, want an error naming %q", err, s)
				}
			}
		})
	}
}

func TestURNListedURNs(t *testing.T) {
	// Exposes a record that hides what the listing showed: T027 learns the bucket URN's form
	// from it.
	for _, tc := range []struct {
		name string
		urn  string // "": not listed
	}{{"another urn", "urn:v1:eu:resource:storageS3:" + uuProject + "/" + uuBucket + "-other"}, {"not listed", ""},
		// Review r1: a listed URN holding the admin's token is server text with a secret in it.
		{"urn holding the admin token", "urn:v1:eu:resource:storageS3:" + uuAdminTok + "/" + uuBucket}} {
		t.Run(tc.name, func(t *testing.T) {
			w := newUUWorld(t)
			if tc.urn == "" {
				w.resources[0].listed = false
			} else {
				w.resources[0].urn = tc.urn
			}
			raw, _, err := uuObserve(t, w)
			rec := uuDecode(t, raw, err)
			want := []string{}
			if tc.urn != "" && !strings.Contains(tc.urn, uuAdminTok) {
				want = []string{tc.urn}
			}
			if !slices.Equal(rec.BucketURNs, want) || rec.BucketListing != "answered 200" {
				t.Errorf("bucket listing %q urns %q, want answered 200 and %q", rec.BucketListing, rec.BucketURNs, want)
			}
		})
	}
}

func TestURNSetupFails(t *testing.T) {
	// Exposes a check that probes without its second identity's policy, or an Undo that leaves
	// what a failed setup created.
	w := newUUWorld(t)
	o := w.options()
	inner := w.srv.Client().Transport
	o.API.HTTP = &http.Client{Transport: uuTrip(func(r *http.Request) (*http.Response, error) {
		w.mu.Lock()
		second := len(w.policies) == 1
		w.mu.Unlock()
		if r.Method == http.MethodPost && r.URL.Path == "/v2/iam/policy" && second {
			return &http.Response{StatusCode: http.StatusForbidden, Body: io.NopCloser(strings.NewReader(`{"message":"refused"}`)), Header: http.Header{}, Request: r}, nil
		}
		return inner.RoundTrip(r)
	})}
	var term bytes.Buffer
	raw, err := IamURN(o).Observe(context.Background(), uuEnv(), &term)
	if err == nil || len(w.probes()) != 0 {
		t.Errorf("Observe = %v after %d probe calls, want an error and none", err, len(w.probes()))
	}
	if rec := uuDecode(t, raw, err); rec.Setup == "" || !strings.Contains(rec.Setup, uuNameA) {
		t.Errorf("setup %q, want it to name %s", rec.Setup, uuNameA)
	}
	if err := IamURN(w.options()).Undo(context.Background(), uuEnv(), io.Discard); err != nil {
		t.Errorf("Undo = %v", err)
	}
	if cl, pol := w.names(); len(cl)+len(pol) != 0 {
		t.Errorf("after Undo clients %q policies %q", cl, pol)
	}
}

func TestURNSlowControl(t *testing.T) {
	// Exposes a control sent past Settle when each control takes 45 s to answer (sends at 0, 45,
	// 90 s; the next is due at 120 s but could go out only at 135 s).
	w := newUUWorld(t)
	w.control["account"] = slices.Repeat([]int{403}, 1000)
	o := w.options()
	inner := w.srv.Client().Transport
	o.API.HTTP = &http.Client{Transport: uuTrip(func(r *http.Request) (*http.Response, error) {
		resp, err := inner.RoundTrip(r)
		if r.URL.Path == "/v1/me" {
			w.mu.Lock()
			w.clock = w.clock.Add(45 * time.Second)
			w.mu.Unlock()
		}
		return resp, err
	})}
	IamURN(o).Observe(context.Background(), uuEnv(), io.Discard)
	ctl := w.calls("account", "control", "")
	if n := len(ctl); n < 2 || ctl[n-1].at.Sub(ctl[0].at) > uuSettle {
		t.Errorf("controls %v: want the last within Settle %s of the first", ctl, uuSettle)
	}
}

func TestURNValidateKeys(t *testing.T) {
	// Exposes a check that accepts an output of another root's shape: a sixth key is refused,
	// before any request, as a missing one is.
	env := uuEnv()
	env["read_path"] = "/cloud/project/" + uuProject
	w := newUUWorld(t)
	c := IamURN(w.options())
	if err := c.Validate(env); err == nil {
		t.Errorf("Validate accepted the keys %v", env)
	}
	if _, err := c.Observe(context.Background(), env, io.Discard); err == nil || len(w.seen()) != 0 {
		t.Errorf("Observe = %v after %v, want a refusal before any request", err, w.seen())
	}
}

func TestURNPartialTags(t *testing.T) {
	// Review r1: the bucket carries a tag the checker cannot read (a key with a space). Exposes a
	// write that puts back fewer tags than the bucket had, and a cleanup that fails on the
	// listing instead of removing the run's key.
	w := newUUWorld(t)
	w.resources[0].tags["odd key"] = "x"
	raw, _, err := uuObserve(t, w)
	if slices.ContainsFunc(w.seen(), func(r uuReq) bool { return r.method == http.MethodPut }) {
		t.Errorf("a write was sent although the bucket's tags were not all readable")
	}
	rec := uuParse(t, raw, err)
	uuWantResults(t, rec, "either", "blocked/no owned target")
	w.tagBucket()
	if err := IamURN(w.options()).Undo(context.Background(), uuEnv(), io.Discard); err != nil {
		t.Errorf("Undo = %v, want nil", err)
	}
	if got := w.tags(uuBucketURN); got[uuTagKey] != "" || got["odd key"] != "x" {
		t.Errorf("bucket tags %v after Undo, want the run's key removed and the rest kept", got)
	}
	// Review r2: the run's key itself with a value of another shape is still seen and removed.
	w.resources[0].tags[uuTagKey] = "odd value"
	if err := IamURN(w.options()).Undo(context.Background(), uuEnv(), io.Discard); err != nil {
		t.Errorf("Undo with an odd value on the run's key = %v, want nil", err)
	}
	if _, ok := w.tags(uuBucketURN)[uuTagKey]; ok {
		t.Errorf("the run's key with an odd value survived Undo: %v", w.tags(uuBucketURN))
	}
}
