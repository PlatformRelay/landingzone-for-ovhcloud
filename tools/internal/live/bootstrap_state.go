package live

import (
	"context"
	"fmt"
	"io"
	"io/fs"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/stacks"
)

// Bootstrap phases state, publish and verify (spec 005 FR-004, FR-008, FR-011, FR-012, research
// R13 phases 5–7), run as BootstrapOptions.Rest after `admin`. T056 fixes the interface and pins
// the behaviour in bootstrap_state_test.go; T057 implements it. This stub reports every phase
// `unchanged` and does nothing.

// Bootstrap phases after `admin`, in run order (research R13).
const (
	PhaseState   = "state"
	PhasePublish = "publish"
	PhaseVerify  = "verify"
)

// StateOptions configures the state, publish and verify phases of one bootstrap run.
type StateOptions struct {
	// Checkout is the owner's checkout: the generated roots stacks/account/bootstrap (`state`) and
	// stacks/account/account-governance (`verify`). Nothing is written inside it.
	Checkout string
	Manifest *stacks.Manifest // spec.org (bucket names), spec.state.project's reference
	Region   string           // Object Storage region of the account bucket as the API names it (GRA)
	Schemas  fs.FS            // schemas/outputs, for the published envelope
	Revision string           // source_revision of the published envelope (the reviewed commit)
	Tofu     string           // the tofu executable
	API      API              // the API the admin credential (BootstrapAccount.Admin) authenticates against
	// Store opens the account bucket with state.env's S3 keys (AWS_ACCESS_KEY_ID,
	// AWS_SECRET_ACCESS_KEY): S3 live, a fake offline.
	Store  func(s3 map[string]string) (ObjectStore, error)
	Stdout io.Writer // LZ-LIVE lines of these phases
}

// BootstrapState runs the state, publish and verify phases of one bootstrap run.
type BootstrapState struct {
	o  StateOptions
	rs []PhaseResult
}

// NewBootstrapState returns the phases for one run; Rest is BootstrapOptions.Rest.
func NewBootstrapState(o StateOptions) *BootstrapState { return &BootstrapState{o: o} }

// Rest runs state, publish and verify for the bound account with its admin credential. A refusal
// is a *Refusal (exit 3), a failure any other error (exit 1); every phase reports one result.
func (s *BootstrapState) Rest(ctx context.Context, a BootstrapAccount) error {
	for _, p := range []string{PhaseState, PhasePublish, PhaseVerify} {
		s.rs = append(s.rs, PhaseResult{Phase: p, Status: StatusUnchanged})
		if s.o.Stdout != nil {
			fmt.Fprintln(s.o.Stdout, "LZ-LIVE "+p+" bootstrap "+StatusUnchanged)
		}
	}
	return nil
}

// Results returns the phases' results in run order.
func (s *BootstrapState) Results() []PhaseResult { return s.rs }
