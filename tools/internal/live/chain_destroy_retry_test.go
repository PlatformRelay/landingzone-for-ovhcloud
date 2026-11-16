package live

import (
	"context"
	"errors"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Spec 007 FR-014 (T036): the chain's destroy of an ephemeral instance, on exit and through
// `live:destroy -- <instance>`, retries its planning steps (init, `plan -destroy`, `show -json`)
// after a transient failure, as the classifier (transient) judges the failed step's captured
// stderr. apply is never retried; a full capture, an empty one, a 4xx and a state-lock error are
// never transient. At most two retries, each printing one `LZ-LIVE retry destroy` line; the
// destroys of one run share a retry budget ending at the destroy context's deadline (on exit) or
// the run's (`live:destroy`), which admits a wait only if, after it, the failed attempt's duration
// plus 5 minutes plus the baseline listing's duration is left. The fake tofu's destroy faults
// (laneDestroyFault) fail chosen destroy steps with chosen stderr. One function per case (the
// complexity gate scores tests too).

const (
	drRuntime = "demo-dev-gra11-runtime"
	drNetwork = "demo-dev-gra11-network"
	// drAnswer500 is run 20261116T120049Z-6301's stderr line of the runtime's `plan -destroy`
	// (spec.md FR-014, evidence/T032.md).
	drAnswer500 = "OVHcloud API error (status code 500): Server::InternalServerError\n"
	drAnswer503 = "OVHcloud API error (status code 503): Server::ServiceUnavailable\n"
	// drMark is in every fake answer: a retry line carrying it carries tofu's output.
	drMark = "Server::"
	// drWait is every wait of the clocked cases; drReserve FR-014's fixed 5 minutes.
	drWait    = 4 * time.Minute
	drReserve = 5 * time.Minute
)

// drSteps counts the destroy steps a stack ran.
type drSteps struct{ init, plan, show, apply int }

func drFail(h *laneHarness, id string, faults ...laneDestroyFault) {
	h.setStack(id, func(s *laneStack) { s.DestroyFail = faults })
}

func drPlanFault(answers ...string) laneDestroyFault {
	return laneDestroyFault{Call: "plan", Answers: answers}
}

// drPhase is the stack's tofu calls after its last forward apply: its destroy's.
func drPhase(r laneRun, id string) []laneCall {
	var out []laneCall
	for _, c := range r.tofu() {
		if c.Stack != id {
			continue
		}
		if c.Cmd == "apply" && !c.Destroy {
			out = nil
			continue
		}
		out = append(out, c)
	}
	return out
}

func drCount(r laneRun, id string) drSteps {
	var s drSteps
	for _, c := range drPhase(r, id) {
		switch c.Cmd {
		case "init":
			s.init++
		case "plan":
			s.plan++
		case "show-json":
			s.show++
		case "apply":
			s.apply++
		}
	}
	return s
}

func drWantSteps(t *testing.T, r laneRun, id string, want drSteps, why string) {
	t.Helper()
	if got := drCount(r, id); got != want {
		t.Errorf("destroy steps of %s %+v, want %+v (%s; events %v)", id, got, want, why, laneEvents(drPhase(r, id)))
	}
}

// drLine is FR-014's retry line, with room for a redacted error after it.
var drLine = regexp.MustCompile(`^LZ-LIVE retry destroy (\S+) in (\S+): transient failure \(attempt ([0-9]+) of ([0-9]+)\)(: .*)?$`)

// drRetried checks the run printed n retry lines, each naming id, attempts 2.. of 3, nothing of
// tofu's output.
func drRetried(t *testing.T, term, id string, n int) {
	t.Helper()
	var lines []string
	for _, l := range strings.Split(term, "\n") {
		if strings.HasPrefix(l, "LZ-LIVE retry destroy") {
			lines = append(lines, l)
		}
	}
	if len(lines) != n {
		t.Errorf("%d LZ-LIVE retry destroy lines, want %d:\n%s", len(lines), n, strings.Join(lines, "\n"))
	}
	for i, l := range lines {
		m := drLine.FindStringSubmatch(l)
		if m == nil || m[1] != id || m[3] != strconv.Itoa(i+2) || m[4] != "3" || strings.Contains(l, drMark) {
			t.Errorf("retry line %q: want `LZ-LIVE retry destroy %s in <wait>: transient failure (attempt %d of 3)`, nothing of tofu's output", l, id, i+2)
		}
	}
}

// drOutcome checks the run printed `LZ-LIVE destroy <id> <want>`.
func drOutcome(t *testing.T, term, id, want string) {
	t.Helper()
	if !slices.Contains(strings.Split(term, "\n"), "LZ-LIVE destroy "+id+" "+want) {
		t.Errorf("no `LZ-LIVE destroy %s %s` line:\n%s", id, want, term)
	}
}

// drJudgedLast checks that the one `show -json` of the destroy, and its apply when apply ran,
// were of the last plan written: the guard judged the replacement plan, and exactly it was applied.
func drJudgedLast(t *testing.T, r laneRun, id string) {
	t.Helper()
	last := ""
	for _, c := range drPhase(r, id) {
		switch {
		case c.Cmd == "plan" && c.Exit == 0:
			last = c.Nonce
		case (c.Cmd == "show-json" || c.Cmd == "apply") && c.Nonce != last:
			t.Errorf("%s of %s on plan %q, want the last plan written (%q)", c.Cmd, id, c.Nonce, last)
		}
	}
	if last == "" {
		t.Errorf("no plan -destroy of %s succeeded (events %v)", id, laneEvents(drPhase(r, id)))
	}
}

// drGaps checks each retry's init started at least its wait after the failed attempt.
func drGaps(t *testing.T, r laneRun, id string, backoff []time.Duration) {
	t.Helper()
	var failed, inits []time.Time
	for _, c := range drPhase(r, id) {
		at, err := time.Parse(time.RFC3339Nano, c.At)
		if err != nil {
			t.Fatalf("call %s of %s without its time: %v", c.Cmd, id, err)
		}
		if c.Exit != 0 {
			failed = append(failed, at)
		}
		if c.Cmd == "init" {
			inits = append(inits, at)
		}
	}
	for i := 0; i+1 < len(inits) && i < len(failed) && i < len(backoff); i++ {
		if gap := inits[i+1].Sub(failed[i]); gap < backoff[i] {
			t.Errorf("retry %d of %s came %v after the failed attempt, want at least %v", i+1, id, gap, backoff[i])
		}
	}
}

// drKept checks a failed destroy kept the stack's state and record (for --cleanup).
func drKept(t *testing.T, h *laneHarness, id string) {
	t.Helper()
	if s, ok := h.stateOf(id); !ok || s.Empty {
		t.Errorf("state of %s emptied although its destroy failed", id)
	}
	if len(h.record(id)) == 0 {
		t.Errorf("record of %s removed although its destroy failed", id)
	}
}

// drNotRetried is the common judgement of a failure that is not transient: one attempt that
// failed at plan, no apply, no retry line, the destroy and the run failed.
func drNotRetried(t *testing.T, h *laneHarness, r chainRun, why string) {
	t.Helper()
	drWantSteps(t, r.laneRun, drRuntime, drSteps{init: 1, plan: 1}, why)
	drRetried(t, h.term.String(), drRuntime, 0)
	drOutcome(t, h.term.String(), drRuntime, "fail")
	drKept(t, h, drRuntime)
	laneExit(t, r.laneRun, 1)
}

// drClockedChain runs a chain with a long deadline (budgetLongRun) on clk, the destroy retries
// waiting drWait each. clk is set so that left of the destroy budget remains when the destroys
// start; slow, when set, is how long the baseline listing takes on clk.
func drClockedChain(t *testing.T, h *laneHarness, left, slow time.Duration) (chainRun, *retryClock) {
	t.Helper()
	clk := &retryClock{spent: budgetLongRun - left - slow}
	ex := chainExec{deadline: budgetLongRun, clock: clk, destroyBackoff: []time.Duration{drWait, drWait}}
	if slow > 0 {
		ex.wrapLister = func(l Lister) Lister { return &drSlowFirst{Lister: l, clk: clk, d: slow} }
	}
	return h.chain(ex), clk
}

// drSlowFirst is a Lister whose first request (the baseline's) takes d on clk.
type drSlowFirst struct {
	Lister
	clk  *retryClock
	d    time.Duration
	once sync.Once
}

func (s *drSlowFirst) Get(ctx context.Context, path, cursor string) ([]byte, string, error) {
	s.once.Do(func() {
		s.clk.mu.Lock()
		s.clk.spent += s.d
		s.clk.mu.Unlock()
	})
	return s.Lister.Get(ctx, path, cursor)
}

func drWaits(clk *retryClock) []time.Duration {
	clk.mu.Lock()
	defer clk.mu.Unlock()
	return slices.Clone(clk.waited)
}

// drRefused checks a destroy whose wait the budget refused: one attempt, no wait, the destroy
// failed naming the refused wait, and the leftover check still ran after the destroys.
func drRefused(t *testing.T, h *laneHarness, r chainRun, id string) {
	t.Helper()
	drWantSteps(t, r.laneRun, id, drSteps{init: 1, plan: 1}, "the wait is refused: no second attempt")
	drOutcome(t, h.term.String(), id, "fail")
	if r.err == nil || !strings.Contains(r.err.Error(), id) || !strings.Contains(r.err.Error(), "wait") {
		t.Errorf("err %v, want it to name %s and the refused wait", r.err, id)
	}
	lastDestroy := r.index(true, func(c laneCall) bool { return c.Destroy })
	if lastList := r.index(true, func(c laneCall) bool { return c.Cmd == "list" }); lastList < lastDestroy {
		t.Errorf("no leftover listing after the last destroy (events %v)", laneEvents(r.calls))
	}
	laneExit(t, r.laneRun, 1)
}

// Case 1: run 6301's `plan -destroy` 500 once: the destroy's planning steps run again, the guard
// judges the new plan, it is applied once, the destroy ends ok and the run passes. Exposes today's
// immediate fail.
func TestChainDestroyRetryTransientPlan(t *testing.T) {
	h := newChainHarness(t)
	drFail(h, drRuntime, drPlanFault(drAnswer500))
	r := h.chain(chainExec{})
	drWantSteps(t, r.laneRun, drRuntime, drSteps{init: 2, plan: 2, show: 1, apply: 1}, "one retry of init and plan -destroy")
	drJudgedLast(t, r.laneRun, drRuntime)
	drGaps(t, r.laneRun, drRuntime, testBackoff)
	drRetried(t, h.term.String(), drRuntime, 1)
	drOutcome(t, h.term.String(), drRuntime, "ok")
	chainDestroyedAfter(t, h, r, chainDestroyOrder, "pass")
	laneExit(t, r.laneRun, 0)
}

// Case 2: a 503 from apply is not retried: apply ran once, the destroy fails and its state and
// record stay for --cleanup. Exposes an apply retried (holds today).
func TestChainDestroyRetryApplyNotRetried(t *testing.T) {
	if !transient(drAnswer503) {
		t.Fatalf("premise: the classifier judges %q transient", drAnswer503)
	}
	h := newChainHarness(t)
	drFail(h, drRuntime, laneDestroyFault{Call: "apply", Answers: []string{drAnswer503, drAnswer503}})
	r := h.chain(chainExec{})
	drWantSteps(t, r.laneRun, drRuntime, drSteps{init: 1, plan: 1, show: 1, apply: 1}, "apply is never retried")
	drRetried(t, h.term.String(), drRuntime, 0)
	drOutcome(t, h.term.String(), drRuntime, "fail")
	drKept(t, h, drRuntime)
	laneExit(t, r.laneRun, 1)
}

// Case 3: a 403, a 404 beside a 500, and a state-lock acquire or release error carrying a 500
// (which the classifier alone judges transient) are not retried, and the lock is never forced.
// Exposes a retried refusal or lock (holds today).
func TestChainDestroyRetryNotTransient(t *testing.T) {
	for _, c := range []struct {
		name, answer string
		lock         bool // a lock error: the classifier alone would retry it
	}{
		{name: "403", answer: "OVHcloud API error (status code 403): Client::Forbidden: \"This call has not been granted\"\n"},
		{name: "404-and-500", answer: "OVHcloud API error (status code 404): Client::NotFound\n" + drAnswer500},
		{name: "lock-acquire-500", lock: true, answer: "Error acquiring the state lock\n\nError message: operation error S3: PutObject, " +
			"https response error StatusCode: 500, RequestID: 0, HostID: 0, api error InternalError: We encountered an internal error. Please try again.\n" + drMark + "\n"},
		{name: "lock-release-500", lock: true, answer: "Error releasing the state lock\n\nError message: operation error S3: DeleteObject, " +
			"https response error StatusCode: 500, RequestID: 0, HostID: 0, api error InternalError: We encountered an internal error. Please try again.\n" + drMark + "\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if transient(c.answer) != c.lock {
				t.Fatalf("premise: transient(%q) = %t, want %t", c.answer, !c.lock, c.lock)
			}
			h := newChainHarness(t)
			drFail(h, drRuntime, drPlanFault(c.answer, c.answer, c.answer))
			r := h.chain(chainExec{})
			drNotRetried(t, h, r, "a 4xx or a lock error is never transient")
			for _, x := range drPhase(r.laneRun, drRuntime) {
				if slices.ContainsFunc(x.Args, func(a string) bool { return strings.HasPrefix(a, "-lock=") || a == "force-unlock" }) {
					t.Errorf("tofu %v: the lock is never disabled or forced", x.Args)
				}
			}
		})
	}
}

