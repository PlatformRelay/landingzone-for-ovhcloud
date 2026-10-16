package live

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Leftover-listing retry (spec 007 FR-001, FR-002, SC-001; V001; T003). A request of the leftover
// check (an array page, a single-object read or the token request) that fails with 429, 5xx or a
// transport failure is retried at most twice, each wait admitted by the context's deadline and the
// run's shared retryBudget, a cancellation ending the wait at once; every other failure is tried
// once. Each failed attempt is a ListingAttempt in the report and in leftovers.json. The cases
// follow spec.md and data-model.md (*Listing attempt*, *Fixed signatures*); the waits are 1 ms on
// a fake clock (listClock), so no case sleeps.

// retryBackoff is the cases' Backoff: three attempts per request, as defaultListingBackoff.
var retryBackoff = []time.Duration{time.Millisecond, time.Millisecond}

const (
	pathNet1   = "/cloud/project/p1/network/private"
	pathNet2   = "/cloud/project/p2/network/private"
	pathUser1  = "/cloud/project/p1/user"
	pathGRA11  = "/cloud/project/p1/region/GRA11"
	pathRegion = "/cloud/project/p1/region"
	pathGroups = "/me/identity/group"
	pathAlert2 = "/cloud/project/p2/alerting"
)

// listClock is the fake waitClock: Now starts where the case puts it and moves only by the waits
// After was asked for. With block set, After never fires and closes started on its first call (a
// wait in progress).
type listClock struct {
	mu      sync.Mutex
	now     time.Time
	waits   []time.Duration
	block   bool
	started chan struct{}
}

func newListClock(now time.Time) *listClock {
	return &listClock{now: now, started: make(chan struct{})}
}

func (c *listClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *listClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.waits = append(c.waits, d)
	if c.block {
		if len(c.waits) == 1 {
			close(c.started)
		}
		return make(chan time.Time)
	}
	c.now = c.now.Add(d)
	ch := make(chan time.Time, 1)
	ch <- c.now
	return ch
}

func (c *listClock) Waits() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.waits)
}

// listAnswer is one scripted answer of scriptLister: err, else body with next as its cursor.
type listAnswer struct {
	err  error
	body string
	next string // the next-page cursor the answer carries ("" on the last page)
}

// scriptLister answers the scripted attempts of a request (key path#page, page from fakeLister's
// cursors "2", "3", …) in order, then hands the request to base; it records every request.
type scriptLister struct {
	base   Lister
	mu     sync.Mutex
	script map[string][]listAnswer
	asked  []string
}

func (s *scriptLister) Get(ctx context.Context, path, cursor string) ([]byte, string, error) {
	page := 1
	if cursor != "" {
		if n, err := strconv.Atoi(cursor); err == nil {
			page = n
		}
	}
	key := fmt.Sprintf("%s#%d", path, page)
	s.mu.Lock()
	s.asked = append(s.asked, key)
	if q := s.script[key]; len(q) > 0 {
		a := q[0]
		s.script[key] = q[1:]
		s.mu.Unlock()
		if a.err != nil {
			return nil, "", a.err
		}
		return []byte(a.body), a.next, nil
	}
	s.mu.Unlock()
	return s.base.Get(ctx, path, cursor)
}

// asks is every request of path, in order.
func (s *scriptLister) asks(path string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, k := range s.asked {
		if strings.HasPrefix(k, path+"#") {
			out = append(out, k)
		}
	}
	return out
}

// times answers err n times.
func times(n int, err error) []listAnswer {
	out := make([]listAnswer, n)
	for i := range out {
		out[i] = listAnswer{err: err}
	}
	return out
}

func status(path string, code int) error { return &statusError{path: path, code: code} }

// retryCtx is a context whose deadline no case reaches.
func retryCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

// retryCheck is the clean synthetic world's check listing through a scriptLister, with the cases'
// Backoff, a fake clock and a generous budget, and its terminal.
func retryCheck(t *testing.T, script map[string][]listAnswer, seed syntheticSeed) (LeftoverCheck, []InventoryEntry, *scriptLister, *listClock, *bytes.Buffer) {
	t.Helper()
	w, _ := loadSynthetic(t)
	c, base, inv := worldCheck(t, w, seed)
	sl := &scriptLister{base: base, script: script}
	clk := newListClock(time.Now())
	term := &bytes.Buffer{}
	c.Lister = sl
	c.Backoff = retryBackoff
	c.Clock = clk
	c.Budget = newRetryBudget(clk.Now().Add(time.Hour), minRetryReserve)
	c.Term = term
	return c, inv, sl, clk, term
}

