package live

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Cases of the leftover-listing retry (spec 007 FR-001, FR-002; T006) that the reviews of T003 and
// T004 deferred to the implementation task: distinct and default backoff values, the classifier's
// edges, the transport error's text, the token request's transport failure, the probe's retry
// line, and the listings keeping their time in the caller's budget (not a copy).

// TestListingRetryBackoffValues: with Backoff {1ms, 2ms} and a listing answering 500 twice, the
// first wait is 1 ms and the second 2 ms, and each retry line names its own wait. Exposes a retry
// that always waits Backoff[0].
func TestListingRetryBackoffValues(t *testing.T) {
	t.Parallel()
	c, inv, _, clk, term := retryCheck(t, map[string][]listAnswer{pathNet1 + "#1": times(2, status(pathNet1, 500))}, syntheticSeed{})
	c.Backoff = []time.Duration{time.Millisecond, 2 * time.Millisecond}
	rep := c.Check(retryCtx(t), inv)
	if rep.Outcome != "pass" || len(rep.Errors) != 0 {
		t.Errorf("500 twice: outcome %q, errors %v; want pass", rep.Outcome, rep.Errors)
	}
	if got, want := clk.Waits(), c.Backoff; !slices.Equal(got, want) {
		t.Errorf("waits %v, want %v", got, want)
	}
	want := []string{
		"LZ-LIVE retry listing " + pathNet1 + " in 1ms: 500 (attempt 2 of 3)",
		"LZ-LIVE retry listing " + pathNet1 + " in 2ms: 500 (attempt 3 of 3)",
	}
	if got := listingRetryLines(term.String()); !slices.Equal(got, want) {
		t.Errorf("retry lines %q, want %q", got, want)
	}
}

// TestListingRetryDefaultBackoff: a nil Backoff is defaultListingBackoff (data-model *Fixed
// signatures*): a listing answering 500 once waits 5 s on the check's clock. Exposes a nil Backoff
// read as "no retry".
func TestListingRetryDefaultBackoff(t *testing.T) {
	t.Parallel()
	c, inv, _, clk, term := retryCheck(t, map[string][]listAnswer{pathNet1 + "#1": times(1, status(pathNet1, 500))}, syntheticSeed{})
	c.Backoff = nil
	rep := c.Check(retryCtx(t), inv)
	if rep.Outcome != "pass" {
		t.Errorf("500 once with the default backoff: outcome %q, errors %v; want pass", rep.Outcome, rep.Errors)
	}
	if got, want := clk.Waits(), []time.Duration{5 * time.Second}; !slices.Equal(got, want) {
		t.Errorf("waits %v, want %v", got, want)
	}
	want := "LZ-LIVE retry listing " + pathNet1 + " in 5s: 500 (attempt 2 of 3)"
	if got := listingRetryLines(term.String()); !slices.Equal(got, []string{want}) {
		t.Errorf("retry lines %q, want %q", got, want)
	}
}

// TestListingRetryEmptyBackoff: an empty non-nil Backoff retries nothing: one request, no wait,
// the attempt recorded with reason `attempts`, and the check fails. Exposes an empty Backoff read
// as the default.
func TestListingRetryEmptyBackoff(t *testing.T) {
	t.Parallel()
	c, inv, sl, clk, term := retryCheck(t, map[string][]listAnswer{pathNet1 + "#1": times(3, status(pathNet1, 500))}, syntheticSeed{})
	c.Backoff = []time.Duration{}
	rep := c.Check(retryCtx(t), inv)
	if rep.Outcome != "fail" {
		t.Errorf("outcome %q, want fail", rep.Outcome)
	}
	if got := sl.asks(pathNet1); len(got) != 1 {
		t.Errorf("%s asked %d times, want once", pathNet1, len(got))
	}
	if got := clk.Waits(); len(got) != 0 {
		t.Errorf("waits %v, want none", got)
	}
	if got := listingRetryLines(term.String()); len(got) != 0 {
		t.Errorf("retry lines %q, want none", got)
	}
	want := ListingAttempt{Path: pathNet1, Attempt: 1, Status: 500, Class: "status", Reason: "attempts"}
	if got := attemptsOf(rep, pathNet1); !slices.Equal(got, []ListingAttempt{want}) {
		t.Errorf("attempts %+v, want %+v", got, want)
	}
}

