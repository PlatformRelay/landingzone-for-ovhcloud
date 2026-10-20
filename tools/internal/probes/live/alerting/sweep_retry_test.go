package alerting

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/live"
)

// T027 (spec 007 FR-001, FR-007; stage B review): the sweep's reads (the alert listing, GET /me,
// the alert read, the listing after a delete) go through the leftover check's retry the runner
// lends the Sweep (live.LeftoverCheck.ReadRetry: its backoff, budget and clock); the DELETE is
// sent once. Errors and retry lines name the alert routes with placeholders, never the project or
// alert id. The fake API is T012's (tWorld) behind a front that answers chosen requests itself.

// tHexID is a 32-hex id, the project's shape.
var tHexID = regexp.MustCompile(`[0-9a-f]{32}`)

// tFront answers a request itself when fail names a status for it (its "METHOD path" and the
// number of that request so far, from 1), closes the connection for -1, cuts the answer's body
// short for -2, answers 403 with a body cut short for -3, 302 with a Location Go cannot parse
// for -4, 503 (or 200 for -7) with a body over the checker's 1 MiB bound for -5 and 404 with a
// body cut short for -6, else hands it to the world.
type tFront struct {
	w    *tWorld
	srv  *httptest.Server
	mu   sync.Mutex
	n    map[string]int
	log  []string
	fail func(req string, n int) int
}

func newTFront(t *testing.T, w *tWorld, fail func(req string, n int) int) *tFront {
	f := &tFront{w: w, n: map[string]int{}, fail: fail}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *tFront) serve(rw http.ResponseWriter, r *http.Request) {
	req := r.Method + " " + r.URL.Path
	f.mu.Lock()
	f.n[req]++
	n := f.n[req]
	f.log = append(f.log, req)
	f.mu.Unlock()
	switch code := f.fail(req, n); {
	case code == -1:
		hj, ok := rw.(http.Hijacker)
		if !ok {
			f.w.t.Error("front: no hijacker")
			return
		}
		conn, _, err := hj.Hijack()
		if err == nil {
			conn.Close()
		}
	case code == -2 || code == -3 || code == -6:
		rw.Header().Set("Content-Length", "100")
		rw.WriteHeader(map[int]int{-2: http.StatusOK, -3: http.StatusForbidden, -6: http.StatusNotFound}[code])
		fmt.Fprint(rw, "[")
	case code == -5 || code == -7:
		rw.WriteHeader(map[int]int{-5: http.StatusServiceUnavailable, -7: http.StatusOK}[code])
		rw.Write(bytes.Repeat([]byte(" "), 1<<20+1))
	case code == -4:
		rw.Header().Set("Location", "http://%zz")
		rw.WriteHeader(http.StatusFound)
	case code != 0:
		rw.WriteHeader(code)
		fmt.Fprint(rw, `{"message":"Internal server error"}`)
	default:
		f.w.serve(rw, r)
	}
}

// count is how many times req was sent.
func (f *tFront) count(req string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.n[req]
}

func (f *tFront) options() Options {
	o := f.w.options()
	o.API = live.API{TokenURL: f.srv.URL + "/token", BaseURL: f.srv.URL + "/v1", HTTP: f.srv.Client()}
	return o
}

// tRetryCtx is a context carrying the leftover check's retry with short waits and a budget an
// hour long, its retry lines written to term.
func tRetryCtx(t *testing.T, term *bytes.Buffer, end time.Time) context.Context {
	return live.LeftoverCheck{Backoff: []time.Duration{time.Millisecond, 2 * time.Millisecond}, Term: term}.ReadRetry(tCtx(t), end)
}

// sweepVia runs the Sweep through the front with the retry in its context.
func (f *tFront) sweepVia(t *testing.T, end time.Time) (string, error) {
	var term bytes.Buffer
	o := f.options()
	o.Project = tOtherProject
	err := Check(o).Sweep(tRetryCtx(t, &term, end), tRun, tProject, &term)
	return term.String(), err
}