// Case 4: a 500 on every attempt: three attempts of the planning steps, no apply, and the error
// names the count. Exposes an unbounded retry or one that drops the count.
func TestChainDestroyRetryEveryAttempt(t *testing.T) {
	h := newChainHarness(t)
	drFail(h, drRuntime, drPlanFault(drAnswer500, drAnswer500, drAnswer500, drAnswer500))
	r := h.chain(chainExec{})
	drWantSteps(t, r.laneRun, drRuntime, drSteps{init: 3, plan: 3}, "three attempts, then fail")
	drGaps(t, r.laneRun, drRuntime, testBackoff)
	drRetried(t, h.term.String(), drRuntime, 2)
	drOutcome(t, h.term.String(), drRuntime, "fail")
	drKept(t, h, drRuntime)
	if r.err == nil || !strings.Contains(r.err.Error(), "attempt 3 of 3") {
		t.Errorf("err %v, want it to name the count (attempt 3 of 3)", r.err)
	}
	laneExit(t, r.laneRun, 1)
}

// Case 5: a wait is started only if, after it, the failed attempt's duration plus 5 minutes plus
// the baseline listing's duration is still left of the destroy budget. refused: less is left; the
// wait is not started, the destroy fails naming it and the leftover check runs. admitted: a
// little more is left; the retry runs. slow-baseline: as admitted, but the baseline listing took
// 2 minutes, which the reserve keeps too. Exposes a retry eating the check's time.
func TestChainDestroyRetryReserve(t *testing.T) {
	const margin, slow = retryMargin, 2 * time.Minute
	t.Run("refused", func(t *testing.T) {
		h := newChainHarness(t)
		drFail(h, drRuntime, drPlanFault(drAnswer500))
		r, clk := drClockedChain(t, h, drWait+drReserve-margin, 0)
		if w := drWaits(clk); len(w) != 0 {
			t.Errorf("waits %v on the run's clock, want none: the wait leaves less than the reserve", w)
		}
		drRefused(t, h, r, drRuntime)
	})
	t.Run("admitted", func(t *testing.T) {
		h := newChainHarness(t)
		drFail(h, drRuntime, drPlanFault(drAnswer500))
		r, clk := drClockedChain(t, h, drWait+drReserve+margin, 0)
		if w := drWaits(clk); !slices.Equal(w, []time.Duration{drWait}) {
			t.Errorf("waits %v on the run's clock, want [%v]: the reserve is left after it", w, drWait)
		}
		drWantSteps(t, r.laneRun, drRuntime, drSteps{init: 2, plan: 2, show: 1, apply: 1}, "the retry fits the budget")
		drOutcome(t, h.term.String(), drRuntime, "ok")
	})
	t.Run("slow-baseline", func(t *testing.T) {
		h := newChainHarness(t)
		drFail(h, drRuntime, drPlanFault(drAnswer500))
		r, clk := drClockedChain(t, h, drWait+drReserve+margin, slow)
		if w := drWaits(clk); len(w) != 0 {
			t.Errorf("waits %v on the run's clock, want none: the baseline listing's %v is part of the reserve", w, slow)
		}
		drRefused(t, h, r, drRuntime)
	})
}

