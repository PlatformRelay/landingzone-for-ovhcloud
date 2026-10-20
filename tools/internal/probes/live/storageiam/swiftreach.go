package storageiam

// The swift-reach check (spec 003 T029, T030; FR-005 C005.1, FR-006 C006.4, C006.5): whether a
// tenant S3 user's cloud-project (OpenStack) user reaches the other tenant's bucket through
// Keystone and Swift, bypassing the S3 bucket-ARN policy (spec 005 G6; UNVERIFIED, as is whether
// Swift serves S3-API buckets at all). lz-live runs it on tests/live/probes/swift-reach, whose
// sensitive output swift_check publishes the variables (data-model.md *Probe checks*).
//
//   - Observe first writes each bucket's seed (lzprobe-swift-reach/seed) with that bucket's
//     cleanup S3 key: the provider has no object resource, so the root cannot (tasks.md T030). The
//     bucket's destroy removes the seed with it (the provider empties a bucket before deleting it,
//     provider docs resources/cloud_project_storage.md:10), and Undo never deletes it (T029). Then
//     each tenant user logs in to Keystone once (POST <auth_url>/auth/tokens, password method,
//     scoped to the project; docs.ovhcloud.com storage-and-backup/object-storage/
//     pcs-curl-commands-memo.mdx:15-19) and, per route (list, read of the seed, write under
//     lzprobe-swift-reach/), asks its own bucket (the control), the other bucket, and its own
//     bucket again with the same token, on the catalog's public object-store endpoint of the
//     published region. JudgeReach weighs the three. A reach is a finding; Observe fails on a
//     finding, and when any step is blocked or not run (C005.1 is then not shown), always
//     returning the record.
//   - Undo lists each bucket with its own cleanup key over S3 and deletes every object under the
//     prefix but the seed, then lists again; anything left fails it. A bucket already deleted (404)
//     or a cleanup key already revoked (403 InvalidAccessKeyId) is undone (coordinator decision
//     2026-10-16: --cleanup reruns Undo after a Sweep). A revoked key passes Undo whether or not
//     its bucket still holds the check's objects; the destroy and Sweep, which empty the bucket
//     through the API, are then the cleanup (review r1).
//   - Sweep finds, with the sandbox admin and by listing, the run's two buckets in every region and
//     its four users by their exact descriptions, empties each bucket before deleting it (a bucket
//     it cannot empty is kept and named), deletes the users, and lists again.
//
// Nothing here prints or records a password, a token, a key or the server's text: of a Keystone
// answer the status, the token (in memory only), the catalog endpoint and role names of a set
// shape; of a Swift answer the status; of an S3 answer the status and error code
// (live.S3Checker); of an API answer what live.APIChecker keeps.

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/live"
)

// SwiftOptions are the swift-reach check's seams; lz-live fills them from the run.
type SwiftOptions struct {
	HTTP  *http.Client     // Keystone, Swift and S3; nil: a default client
	Now   func() time.Time // nil: time.Now
	API   live.API         // Sweep's listings and deletes
	Admin live.Credential  // Sweep: the sandbox admin
}

// SwiftAnswer is one Swift call's answer as JudgeReach weighs it.
type SwiftAnswer struct {
	Status int   // 0: no answer
	Err    error // transport error
}

// The swift-reach root's fixed names (tests/live/probes/swift-reach/main.tf).
const (
	SwiftOutput = "swift_check"
	// KeystoneURL is OVHcloud's Keystone v3 endpoint (docs.ovhcloud.com storage-and-backup/
	// object-storage/pcs-curl-commands-memo.mdx:15; manage-and-operate/iam/
	// authenticate-api-openstack-with-service-account.mdx:87): the passwords go to it only.
	KeystoneURL = "https://auth.cloud.ovh.net/v3"
	SwiftPrefix = "lzprobe-swift-reach/"
	SwiftSeed   = SwiftPrefix + "seed"
)

// SwiftBucket is the run's bucket of one side (a, b).
func SwiftBucket(runID, side string) string {
	return "lzprobe-swift-reach-" + strings.ToLower(runID) + "-" + side
}

// SwiftContainer is the run's Swift-native control container of one side (a, b): the positive
// control that shows the tenant's token and the catalog's Swift endpoint work, so that a 404 on
// the S3 bucket names may read not-applicable (research.md *S3-API buckets are not on the Swift
// plane*, T040, T041).
func SwiftContainer(runID, side string) string {
	return "lzprobe-swift-reach-" + strings.ToLower(runID) + "-swift-" + side
}

