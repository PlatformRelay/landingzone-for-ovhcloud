# Data model: approved experiments

**Re-scope (D113).** RunConfig and RunLease are superseded by spec 005's run core (run id,
deadline, inventory, leftover check); the tag-envelope status of PrincipalRoute is withdrawn
(P25 refuted). See [spec.md](spec.md#re-scope-to-one-project-d113-2026-10-14).

- RunConfig: immutable approved revision, exactly one baseline sandbox project id, region/product list,
  credential/cleanup classes, numeric per-spike runtime/exposure ceilings, explicit refutation stops, freshness/reservation/lag bounds,
  per-credential residual windows, scoped authorization and protected candidate digest.
- RunLease: external id, candidate/config digests, owner/liveness, max exposure, deadline,
  status admitted/running/cleanup-required/cleaned/blocked; idempotent admission/retries.
- ResourceRecord: lease/request id, plane/project/region/id, creation intent and observed
  response, state-writer identity, cleanup action and status. Retained outside fixture state.
- RecoveryPackage: fixture checksum, encrypted backup id/key method, versioned backend,
  resource ids/import manifest, toolchain and independently usable sealed authenticators.
- PrincipalRoute: issuer, authenticator class, credential type, bound resource/actions,
  lifetime/refresh/revoke and approved maximum residual window; observed positive/denial
  pairs and floor-removal/tag-envelope/child-operation subcontrol status. Any missing
  subcontrol blocks the corresponding authority claim. A denied OVH route does not
  imply a denied Keystone/S3 route.
- Qualification: supported experiment scope, evidence digests, costs, cleanup status,
  independent review and pass/refuted/blocked result. No tuple promotion by inferred coverage.
Numeric gate values are external approved inputs, validated before live implementation use.

IAMLimitObservation: object type, current count, limit/source/revision or UNVERIFIED;
PropagationObservation: route/action, bounded apply-to-effective elapsed time and repeated
positive/negative observations. Unmeasured values never become guarantees.

## Probe checks (T029–T036)

Each probe's check is a `live.ProbeCheck` (tools/internal/live/runner.go) that lz-live runs after
the root's apply, as spec 005's `state` and `storageiam` checks are: the root publishes the
check's variables as one sensitive output, Validate refuses unusable ones before any request,
Observe writes `observations.json`, Undo removes whatever Observe may have written and runs on
every way out and on `--cleanup`, where it gets only the published variables. So every output
publishes `run_id` (Public); Validate checks it against `live.RunIDPattern` and compares every
published name exactly with the one derived from it (storageiam's `validate`). Buckets use
`lower(run id)`, IAM names the run id as is. tools/cmd/lz-live/probe.go `probeCheck` maps the
root's directory name to the constructor.

Every probe resource has one owner: the root (destroyed by tofu) or the check (deleted by its
Undo); the checks own IAM clients and policies. Because a lost create answer can leave neither
an output nor a state entry, `ProbeCheck` gains a field (T037, T038), which the runner calls with
only what probe.env records, after destroy and before the leftover check, on every way out and
on `--cleanup`, and keeps probe.env, the passphrase and the root record until it and the
leftover check pass:

```go
// tools/internal/live/runner.go, ProbeCheck
	// Sweep (optional) deletes, by listing exact run-derived names, every resource the root
	// or the check may have created, whether or not an output or a state entry exists; it
	// never reads a published output. nil: none (spec 005's roots).
	Sweep func(ctx context.Context, runID, projectID string, term io.Writer) error
```

These are the signatures the test tasks write
against; a test task's stub has them with bodies that return `errors.New("not implemented")`
(Observe, Undo) or `nil` (Validate), and make no request. Observed values extend each package's
own set; a value outside it is a test failure.

```go
// tools/internal/probes/live/storageiam/swiftreach.go (T029, T030); root swift-reach,
// output swift_check: run_id, auth_url, region, tenant_id, seed_key = lzprobe-swift-reach/seed,
// bucket_a = lzprobe-swift-reach-<lower(run id)>-a, bucket_b = …-b,
// user_a_name, user_a_password, user_b_name, user_b_password (the tenant users under test),
// cleanup_a_key_id, cleanup_a_secret, cleanup_b_key_id, cleanup_b_secret (S3 list and delete on
// that bucket only; Undo's credentials, independent of the tenant users).
// Public: run_id, auth_url, region, tenant_id, seed_key, bucket_a, bucket_b, user_a_name,
// user_b_name. Owners: the root owns buckets, users, S3 credentials and policies (and the seeds
// if the provider can write an object, UNVERIFIED); the check owns what it writes.
// SwiftReach's Sweep lists buckets and users by the run's exact names with the sandbox admin.
type SwiftOptions struct {
	HTTP  *http.Client     // Keystone, Swift and S3; nil: a default client
	Now   func() time.Time // nil: time.Now
	API   live.API         // Sweep's listings and deletes
	Admin live.Credential  // Sweep: the sandbox admin
}
func SwiftReach(o SwiftOptions) *live.ProbeCheck
// JudgeReach: the other bucket's answer next to the own bucket's same-route control.
// "reach" (other 2xx), "denied" (control 2xx, other 401/403, the repeated control with the
// same token 2xx again), "blocked" (anything else, including a repeated control that fails).
func JudgeReach(control, other, repeat SwiftAnswer) string
type SwiftAnswer struct {
	Status int   // 0: no answer
	Err    error // transport error
}

// tools/internal/probes/live/identity/residual.go (T031, T032); root token-residual,
// output residual_check: run_id, project_id, read_path (Public: all). The check creates and
// deletes the client and policy lzprobe-token-residual-<run id>.
type Options struct {
	API      live.API
	Admin    live.Credential // creates, lists and deletes the disposable clients and policies
	Bind     func(context.Context, live.Credential) error // nil: binds nothing
	Now      func() time.Time
	Sleep    func(context.Context, time.Duration) error // nil: a timer that honours ctx
	Interval time.Duration // between polls and control retries; zero: 30 s
	Settle   time.Duration // the bound on control retries (both checks); zero: 5 min
}
func TokenResidual(o Options) *live.ProbeCheck
type Poll struct {
	At     time.Time
	Status int // 0: no answer
	Err    error
}
type Residual struct {
	Observed  string        // "refused-before-expiry", "valid-until-expiry", "blocked"
	Reason    string        // blocked: "expiry", "control", "outages", "deadline"
	Lower     time.Duration // delete to the last allowed poll ("valid ≥ Lower")
	Upper     time.Duration // delete to the first refusal; zero when none observed
	ExpiresIn time.Duration // the validated expires_in of the token answer
}
// ResidualWindow judges only observed polls: an outage bounds nothing, and no validity is
// inferred across a tail of outages or past a deadline stop.
func ResidualWindow(issued, deleted time.Time, expiresIn time.Duration, control Poll, polls []Poll, deadlineHit bool) Residual

// tools/internal/probes/live/identity/urn.go (T033, T034); root iam-urn,
// output urn_check: run_id, project_urn, account_urn, bucket = lzprobe-iam-urn-<lower(run id)>
// (the only resource the root owns, the write's target), bucket_urn (Public: all). The check
// creates and owns the identities and policies lzprobe-iam-urn-<run id>-project|account, each
// granting the probed actions (a read and account:apiovh:iam/resource/edit) and the control.
func IamURN(o Options) *live.ProbeCheck
type URNAnswer struct {
	Status  int
	Actions []string // the IAM actions a refusal named (live.APIChecker)
	Err     error
}
// JudgeURN, once per probed action: "project", "account", "either" (both allowed) or
// "blocked"; a control that never succeeded within Settle makes it "blocked" whatever the probed
// calls answered. The record keeps each result with its action.
func JudgeURN(action string, projectControl, project, accountControl, account URNAnswer) string

// tools/internal/probes/live/state/recovery.go (T035, T036); root locked-recovery,
// output recovery_check: run_id, bucket = lzprobe-locked-recovery-<lower(run id)>, region,
// state_key_id, state_secret, janitor_key_id, janitor_secret (Public: run_id, bucket, region).
type RecoveryOptions struct {
	HTTP *http.Client     // supporting S3 controls; nil: the S3 client's default
	Now  func() time.Time // nil: time.Now
	// Tofu runs the pinned OpenTofu on the disposable backend root; env holds the state key
	// pair only (no OVH_*, no janitor key). lz-live supplies it (scratch HOME, own TF_DATA_DIR,
	// redacted output). kill: start the command, wait until it waits at its approval prompt
	// (holding the lock), SIGKILL it and report how it exited.
	Tofu func(ctx context.Context, env map[string]string, kill bool, args ...string) (TofuRun, error)
	API   live.API        // Sweep's listings and deletes
	Admin live.Credential // Sweep: the sandbox admin
}
type TofuRun struct {
	Exit     int
	Signaled bool   // exited on the SIGKILL the seam sent: the owner-death proof
	Output   []byte // redacted; classified against the fixtures of OpenTofu 1.13.0's diagnostics
}
func LockedRecovery(o RecoveryOptions) *live.ProbeCheck
```

Each check names its observations as the state and storageiam records do (step, operation,
credential, expected, observed, status, code or actions, as_expected) and never records a
credential, an object body or the server's text.