const (
	tListReq  = "GET /v1/cloud/project/" + tProject + "/alerting"
	tReadReq  = "GET /v1/cloud/project/" + tProject + "/alerting/" + alertB
	tDelReq   = "DELETE /v1/cloud/project/" + tProject + "/alerting/" + alertB
	tMeReq    = "GET /v1/me"
	tListPath = "/cloud/project/{serviceName}/alerting"
	tReadPath = "/cloud/project/{serviceName}/alerting/{id}"
)

// tRetriedCase is one transient failure: req's n-th send answers code (-1 closes the connection,
// -2 cuts the body); sends is how often req goes out in a sweep without failures (the listing
// twice: before and after the delete).
type tRetriedCase struct {
	name, req, path, what string
	n, code, sends        int
}

// fails is the front's rule for c. A closed connection fails twice: Go's transport resends a
// request once on its own when a reused connection closes, so the check sees the second.
func (c tRetriedCase) fails(req string, n int) int {
	if req == c.req && (n == c.n || c.code == -1 && n <= 2) {
		return c.code
	}
	return 0
}

// tSweepWorld is a world holding the baseline's A and the run's B, with the run's baseline and state.
func tSweepWorld(t *testing.T) *tWorld {
	w := newTWorld(t)
	w.add(tProject, alertB, tPlanned)
	w.write(tFiles{record: tBaseline(t, nil), state: true})
	return w
}

// A transient failure on each sweep read, once, is retried: the sweep deletes B and passes, the
// failed read is sent twice, the DELETE once, and the retry line names the route's placeholders.
func TestSweepReadRetried(t *testing.T) {
	for _, c := range []tRetriedCase{
		{name: "listing-500", req: tListReq, path: tListPath, what: "500", n: 1, code: 500, sends: 2},
		{name: "listing-429", req: tListReq, path: tListPath, what: "429", n: 1, code: 429, sends: 2},
		{name: "listing-transport", req: tListReq, path: tListPath, what: "transport", n: 1, code: -1, sends: 2},
		{name: "listing-body-cut", req: tListReq, path: tListPath, what: "transport", n: 1, code: -2, sends: 2},
		{name: "listing-503-large", req: tListReq, path: tListPath, what: "503", n: 1, code: -5, sends: 2},
		{name: "me-503", req: tMeReq, path: "/me", what: "503", n: 1, code: 503, sends: 1},
		{name: "read-500", req: tReadReq, path: tReadPath, what: "500", n: 1, code: 500, sends: 1},
		{name: "relist-502", req: tListReq, path: tListPath, what: "502", n: 2, code: 502, sends: 2},
	} {
		t.Run(c.name, func(t *testing.T) { tRetriedRow(t, c) })
	}
}

func tRetriedRow(t *testing.T, c tRetriedCase) {
	w := tSweepWorld(t)
	f := newTFront(t, w, c.fails)
	term, err := f.sweepVia(t, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("Sweep with one %s on %s: %v (terminal %q)", c.what, c.req, err, term)
	}
	if w.has(tProject, alertB) || !w.has(tProject, alertA) {
		t.Errorf("after the sweep B listed %v, A listed %v; want B gone, A kept", w.has(tProject, alertB), w.has(tProject, alertA))
	}
	// Go's transport may resend a closed connection's request itself: no count.
	if got := f.count(c.req); c.code != -1 && got != c.sends+1 {
		t.Errorf("%s sent %d times, want %d (the failed one asked again)", c.req, got, c.sends+1)
	}
	if got := f.count(tDelReq); got != 1 {
		t.Errorf("DELETE sent %d times, want once", got)
	}
	tWantRetryLine(t, term, c.path, c.what)
}

// tWantRetryLine: term has the retry line for path before attempt 2 and no 32-hex id.
func tWantRetryLine(t *testing.T, term, path, what string) {
	t.Helper()
	line := fmt.Sprintf("LZ-LIVE retry listing %s in 1ms: %s (attempt 2 of 3)", path, what)
	if !strings.Contains(term, line) {
		t.Errorf("terminal %q lacks %q", term, line)
	}
	if tHexID.MatchString(term) {
		t.Errorf("terminal %q names a 32-hex id", term)
	}
}