// The Swift-native control's steps, stubbed by T040 (no-ops); T041 implements them:
//   - nativeCreate: the sandbox admin creates both containers (POST
//     /cloud/project/{serviceName}/storage, in the published region) before any tenant call;
//   - nativeUndo: the admin deletes both by listing (DELETE …/storage/{containerId}?recursive=true),
//     lists again and fails on anything left;
//   - nativeSweep: the same with only the run id and the project.
//
// The steps' judging (own container 2xx to list, write and read, per direction, before a 404 on
// the S3 bucket names reads not-applicable; the cross steps under swift_native) is T041's too, and
// so are these signatures: the stubs return nothing only so that T029's cases keep their results.
func (o SwiftOptions) nativeCreate(context.Context, map[string]string, io.Writer) {}

func (o SwiftOptions) nativeUndo(context.Context, map[string]string, io.Writer) {}

func (o SwiftOptions) nativeSweep(context.Context, string, string, io.Writer) {}

// SwiftUsers are the descriptions of the run's cloud project users: the tenant users under test
// and the bucket-scoped cleanup users.
func SwiftUsers(runID string) []string {
	p := "lzprobe-swift-reach-" + strings.ToLower(runID)
	return []string{p + "-a", p + "-b", p + "-a-cleanup", p + "-b-cleanup"}
}

var (
	swiftPublic = []string{"auth_url", "bucket_a", "bucket_b", "region", "run_id", "seed_key", "tenant_id", "user_a_name", "user_b_name"}
	swiftKeys   = slices.Sorted(slices.Values(append(slices.Clone(swiftPublic),
		"user_a_password", "user_b_password", "cleanup_a_key_id", "cleanup_a_secret", "cleanup_b_key_id", "cleanup_b_secret")))
	swiftUserName = regexp.MustCompile(`^[\x21-\x7e]{1,64}$`)
	swiftKeyID    = regexp.MustCompile(`^[A-Za-z0-9]{8,128}$`)
	// swiftRole is a Keystone role name as recorded (objectstore_operator).
	swiftRole = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)
	// swiftHost is an OVHcloud Swift endpoint host (storage.<region>.cloud.ovh.net,
	// pcs-curl-commands-memo.mdx:16): the token goes to no other.
	swiftHost = regexp.MustCompile(`^storage\.[a-z0-9-]+\.cloud\.ovh\.net$`)
)

// Observed values of a step (and notRun); denied is the expected one.
const (
	reach   = "reach"
	denied  = "denied"
	blocked = "blocked"
)

func ok2xx(a SwiftAnswer) bool { return a.Err == nil && a.Status >= 200 && a.Status <= 299 }

// JudgeReach: the other bucket's answer next to the own bucket's same-route control.
// "reach" (other 2xx), "denied" (control 2xx, other 401/403, the repeated control with the
// same token 2xx again), "blocked" (anything else, including a repeated control that fails).
func JudgeReach(control, other, repeat SwiftAnswer) string {
	refusal := other.Err == nil && (other.Status == http.StatusUnauthorized || other.Status == http.StatusForbidden)
	switch {
	case ok2xx(other):
		return reach
	case ok2xx(control) && refusal && ok2xx(repeat):
		return denied
	}
	return blocked
}

// whyBlocked says, for a blocked step, which answer kept it from a denial.
func whyBlocked(control, other, repeat SwiftAnswer) string {
	answered := func(a SwiftAnswer) string {
		if a.Err != nil {
			return "gave no answer"
		}
		return fmt.Sprintf("answered %d", a.Status)
	}
	switch {
	case !ok2xx(control):
		return "the own bucket's control " + answered(control) + ", so a refusal shows nothing"
	case other.Err != nil || other.Status != http.StatusUnauthorized && other.Status != http.StatusForbidden:
		return "the other bucket " + answered(other) + ", not a refusal"
	}
	return "the repeated control " + answered(repeat) + " with the same token: the token or the route was lost"
}