// listingRetryLines are the terminal's retry lines.
func listingRetryLines(term string) []string {
	var out []string
	for _, l := range strings.Split(term, "\n") {
		if strings.HasPrefix(l, "LZ-LIVE retry listing ") {
			out = append(out, l)
		}
	}
	return out
}

// attemptsOf are the report's attempt rows for path.
func attemptsOf(rep Report, path string) []ListingAttempt {
	var out []ListingAttempt
	for _, a := range rep.Attempts {
		if a.Path == path {
			out = append(out, a)
		}
	}
	return out
}

// TestListingRetryTransient500: a listing answering 500 once is retried after the first wait: the
// check passes, prints one retry line and records one retried attempt. Exposes today's single
// attempt (spec 006 T027's `fail`).
func TestListingRetryTransient500(t *testing.T) {
	t.Parallel()
	c, inv, sl, clk, term := retryCheck(t, map[string][]listAnswer{pathNet1 + "#1": times(1, status(pathNet1, 500))}, syntheticSeed{})
	rep := c.Check(retryCtx(t), inv)
	if rep.Outcome != "pass" || len(rep.Errors) != 0 || len(rep.Leftovers) != 0 {
		t.Errorf("a listing answering 500 once: outcome %q, errors %v, leftovers %v; want pass", rep.Outcome, rep.Errors, leftoverKeys(rep.Leftovers))
	}
	want := fmt.Sprintf("LZ-LIVE retry listing %s in %v: 500 (attempt 2 of 3)", pathNet1, time.Millisecond)
	if got := listingRetryLines(term.String()); !slices.Equal(got, []string{want}) {
		t.Errorf("retry lines %q, want exactly %q", got, want)
	}
	if got, want := rep.Attempts, []ListingAttempt{{Path: pathNet1, Attempt: 1, Status: 500, Class: "status", Retried: true}}; !slices.Equal(got, want) {
		t.Errorf("attempts %+v, want %+v", got, want)
	}
	// The network listing has two pages: page 1 twice (the 500, then its retry), then page 2.
	if got, want := sl.asks(pathNet1), []string{pathNet1 + "#1", pathNet1 + "#1", pathNet1 + "#2"}; !slices.Equal(got, want) {
		t.Errorf("requests %v, want %v", got, want)
	}
	if got := clk.Waits(); !slices.Equal(got, retryBackoff[:1]) {
		t.Errorf("waits %v, want the first backoff %v", got, retryBackoff[:1])
	}
}

// scriptAPI is the fake listing API (fakeListAPI) behind a front that answers the scripted
// attempts of a route ("token", or an API route as fakeListAPI names it) in order, then hands the
// request on; a fault is a status code, "reset" (closed before any answer) or "body-reset" (the
// headers of a 200 and part of its body, then closed). Its client keeps no connection alive, so
// net/http cannot re-send a GET on its own (spec 003 T029 lesson) and every attempt is counted.
type scriptAPI struct {
	*httptest.Server
	base   *fakeListAPI
	mu     sync.Mutex
	script map[string][]string
	hits   map[string]int
}