// TestTransientListingStatusEdges: 428 and 430 (4xx beside 429) and 600 (beyond 5xx) are not
// retried. Exposes a classifier that retries every 4xx from 429 up, or every status from 500 up.
func TestTransientListingStatusEdges(t *testing.T) {
	t.Parallel()
	for _, code := range []int{428, 430, 600} {
		if class, retry := transientListing(status(pathNet1, code)); class != "status" || retry {
			t.Errorf("%d: transientListing = (%q, %v), want (\"status\", false)", code, class, retry)
		}
	}
}

// timeoutErr is a net.Error that timed out.
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

// TestTransportErrorText pins T006's choice for transportError.Error(): it names the request and
// the kind of failure, judged by type, and never quotes the inner error, whose text a hostile
// answer can fill (an unparseable Location header Go quotes) and which no redactor sees before the
// report. Exposes an Error() that quotes its inner error.
func TestTransportErrorText(t *testing.T) {
	t.Parallel()
	const quoted = "Location /%ZZsecret-4e1d"
	for _, tc := range []struct {
		name string
		err  *transportError
		want string
	}{
		{"send", &transportError{path: pathNet1, err: &url.Error{Op: "Get", URL: quoted, Err: errors.New(quoted)}}, "GET " + pathNet1 + ": transport failure (request failed)"},
		{"timeout", &transportError{path: pathNet1, err: &url.Error{Op: "Get", URL: quoted, Err: timeoutErr{}}}, "GET " + pathNet1 + ": transport failure (timed out)"},
		{"cancelled", &transportError{path: pathNet1, err: fmt.Errorf("%s: %w", quoted, context.Canceled)}, "GET " + pathNet1 + ": transport failure (cancelled)"},
		{"body", &transportError{path: pathNet1, err: errors.New("unexpected EOF; " + quoted)}, "GET " + pathNet1 + ": transport failure (reading the answer failed)"},
		{"token", &transportError{path: tokenPath, err: &url.Error{Op: "Post", URL: quoted, Err: errors.New(quoted)}}, "token endpoint: transport failure (request failed)"},
	} {
		if got := tc.err.Error(); got != tc.want || strings.Contains(got, "secret-4e1d") {
			t.Errorf("%s: Error() = %q, want %q (never the inner error's text)", tc.name, got, tc.want)
		}
	}
}

// TestListingRetryTokenTransport: a token request whose connection closes before any answer is
// retried as `transport` under the path "token", and the check passes. Exposes a token transport
// failure cached for the whole check.
func TestListingRetryTokenTransport(t *testing.T) {
	t.Parallel()
	c, inv, s, _ := retryAPICheck(t, map[string][]string{"token": {"reset"}})
	rep := c.Check(retryCtx(t), inv)
	if rep.Outcome != "pass" || len(rep.Errors) != 0 {
		t.Errorf("token reset once: outcome %q, errors %v; want pass", rep.Outcome, rep.Errors)
	}
	if got := s.Hits("token"); got != 2 {
		t.Errorf("token requested %d times, want 2 (one retry)", got)
	}
	want := ListingAttempt{Path: tokenPath, Attempt: 1, Class: "transport", Retried: true}
	if got := attemptsOf(rep, tokenPath); !slices.Equal(got, []ListingAttempt{want}) {
		t.Errorf("token attempts %+v, want %+v", got, want)
	}
}