// SwiftObservation is one step's answers as observations.json keeps them.
type SwiftObservation struct {
	Step       string `json:"step"` // <user>-<route>-<other side>, e.g. a-list-b
	Route      string `json:"route"`
	Credential string `json:"credential"` // user-a, user-b
	Bucket     string `json:"bucket"`     // the other bucket
	Expected   string `json:"expected"`
	Observed   string `json:"observed"` // reach, denied, blocked, not-run
	Status     int    `json:"status"`   // the other bucket's; 0: no answer or not asked
	Control    int    `json:"control_status"`
	Repeat     int    `json:"repeat_status"`
	Reason     string `json:"reason,omitempty"`
	AsExpected bool   `json:"as_expected"`
}

// SwiftRecord is the swift-reach check's observations.json.
type SwiftRecord struct {
	Check string `json:"check"`
	// Seeds: per bucket, the seed write's answer ("written 200", "refused 403 AccessDenied", …).
	Seeds map[string]string `json:"seeds"`
	// Roles: per tenant user that logged in, the role names its token carries (a project-wide
	// Swift role would explain a reach; T025).
	Roles        map[string][]string `json:"roles"`
	Observations []SwiftObservation  `json:"observations"`
	Findings     []string            `json:"findings"`
}

// SwiftReach is the probe check lz-live runs on the swift-reach root.
func SwiftReach(o SwiftOptions) *live.ProbeCheck {
	return &live.ProbeCheck{
		Output:   SwiftOutput,
		Public:   slices.Clone(swiftPublic),
		Validate: validateSwift,
		Observe: func(ctx context.Context, env map[string]string, term io.Writer) ([]byte, error) {
			if err := validateSwift(env); err != nil {
				return nil, err
			}
			rec, err := o.observe(ctx, env, term)
			raw, jerr := json.Marshal(rec)
			return raw, errors.Join(err, jerr)
		},
		Undo: func(ctx context.Context, env map[string]string, term io.Writer) error {
			if err := validateSwift(env); err != nil {
				return err
			}
			return o.undo(ctx, env, term)
		},
		Sweep: o.sweep,
	}
}

// validateSwift refuses published variables that are not the swift-reach root's of one run: the
// keys, none empty, the run id, the names derived from it, the Keystone URL, the region, the
// project and the shapes of the user names and key ids. Refusals name keys and expected public
// values, never a secret.
func validateSwift(env map[string]string) error {
	if got := slices.Sorted(maps.Keys(env)); !slices.Equal(got, swiftKeys) {
		return fmt.Errorf("output %s publishes the keys %s, want %s", SwiftOutput, strings.Join(got, ", "), strings.Join(swiftKeys, ", "))
	}
	for _, k := range swiftKeys {
		if env[k] == "" {
			return fmt.Errorf("output %s publishes an empty %s", SwiftOutput, k)
		}
	}
	if !live.RunIDPattern.MatchString(env["run_id"]) {
		return fmt.Errorf("output %s: run_id is not a run id", SwiftOutput)
	}
	want := map[string]string{
		"auth_url": KeystoneURL, "bucket_a": SwiftBucket(env["run_id"], "a"), "bucket_b": SwiftBucket(env["run_id"], "b"), "seed_key": SwiftSeed,
	}
	for _, k := range slices.Sorted(maps.Keys(want)) {
		if env[k] != want[k] {
			return fmt.Errorf("output %s: %s is not %s", SwiftOutput, k, want[k])
		}
	}
	switch {
	case !regionName.MatchString(env["region"]):
		return fmt.Errorf("output %s: region is not a region name", SwiftOutput)
	case !projectID.MatchString(env["tenant_id"]):
		return fmt.Errorf("output %s: tenant_id is not a project id", SwiftOutput)
	}
	for _, k := range []string{"user_a_name", "user_b_name"} {
		if !swiftUserName.MatchString(env[k]) {
			return fmt.Errorf("output %s: %s is not a user name", SwiftOutput, k)
		}
	}
	for _, k := range []string{"cleanup_a_key_id", "cleanup_b_key_id"} {
		if !swiftKeyID.MatchString(env[k]) {
			return fmt.Errorf("output %s: %s is not an access key id", SwiftOutput, k)
		}
	}
	return nil
}