// Case 5, the failed attempt's duration (review r1): the reserve keeps it too. The runtime's
// failed attempt takes 2 minutes on the run's clock; after the wait, 5 minutes and a little more
// are left, but not 5 minutes plus that attempt: the wait is refused. Exposes an admission that
// leaves the attempt's duration out.
func TestChainDestroyRetryReserveSlowAttempt(t *testing.T) {
	const slow = 2 * time.Minute
	h := newChainHarness(t)
	drFail(h, drRuntime, drPlanFault(drAnswer500))
	clk := &retryClock{spent: budgetLongRun - (drWait + drReserve + slow + retryMargin)}
	var once sync.Once
	slowPlan := func(args []string) {
		if slices.Contains(args, "plan") && slices.Contains(args, "-destroy") {
			once.Do(func() {
				clk.mu.Lock()
				clk.spent += slow
				clk.mu.Unlock()
			})
		}
	}
	r := h.chain(chainExec{deadline: budgetLongRun, clock: clk, destroyBackoff: []time.Duration{drWait, drWait},
		children: &childDeadlines{onStart: slowPlan}})
	if w := drWaits(clk); len(w) != 0 {
		t.Errorf("waits %v on the run's clock, want none: the failed attempt's %v is part of the reserve", w, slow)
	}
	drRefused(t, h, r, drRuntime)
}

