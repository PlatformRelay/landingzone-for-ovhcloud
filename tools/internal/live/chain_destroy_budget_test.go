package live

import (
	"slices"
	"syscall"
	"testing"
	"time"
)

// Spec 007 FR-014 (T037), the two cases T036 left: the destroys' retry budget is separate from the
// leftover check's, and a destroy-on-exit's wait is not cut short by the run's stop signal.

// Case 12: a destroy retry does not shrink the leftover check's state-read admissions. The runtime's
// destroy waits drWait with drReserve and a little more left after it; the check's state read of
// demo-state then needs one retryWait, which leaves more than the check's 30 s floor but less than
// the destroys' 5 min reserve: a budget of its own admits it, a budget shared with the destroys
// refuses it. Exposes the destroys' reserve kept in the check's budget.
func TestChainDestroyRetryBudgetSeparate(t *testing.T) {
	h := newChainHarness(t)
	drFail(h, drRuntime, drPlanFault(drAnswer500))
	h.setStack("demo-state", func(s *laneStack) {
		s.ReadFail = &laneReadFail{Call: "show-state", Answers: []string{
			"Error: reading the state: GET /v1/iam/policy: 503 Service Unavailable (" + chainAnswerMark + ")"}}
	})
	clk := &retryClock{spent: budgetLongRun - (drWait + drReserve + retryMargin)}
	r := h.chain(chainExec{deadline: budgetLongRun, clock: clk, destroyBackoff: []time.Duration{drWait, drWait},
		stateBackoff: []time.Duration{retryWait, retryWait}})
	if w := drWaits(clk); !slices.Equal(w, []time.Duration{drWait, retryWait}) {
		t.Errorf("waits %v on the run's clock, want [%v %v]: the destroy's, then the state read's in a budget of its own", w, drWait, retryWait)
	}
	drWantSteps(t, r.laneRun, drRuntime, drSteps{init: 2, plan: 2, show: 1, apply: 1}, "the destroy's wait is admitted")
	if n := retryReads(r, "demo-state"); n != 2 {
		t.Errorf("%d state reads of demo-state in the leftover check, want 2 (one retry)", n)
	}
	rep := chainReport(t, r.runDir)
	if rep.Outcome != "pass" || len(rep.Errors) != 0 {
		t.Errorf("report outcome %q, errors %v; want pass: the destroys' reserve is not the check's", rep.Outcome, rep.Errors)
	}
	laneExit(t, r.laneRun, 0)
}

// Case 13: a destroy-on-exit's wait is not cut short by a stop signal. SIGINT stops the runtime's
// forward apply; its destroy then fails once with run 6301's 500, waits its full (real) wait under
// the stopped run, plans again and ends ok. Exposes a wait that ends on the run's stop (a
// `live:destroy` wait ending on cancellation is TestChainDestroyRetryLiveDestroyCancel).
func TestChainDestroyRetryOnExitSignal(t *testing.T) {
	backoff := []time.Duration{500 * time.Millisecond, 500 * time.Millisecond}
	h := newChainHarness(t)
	h.setStack(drRuntime, func(s *laneStack) { s.Block = "apply+events" })
	drFail(h, drRuntime, drPlanFault(drAnswer500))
	r := h.chain(chainExec{signal: syscall.SIGINT, destroyBackoff: backoff})
	drWantSteps(t, r.laneRun, drRuntime, drSteps{init: 2, plan: 2, show: 1, apply: 1}, "the wait outlives the stop signal")
	drGaps(t, r.laneRun, drRuntime, backoff)
	drRetried(t, h.term.String(), drRuntime, 1)
	drOutcome(t, h.term.String(), drRuntime, "ok")
	if ExitCode(r.err) == 0 {
		t.Errorf("the interrupted run exited 0")
	}
}