func newScriptAPI(t *testing.T, responses map[string][]json.RawMessage, script map[string][]string) *scriptAPI {
	t.Helper()
	s := &scriptAPI{base: newFakeListAPI(t, responses), script: script, hits: map[string]int{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

func (s *scriptAPI) API() API {
	return API{TokenURL: s.URL + "/auth/oauth2/token", BaseURL: s.URL + "/v1",
		HTTP: &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}}
}

func (s *scriptAPI) serve(w http.ResponseWriter, r *http.Request) {
	route := "token"
	if r.URL.Path != "/auth/oauth2/token" {
		_, rest, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
		route = "/" + rest
	}
	s.mu.Lock()
	s.hits[route]++
	fault := ""
	if q := s.script[route]; len(q) > 0 {
		fault, s.script[route] = q[0], q[1:]
	}
	s.mu.Unlock()
	hijack := func(raw string) {
		conn, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			panic(err)
		}
		buf.WriteString(raw)
		buf.Flush()
		conn.Close()
	}
	switch fault {
	case "":
		s.base.Config.Handler.ServeHTTP(w, r)
	case "reset":
		hijack("")
	case "body-reset":
		hijack("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 64\r\n\r\n[\"GRA")
	default:
		code, err := strconv.Atoi(fault)
		if err != nil {
			panic("unknown fault " + fault)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		w.Write([]byte(`{"message":"scripted"}`))
	}
}

func (s *scriptAPI) Hits(route string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits[route]
}

// apiHits is every request but the token's.
func (s *scriptAPI) apiHits() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for route, h := range s.hits {
		if route != "token" {
			n += h
		}
	}
	return n
}

// retryAPICheck is retryCheck listing through a scriptAPI with the run's credential.
func retryAPICheck(t *testing.T, script map[string][]string) (LeftoverCheck, []InventoryEntry, *scriptAPI, *bytes.Buffer) {
	t.Helper()
	w, _ := loadSynthetic(t)
	c, inv, _, _, term := retryCheck(t, nil, syntheticSeed{})
	s := newScriptAPI(t, w.Responses, script)
	c.Lister = nil
	c.API = s.API()
	c.Cred = listCred
	return c, inv, s, term
}

// TestListingRetryClasses: 503, 429 and a connection reset while sending, on three listings once
// each, are each retried once and the check passes. Exposes a classification that retries only
// 500.
func TestListingRetryClasses(t *testing.T) {
	t.Parallel()
	script := map[string][]string{pathUser1: {"503"}, pathGroups: {"429"}, pathAlert2: {"reset"}}
	c, inv, s, term := retryAPICheck(t, script)
	rep := c.Check(retryCtx(t), inv)
	if rep.Outcome != "pass" || len(rep.Errors) != 0 {
		t.Errorf("503, 429 and a reset once each: outcome %q, errors %v; want pass", rep.Outcome, rep.Errors)
	}
	if got := listingRetryLines(term.String()); len(got) != 3 {
		t.Errorf("retry lines %q, want three", got)
	}
	for _, w := range []struct {
		path, label string
		row         ListingAttempt
	}{
		{pathUser1, ": 503 (attempt 2 of 3)", ListingAttempt{Path: pathUser1, Attempt: 1, Status: 503, Class: "status", Retried: true}},
		{pathGroups, ": 429 (attempt 2 of 3)", ListingAttempt{Path: pathGroups, Attempt: 1, Status: 429, Class: "status", Retried: true}},
		{pathAlert2, ": transport (attempt 2 of 3)", ListingAttempt{Path: pathAlert2, Attempt: 1, Class: "transport", Retried: true}},
	} {
		if got := s.Hits(w.path); got != 2 {
			t.Errorf("%s asked %d times, want 2 (one retry)", w.path, got)
		}
		if got := attemptsOf(rep, w.path); !slices.Equal(got, []ListingAttempt{w.row}) {
			t.Errorf("%s attempts %+v, want %+v", w.path, got, w.row)
		}
		if !slices.ContainsFunc(listingRetryLines(term.String()), func(l string) bool {
			return strings.HasPrefix(l, "LZ-LIVE retry listing "+w.path+" in ") && strings.HasSuffix(l, w.label)
		}) {
			t.Errorf("no retry line for %s ending %q in %q", w.path, w.label, term.String())
		}
	}
}

// TestListingRetryBodyReadReset: an answer whose headers arrive and whose connection closes in the
// middle of the body fails as `transport` and is retried. Exposes a body-read reset classed
// `decode` (not retried).
func TestListingRetryBodyReadReset(t *testing.T) {
	t.Parallel()
	c, inv, s, _ := retryAPICheck(t, map[string][]string{pathRegion: {"body-reset"}})
	rep := c.Check(retryCtx(t), inv)
	if rep.Outcome != "pass" || len(rep.Errors) != 0 {
		t.Errorf("a body read reset once: outcome %q, errors %v; want pass", rep.Outcome, rep.Errors)
	}
	if got := s.Hits(pathRegion); got != 2 {
		t.Errorf("%s asked %d times, want 2", pathRegion, got)
	}
	want := ListingAttempt{Path: pathRegion, Attempt: 1, Class: "transport", Retried: true}
	if got := attemptsOf(rep, pathRegion); !slices.Equal(got, []ListingAttempt{want}) {
		t.Errorf("attempts %+v, want %+v", got, want)
	}
}

// TestListingRetryToken: the token request the first listing needs is retried after a 503 and the
// check passes; after a 401 it is not retried, and every listing fails with the cached refusal.
// Exposes a token 5xx cached for the whole check, or a token 4xx retried.
func TestListingRetryToken(t *testing.T) {
	t.Parallel()
	t.Run("503-once", func(t *testing.T) {
		t.Parallel()
		c, inv, s, term := retryAPICheck(t, map[string][]string{"token": {"503"}})
		rep := c.Check(retryCtx(t), inv)
		if rep.Outcome != "pass" || len(rep.Errors) != 0 {
			t.Errorf("token 503 once: outcome %q, errors %v; want pass", rep.Outcome, rep.Errors)
		}
		if got := s.Hits("token"); got != 2 {
			t.Errorf("token requested %d times, want 2 (one retry)", got)
		}
		want := ListingAttempt{Path: "token", Attempt: 1, Status: 503, Class: "status", Retried: true}
		if got := attemptsOf(rep, "token"); !slices.Equal(got, []ListingAttempt{want}) {
			t.Errorf("token attempts %+v, want %+v", got, want)
		}
		if got := listingRetryLines(term.String()); len(got) != 1 || !strings.HasSuffix(got[0], ": 503 (attempt 2 of 3)") {
			t.Errorf("retry lines %q, want one ending %q", got, ": 503 (attempt 2 of 3)")
		}
	})
	t.Run("401", func(t *testing.T) {
		t.Parallel()
		c, inv, s, term := retryAPICheck(t, map[string][]string{"token": {"401", "401", "401", "401", "401"}})
		rep := c.Check(retryCtx(t), inv)
		if rep.Outcome != "fail" {
			t.Errorf("token 401: outcome %q, want fail", rep.Outcome)
		}
		if got := s.Hits("token"); got != 1 {
			t.Errorf("token requested %d times, want 1 (a 4xx is never retried, the refusal is cached)", got)
		}
		if got := s.apiHits(); got != 0 {
			t.Errorf("%d listing requests reached the API without a token, want 0", got)
		}
		if len(rep.Errors) < 2 {
			t.Errorf("errors %v, want every listing to fail", rep.Errors)
		}
		for _, e := range rep.Errors {
			if !strings.Contains(e, "401") {
				t.Errorf("error %q does not carry the cached refusal (401)", e)
			}
		}
		if got := listingRetryLines(term.String()); len(got) != 0 {
			t.Errorf("retry lines %q after a token 401, want none", got)
		}
		rows := attemptsOf(rep, "token")
		if !slices.ContainsFunc(rows, func(a ListingAttempt) bool { return !a.Retried && a.Reason == "permanent" }) {
			t.Errorf("token attempts %+v, want one not retried with reason permanent", rows)
		}
		if slices.ContainsFunc(rep.Attempts, func(a ListingAttempt) bool { return a.Retried }) {
			t.Errorf("attempts %+v: a token 401 was recorded as retried", rep.Attempts)
		}
	})
}

// TestListingRetryResumesPage: page 2 of a paginated listing answering 500 once is asked again
// with page 2's cursor, and the listing holds each item once. Exposes a restart from page 1 or a
// dropped cursor.
func TestListingRetryResumesPage(t *testing.T) {
	t.Parallel()
	clean, cinv, _, _, _ := retryCheck(t, nil, syntheticSeed{})
	base := clean.Check(retryCtx(t), cinv)
	c, inv, sl, _, _ := retryCheck(t, map[string][]listAnswer{pathNet1 + "#2": times(1, status(pathNet1, 500))}, syntheticSeed{})
	rep := c.Check(retryCtx(t), inv)
	if rep.Outcome != "pass" || len(rep.Errors) != 0 {
		t.Errorf("page 2 answering 500 once: outcome %q, errors %v; want pass", rep.Outcome, rep.Errors)
	}
	if got, want := sl.asks(pathNet1), []string{pathNet1 + "#1", pathNet1 + "#2", pathNet1 + "#2"}; !slices.Equal(got, want) {
		t.Errorf("requests %v, want %v (resume at page 2's cursor)", got, want)
	}
	if got, want := len(rep.Listings[tNetwork]), len(base.Listings[tNetwork]); got != want {
		t.Errorf("network listing holds %d items, want %d as without the fault", got, want)
	}
	seen := map[string]bool{}
	for _, id := range rep.Listed[tNetwork] {
		if seen[id] {
			t.Errorf("network %s listed twice", id)
		}
		seen[id] = true
	}
}

// TestListingRetryObjectRead: a single-object read (a region detail) answering 502 once is
// retried. Exposes retries on array listings only.
func TestListingRetryObjectRead(t *testing.T) {
	t.Parallel()
	c, inv, sl, _, _ := retryCheck(t, map[string][]listAnswer{pathGRA11 + "#1": times(1, status(pathGRA11, 502))}, syntheticSeed{})
	rep := c.Check(retryCtx(t), inv)
	if rep.Outcome != "pass" || len(rep.Errors) != 0 {
		t.Errorf("a region detail answering 502 once: outcome %q, errors %v; want pass", rep.Outcome, rep.Errors)
	}
	if got := sl.asks(pathGRA11); len(got) != 2 {
		t.Errorf("%s asked %d times, want 2", pathGRA11, len(got))
	}
}

// TestListingRetryExhausted: 500 on every attempt ends `fail` after three attempts, the error naming
// the path, 500 and "attempt 3 of 3", each attempt recorded and the last with reason `attempts`.
// Exposes an unbounded retry or a `pass` on exhaustion.
func TestListingRetryExhausted(t *testing.T) {
	t.Parallel()
	c, inv, sl, clk, term := retryCheck(t, map[string][]listAnswer{pathNet1 + "#1": times(6, status(pathNet1, 500))}, syntheticSeed{})
	rep := c.Check(retryCtx(t), inv)
	if rep.Outcome != "fail" {
		t.Errorf("500 on every attempt: outcome %q, want fail", rep.Outcome)
	}
	if got := sl.asks(pathNet1); len(got) != 3 {
		t.Errorf("%s asked %d times, want 3", pathNet1, len(got))
	}
	if !slices.ContainsFunc(rep.Errors, func(e string) bool {
		return strings.Contains(e, pathNet1) && strings.Contains(e, "500") && strings.Contains(e, "attempt 3 of 3")
	}) {
		t.Errorf("errors %v: none names %s, 500 and \"attempt 3 of 3\"", rep.Errors, pathNet1)
	}
	want := []ListingAttempt{
		{Path: pathNet1, Attempt: 1, Status: 500, Class: "status", Retried: true},
		{Path: pathNet1, Attempt: 2, Status: 500, Class: "status", Retried: true},
		{Path: pathNet1, Attempt: 3, Status: 500, Class: "status", Reason: "attempts"},
	}
	if got := attemptsOf(rep, pathNet1); !slices.Equal(got, want) {
		t.Errorf("attempts %+v, want %+v", got, want)
	}
	if got := len(listingRetryLines(term.String())); got != 2 {
		t.Errorf("%d retry lines, want 2", got)
	}
	if got := clk.Waits(); !slices.Equal(got, retryBackoff) {
		t.Errorf("waits %v, want %v", got, retryBackoff)
	}
}

// TestListingRetryStillReconciles: a listing that answers 500 once and then lists a network named
// with the run id still reports that network. Exposes a retry path that skips reconciliation.
func TestListingRetryStillReconciles(t *testing.T) {
	t.Parallel()
	w, _ := loadSynthetic(t)
	seed := syntheticSeed{Replace: map[string][]json.RawMessage{
		pathNet2:                      {json.RawMessage(`[{"id":"pn-retry","name":"net-` + w.RunID + `","status":"ACTIVE","type":"private","vlanId":7,"regions":[]}]`)},
		pathNet2 + "/pn-retry/subnet": {json.RawMessage(`[]`)},
	}}
	c, inv, _, _, _ := retryCheck(t, map[string][]listAnswer{pathNet2 + "#1": times(1, status(pathNet2, 500))}, seed)
	rep := c.Check(retryCtx(t), inv)
	if got, want := leftoverKeys(rep.Leftovers), []string{tNetwork + " pn-retry"}; rep.Outcome != "fail" || !slices.Equal(got, want) || len(rep.Errors) != 0 {
		t.Errorf("500 once, then a network named with the run id: outcome %q, leftovers %v, errors %v; want fail with exactly %v and no error",
			rep.Outcome, got, rep.Errors, want)
	}
}

// TestListingRetryPermanent: a 403, a 404 where absence is not admitted and a 200 whose body is not
// JSON are each tried once and fail (pin: holds today); each is recorded as not retried, reason
// `permanent` (red until T006). Exposes a refusal retried, or recorded as retryable.
func TestListingRetryPermanent(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, path string
		answer     listAnswer
		row        ListingAttempt
	}{
		{"403", pathUser1, listAnswer{err: status(pathUser1, 403)}, ListingAttempt{Path: pathUser1, Attempt: 1, Status: 403, Class: "status", Reason: "permanent"}},
		{"404", pathNet1, listAnswer{err: status(pathNet1, 404)}, ListingAttempt{Path: pathNet1, Attempt: 1, Status: 404, Class: "status", Reason: "permanent"}},
		{"not-json", pathUser1, listAnswer{body: "<html>maintenance</html>"}, ListingAttempt{Path: pathUser1, Attempt: 1, Class: "decode", Reason: "permanent"}},
	} {
		t.Run(tc.name+"/once", func(t *testing.T) {
			t.Parallel()
			c, inv, sl, clk, term := retryCheck(t, map[string][]listAnswer{tc.path + "#1": {tc.answer, tc.answer, tc.answer}}, syntheticSeed{})
			rep := c.Check(retryCtx(t), inv)
			if rep.Outcome != "fail" {
				t.Errorf("outcome %q, want fail", rep.Outcome)
			}
			if got := sl.asks(tc.path); len(got) != 1 {
				t.Errorf("%s asked %d times, want once", tc.path, len(got))
			}
			if got := clk.Waits(); len(got) != 0 {
				t.Errorf("waits %v, want none", got)
			}
			if got := listingRetryLines(term.String()); len(got) != 0 {
				t.Errorf("retry lines %q, want none", got)
			}
		})
		t.Run(tc.name+"/record", func(t *testing.T) {
			t.Parallel()
			c, inv, _, _, _ := retryCheck(t, map[string][]listAnswer{tc.path + "#1": {tc.answer, tc.answer, tc.answer}}, syntheticSeed{})
			rep := c.Check(retryCtx(t), inv)
			got := attemptsOf(rep, tc.path)
			if tc.row.Class == "decode" && len(got) == 1 {
				got[0].Status = 0 // the status of an undecodable 200 is not fixed
			}
			if !slices.Equal(got, []ListingAttempt{tc.row}) {
				t.Errorf("attempts %+v, want %+v", got, tc.row)
			}
		})
	}
}