// Case 1, `show -json` (review r1): FR-014 retries the third planning step too. A transient
// failure of `show -json` plans again; the guard judges the replacement plan, which alone is
// applied. Exposes an init-and-plan-only retry.
func TestChainDestroyRetryTransientShow(t *testing.T) {
	h := newChainHarness(t)
	drFail(h, drRuntime, laneDestroyFault{Call: "show-json", Answers: []string{drAnswer500}})
	r := h.chain(chainExec{})
	drWantSteps(t, r.laneRun, drRuntime, drSteps{init: 2, plan: 2, show: 2, apply: 1}, "one retry after show -json's 500")
	var shows []string
	for _, c := range drPhase(r.laneRun, drRuntime) {
		if c.Cmd == "show-json" && c.Exit == 0 {
			shows = append(shows, c.Nonce)
		}
	}
	if len(shows) != 1 {
		t.Errorf("%d successful show -json calls of %s, want 1 (of the replacement plan)", len(shows), drRuntime)
	}
	drJudgedLastApplied(t, r.laneRun, drRuntime)
	drRetried(t, h.term.String(), drRuntime, 1)
	drOutcome(t, h.term.String(), drRuntime, "ok")
	laneExit(t, r.laneRun, 0)
}

// drJudgedLastApplied checks the apply ran on the last plan written and a successful `show -json`
// of that plan came before it.
func drJudgedLastApplied(t *testing.T, r laneRun, id string) {
	t.Helper()
	last, judged := "", false
	for _, c := range drPhase(r, id) {
		switch {
		case c.Cmd == "plan" && c.Exit == 0:
			last, judged = c.Nonce, false
		case c.Cmd == "show-json" && c.Exit == 0 && c.Nonce == last:
			judged = true
		case c.Cmd == "apply" && (c.Nonce != last || !judged):
			t.Errorf("apply of %s on plan %q (last plan %q, judged %t): want exactly the last plan, judged", id, c.Nonce, last, judged)
		}
	}
}