// tPersistentCase is a read that always answers 500, with the time left in the budget, the sends
// expected and the error's tail.
type tPersistentCase struct {
	name, req, path string
	left            time.Duration
	sends           int
	want            string
}

// A sweep read that keeps failing fails the sweep after three attempts, each one recorded (the
// error names the last as 3 of 3, two retry lines precede it) and nothing deleted; a budget that
// admits no wait sends it once and says so.
func TestSweepReadPersistent(t *testing.T) {
	for _, c := range []tPersistentCase{
		{name: "listing", req: tListReq, path: tListPath, left: time.Hour, sends: 3, want: "answered 500 (attempt 3 of 3)"},
		{name: "read", req: tReadReq, path: tReadPath, left: time.Hour, sends: 3, want: "answered 500 (attempt 3 of 3)"},
		{name: "listing-budget", req: tListReq, path: tListPath, left: 10 * time.Second, sends: 1, want: "answered 500 (attempt 1 of 3, not retried: budget)"},
	} {
		t.Run(c.name, func(t *testing.T) { tPersistentRow(t, c) })
	}
}

// tAlways500 is a front rule: req always answers 500.
func tAlways500(req string) func(string, int) int {
	return func(r string, _ int) int {
		if r == req {
			return 500
		}
		return 0
	}
}

func tPersistentRow(t *testing.T, c tPersistentCase) {
	w := tSweepWorld(t)
	f := newTFront(t, w, tAlways500(c.req))
	term, err := f.sweepVia(t, time.Now().Add(c.left))
	if err == nil {
		t.Fatalf("Sweep passed although %s always answered 500 (terminal %q)", c.req, term)
	}
	if got := f.count(c.req); got != c.sends {
		t.Errorf("%s sent %d times, want %d", c.req, got, c.sends)
	}
	if want := "GET " + c.path + " " + c.want; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q lacks %q", err, want)
	}
	if got := strings.Count(term, "LZ-LIVE retry listing "+c.path); got != c.sends-1 {
		t.Errorf("terminal has %d retry lines for %s, want %d: %q", got, c.path, c.sends-1, term)
	}
	if f.count(tDelReq) != 0 || !w.has(tProject, alertB) {
		t.Errorf("DELETE sent %d times, B listed %v; want none sent, B kept", f.count(tDelReq), w.has(tProject, alertB))
	}
	if tHexID.MatchString(err.Error() + term) {
		t.Errorf("error %q or terminal %q names a 32-hex id", err, term)
	}
}

// A DELETE answered 500 is sent once and fails the sweep, with no retry line (a pin: the DELETE
// was single-shot before T027 and stays so).
func TestSweepDeleteOnce(t *testing.T) {
	w := tSweepWorld(t)
	f := newTFront(t, w, tAlways500(tDelReq))
	term, err := f.sweepVia(t, time.Now().Add(time.Hour))
	if err == nil {
		t.Fatalf("Sweep passed although the DELETE answered 500 (terminal %q)", term)
	}
	if got := f.count(tDelReq); got != 1 {
		t.Errorf("DELETE sent %d times, want once: a DELETE is never retried", got)
	}
	if strings.Contains(term, "LZ-LIVE retry") {
		t.Errorf("terminal %q has a retry line for the DELETE", term)
	}
}

