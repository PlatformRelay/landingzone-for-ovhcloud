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
every way out and on `--cleanup`. tools/cmd/lz-live/probe.go `probeCheck` maps the root's
directory name to the constructor. These are the signatures the test tasks write against; a
test task's stub has them with bodies that return `errors.New("not implemented")` (Observe,
Undo) or `nil` (Validate). Observed values extend each package's own set; a value outside it is
a test failure.

```go
// tools/internal/probes/live/storageiam/swiftreach.go (T029, T030); root swift-reach,
// output swift_check: auth_url, region, tenant_id, user_name, user_password, own_bucket,
// other_bucket (Public: all but user_password).
type SwiftOptions struct {
	HTTP *http.Client     // Keystone and Swift; nil: a default client
	Now  func() time.Time // nil: time.Now
}
func SwiftReach(o SwiftOptions) *live.ProbeCheck
// JudgeReach: the other bucket's answer next to the own bucket's same-route control.
// "reach" (other 2xx), "denied" (control 2xx, other 401/403), "blocked" (anything else).
func JudgeReach(control, other SwiftAnswer) string
type SwiftAnswer struct {
	Status int   // 0: no answer
	Err    error // transport error
}

// tools/internal/probes/live/identity/residual.go (T031, T032); root token-residual,
// output residual_check: project_id, read_path (Public: both).
type Options struct {
	API      live.API
	Admin    live.Credential // creates and deletes the disposable client and policy
	Bind     func(context.Context, live.Credential) error // nil: binds nothing
	Now      func() time.Time
	Sleep    func(context.Context, time.Duration) error // nil: a timer that honours ctx
	Interval time.Duration // between polls; zero: 30 s
}
func TokenResidual(o Options) *live.ProbeCheck
type Poll struct {
	At     time.Time
	Status int // 0: no answer
	Err    error
}
// ResidualWindow: "refused-after" with the window from deleted to the first 401/403,
// "valid-until-expiry", or "blocked" (control refused, only outages, or the deadline).
func ResidualWindow(deleted, expiry time.Time, control Poll, polls []Poll) (observed string, window time.Duration)

// tools/internal/probes/live/identity/urn.go (T033, T034); root iam-urn,
// output urn_check: project_urn, account_urn, action, call (Public: all), and the two
// identities' client id and secret.
func IamURN(o Options) *live.ProbeCheck
type URNAnswer struct {
	Status  int
	Actions []string // the IAM actions a refusal named (live.APIChecker)
	Err     error
}
// JudgeURN: "project", "account", "either" (both allowed) or "blocked".
func JudgeURN(action string, project, account URNAnswer) string

// tools/internal/probes/live/state/recovery.go (T035, T036); root locked-recovery,
// output recovery_check: the state check's six keys (bucket, region, state_key_id,
// state_secret, janitor_key_id, janitor_secret).
func LockedRecovery(o Options) *live.ProbeCheck // state.Options: HTTP, Now
```

Each check names its observations as the state and storageiam records do (step, operation,
credential, expected, observed, status, code or actions, as_expected) and never records a
credential, an object body or the server's text.