// client is the check's HTTP client: the given one or a default, never following a redirect (a
// redirect would carry a token or a password elsewhere; a 3xx is an answer, judged blocked).
func (o SwiftOptions) client() *http.Client {
	c := http.Client{Timeout: 30 * time.Second}
	if o.HTTP != nil {
		c = *o.HTTP
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &c
}

// side is one direction: a tenant user, its own bucket and the other one.
type side struct {
	name, other, ownBucket, otherBucket string // name, other: a, b
	user, password                      string
}

// session is a tenant user's Keystone login: the token and the Swift account URL, or why not.
type session struct {
	token, swift string
	roles        []string
	why          string
}

// maxKeystone bounds a Keystone token answer (its catalog) and what is read of a Swift answer.
const maxKeystone = 1 << 20

// login asks Keystone for a project-scoped token with the user's password; why says, without
// any answer text, why there is no usable session.
func (o SwiftOptions) login(ctx context.Context, env map[string]string, s side) session {
	body, _ := json.Marshal(map[string]any{"auth": map[string]any{
		"identity": map[string]any{"methods": []string{"password"}, "password": map[string]any{
			"user": map[string]any{"name": s.user, "domain": map[string]string{"id": "default"}, "password": s.password}}},
		"scope": map[string]any{"project": map[string]any{"id": env["tenant_id"], "domain": map[string]string{"id": "default"}}},
	}})
	who := "user " + s.name + "'s Keystone login"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, KeystoneURL+"/auth/tokens", bytes.NewReader(body))
	if err != nil {
		return session{why: who + ": bad request"}
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.client().Do(req)
	if err != nil {
		return session{why: who + " gave no answer"}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxKeystone+1))
	tok := resp.Header.Get("X-Subject-Token")
	switch {
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return session{why: fmt.Sprintf("%s answered %d", who, resp.StatusCode)}
	case err != nil || len(raw) > maxKeystone:
		return session{why: who + ": unreadable answer"}
	case tok == "":
		return session{why: who + ": no token in the answer"}
	}
	var ans struct {
		Token struct {
			Roles []struct {
				Name string `json:"name"`
			} `json:"roles"`
			Catalog []struct {
				Type      string `json:"type"`
				Endpoints []struct {
					Interface string `json:"interface"`
					Region    string `json:"region"`
					URL       string `json:"url"`
				} `json:"endpoints"`
			} `json:"catalog"`
		} `json:"token"`
	}
	if json.Unmarshal(raw, &ans) != nil {
		return session{why: who + ": the answer is not a Keystone token"}
	}
	ses := session{token: tok, roles: []string{}}
	for _, r := range ans.Token.Roles {
		// Shape only here; observe drops a role that echoes a secret of the run.
		if swiftRole.MatchString(r.Name) && !slices.Contains(ses.roles, r.Name) {
			ses.roles = append(ses.roles, r.Name)
		}
	}
	slices.Sort(ses.roles)
	for _, c := range ans.Token.Catalog {
		for _, e := range c.Endpoints {
			if c.Type == "object-store" && e.Interface == "public" && e.Region == env["region"] && swiftAccount(e.URL, env["tenant_id"]) {
				ses.swift = e.URL
				return ses
			}
		}
	}
	return session{why: who + ": the catalog has no public object-store endpoint https://storage.<region>.cloud.ovh.net/v1/AUTH_<tenant_id> for region " + env["region"]}
}

// swiftAccount: the endpoint is the project's Swift account on an OVHcloud storage host.
func swiftAccount(raw, tenant string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.User == nil && u.Port() == "" && swiftHost.MatchString(u.Host) &&
		u.Path == "/v1/AUTH_"+tenant && u.RawQuery == "" && u.Fragment == ""
}

// swiftCall sends one Swift request with the token and returns its answer; nothing of the body is
// kept.
func (o SwiftOptions) swiftCall(ctx context.Context, ses session, method, bucket, key string) SwiftAnswer {
	u := ses.swift + "/" + bucket + "?format=json"
	var body io.Reader = http.NoBody
	if key != "" {
		u = ses.swift + "/" + bucket + "/" + key
	}
	if method == http.MethodPut {
		body = strings.NewReader("lzprobe-swift-reach write\n")
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return SwiftAnswer{Err: errors.New("bad request")}
	}
	req.Header.Set("X-Auth-Token", ses.token)
	resp, err := o.client().Do(req)
	if err != nil {
		return SwiftAnswer{Err: errors.New("no answer")}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxKeystone))
	return SwiftAnswer{Status: resp.StatusCode}
}

// route is one Swift route a step observes: a container listing (no key), a read of the seed, or
// a write, whose key is one under the prefix per request.
type route struct{ name, method, key string }

var swiftRoutes = []route{{"list", http.MethodGet, ""}, {"read", http.MethodGet, SwiftSeed}, {"write", http.MethodPut, ""}}