// drHoldClock is a waitClock whose waits never end on their own within the test: After marks the
// run blocked (exec then cancels the run) and fires only after 15 s.
type drHoldClock struct {
	log   string
	mu    sync.Mutex
	waits []time.Duration
}

func (c *drHoldClock) Now() time.Time { return time.Now() }

func (c *drHoldClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	c.waits = append(c.waits, d)
	c.mu.Unlock()
	_ = os.WriteFile(c.log+".blocked", []byte(drRuntime), 0o600)
	return time.After(15 * time.Second)
}

// Case 6: `live:destroy -- <instance>` retries as the chain's destroy does (here), with its budget
// at the run's deadline (…Deadline), and a cancelled run ends its wait (…Cancel). Exposes a retry
// only on destroy-on-exit, a budget ending elsewhere, or a wait that ignores cancellation.
func TestChainDestroyRetryLiveDestroy(t *testing.T) {
	t.Run("retried", func(t *testing.T) {
		h := newChainHarness(t)
		h.steady()
		h.noTimeout = true // the run's own deadline bounds it, not exec's 2 minutes
		drFail(h, drRuntime, drPlanFault(drAnswer500))
		r := h.run(VerbDestroy, drRuntime)
		drWantSteps(t, r, drRuntime, drSteps{init: 2, plan: 2, show: 1, apply: 1}, "one retry of init and plan -destroy")
		drJudgedLast(t, r, drRuntime)
		drRetried(t, h.term.String(), drRuntime, 1)
		drOutcome(t, h.term.String(), drRuntime, "ok")
		if s, ok := h.stateOf(drRuntime); !ok || !s.Empty {
			t.Errorf("state of %s not destroyed", drRuntime)
		}
		if len(h.record(drRuntime)) != 0 {
			t.Errorf("record of %s kept after its destroy", drRuntime)
		}
		laneExit(t, r, 0)
	})
}