// TestListingRetryRecordedInLeftoversJSON: a run whose leftover check retried a listing twice
// writes both failed attempts into leftovers.json (path, attempt, status, class, retried), and no
// token. Exposes attempts kept only in memory.
func TestListingRetryRecordedInLeftoversJSON(t *testing.T) {
	syn, _ := loadSynthetic(t)
	w := newRunWorld(t)
	s := newScriptAPI(t, syn.Responses, map[string][]string{pathNet1: {"500", "503"}})
	r := w.runner(t, w.stack(t, "a", true, tofuStack{}))
	r.Leftovers.Lister = nil
	r.Leftovers.API = s.API()
	r.Leftovers.Cred = listCred
	r.Leftovers.Backoff = retryBackoff
	err, _ := execute(t, r)
	if err != nil {
		t.Errorf("run whose listing answered 500 and 503 before a 200: %v", err)
	}
	raw, rerr := os.ReadFile(filepath.Join(w.runDir, "leftovers.json"))
	if rerr != nil {
		t.Fatalf("leftovers.json: %v", rerr)
	}
	assertNoCredentialIn(t, "leftovers.json", string(raw))
	type row struct {
		Path    string `json:"path"`
		Attempt int    `json:"attempt"`
		Status  int    `json:"status"`
		Class   string `json:"class"`
		Retried bool   `json:"retried"`
		Reason  string `json:"reason"`
	}
	var rec struct {
		Attempts []row `json:"attempts"`
	}
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatalf("leftovers.json: %v", err)
	}
	want := []row{
		{Path: pathNet1, Attempt: 1, Status: 500, Class: "status", Retried: true},
		{Path: pathNet1, Attempt: 2, Status: 503, Class: "status", Retried: true},
	}
	if !slices.Equal(rec.Attempts, want) {
		t.Errorf("leftovers.json attempts %+v, want %+v:\n%s", rec.Attempts, want, raw)
	}
}