// observe writes the seeds, logs both users in and observes the six steps; the record is
// complete whatever fails.
func (o SwiftOptions) observe(ctx context.Context, env map[string]string, term io.Writer) (SwiftRecord, error) {
	rec := SwiftRecord{Check: "swift-reach", Seeds: map[string]string{}, Roles: map[string][]string{}, Observations: []SwiftObservation{}, Findings: []string{}}
	o.nativeCreate(ctx, env, term)
	for _, b := range []string{"a", "b"} {
		rec.Seeds[env["bucket_"+b]] = o.seed(ctx, env, b)
		fmt.Fprintf(term, "LZ-LIVE observe seed %s %s\n", env["bucket_"+b], rec.Seeds[env["bucket_"+b]])
	}
	sides := []side{
		{name: "a", other: "b", ownBucket: env["bucket_a"], otherBucket: env["bucket_b"], user: env["user_a_name"], password: env["user_a_password"]},
		{name: "b", other: "a", ownBucket: env["bucket_b"], otherBucket: env["bucket_a"], user: env["user_b_name"], password: env["user_b_password"]},
	}
	sessions := map[string]session{}
	for _, s := range sides {
		ses := session{why: "not run: the run's context ended"}
		if ctx.Err() == nil {
			ses = o.login(ctx, env, s)
		}
		sessions[s.name] = ses
	}
	// A role name that holds or is held by a secret of the run (either token, a password, a
	// cleanup key) is dropped: the tokens are not in the run's redactor (review r1, r2).
	secrets := []string{sessions["a"].token, sessions["b"].token}
	for _, k := range swiftKeys {
		if !slices.Contains(swiftPublic, k) {
			secrets = append(secrets, env[k])
		}
	}
	for _, s := range sides {
		ses := sessions[s.name]
		ses.roles = slices.DeleteFunc(ses.roles, func(role string) bool {
			return slices.ContainsFunc(secrets, func(x string) bool { return x != "" && (strings.Contains(role, x) || strings.Contains(x, role)) })
		})
		sessions[s.name] = ses
		if ses.why == "" {
			rec.Roles["user-"+s.name] = ses.roles
			fmt.Fprintf(term, "LZ-LIVE observe user-%s roles %s\n", s.name, strings.Join(ses.roles, ","))
		} else {
			fmt.Fprintf(term, "LZ-LIVE observe user-%s %s\n", s.name, ses.why)
		}
	}
	nobody := sessions["a"].why != "" && sessions["b"].why != ""
	var unproven []string
	for _, s := range sides {
		for _, r := range swiftRoutes {
			ob := o.step(ctx, s, sessions[s.name], r, nobody, sessions)
			rec.Observations = append(rec.Observations, ob)
			fmt.Fprintf(term, "LZ-LIVE observe %s %s other %d control %d repeat %d", ob.Step, ob.Observed, ob.Status, ob.Control, ob.Repeat)
			if ob.Reason != "" {
				fmt.Fprintf(term, " (%s)", ob.Reason)
			}
			fmt.Fprintln(term)
			switch ob.Observed {
			case reach:
				rec.Findings = append(rec.Findings, ob.Step)
				fmt.Fprintf(term, "LZ-LIVE finding %s: expected denied, observed reach %d: user %s's token reaches bucket %s through Swift\n", ob.Step, ob.Status, s.name, s.otherBucket)
			case blocked, notRun:
				unproven = append(unproven, ob.Step)
			}
		}
	}
	var errs []error
	for _, f := range rec.Findings {
		errs = append(errs, fmt.Errorf("%s: expected denied, observed reach", f))
	}
	if len(unproven) > 0 {
		errs = append(errs, fmt.Errorf("swift-reach: C005.1 not shown: %s blocked or not run", strings.Join(unproven, ", ")))
	}
	return rec, errors.Join(errs...)
}