// Case 6, budget: `live:destroy`'s budget ends at the run's deadline. The run's deadline is
// 10 min and each wait 1 min: a wait is admitted while 6 min (the wait
// and the 5 min reserve) are left before the run's deadline. A budget ending at the
// destroy-on-exit's 45 min, or none, admits both.
func TestChainDestroyRetryLiveDestroyDeadline(t *testing.T) {
	const deadline, wait = 10 * time.Minute, time.Minute
	for _, c := range []struct {
		name  string
		left  time.Duration
		admit bool
	}{
		{name: "deadline-admits", left: wait + drReserve + retryMargin, admit: true},
		{name: "deadline-refuses", left: wait + drReserve - retryMargin},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newChainHarness(t)
			h.steady()
			h.noTimeout = true
			drFail(h, drRuntime, drPlanFault(drAnswer500))
			clk := &retryClock{spent: deadline - c.left}
			h.deadline, h.destroyClock, h.destroyBackoff = deadline, clk, []time.Duration{wait, wait}
			r := h.run(VerbDestroy, drRuntime)
			if !c.admit {
				drWantSteps(t, r, drRuntime, drSteps{init: 1, plan: 1}, "the wait is refused at the run's deadline")
				drOutcome(t, h.term.String(), drRuntime, "fail")
				laneExit(t, r, 1)
				return
			}
			if w := drWaits(clk); !slices.Equal(w, []time.Duration{wait}) {
				t.Errorf("waits %v on the run's clock, want [%v]", w, wait)
			}
			drWantSteps(t, r, drRuntime, drSteps{init: 2, plan: 2, show: 1, apply: 1}, "the retry fits before the run's deadline")
			laneExit(t, r, 0)
		})
	}
}

