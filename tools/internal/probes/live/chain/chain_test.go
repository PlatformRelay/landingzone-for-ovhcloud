//go:build live

// L7 chain assertions (spec 005 V010; FR-004, FR-008, FR-010, FR-011, SC-003; research R17,
// R20; guard G15; tests T048, implementation T062). The `live` tag keeps them out of offline
// discovery and the default `go test ./...`:
//
//	go -C tools test -tags live ./internal/probes/live/chain -run TestChainObservations -count=1
//
// Offline, they judge the recorded fake observation sets in tests/live/chain/<case>/
// (observations.json in the collector's format, expected.json the outcome per assertion; written
// by hand, since the format is the collector's own, not a tool's). With LZ_L7_OBSERVATIONS set to
// a live run's <run-dir>/observations.json (T049), they also judge the real observations: every
// assertion must pass or report a known deviation.

package chain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/live"
)

// l7 is the L7 assertion set (tasks.md T048): versioned state buckets, encrypted state object,
// lock contention refused, mandatory tags on bucket and project URN, tenant IAM write denied,
// tenant S3 read of the account bucket or another tenant's bucket denied, outputs.json
// schema-valid, the platform and tenant deployer credentials binding (P26), the KD-1 canary.
var l7 = []string{
	"bucket-tags", "deployer-binding", "kd1-canary", "outputs-schema", "project-tags", "state-bucket-versioned",
	"state-lock-contention", "state-object-encrypted", "tenant-iam-write", "tenant-s3-read",
}

const fixtures = "../../../../../tests/live/chain"

type expected struct {
	Outcomes        map[string]string `json:"outcomes"`
	KnownDeviations []string          `json:"known_deviations"`
	Notes           map[string]string `json:"notes"` // assertion -> text its detail must carry
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

// byAssertion indexes the assessments; every L7 assertion is assessed exactly once.
func byAssertion(t *testing.T, as []live.Assessment) map[string]live.Assessment {
	t.Helper()
	out := map[string]live.Assessment{}
	for _, a := range as {
		if _, dup := out[a.Assertion]; dup {
			t.Errorf("%s assessed twice", a.Assertion)
		}
		out[a.Assertion] = a
	}
	for _, name := range l7 {
		if _, ok := out[name]; !ok {
			t.Errorf("%s not assessed: an observation set missing an assertion is a fail, never silence", name)
		}
	}
	return out
}

func judge(t *testing.T, set live.ObservationSet, want expected) {
	t.Helper()
	as := live.Assess(set)
	got := byAssertion(t, as)
	for name, outcome := range want.Outcomes {
		a := got[name]
		if a.Outcome != outcome {
			t.Errorf("%s: %s (%s), want %s", name, a.Outcome, a.Detail, outcome)
		}
		if outcome == "known-deviation" && a.Deviation != "KD-1" {
			t.Errorf("%s: deviation %q, want KD-1 (spec *Known deviations*)", name, a.Deviation)
		}
		if outcome != "known-deviation" && a.Deviation != "" {
			t.Errorf("%s: deviation %q on outcome %s", name, a.Deviation, a.Outcome)
		}
	}
	for name, note := range want.Notes {
		if !strings.Contains(got[name].Detail, note) {
			t.Errorf("%s: detail %q, want a note naming %s", name, got[name].Detail, note)
		}
	}
	if kd := live.KnownDeviations(as); !slices.Equal(kd, want.KnownDeviations) && !(len(kd) == 0 && len(want.KnownDeviations) == 0) {
		t.Errorf("known deviations %v, want %v", kd, want.KnownDeviations)
	}
}

func TestChainObservations(t *testing.T) {
	cases, err := os.ReadDir(fixtures)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, c := range cases {
		if !c.IsDir() {
			continue
		}
		n++
		t.Run(c.Name(), func(t *testing.T) {
			var set live.ObservationSet
			var want expected
			readJSON(t, filepath.Join(fixtures, c.Name(), "observations.json"), &set)
			readJSON(t, filepath.Join(fixtures, c.Name(), "expected.json"), &want)
			judge(t, set, want)
			// The order of observations does not change an outcome (review r1: a failing subject
			// first or last is judged alike).
			set.Observations = slices.Clone(set.Observations)
			slices.Reverse(set.Observations)
			judge(t, set, want)
		})
	}
	if n < 16 {
		t.Errorf("%d observation cases under %s, want the 16 recorded ones", n, fixtures)
	}

	// An observation set missing any assertion fails that assertion, and only that one.
	var base live.ObservationSet
	readJSON(t, filepath.Join(fixtures, "all-pass", "observations.json"), &base)
	for _, name := range l7 {
		t.Run("missing-"+name, func(t *testing.T) {
			set := base
			set.Observations = slices.DeleteFunc(slices.Clone(base.Observations), func(o live.Observation) bool { return o.Assertion == name })
			want := expected{Outcomes: map[string]string{}}
			for _, a := range l7 {
				want.Outcomes[a] = "pass"
			}
			want.Outcomes[name] = "fail"
			judge(t, set, want)
		})
	}

	// Each required subject (review r2): deployer-binding needs platform and tenant (P26), the
	// canary both KD-1 calls; one missing fails the assertion.
	for _, req := range [][2]string{{"deployer-binding", "platform"}, {"deployer-binding", "tenant"}, {"kd1-canary", "bulkDeleteObjects"}, {"kd1-canary", "DELETE"}} {
		t.Run("missing-subject-"+req[0]+"-"+req[1], func(t *testing.T) {
			set := base
			set.Observations = slices.DeleteFunc(slices.Clone(base.Observations), func(o live.Observation) bool { return o.Assertion == req[0] && o.Subject == req[1] })
			want := expected{Outcomes: map[string]string{}}
			for _, a := range l7 {
				want.Outcomes[a] = "pass"
			}
			want.Outcomes[req[0]] = "fail"
			judge(t, set, want)
		})
	}

	// G15 over the flag: the canary deleted by the tenant is a known deviation only with the flag.
	for _, flag := range []bool{true, false} {
		set := base
		set.SharedStateProject = flag
		set.Observations = slices.Clone(base.Observations)
		for i, o := range set.Observations {
			if o.Assertion == "kd1-canary" && o.Subject == "DELETE" {
				set.Observations[i].Observed = "allowed"
			}
		}
		a := byAssertion(t, live.Assess(set))["kd1-canary"]
		if flag && (a.Outcome != "known-deviation" || a.Deviation != "KD-1") {
			t.Errorf("observed canary deletion with shared_state_project: %s %q, want known-deviation KD-1 (never pass)", a.Outcome, a.Deviation)
		}
		if !flag && a.Outcome != "fail" {
			t.Errorf("observed canary deletion without shared_state_project: %s, want fail", a.Outcome)
		}
	}

	// A live run's observations (T049): no assertion fails; a known deviation is reported.
	if p := os.Getenv("LZ_L7_OBSERVATIONS"); p != "" {
		t.Run("live-run", func(t *testing.T) {
			var set live.ObservationSet
			readJSON(t, p, &set)
			for name, a := range byAssertion(t, live.Assess(set)) {
				switch a.Outcome {
				case "pass":
				case "known-deviation":
					t.Logf("%s: known-deviation %s (%s)", name, a.Deviation, a.Detail)
				default:
					t.Errorf("%s: %s (%s)", name, a.Outcome, a.Detail)
				}
			}
		})
	}
}