// TestProbeListingRetryLine (FR-001: each retry prints one line): a probe whose leftover listing
// answers 500 once, with room in the budget, prints the retry line on the run's terminal. Exposes
// a probe core that leaves the check's Term unset (T004 review: deferred to T006).
func TestProbeListingRetryLine(t *testing.T) {
	const wait = time.Minute
	w := newRunWorld(t)
	p := w.probe(t, tempPrivate(t), tofuStack{})
	p.Run.Leftovers.Lister = &scriptLister{base: p.Run.Leftovers.Lister, script: map[string][]listAnswer{pathNet1 + "#1": times(1, status(pathNet1, 500))}}
	clk := &destroyClock{t: t, left: minRetryReserve + wait + 5*time.Second}
	p.Run.Leftovers.Backoff = []time.Duration{wait, wait}
	p.Run.Leftovers.Clock = clk
	p.Run.childStarted = clk.started
	if err := p.Start(context.Background()); err != nil {
		t.Errorf("probe whose listing answered 500 once: %v", err)
	}
	want := "LZ-LIVE retry listing " + pathNet1 + " in 1m0s: 500 (attempt 2 of 3)"
	if got := listingRetryLines(w.term.String()); !slices.Equal(got, []string{want}) {
		t.Errorf("retry lines on the run's terminal %q, want %q", got, want)
	}
}

// slowLister answers path's first first-page request with 500 after took on the check's clock.
type slowLister struct {
	Lister
	clk  *listClock
	path string
	took time.Duration
	once sync.Once
}

func (s *slowLister) Get(ctx context.Context, path, cursor string) ([]byte, string, error) {
	slow := false
	if path == s.path && cursor == "" {
		s.once.Do(func() { slow = true })
	}
	if !slow {
		return s.Lister.Get(ctx, path, cursor)
	}
	s.clk.mu.Lock()
	s.clk.now = s.clk.now.Add(s.took)
	s.clk.mu.Unlock()
	return nil, "", status(path, 500)
}

// TestListingRetryKeepsTimeInSharedBudget (spec 006 FR-008 and data-model *Retry budget*: one
// budget per leftover check, its reserve estimated from the slowest listing so far; spec 007
// FR-001: the listings wait in the run's shared budget): a listing request that takes 2 min on the
// check's clock raises the reserve of the very budget the caller passed. Exposes a check that
// waits in a copy of the budget taken when it starts (G24 "fresh budget for listings"), or never
// keeps a listing's time.
func TestListingRetryKeepsTimeInSharedBudget(t *testing.T) {
	t.Parallel()
	c, inv, _, clk, _ := retryCheck(t, nil, syntheticSeed{})
	const took = 2 * time.Minute
	c.Lister = &slowLister{Lister: c.Lister, clk: clk, path: pathNet1, took: took}
	b := newRetryBudget(clk.Now().Add(time.Hour), minRetryReserve)
	c.Budget = b
	rep := c.Check(retryCtx(t), inv)
	if rep.Outcome != "pass" {
		t.Errorf("a slow 500 once with an hour left: outcome %q, errors %v; want pass", rep.Outcome, rep.Errors)
	}
	if b.reserve < took {
		t.Errorf("the caller's budget keeps %v, want at least %v: the listing's slow request was kept elsewhere", b.reserve, took)
	}
}

// Review round 1 (T006): the token request's own attempts, its body read and its 4xx, a
// cancellation racing a fired wait, an answer whose elements do not decode, and a deadline that
// ends a request.

// TestListingRetryTokenBodyReset (FR-001: a transport failure while reading the answer's body,
// for the token request too): a token answer whose connection closes mid-body is retried as
// `transport` and the check passes. Exposes a token body-read failure classed `decode` and cached.
func TestListingRetryTokenBodyReset(t *testing.T) {
	t.Parallel()
	c, inv, s, _ := retryAPICheck(t, map[string][]string{"token": {"body-reset"}})
	rep := c.Check(retryCtx(t), inv)
	if rep.Outcome != "pass" || len(rep.Errors) != 0 {
		t.Errorf("token body reset once: outcome %q, errors %v; want pass", rep.Outcome, rep.Errors)
	}
	if got := s.Hits("token"); got != 2 {
		t.Errorf("token requested %d times, want 2 (one retry)", got)
	}
	want := ListingAttempt{Path: tokenPath, Attempt: 1, Class: "transport", Retried: true}
	if got := attemptsOf(rep, tokenPath); !slices.Equal(got, []ListingAttempt{want}) {
		t.Errorf("token attempts %+v, want %+v", got, want)
	}
}