// Case 6, cancellation: a `live:destroy` wait ends when its run is cancelled.
func TestChainDestroyRetryLiveDestroyCancel(t *testing.T) {
	t.Run("cancelled", func(t *testing.T) {
		h := newChainHarness(t)
		h.steady()
		h.noTimeout = true // the run's own deadline bounds it, not exec's 2 minutes
		drFail(h, drRuntime, drPlanFault(drAnswer500))
		clk := &drHoldClock{log: h.world.Log}
		h.destroyClock, h.destroyBackoff = clk, []time.Duration{time.Minute, time.Minute}
		defer os.Remove(h.world.Log + ".blocked")
		start := time.Now()
		r := h.run(VerbDestroy, drRuntime)
		took := time.Since(start)
		clk.mu.Lock()
		waits := slices.Clone(clk.waits)
		clk.mu.Unlock()
		if len(waits) != 1 {
			t.Errorf("waits %v, want one: the retry's wait is the case's premise", waits)
		}
		if took > 10*time.Second {
			t.Errorf("the run took %v after its cancellation: the wait ignored it", took)
		}
		drWantSteps(t, r, drRuntime, drSteps{init: 1, plan: 1}, "cancelled during the wait: no second attempt")
		if !errors.Is(r.err, context.Canceled) {
			t.Errorf("err %v, want the cancellation", r.err)
		}
		if ExitCode(r.err) == 0 {
			t.Errorf("the cancelled run exited 0")
		}
	})
}

