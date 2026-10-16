package live

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Project alias refusal in the bootstrap's setup, 007 T008 (FR-003, SC-004, V002; research R2):
// the bootstrap's Rest on an account whose two tenant refs resolve to one project stops with
// project-alias before its state phase starts tofu, naming both refs and no id; the publish and
// verify bindings (after state ran tofu) are too late. The sandbox's KD-1 account still binds.
// The aliased manifest is alias_lane_test.go's sandboxTwoTenants (the checkout's sandbox manifest
// plus tenant shop on SHOP_DEV), whose state phase runs tofu unless setup refuses; ids are built
// here (account.env gains LZ_PROJECT_ID_SHOP_DEV, the sandbox's one project).

// TestBootstrapProjectAlias: two tenant refs on one project end the bootstrap with project-alias
// in the state phase, with no tofu child, no publish or verify, and no project id in the error,
// the run's output or the process's stderr.
func TestBootstrapProjectAlias(t *testing.T) {
	h := newBSSHarness(t)
	h.seedCurrentSandbox()
	h.m = sandboxTwoTenants(t, h.checkout)
	p := h.accountPath("account.env")
	bsWrite(t, p, bsRead(t, p)+"LZ_PROJECT_ID_SHOP_DEV="+bsOldProject+"\n")
	rs, err := h.run(false)
	bsExit(t, err, RefusalExit)
	var ref *Refusal
	if !errors.As(err, &ref) || ref.Condition != CondProjectAlias {
		t.Errorf("err %v, want the %s refusal", err, CondProjectAlias)
	}
	for _, name := range []string{"DEMO_DEV", "SHOP_DEV"} {
		if ref != nil && !strings.Contains(ref.Detail, name) {
			t.Errorf("refusal %q does not name %s", ref.Detail, name)
		}
	}
	bssNoTofu(t, h)
	bsWantStatus(t, rs, PhaseState, StatusFail)
	for _, ph := range []string{PhasePublish, PhaseVerify} {
		if r, ok := bsResult(rs, ph); ok {
			t.Errorf("%s %s after the alias refusal", ph, r.Status)
		}
	}
	bootstrapAliasNoID(t, h, err)
}

// bootstrapAliasNoID: neither the error, the run's output nor the process's own stdout and stderr
// hold a project id.
func bootstrapAliasNoID(t *testing.T, h *bssHarness, err error) {
	t.Helper()
	out := map[string]string{"output": h.stdout.String()[h.outFrom:]}
	for _, name := range []string{"process-stdout.txt", "process-stderr.txt"} {
		raw, rerr := os.ReadFile(filepath.Join(h.base, name))
		if rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
			t.Fatal(rerr)
		}
		out[name] = string(raw)
	}
	if err != nil {
		out["error"] = err.Error()
	}
	for where, s := range out {
		if strings.Contains(s, bsOldProject) || aliasHexID.MatchString(s) {
			t.Errorf("the %s holds a project id: %q", where, s)
		}
	}
}

// TestBootstrapProjectAliasSandboxPin (pin): the sandbox's KD-1 account (STATE = DEMO_DEV under
// the flag) is not refused as an alias: the bootstrap passes and its state phase runs tofu.
func TestBootstrapProjectAliasSandboxPin(t *testing.T) {
	h := newBSSHarness(t)
	h.seedCurrentSandbox()
	_, err := h.run(false)
	var ref *Refusal
	if errors.As(err, &ref) && ref.Condition == CondProjectAlias {
		t.Fatalf("the sandbox's KD-1 binding refused: %v", err)
	}
	if err != nil {
		t.Errorf("bootstrap of the sandbox account: %v, want a pass", err)
	}
	if len(h.runCalls()) == 0 {
		t.Error("the state phase started no tofu")
	}
}

// TestBootstrapBindingMalformed (T009, review round 1): an account.env whose LZ_PROJECT_ID_<REF>
// is not a project id fails the state phase in its setup (exit 1, not a refusal), with no tofu,
// no publish or verify, and a detail naming the variable, not its value. Before T009 the binding
// was read only at publish, after state had run tofu.
func TestBootstrapBindingMalformed(t *testing.T) {
	const bad = "not-a-project-id"
	h := newBSSHarness(t)
	h.seedCurrentSandbox()
	p := h.accountPath("account.env")
	bsWrite(t, p, bsRead(t, p)+"LZ_PROJECT_ID_SPARE="+bad+"\n")
	rs, err := h.run(false)
	bsExit(t, err, 1)
	bssNoTofu(t, h)
	bsWantStatus(t, rs, PhaseState, StatusFail)
	for _, ph := range []string{PhasePublish, PhaseVerify} {
		if r, ok := bsResult(rs, ph); ok {
			t.Errorf("%s %s after the malformed binding", ph, r.Status)
		}
	}
	if err == nil || !strings.Contains(err.Error(), "LZ_PROJECT_ID_SPARE") || strings.Contains(err.Error(), bad) {
		t.Errorf("err %v, want one naming LZ_PROJECT_ID_SPARE without its value", err)
	}
}