// Read retries only reads: a DELETE through it is refused before any request.
func TestReadRefusesDelete(t *testing.T) {
	w := newTWorld(t)
	w.add(tProject, alertB, tPlanned)
	f := newTFront(t, w, func(string, int) int { return 0 })
	o := f.options()
	var term bytes.Buffer
	ctx := tRetryCtx(t, &term, time.Now().Add(time.Hour))
	c, err := live.NewAPIChecker(ctx, o.API, o.Admin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Read(ctx, live.APICall{Op: live.APIDeleteAlert, Project: tProject, ID: alertB}); err == nil {
		t.Error("Read of a DeleteAlert passed")
	}
	if got := f.count(tDelReq); got != 0 {
		t.Errorf("DELETE sent %d times through Read, want none", got)
	}
}

// tRouteCase is one alert call whose request req gets body (none: the connection is closed).
type tRouteCase struct {
	name  string
	call  live.APICall
	req   string
	body  string
	route string
}

// The alert routes' errors name {serviceName} and {id}, never the project or the alert id: a
// listing or a read that does not decode, and a connection closed without an answer.
func TestAlertErrorsNameRoute(t *testing.T) {
	for _, c := range []tRouteCase{
		{name: "listing-decode", call: live.APICall{Op: live.APIListAlerts, Project: tProject}, req: tListReq, body: `{"not":"a list"}`, route: "GET " + tListPath},
		{name: "read-decode", call: live.APICall{Op: live.APIGetAlert, Project: tProject, ID: alertB}, req: tReadReq, body: `{"id":"other"}`, route: "GET " + tReadPath},
		{name: "listing-closed", call: live.APICall{Op: live.APIListAlerts, Project: tProject}, req: tListReq, route: "GET " + tListPath},
		{name: "delete-closed", call: live.APICall{Op: live.APIDeleteAlert, Project: tProject, ID: alertB}, req: tDelReq, route: "DELETE " + tReadPath},
	} {
		t.Run(c.name, func(t *testing.T) { tRouteRow(t, c) })
	}
}

// serve answers req with c.body, or closes the connection; anything else goes to the world.
func (c tRouteCase) serve(w *tWorld) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method+" "+r.URL.Path != c.req:
			w.serve(rw, r)
		case c.body != "":
			fmt.Fprint(rw, c.body)
		default:
			if conn, _, err := rw.(http.Hijacker).Hijack(); err == nil {
				conn.Close()
			}
		}
	}
}

func tRouteRow(t *testing.T, c tRouteCase) {
	w := newTWorld(t)
	srv := httptest.NewServer(c.serve(w))
	t.Cleanup(srv.Close)
	o := w.options()
	o.API = live.API{TokenURL: srv.URL + "/token", BaseURL: srv.URL + "/v1", HTTP: srv.Client()}
	chk, err := live.NewAPIChecker(tCtx(t), o.API, o.Admin)
	if err != nil {
		t.Fatal(err)
	}
	_, err = chk.Do(tCtx(t), c.call)
	if err == nil {
		t.Fatalf("%s passed", c.name)
	}
	msg := err.Error()
	if tHexID.MatchString(msg) || strings.Contains(msg, alertB) {
		t.Errorf("error %q names the project or the alert id", msg)
	}
	if !strings.HasPrefix(msg, c.route) {
		t.Errorf("error %q does not start with the route %q", msg, c.route)
	}
}

// tStatusCase is a read whose answer is a status the retry must judge by the status first.
type tStatusCase struct {
	name, req, path string
	code            int
	want            string
}

// A sweep read is judged by its status before its body or headers (spec 007 FR-001, T006): a 403
// whose body is cut short and a 302 whose Location Go cannot parse are that status, never a
// transport failure, so neither is retried; a plain 403 is a failed attempt, recorded (the error
// is the attempt's, not an answer's) and not retried.
func TestSweepReadStatusFirst(t *testing.T) {
	for _, c := range []tStatusCase{
		{name: "403-body-cut", req: tListReq, path: tListPath, code: -3, want: "answered 403"},
		{name: "302-bad-location", req: tListReq, path: tListPath, code: -4, want: "answered 302"},
		{name: "403", req: tListReq, path: tListPath, code: 403, want: "answered 403"},
		{name: "me-403", req: tMeReq, path: "/me", code: 403, want: "answered 403"},
		{name: "listing-404", req: tListReq, path: tListPath, code: 404, want: "answered 404"},
		{name: "me-404", req: tMeReq, path: "/me", code: 404, want: "answered 404"},
	} {
		t.Run(c.name, func(t *testing.T) { tStatusRow(t, c) })
	}
}