// step observes one route of one side: the own bucket's control, the other bucket and the
// repeated control, with one token.
func (o SwiftOptions) step(ctx context.Context, s side, ses session, r route, nobody bool, sessions map[string]session) SwiftObservation {
	ob := SwiftObservation{Step: s.name + "-" + r.name + "-" + s.other, Route: r.name, Credential: "user-" + s.name, Bucket: s.otherBucket, Expected: denied}
	switch {
	case nobody:
		ob.Observed, ob.Reason = notRun, "no tenant user could log in: "+sessions["a"].why+"; "+sessions["b"].why
	case ses.why != "":
		ob.Observed, ob.Reason = blocked, ses.why
	case ctx.Err() != nil:
		ob.Observed, ob.Reason = notRun, "not run: the run's context ended"
	default:
		key := func(which string) string {
			if r.method != http.MethodPut {
				return r.key
			}
			return SwiftPrefix + "write-" + s.name + "-" + which
		}
		control := o.swiftCall(ctx, ses, r.method, s.ownBucket, key("control"))
		other := o.swiftCall(ctx, ses, r.method, s.otherBucket, key("other"))
		repeat := o.swiftCall(ctx, ses, r.method, s.ownBucket, key("repeat"))
		ob.Control, ob.Status, ob.Repeat = control.Status, other.Status, repeat.Status
		ob.Observed = JudgeReach(control, other, repeat)
		if ob.Observed == blocked {
			ob.Reason = whyBlocked(control, other, repeat)
		}
	}
	ob.AsExpected = ob.Observed == denied
	return ob
}

// seed writes the bucket's seed with its cleanup key (the root's policy allows that one write)
// and says how it went; a failed write is no error here: the read steps' controls show it.
func (o SwiftOptions) seed(ctx context.Context, env map[string]string, b string) string {
	s3, err := o.s3(env, b)
	if err != nil {
		return "not written: " + err.Error()
	}
	a, err := s3.Do(ctx, live.S3Call{Op: live.S3PutObject, Bucket: env["bucket_"+b], Key: SwiftSeed, Body: []byte("lzprobe-swift-reach seed\n")})
	switch {
	case err != nil:
		return "not written: no answer"
	case a.Status < 200 || a.Status > 299:
		return fmt.Sprintf("refused %d %s", a.Status, a.Code)
	}
	return fmt.Sprintf("written %d", a.Status)
}

// s3 is the S3 client of bucket b's cleanup key.
func (o SwiftOptions) s3(env map[string]string, b string) (*live.S3Checker, error) {
	return live.NewS3Checker(live.S3Options{Region: env["region"], HTTP: o.HTTP, Now: o.Now,
		Keys: map[string]string{"AWS_ACCESS_KEY_ID": env["cleanup_"+b+"_key_id"], "AWS_SECRET_ACCESS_KEY": env["cleanup_"+b+"_secret"]}})
}

// versionList is a ListObjectVersions answer: the versions and delete markers.
type versionList struct {
	XMLName   xml.Name
	Truncated bool           `xml:"IsTruncated"`
	Versions  []listedObject `xml:"Version"`
	Markers   []listedObject `xml:"DeleteMarker"`
}

type listedObject struct {
	Key string `xml:"Key"`
}

// removedAlready: an S3 answer saying the bucket or the key no longer exists (Undo after a Sweep).
func removedAlready(a live.S3Answer) bool {
	return a.Status == http.StatusNotFound || a.Status == http.StatusForbidden && a.Code == "InvalidAccessKeyId"
}

// undo removes, in both buckets with each bucket's own cleanup key, every object under the prefix
// but the seed, then lists again. Every failure is in the error, which names the bucket and what
// is left; the other bucket is still undone.
func (o SwiftOptions) undo(ctx context.Context, env map[string]string, term io.Writer) error {
	var errs []error
	for _, b := range []string{"a", "b"} {
		errs = append(errs, o.undoBucket(ctx, env, b, term))
	}
	o.nativeUndo(ctx, env, term)
	return errors.Join(errs...)
}