// TestListingRetryDeadline: with the context's deadline 3 ms away on the check's clock and a 10 ms
// backoff, a 500 starts no wait and is recorded with reason `deadline`. Exposes a wait past the
// deadline.
func TestListingRetryDeadline(t *testing.T) {
	t.Parallel()
	c, inv, sl, _, _ := retryCheck(t, map[string][]listAnswer{pathNet1 + "#1": times(6, status(pathNet1, 500))}, syntheticSeed{})
	end := time.Now().Add(time.Hour) // the real deadline: the context never ends while the case runs
	ctx, cancel := context.WithDeadline(context.Background(), end)
	defer cancel()
	clk := newListClock(end.Add(-3 * time.Millisecond)) // on the check's clock, 3 ms are left
	c.Clock = clk
	c.Backoff = []time.Duration{10 * time.Millisecond, 10 * time.Millisecond}
	c.Budget = newRetryBudget(end.Add(time.Hour), 0) // the budget alone admits the wait
	rep := c.Check(ctx, inv)
	if got := clk.Waits(); len(got) != 0 {
		t.Errorf("waits %v started 3 ms before the deadline, want none", got)
	}
	if got := sl.asks(pathNet1); len(got) != 1 {
		t.Errorf("%s asked %d times, want once", pathNet1, len(got))
	}
	want := ListingAttempt{Path: pathNet1, Attempt: 1, Status: 500, Class: "status", Reason: "deadline"}
	if got := attemptsOf(rep, pathNet1); !slices.Equal(got, []ListingAttempt{want}) {
		t.Errorf("attempts %+v, want %+v", got, want)
	}
	if rep.Outcome == "pass" {
		t.Error("outcome pass with a listing that failed")
	}
}