// TestListingRetryToken429 (FR-001: a 4xx answer from the token endpoint is never retried and
// stays cached): a token 429 is asked once and recorded `permanent`. Exposes the listings' 429
// rule applied to the token request.
func TestListingRetryToken429(t *testing.T) {
	t.Parallel()
	c, inv, s, term := retryAPICheck(t, map[string][]string{"token": {"429", "429", "429", "429", "429"}})
	rep := c.Check(retryCtx(t), inv)
	if rep.Outcome != "fail" {
		t.Errorf("token 429: outcome %q, want fail", rep.Outcome)
	}
	if got := s.Hits("token"); got != 1 {
		t.Errorf("token requested %d times, want 1", got)
	}
	if got := listingRetryLines(term.String()); len(got) != 0 {
		t.Errorf("retry lines %q, want none", got)
	}
	want := ListingAttempt{Path: tokenPath, Attempt: 1, Status: 429, Class: "status", Reason: "permanent"}
	if got := attemptsOf(rep, tokenPath); !slices.Equal(got, []ListingAttempt{want}) {
		t.Errorf("token attempts %+v, want %+v", got, want)
	}
}

// TestListingRetryTokenAttemptsPerCheck (FR-001: one request, at most twice after the first
// attempt): a token endpoint answering 503 for the whole check is asked three times in all, its
// attempts numbered 1 to 3, and no listing reaches the API. Exposes token attempts counted afresh
// for each listing.
func TestListingRetryTokenAttemptsPerCheck(t *testing.T) {
	t.Parallel()
	c, inv, s, _ := retryAPICheck(t, map[string][]string{"token": slices.Repeat([]string{"503"}, 60)})
	rep := c.Check(retryCtx(t), inv)
	if rep.Outcome != "fail" {
		t.Errorf("token 503 throughout: outcome %q, want fail", rep.Outcome)
	}
	if got := s.Hits("token"); got != 3 {
		t.Errorf("token requested %d times in the check, want 3", got)
	}
	if got := s.apiHits(); got != 0 {
		t.Errorf("%d listing requests reached the API without a token, want 0", got)
	}
	want := []ListingAttempt{
		{Path: tokenPath, Attempt: 1, Status: 503, Class: "status", Retried: true},
		{Path: tokenPath, Attempt: 2, Status: 503, Class: "status", Retried: true},
		{Path: tokenPath, Attempt: 3, Status: 503, Class: "status", Reason: "attempts"},
	}
	if got := attemptsOf(rep, tokenPath); !slices.Equal(got, want) {
		t.Errorf("token attempts %+v, want %+v", got, want)
	}
}

// TestListingRetryTokenOwnAttempts: two token 503s, then a token, then the first listing's 500:
// the listing still has its own retry. Exposes token failures spending the listing's attempts.
func TestListingRetryTokenOwnAttempts(t *testing.T) {
	t.Parallel()
	c, inv, s, _ := retryAPICheck(t, map[string][]string{"token": {"503", "503"}, pathRegion: {"500"}})
	rep := c.Check(retryCtx(t), inv)
	if rep.Outcome != "pass" || len(rep.Errors) != 0 {
		t.Errorf("outcome %q, errors %v; want pass", rep.Outcome, rep.Errors)
	}
	if got := s.Hits(pathRegion); got != 2 {
		t.Errorf("%s asked %d times, want 2", pathRegion, got)
	}
	want := ListingAttempt{Path: pathRegion, Attempt: 1, Status: 500, Class: "status", Retried: true}
	if got := attemptsOf(rep, pathRegion); !slices.Equal(got, []ListingAttempt{want}) {
		t.Errorf("%s attempts %+v, want %+v", pathRegion, got, want)
	}
}

