package live

// L7 chain observations (V010; FR-004, FR-008, FR-010, FR-011, SC-003; research R17, R20; guard
// G15): the types the collector writes to <run-dir>/observations.json, the judge that turns them
// into one assessment per assertion, the KD-1 canary collector and the run's trap. Tests T048 in
// observe_test.go and tools/internal/probes/live/chain/ (build tag live); implementation T062.
//
// This file is the T048 stub: the signatures the tests fix, with permissive bodies. T062 replaces
// the bodies.

import (
	"context"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/stacks"
)

// Observation is one observed fact of a live chain: the assertion it bears on, what was looked
// at (a bucket, a state key, an authority, a call), the observed value and a redacted detail.
// Positive assertions observe `holds` or `violated`; negative ones (a request that must be
// refused) `denied` or `allowed`; either may observe `error` when the fact could not be read.
type Observation struct {
	Assertion string `json:"assertion"`
	Subject   string `json:"subject"`
	Observed  string `json:"observed"`
	Detail    string `json:"detail,omitempty"`
}

// ObservationSet is <run-dir>/observations.json: the run, the sandbox flag the run's manifest
// carried (spec.sandbox.shared_state_project, KD-1) and every observation.
type ObservationSet struct {
	RunID              string        `json:"run_id"`
	SharedStateProject bool          `json:"shared_state_project"`
	Observations       []Observation `json:"observations"`
}

// Assessment is the judge's outcome for one assertion: pass, fail or known-deviation (with the
// deviation's id from spec *Known deviations*), and why.
type Assessment struct {
	Assertion string `json:"assertion"`
	Outcome   string `json:"outcome"`
	Deviation string `json:"deviation,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

// Assess judges an observation set: one assessment per assertion of the L7 set (and one per
// assertion the set names that is not in it).
func Assess(set ObservationSet) []Assessment {
	seen := map[string]bool{}
	var out []Assessment
	for _, o := range set.Observations {
		if !seen[o.Assertion] {
			seen[o.Assertion] = true
			out = append(out, Assessment{Assertion: o.Assertion, Outcome: "pass"})
		}
	}
	return out
}

// KnownDeviations returns the ids of the deviations the assessments report, sorted, once each.
func KnownDeviations(as []Assessment) []string { return nil }

// Trap holds the cleanups of resources a run creates outside its stacks (the KD-1 canary); the
// chain's destroy-on-exit runs it.
type Trap struct{}

// Add registers a cleanup under a name.
func (t *Trap) Add(name string, cleanup func(context.Context) error) {}

// Names lists the registered cleanups.
func (t *Trap) Names() []string { return nil }

// Run runs every registered cleanup once; the error names each that failed.
func (t *Trap) Run(ctx context.Context) error { return nil }

// CanaryOptions configures the KD-1 canary (research R20).
type CanaryOptions struct {
	API API
	// Admin is the bootstrap authority (sandbox.env): it creates the canary and its object and
	// deletes what is left of it in the trap.
	Admin Credential
	// Tenant is the tenant deployer: the KD-1 calls (bulkDeleteObjects, DELETE) are its.
	Tenant Credential
	// Project and Region locate the state buckets (spec.state.project, resolved; the storage
	// region of the account bucket).
	Project, Region string
	Org, RunID      string
	Trap            *Trap
}

// ObserveCanary observes KD-1: a disposable canary bucket with one object in the state project,
// then the tenant deployer's bulkDeleteObjects and DELETE on it through the management API.
func ObserveCanary(ctx context.Context, o CanaryOptions) []Observation { return nil }

// ObserveRun is what the chain hands its observer, once every stack of the run is applied.
type ObserveRun struct {
	RunID, RunDir string
	Manifest      *stacks.Manifest
	Trap          *Trap
}

// Observer collects a chain's L7 observations (ChainOptions.Observer; nil: the collector).
type Observer interface {
	Observe(ctx context.Context, run ObserveRun) ([]Observation, error)
}