// TestListingRetryBudget: a retryBudget that refuses the wait, and a nil Budget, start no wait;
// the attempt is recorded with reason `budget`. Exposes listing waits outside the shared budget,
// or nil read as "deadline only".
func TestListingRetryBudget(t *testing.T) {
	t.Parallel()
	for name, budget := range map[string]func(now time.Time) *retryBudget{
		"refuses": func(now time.Time) *retryBudget { return newRetryBudget(now.Add(time.Minute), 2*time.Minute) },
		"nil":     func(time.Time) *retryBudget { return nil },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c, inv, sl, clk, _ := retryCheck(t, map[string][]listAnswer{pathNet1 + "#1": times(6, status(pathNet1, 500))}, syntheticSeed{})
			c.Budget = budget(clk.Now())
			rep := c.Check(retryCtx(t), inv)
			if got := clk.Waits(); len(got) != 0 {
				t.Errorf("waits %v, want none", got)
			}
			if got := sl.asks(pathNet1); len(got) != 1 {
				t.Errorf("%s asked %d times, want once", pathNet1, len(got))
			}
			want := ListingAttempt{Path: pathNet1, Attempt: 1, Status: 500, Class: "status", Reason: "budget"}
			if got := attemptsOf(rep, pathNet1); !slices.Equal(got, []ListingAttempt{want}) {
				t.Errorf("attempts %+v, want %+v", got, want)
			}
			if rep.Outcome == "pass" {
				t.Error("outcome pass with a listing that failed")
			}
		})
	}
}