// raceClock cancels the context while it hands out a wait that has already fired: both are ready
// when the check selects.
type raceClock struct {
	*listClock
	cancel func(error)
}

func (c raceClock) After(d time.Duration) <-chan time.Time {
	ch := c.listClock.After(d)
	c.cancel(errors.New("interrupt during the wait"))
	return ch
}

// TestListingRetryCancelRace (FR-001: a cancellation during a wait ends it with no further
// attempt): when the cancellation and the fired wait are ready together, no further attempt is
// made, whichever the select picks. Twenty runs, so a coin-flip choice survives with odds 2^-20.
// Exposes a wait that trusts the timer without looking at the context again.
func TestListingRetryCancelRace(t *testing.T) {
	t.Parallel()
	for i := range 20 {
		c, inv, sl, clk, _ := retryCheck(t, map[string][]listAnswer{pathNet1 + "#1": times(3, status(pathNet1, 500))}, syntheticSeed{})
		ctx, cancel := context.WithCancelCause(retryCtx(t))
		c.Clock = raceClock{listClock: clk, cancel: cancel}
		rep := c.Check(ctx, inv)
		if got := sl.asks(pathNet1); len(got) != 1 {
			t.Fatalf("run %d: %s asked %d times, want once (cancelled during the wait)", i, pathNet1, len(got))
		}
		want := ListingAttempt{Path: pathNet1, Attempt: 1, Status: 500, Class: "status", Reason: "cancelled"}
		if got := attemptsOf(rep, pathNet1); !slices.Equal(got, []ListingAttempt{want}) {
			t.Fatalf("run %d: attempts %+v, want %+v", i, got, want)
		}
	}
}

// TestListingRetryElementDecode (FR-002: every failed attempt): an answer that is a JSON array
// whose elements are not ids is recorded `decode`, `permanent`. Exposes a decode failure past the
// array level left out of the record.
func TestListingRetryElementDecode(t *testing.T) {
	t.Parallel()
	c, inv, _, _, _ := retryCheck(t, map[string][]listAnswer{pathRegion + "#1": {{body: "[7]"}}}, syntheticSeed{})
	rep := c.Check(retryCtx(t), inv)
	if rep.Outcome != "fail" {
		t.Errorf("outcome %q, want fail", rep.Outcome)
	}
	want := ListingAttempt{Path: pathRegion, Attempt: 1, Class: "decode", Reason: "permanent"}
	if got := attemptsOf(rep, pathRegion); !slices.Equal(got, []ListingAttempt{want}) {
		t.Errorf("attempts %+v, want %+v", got, want)
	}
}

// deadlineLister ends the context with the deadline as its cause during path's first request,
// which then fails in transport, as a request cut by the destroy context's deadline does.
type deadlineLister struct {
	Lister
	path string
	end  func(error)
}

func (d *deadlineLister) Get(ctx context.Context, path, cursor string) ([]byte, string, error) {
	if path != d.path {
		return d.Lister.Get(ctx, path, cursor)
	}
	d.end(context.DeadlineExceeded)
	return nil, "", &transportError{path: path, err: context.DeadlineExceeded}
}

// TestListingRetryDeadlineEndsRequest (FR-002: why no retry followed): a request cut by the
// context's deadline is recorded with reason `deadline`, not `cancelled`. Exposes a deadline filed
// as a cancellation.
func TestListingRetryDeadlineEndsRequest(t *testing.T) {
	t.Parallel()
	c, inv, _, _, _ := retryCheck(t, nil, syntheticSeed{})
	ctx, end := context.WithCancelCause(retryCtx(t))
	c.Lister = &deadlineLister{Lister: c.Lister, path: pathNet1, end: end}
	rep := c.Check(ctx, inv)
	want := ListingAttempt{Path: pathNet1, Attempt: 1, Class: "transport", Reason: "deadline"}
	if got := attemptsOf(rep, pathNet1); !slices.Equal(got, []ListingAttempt{want}) {
		t.Errorf("attempts %+v, want %+v", got, want)
	}
}

