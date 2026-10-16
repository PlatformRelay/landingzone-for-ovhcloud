package live

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Shared-budget wiring of the leftover listing retry (spec 007 FR-001, SC-001; V001; T004). A
// wait before a listing retry starts only when the context's deadline and the run's shared retry
// budget both admit it: in a chain, the budget its retained-state reads used (spec 006 FR-008);
// in a probe, one built from the destroy context's deadline. The chain cases run the whole chain
// through ChainOptions with the chain harness (fake children, fake listing API, retryClock); the
// probe case runs Probe.Start with the run world's fakes and a clock anchored at the destroy
// context's deadline.

// finalFault is the chain's Lister seam with one fault: the first first-page request of the final
// reconciliation (once atFinal marked it, the first listing after an apply) answers 500 after the
// fake API served it, as a server failing that request would; every other request passes through.
// It counts the final reconciliation's requests per path and cursor.
type finalFault struct {
	Lister
	final *atomic.Bool
	mu    sync.Mutex
	path  string         // the request that answered 500 ("" until one did)
	asked map[string]int // path#cursor -> requests of the final reconciliation
}

func (f *finalFault) Get(ctx context.Context, path, cursor string) ([]byte, string, error) {
	body, next, err := f.Lister.Get(ctx, path, cursor)
	if !f.final.Load() {
		return body, next, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked[path+"#"+cursor]++
	if f.path == "" && err == nil && cursor == "" {
		f.path = path
		return nil, "", &statusError{path: path, code: 500}
	}
	return body, next, err
}

// failed is the path that answered 500 and how often its first page was asked in the final
// reconciliation.
func (f *finalFault) failed() (string, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.path, f.asked[f.path+"#"]
}

// The chain cases' times. account-governance's state read answers one 503 and is retried after
// retryWait (4 min) on the run's clock. budgetSpentLeft is what the leftover check has when it
// starts in the spent case: the retained retry's wait (4 min) and its slow second read
// (budgetSlowRead, which raises the shared budget's reserve to at least 3 min) leave about 1 min
// 30 s, so the listing's first wait (defaultListingBackoff[0], 5 s) would leave less than that
// reserve: the shared budget refuses it, while a budget built afresh for the listings (reserve
// 30 s) or none read as unbounded admits it, as the deadline does. budgetLeftLeft leaves room for
// every wait.
const (
	budgetSlowRead  = 3 * time.Minute
	budgetSpentLeft = 8*time.Minute + 30*time.Second
	budgetLeftLeft  = 30 * time.Minute
)

// listingBudgetChain runs a complete chain in which account-governance's retained-state read is
// retried once (and, with slow, its second read takes budgetSlowRead on the run's clock), with
// left of the destroy budget when the leftover check starts, and the final reconciliation's first
// listing answering 500 once.
func listingBudgetChain(t *testing.T, left time.Duration, slow bool) (*laneHarness, chainRun, *retryClock, Report, *finalFault) {
	t.Helper()
	h := newChainHarness(t)
	h.setStack("account-governance", func(s *laneStack) {
		s.ReadFail = &laneReadFail{Call: "show-state", Answers: []string{
			"Error: reading the state: GET /v1/iam/policy: 503 Service Unavailable (" + chainAnswerMark + ")"}}
	})
	clk := &retryClock{spent: budgetLongRun - left}
	var final atomic.Bool
	ff := &finalFault{final: &final, asked: map[string]int{}}
	ex := chainExec{deadline: budgetLongRun, clock: clk, stateBackoff: []time.Duration{retryWait, retryWait},
		atFinal:    func(string, *fakeListAPI) { final.Store(true) },
		wrapLister: func(l Lister) Lister { ff.Lister = l; return ff }}
	var slowed atomic.Bool
	if slow {
		// The first `show -json` after the retained read's wait (its retry) takes budgetSlowRead.
		ex.children = &childDeadlines{onStart: func(args []string) {
			if _, n := clk.total(); n >= 1 && len(args) == 3 && args[1] == "show" && args[2] == "-json" &&
				slowed.CompareAndSwap(false, true) {
				clk.advance(budgetSlowRead)
			}
		}}
	}
	r := h.chain(ex)
	if slow && !slowed.Load() {
		t.Fatalf("the retained read's retry never ran (events %v)", laneEvents(r.calls))
	}
	if n := retryReads(r, "account-governance"); n != 2 {
		t.Fatalf("%d reads of account-governance, want 2: the retained read's retry is the case's premise", n)
	}
	if p, _ := ff.failed(); p == "" {
		t.Fatalf("the final reconciliation never listed (events %v)", laneEvents(r.calls))
	}
	return h, r, clk, chainReport(t, r.runDir), ff
}

// TestChainListingRetryBudgetSpent (FR-001, SC-001: in a chain whose retained-state retries spent
// the shared budget, the next transient listing is not retried): the retained read's retry and its
// slow second read leave less than the reserve plus the listing's first wait. The listing that
// answers 500 is asked once, no listing wait starts, its attempt is recorded with reason `budget`,
// and the report is not `pass`. Exposes a listing check built with a fresh budget, or with a nil
// budget read as unbounded.
func TestChainListingRetryBudgetSpent(t *testing.T) {
	_, r, clk, rep, ff := listingBudgetChain(t, budgetSpentLeft, true)
	path, asked := ff.failed()
	if asked != 1 {
		t.Errorf("the final reconciliation asked %s %d times, want 1: the shared budget has no room for its wait", path, asked)
	}
	if got, n := clk.total(); n != 1 {
		t.Errorf("the run took %d waits (%v), want 1 (the retained read's): the listing's wait leaves less than the reserve", n, got)
	}
	want := []ListingAttempt{{Path: path, Attempt: 1, Status: 500, Class: "status", Retried: false, Reason: "budget"}}
	if got := attemptsOf(rep, path); !slices.Equal(got, want) {
		t.Errorf("leftovers.json attempts of %s %+v, want %+v: a wait the shared budget refuses is recorded with reason budget",
			path, got, want)
	}
	if rep.Outcome == "pass" {
		t.Errorf("report outcome pass although %s answered 500 and was not listed again", path)
	}
	if ExitCode(r.err) == 0 {
		t.Errorf("the run exited 0 (%v) with a listing that failed", r.err)
	}
}

// TestChainListingRetryBudgetLeft (FR-001): the same chain with room left after the retained read:
// the listing that answers 500 is retried after defaultListingBackoff[0] on the run's clock (the
// one its retained reads wait on), the retry line names it, its attempt is recorded as retried, and
// the report is `pass`. Exposes a listing check that never receives the budget (nil admits no
// wait) or waits on another clock.
func TestChainListingRetryBudgetLeft(t *testing.T) {
	h, r, clk, rep, ff := listingBudgetChain(t, budgetLeftLeft, false)
	path, asked := ff.failed()
	if asked != 2 {
		t.Errorf("the final reconciliation asked %s %d times, want 2 (one retry)", path, asked)
	}
	clk.mu.Lock()
	waits := slices.Clone(clk.waited)
	clk.mu.Unlock()
	if want := []time.Duration{retryWait, defaultListingBackoff[0]}; !slices.Equal(waits, want) {
		t.Errorf("waits on the run's clock %v, want %v: the retained read's, then the listing's first", waits, want)
	}
	want := []ListingAttempt{{Path: path, Attempt: 1, Status: 500, Class: "status", Retried: true}}
	if got := attemptsOf(rep, path); !slices.Equal(got, want) {
		t.Errorf("leftovers.json attempts of %s %+v, want %+v", path, got, want)
	}
	prefix := "LZ-LIVE retry listing " + path + " in "
	if got := listingRetryLines(h.term.String()); len(got) != 1 || !strings.HasPrefix(got[0], prefix) {
		t.Errorf("retry lines %q, want one starting %q (FR-001)", got, prefix)
	}
	if rep.Outcome != "pass" || len(rep.Errors) != 0 {
		t.Errorf("report outcome %q, errors %v; want pass: the retry fits the budget", rep.Outcome, rep.Errors)
	}
	laneExit(t, r.laneRun, 0)
}

// destroyClock is the probe case's waitClock: Now is left before the latest deadline a child of the
// run started under (the destroy context's, DefaultDeadline from the destroy's start, after the
// run's own 30 s), plus the waits After was asked for. A budget ending at that deadline admits a
// wait exactly when left - wait is at least the reserve.
type destroyClock struct {
	t     *testing.T
	mu    sync.Mutex
	end   time.Time
	left  time.Duration
	waits []time.Duration
}

func (c *destroyClock) started(ctx context.Context, _ []string) {
	if at, ok := ctx.Deadline(); ok {
		c.mu.Lock()
		if at.After(c.end) {
			c.end = at
		}
		c.mu.Unlock()
	}
}

func (c *destroyClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.nowLocked()
}

func (c *destroyClock) nowLocked() time.Time {
	if c.end.IsZero() {
		c.t.Errorf("the check's clock was read before any child ran under a deadline")
		return time.Now()
	}
	at := c.end.Add(-c.left)
	for _, w := range c.waits {
		at = at.Add(w)
	}
	return at
}

func (c *destroyClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.waits = append(c.waits, d)
	ch := make(chan time.Time, 1)
	ch <- c.nowLocked()
	return ch
}

// TestProbeListingRetryBudget (FR-001: in a probe, the budget is one built from the destroy
// context's deadline): a probe run through Probe.Start whose leftover listing answers 500 once,
// with a 1 min backoff and the check's clock left before the destroy context's deadline. admitted:
// 5 s more than the reserve and the wait are left: the listing is retried after the wait, its
// attempt is recorded as retried, and the probe passes. refused: 5 s less: no wait, one request, the
// attempt recorded with reason `budget`, and the probe does not pass. Exposes a probe core that
// passes no budget (admitted fails), or one ending elsewhere: at the run's deadline (admitted
// fails) or unbounded (refused fails).
func TestProbeListingRetryBudget(t *testing.T) {
	const wait = time.Minute
	for _, c := range []struct {
		name  string
		left  time.Duration
		admit bool
	}{
		{name: "admitted", left: minRetryReserve + wait + 5*time.Second, admit: true},
		{name: "refused", left: minRetryReserve + wait - 5*time.Second},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := tempPrivate(t)
			w := newRunWorld(t)
			p := w.probe(t, root, tofuStack{})
			sl := &scriptLister{base: p.Run.Leftovers.Lister, script: map[string][]listAnswer{pathNet1 + "#1": times(1, status(pathNet1, 500))}}
			clk := &destroyClock{t: t, left: c.left}
			p.Run.Leftovers.Lister = sl
			p.Run.Leftovers.Backoff = []time.Duration{wait, wait}
			p.Run.Leftovers.Clock = clk
			p.Run.childStarted = clk.started
			start := time.Now()
			err := p.Start(context.Background())
			clk.mu.Lock()
			end := clk.end
			clk.mu.Unlock()
			if end.Sub(start) < DefaultDeadline-time.Minute {
				t.Fatalf("latest child deadline %v after the start, want the destroy context's (about %v): the clock's anchor is the case's premise",
					end.Sub(start), DefaultDeadline)
			}
			var rec struct {
				Attempts []ListingAttempt `json:"attempts"`
			}
			readJSON(t, filepath.Join(w.runDir, "leftovers.json"), &rec)
			got := attemptsOf(Report{Attempts: rec.Attempts}, pathNet1)
			clk.mu.Lock()
			waits := slices.Clone(clk.waits)
			clk.mu.Unlock()
			if c.admit {
				if err != nil {
					t.Errorf("probe whose listing answered 500 once with %v left before the destroy deadline: %v\n%s", c.left, err, w.term)
				}
				if want := []string{pathNet1 + "#1", pathNet1 + "#1", pathNet1 + "#2"}; !slices.Equal(sl.asks(pathNet1), want) {
					t.Errorf("requests %v, want %v (page 1 retried once)", sl.asks(pathNet1), want)
				}
				if !slices.Equal(waits, []time.Duration{wait}) {
					t.Errorf("waits %v, want [%v]: the budget ending at the destroy context's deadline admits it", waits, wait)
				}
				want := []ListingAttempt{{Path: pathNet1, Attempt: 1, Status: 500, Class: "status", Retried: true}}
				if !slices.Equal(got, want) {
					t.Errorf("leftovers.json attempts %+v, want %+v", got, want)
				}
				return
			}
			if ExitCode(err) == 0 {
				t.Errorf("probe exited 0 (%v) although its listing failed and the wait did not fit the budget", err)
			}
			if want := []string{pathNet1 + "#1"}; !slices.Equal(sl.asks(pathNet1), want) {
				t.Errorf("requests %v, want %v: a wait leaving less than the reserve before the destroy deadline is refused", sl.asks(pathNet1), want)
			}
			if len(waits) != 0 {
				t.Errorf("waits %v, want none", waits)
			}
			want := []ListingAttempt{{Path: pathNet1, Attempt: 1, Status: 500, Class: "status", Retried: false, Reason: "budget"}}
			if !slices.Equal(got, want) {
				t.Errorf("leftovers.json attempts %+v, want %+v", got, want)
			}
		})
	}
}