// TestListingRetryCancelled: a cancellation during a wait ends it at once with the context's
// cause, reason `cancelled`, and no further attempt of the request. Exposes a sleep that ignores
// cancellation.
func TestListingRetryCancelled(t *testing.T) {
	t.Parallel()
	c, inv, sl, clk, _ := retryCheck(t, map[string][]listAnswer{pathNet1 + "#1": times(6, status(pathNet1, 500))}, syntheticSeed{})
	clk.block = true
	ctx, cancel := context.WithCancelCause(retryCtx(t))
	defer cancel(nil)
	cause := errors.New("operator interrupt 7c41")
	done := make(chan Report, 1)
	go func() { done <- c.Check(ctx, inv) }()
	var rep Report
	select {
	case <-clk.started:
		cancel(cause)
	case rep = <-done:
		t.Fatalf("the check ended without starting a wait (outcome %q, errors %v)", rep.Outcome, rep.Errors)
	case <-time.After(10 * time.Second):
		t.Fatal("no wait started within 10 s")
	}
	select {
	case rep = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the wait did not end within 5 s of the cancellation")
	}
	if got := sl.asks(pathNet1); len(got) != 1 {
		t.Errorf("%s asked %d times, want once (no attempt after the cancellation)", pathNet1, len(got))
	}
	if !slices.ContainsFunc(rep.Errors, func(e string) bool { return strings.Contains(e, pathNet1) && strings.Contains(e, cause.Error()) }) {
		t.Errorf("errors %v: none names %s with the context's cause %q", rep.Errors, pathNet1, cause)
	}
	want := ListingAttempt{Path: pathNet1, Attempt: 1, Status: 500, Class: "status", Reason: "cancelled"}
	if got := attemptsOf(rep, pathNet1); !slices.Equal(got, []ListingAttempt{want}) {
		t.Errorf("attempts %+v, want %+v", got, want)
	}
	if rep.Outcome == "pass" {
		t.Error("outcome pass after a cancelled retry")
	}
}