// Case 7: a consumer stack (the runtime reads the network's outputs) retried as in case 1 adapts
// its inputs once: no artefact read between its attempts, the same var files on both plans, and
// the destroy ends ok. Exposes the inputs directory refused on attempt 2.
func TestChainDestroyRetryConsumerInputs(t *testing.T) {
	h := newChainHarness(t)
	drFail(h, drRuntime, drPlanFault(drAnswer500))
	r := h.chain(chainExec{})
	var plans [][]string
	first, last := -1, -1
	for i, c := range r.calls {
		if c.Stack == drRuntime && c.Cmd == "plan" && c.Destroy {
			plans = append(plans, c.VarFiles)
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if len(plans) != 2 || len(plans[0]) == 0 || !slices.Equal(plans[0], plans[1]) {
		t.Errorf("plan -destroy var files %v, want two plans with the same, non-empty files (inputs written once)", plans)
	}
	for i := first + 1; first >= 0 && i < last; i++ {
		if r.calls[i].Cmd == "get" {
			t.Errorf("artefact read %s between the runtime's attempts: the inputs are adapted once, before the first", r.calls[i].Stack)
		}
	}
	drOutcome(t, h.term.String(), drRuntime, "ok")
	laneExit(t, r.laneRun, 0)
}

// Case 8: a capture that reached its size limit (carrying a 500, which the classifier alone
// would retry) and a failure with no captured output are not retried. Exposes a truncated or
// empty capture judged transient (holds today).
func TestChainDestroyRetryCapture(t *testing.T) {
	full := drAnswer500 + strings.Repeat("x", errLimit) + "\n"
	if !transient(full) {
		t.Fatal("premise: the classifier judges the full capture transient")
	}
	for _, c := range []struct{ name, answer string }{{"capture-limit", full}, {"empty", ""}} {
		t.Run(c.name, func(t *testing.T) {
			h := newChainHarness(t)
			drFail(h, drRuntime, drPlanFault(c.answer, c.answer, c.answer))
			r := h.chain(chainExec{})
			drNotRetried(t, h, r, "a full or empty capture is never transient")
		})
	}
}

// Case 9: a transient failure of init is retried like case 1. Exposes a plan-only retry.
func TestChainDestroyRetryTransientInit(t *testing.T) {
	h := newChainHarness(t)
	drFail(h, drRuntime, laneDestroyFault{Call: "init", Answers: []string{drAnswer500}})
	r := h.chain(chainExec{})
	drWantSteps(t, r.laneRun, drRuntime, drSteps{init: 2, plan: 1, show: 1, apply: 1}, "one retry after init's 500")
	drRetried(t, h.term.String(), drRuntime, 1)
	drOutcome(t, h.term.String(), drRuntime, "ok")
	laneExit(t, r.laneRun, 0)
}

// Case 10: the replacement plan of a retry is judged by the guard; one it refuses fails the
// destroy without apply. The refusal-without-apply part holds today (the guard runs on every plan
// shown); the judged replacement does not (no retry today).
func TestChainDestroyRetryGuardRefusesReplacement(t *testing.T) {
	h := newChainHarness(t)
	drFail(h, drRuntime, drPlanFault(drAnswer500))
	h.setStack(drRuntime, func(s *laneStack) { s.DestroyPlan = h.destroyDoc(h.row(drRuntime), true) })
	r := h.chain(chainExec{})
	drWantSteps(t, r.laneRun, drRuntime, drSteps{init: 2, plan: 2, show: 1}, "the replacement plan judged, refused, not applied")
	drJudgedLast(t, r.laneRun, drRuntime)
	drOutcome(t, h.term.String(), drRuntime, "fail")
	drKept(t, h, drRuntime)
	var ref *Refusal
	if !errors.As(r.err, &ref) || ref.Condition != CondRetained {
		t.Errorf("err %v, want the guard's %s refusal", r.err, CondRetained)
	}
	if ExitCode(r.err) == 0 {
		t.Errorf("the run exited 0 with a refused destroy plan")
	}
}

// Case 11: the destroys of one run share one budget: the first destroy's admitted wait is spent
// time for the second's admission. shared: room for one wait and the reserve after it, not two;
// the runtime (destroyed first) waits, the network's wait is refused. room-for-both: room for two;
// both wait. Exposes a budget per destroy.
func TestChainDestroyRetrySharedBudget(t *testing.T) {
	for _, c := range []struct {
		name  string
		left  time.Duration
		waits []time.Duration
	}{
		{name: "shared", left: 2*drWait + drReserve - retryMargin, waits: []time.Duration{drWait}},
		{name: "room-for-both", left: 2*drWait + drReserve + retryMargin, waits: []time.Duration{drWait, drWait}},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newChainHarness(t)
			drFail(h, drRuntime, drPlanFault(drAnswer500))
			drFail(h, drNetwork, drPlanFault(drAnswer500))
			r, clk := drClockedChain(t, h, c.left, 0)
			if w := drWaits(clk); !slices.Equal(w, c.waits) {
				t.Errorf("waits %v on the run's clock, want %v", w, c.waits)
			}
			drWantSteps(t, r.laneRun, drRuntime, drSteps{init: 2, plan: 2, show: 1, apply: 1}, "the first destroy's wait is admitted")
			drOutcome(t, h.term.String(), drRuntime, "ok")
			if len(c.waits) == 2 {
				drWantSteps(t, r.laneRun, drNetwork, drSteps{init: 2, plan: 2, show: 1, apply: 1}, "room for the second wait")
				laneExit(t, r.laneRun, 0)
				return
			}
			drRefused(t, h, r, drNetwork)
		})
	}
}