// Review round 2 (T006): answers whose status decides, whatever happens to their body or their
// Location header, and token answers recorded under the token request.

// frontAPI is s behind a front that answers route's first request with raw (written on the bare
// connection, then closed) and hands every other request to s; hits counts route's raw answers.
func frontAPI(t *testing.T, s *scriptAPI, route, raw string) (API, *atomic.Int32) {
	t.Helper()
	hits := &atomic.Int32{}
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rt := "token"
		if r.URL.Path != "/auth/oauth2/token" {
			_, rest, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
			rt = "/" + rest
		}
		if rt != route || !hits.CompareAndSwap(0, 1) {
			s.serve(w, r)
			return
		}
		conn, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			panic(err)
		}
		buf.WriteString(raw)
		buf.Flush()
		conn.Close()
	}))
	t.Cleanup(front.Close)
	a := s.API()
	a.TokenURL, a.BaseURL = front.URL+"/auth/oauth2/token", front.URL+"/v1"
	return a, hits
}

// TestListingRetryStatusDecides (FR-001: any status but 429 and 5xx is tried once): a 403 whose
// body breaks off, and a 302 whose Location Go cannot parse, are each asked once and recorded
// with their status, `permanent`. Exposes a transport failure judged before the answer's status.
func TestListingRetryStatusDecides(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, raw string
		code      int
	}{
		{"403-body-reset", "HTTP/1.1 403 Forbidden\r\nContent-Type: application/json\r\nContent-Length: 64\r\n\r\n{\"mess", 403},
		{"302-bad-location", "HTTP/1.1 302 Found\r\nLocation: /%ZZ\r\nContent-Length: 0\r\n\r\n", 302},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, inv, s, term := retryAPICheck(t, nil)
			var hits *atomic.Int32
			c.API, hits = frontAPI(t, s, pathUser1, tc.raw)
			rep := c.Check(retryCtx(t), inv)
			if rep.Outcome != "fail" {
				t.Errorf("outcome %q, want fail", rep.Outcome)
			}
			if got := int(hits.Load()) + s.Hits(pathUser1); got != 1 {
				t.Errorf("%s asked %d times, want once", pathUser1, got)
			}
			if got := listingRetryLines(term.String()); len(got) != 0 {
				t.Errorf("retry lines %q, want none", got)
			}
			want := ListingAttempt{Path: pathUser1, Attempt: 1, Status: tc.code, Class: "status", Reason: "permanent"}
			if got := attemptsOf(rep, pathUser1); !slices.Equal(got, []ListingAttempt{want}) {
				t.Errorf("attempts %+v, want %+v", got, want)
			}
		})
	}
}

// TestListingRetryTokenAnswerRecorded (FR-002): a token answer without an access token, and a
// token 302 whose Location Go cannot parse, are recorded under "token" (decode, and status 302),
// `permanent`, and under no listing. Exposes a token failure filed under the listing that asked.
func TestListingRetryTokenAnswerRecorded(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, raw string
		row       ListingAttempt
	}{
		{"no-access-token", "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 2\r\n\r\n{}",
			ListingAttempt{Path: tokenPath, Attempt: 1, Class: "decode", Reason: "permanent"}},
		{"302-bad-location", "HTTP/1.1 302 Found\r\nLocation: /%ZZ\r\nContent-Length: 0\r\n\r\n",
			ListingAttempt{Path: tokenPath, Attempt: 1, Status: 302, Class: "status", Reason: "permanent"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, inv, s, _ := retryAPICheck(t, nil)
			c.API, _ = frontAPI(t, s, "token", tc.raw)
			rep := c.Check(retryCtx(t), inv)
			if rep.Outcome != "fail" {
				t.Errorf("outcome %q, want fail", rep.Outcome)
			}
			if !slices.Equal(rep.Attempts, []ListingAttempt{tc.row}) {
				t.Errorf("attempts %+v, want only %+v", rep.Attempts, tc.row)
			}
		})
	}
}