// TestListingRetryRedacted: with Red holding the bearer token and a transport error quoting it,
// neither the retry line nor the attempt record carries the token. Exposes an unredacted retry
// line.
func TestListingRetryRedacted(t *testing.T) {
	t.Parallel()
	const tok = "lzretry-TOKEN-7f3a9c2e1d"
	failure := fmt.Errorf("connect via %s: %w", tok, &transportError{path: pathNet1, err: errors.New("connection reset; bearer " + tok)})
	c, inv, _, _, term := retryCheck(t, map[string][]listAnswer{pathNet1 + "#1": times(1, failure)}, syntheticSeed{})
	c.Red = NewRedactor(tok)
	rep := c.Check(retryCtx(t), inv)
	if rep.Outcome != "pass" {
		t.Errorf("a transport failure once: outcome %q, errors %v; want pass", rep.Outcome, rep.Errors)
	}
	if got := listingRetryLines(term.String()); len(got) != 1 {
		t.Errorf("retry lines %q, want one", got)
	}
	if got := attemptsOf(rep, pathNet1); len(got) != 1 || got[0].Class != "transport" {
		t.Errorf("attempts %+v, want one transport attempt", got)
	}
	record, err := json.Marshal(rep.Attempts)
	if err != nil {
		t.Fatal(err)
	}
	for what, text := range map[string]string{"terminal": term.String(), "attempt record": string(record)} {
		for _, frag := range []string{tok, "7f3a9c2e1d", "lzretry-TOKEN"} {
			if strings.Contains(text, frag) {
				t.Errorf("%s carries the token (%q): %s", what, frag, text)
			}
		}
	}
}

// TestListingRetryRepeatingCursor: a listing whose page 2 names itself as the next page (a
// repeating cursor, a page bound) is not retried (pin: holds today) and is recorded as class
// `bounds`, not retried, reason `permanent` (red until T006). Exposes a retry of a bounds failure
// (FR-001: a page bound or a repeating cursor is never retried).
func TestListingRetryRepeatingCursor(t *testing.T) {
	t.Parallel()
	script := func() map[string][]listAnswer {
		return map[string][]listAnswer{
			pathNet1 + "#1": {{body: "[]", next: "2"}},
			pathNet1 + "#2": {{body: "[]", next: "2"}, {body: "[]", next: "2"}, {body: "[]", next: "2"}},
		}
	}
	t.Run("once", func(t *testing.T) {
		t.Parallel()
		c, inv, sl, clk, term := retryCheck(t, script(), syntheticSeed{})
		rep := c.Check(retryCtx(t), inv)
		if rep.Outcome != "fail" {
			t.Errorf("outcome %q, want fail", rep.Outcome)
		}
		if got, want := sl.asks(pathNet1), []string{pathNet1 + "#1", pathNet1 + "#2"}; !slices.Equal(got, want) {
			t.Errorf("requests %v, want %v (a repeating cursor is not retried)", got, want)
		}
		if got := clk.Waits(); len(got) != 0 {
			t.Errorf("waits %v, want none", got)
		}
		if got := listingRetryLines(term.String()); len(got) != 0 {
			t.Errorf("retry lines %q, want none", got)
		}
	})
	t.Run("record", func(t *testing.T) {
		t.Parallel()
		c, inv, _, _, _ := retryCheck(t, script(), syntheticSeed{})
		rep := c.Check(retryCtx(t), inv)
		got := attemptsOf(rep, pathNet1)
		if len(got) == 1 {
			got[0].Status, got[0].Attempt = 0, 0 // neither is fixed for a bounds failure
		}
		if want := (ListingAttempt{Path: pathNet1, Class: "bounds", Reason: "permanent"}); !slices.Equal(got, []ListingAttempt{want}) {
			t.Errorf("attempts %+v, want one %+v", got, want)
		}
	})
}

// TestTransientListing: the classification data-model.md fixes for transientListing: a status of
// 429 or 500-599 is retried, any other status is not, a transport failure (wrapped or not) is
// retried, anything else is a decode or bounds failure and not retried. Exposes a classifier that
// retries 4xx or misses a transport failure.
func TestTransientListing(t *testing.T) {
	t.Parallel()
	tr := &transportError{path: pathNet1, err: errors.New("connection reset by peer")}
	for _, tc := range []struct {
		name  string
		err   error
		class string
		retry bool
	}{
		{"429", status(pathNet1, 429), "status", true},
		{"500", status(pathNet1, 500), "status", true},
		{"503", status(pathNet1, 503), "status", true},
		{"599", status(pathNet1, 599), "status", true},
		{"400", status(pathNet1, 400), "status", false},
		{"401", status("token", 401), "status", false},
		{"403", status(pathNet1, 403), "status", false},
		{"404", status(pathNet1, 404), "status", false},
		{"transport", tr, "transport", true},
		{"wrapped-transport", fmt.Errorf("listing: %w", tr), "transport", true},
	} {
		if class, retry := transientListing(tc.err); class != tc.class || retry != tc.retry {
			t.Errorf("%s: transientListing = (%q, %v), want (%q, %v)", tc.name, class, retry, tc.class, tc.retry)
		}
	}
	if class, retry := transientListing(errors.New("listing: not a JSON array")); retry || (class != "decode" && class != "bounds") {
		t.Errorf("other failure: transientListing = (%q, %v), want (decode or bounds, false)", class, retry)
	}
}