func (o SwiftOptions) undoBucket(ctx context.Context, env map[string]string, b string, term io.Writer) error {
	bucket := env["bucket_"+b]
	s3, err := o.s3(env, b)
	if err != nil {
		return fmt.Errorf("undo %s: %w", bucket, err)
	}
	// mine lists the check's objects, or says the bucket or the key is gone.
	mine := func() ([]listedObject, bool, error) {
		a, err := s3.Do(ctx, live.S3Call{Op: live.S3ListObjectVersions, Bucket: bucket})
		switch {
		case err != nil:
			return nil, false, fmt.Errorf("undo %s: %w", bucket, err)
		case removedAlready(a):
			return nil, true, nil
		case a.Status < 200 || a.Status > 299:
			return nil, false, fmt.Errorf("undo %s: ListObjectVersions answered %d %s", bucket, a.Status, a.Code)
		}
		var l versionList
		// Another root (an <Error> answered 200, review r1) is not an empty listing.
		if xml.Unmarshal(a.Body, &l) != nil || l.XMLName.Local != "ListVersionsResult" {
			return nil, false, fmt.Errorf("undo %s: the listing is not a ListVersionsResult", bucket)
		}
		if l.Truncated {
			return nil, false, fmt.Errorf("undo %s: the listing is truncated (more than one page)", bucket)
		}
		return slices.DeleteFunc(append(l.Versions, l.Markers...), func(v listedObject) bool {
			return !strings.HasPrefix(v.Key, SwiftPrefix) || v.Key == SwiftSeed
		}), false, nil
	}
	found, gone, err := mine()
	if err != nil {
		return err
	}
	if gone {
		fmt.Fprintf(term, "LZ-LIVE undo %s already removed (bucket or cleanup key gone)\n", bucket)
		return nil
	}
	var errs []error
	removed := 0
	// By key, once each: the buckets are unversioned (main.tf), and the cleanup policy holds no
	// s3:DeleteObjectVersion (not an accepted action, tests/fixtures/ovhcloud/s3-policy-actions.json).
	var keys []string
	for _, v := range found {
		if !slices.Contains(keys, v.Key) {
			keys = append(keys, v.Key)
		}
	}
	for _, key := range keys {
		a, err := s3.Do(ctx, live.S3Call{Op: live.S3DeleteObject, Bucket: bucket, Key: key})
		switch {
		case err != nil:
			// The checker's error names the key; the key is listing text (review r2).
			errs = append(errs, fmt.Errorf("undo %s: DeleteObject %s gave no answer", bucket, keyName(key, env)))
		case a.Status == http.StatusNotFound:
		case a.Status < 200 || a.Status > 299:
			errs = append(errs, fmt.Errorf("undo %s: DeleteObject %s answered %d %s", bucket, keyName(key, env), a.Status, a.Code))
		default:
			removed++
		}
	}
	left, gone, err := mine()
	switch {
	case err != nil:
		return errors.Join(append(errs, err)...)
	case gone:
		fmt.Fprintf(term, "LZ-LIVE undo %s already removed (bucket or cleanup key gone)\n", bucket)
		return errors.Join(errs...)
	case len(left) > 0:
		var names []string
		for _, v := range left {
			names = append(names, keyName(v.Key, env))
		}
		errs = append(errs, fmt.Errorf("undo %s: %s still listed after the delete", bucket, strings.Join(names, ", ")))
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	fmt.Fprintf(term, "LZ-LIVE undo %s removed %d objects under %s\n", bucket, removed, SwiftPrefix)
	return nil
}

// safeKey is an object key under the prefix that Undo may name: lower-case letters, digits and
// dashes after it, as the check's own keys are.
var safeKey = regexp.MustCompile(`^lzprobe-swift-reach/[a-z0-9-]{1,64}$`)

// keyName is how an error names a listed key: the key when it is safe-shaped and holds no secret
// of the run, else a placeholder (a listing is server text; review r2).
func keyName(key string, env map[string]string) string {
	if !safeKey.MatchString(key) {
		return "an object of another shape"
	}
	for _, k := range swiftKeys {
		if !slices.Contains(swiftPublic, k) && env[k] != "" && (strings.Contains(key, env[k]) || strings.Contains(strings.ToLower(env[k]), strings.TrimPrefix(key, SwiftPrefix))) {
			return "an object of another shape"
		}
	}
	return key
}

// sweepRounds bounds the list-and-bulk-delete rounds that empty one bucket (one listing holds at
// most 1000 objects, kb/api/v1/cloud.json).
const sweepRounds = 3

// sweep is the check's Sweep (data-model.md *Probe checks*): it deletes, by listing the run's
// exact names with the sandbox admin, the buckets and users the root may have created, whether or
// not an output or a state entry exists; each bucket is emptied first, and one it cannot empty is
// kept and named (coordinator decision 2026-10-16). A failed listing or delete does not stop the
// others; the second listing decides what is left.
func (o SwiftOptions) sweep(ctx context.Context, runID, project string, term io.Writer) error {
	if !live.RunIDPattern.MatchString(runID) {
		return errors.New("sweep swift-reach: the run id is not a run id")
	}
	if !projectID.MatchString(project) {
		return errors.New("sweep swift-reach: the project is not a project id")
	}
	buckets, users := []string{SwiftBucket(runID, "a"), SwiftBucket(runID, "b")}, SwiftUsers(runID)
	admin, err := live.NewAPIChecker(ctx, o.API, o.Admin)
	if err != nil {
		return fmt.Errorf("sweep swift-reach: admin identity: %w", err)
	}
	// do sends a call on the project; a non-2xx answer is an error, but a 404 to a delete (gone
	// already) or to a region's bucket listing where the region offers no bucket service.
	do := func(call live.APICall) (live.APIAnswer, error) {
		call.Project = project
		a, err := admin.Do(ctx, call)
		absent := a.Status == http.StatusNotFound && (a.NoBucketService || call.Op == live.APIDeleteBucket || call.Op == live.APIDeleteUser)
		if err == nil && (a.Status < 200 || a.Status > 299) && !absent {
			err = fmt.Errorf("%s answered %d %s", call.Op, a.Status, strings.Join(a.Actions, ","))
		}
		return a, err
	}
	// list finds the run's buckets (region → names) and users (id → description).
	list := func() (map[string][]string, map[string]string, []error) {
		var errs []error
		found, people := map[string][]string{}, map[string]string{}
		if a, err := do(live.APICall{Op: live.APIListRegions}); err != nil {
			errs = append(errs, err)
		} else {
			for _, r := range a.Names {
				b, err := do(live.APICall{Op: live.APIListBuckets, Region: r})
				if err != nil {
					errs = append(errs, fmt.Errorf("region %s: %w", r, err))
				}
				for _, n := range b.Names {
					if slices.Contains(buckets, n) {
						found[r] = append(found[r], n)
					}
				}
			}
		}
		if a, err := do(live.APICall{Op: live.APIListUsers}); err != nil {
			errs = append(errs, err)
		} else {
			for _, u := range a.Users {
				if slices.Contains(users, u.Description) {
					people[u.ID] = u.Description
				}
			}
		}
		return found, people, errs
	}
	found, people, errs := list()
	removed := 0
	for _, r := range slices.Sorted(maps.Keys(found)) {
		for _, b := range found[r] {
			if err := empty(do, r, b); err != nil {
				errs = append(errs, fmt.Errorf("bucket %s not deleted: %w", b, err))
				continue
			}
			if _, err := do(live.APICall{Op: live.APIDeleteBucket, Region: r, Name: b}); err != nil {
				errs = append(errs, fmt.Errorf("bucket %s: %w", b, err))
				continue
			}
			removed++
		}
	}
	for _, id := range slices.Sorted(maps.Keys(people)) {
		if _, err := do(live.APICall{Op: live.APIDeleteUser, ID: id}); err != nil {
			errs = append(errs, fmt.Errorf("user %s: %w", people[id], err))
			continue
		}
		removed++
	}
	o.nativeSweep(ctx, runID, project, term)
	found, people, lerrs := list()
	errs = append(errs, lerrs...)
	for _, r := range slices.Sorted(maps.Keys(found)) {
		errs = append(errs, fmt.Errorf("bucket %s (%s) still listed", strings.Join(found[r], ", "), r))
	}
	for _, id := range slices.Sorted(maps.Keys(people)) {
		errs = append(errs, fmt.Errorf("user %s still listed", people[id]))
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("sweep swift-reach %s: %w", runID, err)
	}
	fmt.Fprintf(term, "LZ-LIVE sweep swift-reach removed %d buckets and users of run %s\n", removed, runID)
	return nil
}

// empty deletes every object version of a bucket, in rounds, until a listing shows none.
func empty(do func(live.APICall) (live.APIAnswer, error), region, bucket string) error {
	for range sweepRounds {
		a, err := do(live.APICall{Op: live.APIListObjects, Region: region, Name: bucket})
		if err != nil {
			return err
		}
		if len(a.Objects) == 0 {
			return nil
		}
		objs := a.Objects[:min(len(a.Objects), 1000)]
		for i := range objs {
			// An unversioned object lists as version "null": deleted by key (UNVERIFIED whether
			// the API would also take "null").
			if objs[i].VersionID == "null" {
				objs[i].VersionID = ""
			}
		}
		if _, err := do(live.APICall{Op: live.APIBulkDeleteObjects, Region: region, Name: bucket, Objects: objs}); err != nil {
			return err
		}
	}
	return fmt.Errorf("it still holds objects after %d rounds of deletes", sweepRounds)
}