func tStatusRow(t *testing.T, c tStatusCase) {
	w := tSweepWorld(t)
	f := newTFront(t, w, func(req string, _ int) int {
		if req == c.req {
			return c.code
		}
		return 0
	})
	term, err := f.sweepVia(t, time.Now().Add(time.Hour))
	if err == nil {
		t.Fatalf("Sweep passed although %s answered %d (terminal %q)", c.req, c.code, term)
	}
	if got := f.count(c.req); got != 1 {
		t.Errorf("%s sent %d times, want once: a 4xx or 3xx is not retried", c.req, got)
	}
	if want := "GET " + c.path + " " + c.want; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q lacks %q (the recorded attempt's status)", err, want)
	}
	if strings.Contains(term, "LZ-LIVE retry") || f.count(tDelReq) != 0 {
		t.Errorf("terminal %q has a retry line, or DELETE sent %d times", term, f.count(tDelReq))
	}
}

// An alert gone before its read (404) needs nothing: no DELETE, no retry, the sweep passes (a pin:
// FR-007's 404 as gone holds through Read).
func TestSweepReadGone(t *testing.T) {
	w := tSweepWorld(t)
	f := newTFront(t, w, func(req string, _ int) int {
		if req == tReadReq {
			return http.StatusNotFound
		}
		return 0
	})
	term, err := f.sweepVia(t, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("Sweep with B gone before its read: %v (terminal %q)", err, term)
	}
	if f.count(tReadReq) != 1 || f.count(tDelReq) != 0 {
		t.Errorf("read sent %d times, DELETE %d times; want once and none", f.count(tReadReq), f.count(tDelReq))
	}
}

// An alert gone before its read whose 404 answer's body broke off is still gone (status first): no
// DELETE, no retry, the sweep passes.
func TestSweepReadGoneCutBody(t *testing.T) {
	w := tSweepWorld(t)
	f := newTFront(t, w, func(req string, _ int) int {
		if req == tReadReq {
			return -6
		}
		return 0
	})
	term, err := f.sweepVia(t, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("Sweep with B gone (404, body cut) before its read: %v (terminal %q)", err, term)
	}
	if f.count(tReadReq) != 1 || f.count(tDelReq) != 0 {
		t.Errorf("read sent %d times, DELETE %d times; want once and none", f.count(tReadReq), f.count(tDelReq))
	}
}

// A single-shot alert call (the sweep's DELETE, Observe's read) that times out says so, with the
// route: the kind of failure is kept where the path is replaced.
func TestAlertErrorKeepsKind(t *testing.T) {
	w := newTWorld(t)
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			<-block
			return
		}
		w.serve(rw, r)
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(block) }) // runs first: the blocked handler returns before Close waits
	cl := srv.Client()
	cl.Timeout = 200 * time.Millisecond
	chk, err := live.NewAPIChecker(tCtx(t), live.API{TokenURL: srv.URL + "/token", BaseURL: srv.URL + "/v1", HTTP: cl}, w.options().Admin)
	if err != nil {
		t.Fatal(err)
	}
	_, err = chk.Do(tCtx(t), live.APICall{Op: live.APIDeleteAlert, Project: tProject, ID: alertB})
	if want := "DELETE " + tReadPath + ": timed out"; err == nil || err.Error() != want {
		t.Errorf("error %v, want %q", err, want)
	}
}

// A 2xx answer over the checker's bound is not a transport failure: sent once, the sweep fails.
func TestSweepReadLargeAnswer(t *testing.T) {
	w := tSweepWorld(t)
	f := newTFront(t, w, func(req string, _ int) int {
		if req == tListReq {
			return -7
		}
		return 0
	})
	term, err := f.sweepVia(t, time.Now().Add(time.Hour))
	if err == nil || !strings.Contains(err.Error(), "GET "+tListPath+": answer larger than") {
		t.Fatalf("Sweep error %v, want the listing's oversized answer (terminal %q)", err, term)
	}
	if got := f.count(tListReq); got != 1 || strings.Contains(term, "LZ-LIVE retry") {
		t.Errorf("listing sent %d times, terminal %q; want once, no retry", got, term)
	}
}
