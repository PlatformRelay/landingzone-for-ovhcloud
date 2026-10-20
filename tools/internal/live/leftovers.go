package live

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Leftover check G9 (FR-011, research R12 *Leftover kind matrix*): after destroy, independent
// listings of every resource type the slice or its probes create are reconciled against the
// inventory, the retained instances' states and the admin exemption. It fails closed: a listing
// error, a missing API client, an unparseable listing or a created type outside the matrix is
// `fail`. It lists through lz-live's own read-only API client (T084): ovhcloud 0.15.0 has no `api`
// command (premise P18 refuted, evidence/T009.md).
// The parser follows the API response schemas in kb/api (synthetic listings, T054); T065 qualified
// it on captured listings, which T087 replaced by those of the plan-only run 20261007T133803Z-aeee
// (tests/fixtures/ovhcloud/captured/; its provenance.json names the kinds still uncaptured). That
// run recorded the region details (listing ovh_cloud_project_region), which qualify the per-region
// storage rule (withoutBucketService) on the project's 18 regions.

// Lister returns one page of an OVHcloud API GET listing. cursor "" asks for the first page; next
// is "" on the last page.
type Lister interface {
	Get(ctx context.Context, path, cursor string) (body []byte, next string, err error)
}

// Project is a public cloud project the slice uses, with its IAM URN (resource tags).
type Project struct {
	ID  string
	URN string
}

// Exemption is the admin exemption: exactly the lz-sandbox-admin client id (sandbox.env
// OVH_CLIENT_ID) and its policy id (account.env LZ_ADMIN_POLICY_ID). Never matched by name.
type Exemption struct {
	ClientID string
	PolicyID string
}

// LoadExemption reads the exemption from <configRoot>/sandbox.env and
// <configRoot>/accounts/<account>/account.env (credential files: private, regular, own); both ids
// are required.
func LoadExemption(configRoot, account string) (Exemption, error) {
	sb, err := ReadCredentialFile(filepath.Join(configRoot, "sandbox.env"))
	if err != nil {
		return Exemption{}, err
	}
	dir, err := AccountDir(configRoot, account)
	if err != nil {
		return Exemption{}, err
	}
	acc, err := ReadCredentialFile(filepath.Join(dir, "account.env"))
	if err != nil {
		return Exemption{}, err
	}
	ex := Exemption{ClientID: sb["OVH_CLIENT_ID"], PolicyID: acc["LZ_ADMIN_POLICY_ID"]}
	if ex.ClientID == "" || ex.PolicyID == "" {
		return Exemption{}, errors.New("admin exemption incomplete: OVH_CLIENT_ID in sandbox.env and LZ_ADMIN_POLICY_ID in account.env are required")
	}
	return ex, nil
}

// LeftoverCheck lists every matrix kind.
type LeftoverCheck struct {
	Lister   Lister // tests; nil lists through API with Cred
	Projects []Project
	Prefix   string              // name prefix of what the run creates ("<org>-", "lzprobe-")
	RunID    string              // the run id; a name, id or tag value holding it matches
	Retained map[string][]string // provider type -> ids held in retained instances' states
	Exempt   Exemption
	// API and Cred are lz-live's own API client and the credential the run binds: when Lister is
	// nil the check lists through them, GET only (T083, T084).
	API  API
	Cred Credential
	// Red is the run's redactor (FR-004): when non-nil, the bearer token the check obtains is
	// added to it before the first listing.
	Red *Redactor
	// Backoff is the wait before each retry of a transient listing failure (spec 007 FR-001): nil
	// is defaultListingBackoff, an empty non-nil slice retries nothing.
	Backoff []time.Duration
	// Budget is the run's shared retry budget (spec 006 FR-008); nil admits no wait.
	Budget *retryBudget
	// Clock is the time a retry reads and waits on; nil is wallClock{}.
	Clock waitClock
	// Term receives one line per retry; nil discards them.
	Term io.Writer
	// Before is provider type -> the ids a probe's baseline listed before its first apply (spec
	// 007 FR-006), consulted for namelessKinds only: an id there is not a candidate, an id absent
	// is. nil, or no entry for a kind, leaves the inventory and run-id matching for that kind.
	Before map[string][]string
	// beforeProject is the project Before was listed in (the probe's recorded project): Before
	// judges only that project's resources, and another project's keep the matching above, since
	// a probe creates nothing outside its project. "" judges every project's (tests).
	beforeProject string
	// lent is the retry the runner lent the run's Sweep (spec 007 T027): when set, Check retries
	// with it, so the Sweep's waits spent its budget and its attempts are in the report. nil: a
	// retry of the check's own.
	lent *listRetry
}

// namelessKinds are the kinds whose resources carry no run-derived name or tag, so a probe's
// baseline lists them before its first apply (spec 007 FR-006).
var namelessKinds = []string{tAlert}

// probeBaseline lists the namelessKinds of the run's recorded project through check's lister and
// writes baseline.json to the record directory and its recovery copy under state/baselines/
// (data-model *Probe baseline*), both synced, before the first apply; a listing that failed on
// every attempt is written with complete false and its errors, which it returns. The record copy
// goes first and the recovery copy only for a complete listing; neither is ever rewritten. A
// complete baseline becomes the run's Before.
func (s *session) probeBaseline(ctx context.Context, check LeftoverCheck) error {
	project := s.r.Vars["TF_VAR_project_id"]
	b, listErr := listBaseline(ctx, check, project)
	b.RunID = s.r.ID
	raw, err := json.Marshal(b)
	if err != nil {
		return err
	}
	raw = []byte(s.red.Redact(string(raw)))
	if err := writeOnce(filepath.Join(s.r.Dir, baselineFile), raw); err != nil {
		return fmt.Errorf("probe baseline of run %s: %w", s.r.ID, err)
	}
	if listErr != nil {
		return listErr
	}
	if err := writeRecoveryCopy(s.r.baseline.root, s.r.baseline.rel, raw); err != nil {
		return fmt.Errorf("probe baseline of run %s, recovery copy: %w", s.r.ID, err)
	}
	s.r.Leftovers.Before, s.r.Leftovers.beforeProject = b.Listed, project
	return nil
}

// listBaseline lists the alerts of project (the one nameless kind, namelessKinds) with check's
// lister, retry, clock and budget, and returns the baseline without its run id; its errors name
// the project as <project>, never by id (the record holds only its digest).
func listBaseline(ctx context.Context, check LeftoverCheck, project string) (Baseline, error) {
	var errs []string
	b := Baseline{ProjectSHA256: projectDigest(project), Listed: map[string][]string{}, Errors: []string{}}
	if src, err := check.source(); err != nil {
		errs = append(errs, err.Error())
	} else {
		l := lister{ctx: ctx, l: src, errs: &errs, rec: map[string][]json.RawMessage{}, try: check.retry()}
		if base, ok := l.under("/cloud/project", project); ok {
			if ids, ok := l.strings(base + "/alerting"); ok {
				b.Listed[tAlert] = append([]string{}, ids...)
			}
		}
	}
	for _, e := range errs {
		b.Errors = append(b.Errors, strings.ReplaceAll(e, project, "<project>"))
	}
	b.Complete = len(b.Errors) == 0
	if !b.Complete {
		return b, fmt.Errorf("probe baseline: the alert listing failed, nothing created: %s", strings.Join(b.Errors, "; "))
	}
	return b, nil
}

// source is the check's lister: Lister, or lz-live's API client with the run's redactor.
func (c LeftoverCheck) source() (Lister, error) {
	if c.Lister != nil {
		return c.Lister, nil
	}
	if c.API.BaseURL == "" {
		return nil, errors.New("no API client")
	}
	al := newAPILister(c.API, c.Cred)
	al.red = c.Red
	return al, nil
}

// defaultListingBackoff is the leftover check's retry waits (spec 007 FR-001): three attempts per
// request.
var defaultListingBackoff = []time.Duration{5 * time.Second, 15 * time.Second}

// transientListing classifies a listing failure (spec 007 FR-001, data-model *Listing attempt*):
// a status of 429 or 500-599 and a transport failure are retried; any other status, and anything
// else (an answer that does not decode), are not. The token endpoint's 429 is a 4xx and, as every
// 4xx from it, never retried (FR-001).
func transientListing(err error) (class string, retry bool) {
	var se *statusError
	if errors.As(err, &se) {
		tooMany := se.code == http.StatusTooManyRequests && se.path != tokenPath
		return "status", tooMany || se.code >= 500 && se.code <= 599
	}
	var te *transportError
	if errors.As(err, &te) {
		return "transport", true
	}
	return "decode", false
}

// transportError is a request that failed in transport: while sending, while reading the
// answer's body, or the token request's transport (spec 007 FR-001). Its text names the kind of
// failure from err's type only and never quotes err, whose text may carry what the answer chose
// (a Location header) or the request (spec 007 T006: pinned by TestTransportErrorText).
type transportError struct {
	path string
	err  error
}

func (e *transportError) Error() string {
	return fmt.Sprintf("%s: transport failure (%s)", requestName(e.path), transportKind(e.err))
}

func (e *transportError) Unwrap() error { return e.err }

// transportKind is a transport failure's kind, by type: a failed Do is a *url.Error, a failed
// body read is not.
func transportKind(err error) string {
	var ue *url.Error
	switch {
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ue) && ue.Timeout():
		return "timed out"
	case errors.As(err, &ue):
		return "request failed"
	}
	return "reading the answer failed"
}

// tokenPath is the path a failed token request is recorded under (data-model *Listing attempt*).
const tokenPath = "token"

// requestName names a request in an error: "token endpoint" for the token request (the text
// binding.go gives it), else "GET <path>".
func requestName(path string) string {
	if path == tokenPath {
		return "token endpoint"
	}
	return "GET " + path
}

// tokenRefused is the cached refusal of the token request, returned to a later listing without a
// request: no attempt of that listing.
type tokenRefused struct{ err error }

func (e *tokenRefused) Error() string { return e.err.Error() }

func (e *tokenRefused) Unwrap() error { return e.err }

// tokenFailure is a failed token request as the leftover check classifies it: a non-200 answer is
// a *statusError and a failed send a *transportError, both with path "token" (data-model *Fixed
// signatures*); anything else keeps quietToken's text.
func tokenFailure(err error) error {
	var ce *callError
	switch {
	case !errors.As(err, &ce):
		return &tokenAnswer{quietToken(err)}
	case ce.code != 0:
		return &statusError{path: tokenPath, code: ce.code}
	case ce.sent != nil:
		return &transportError{path: tokenPath, err: ce.sent}
	}
	return &tokenAnswer{err}
}

// tokenAnswer is a token answer the check cannot use (no access token, or a body that does not
// decode): not retried, recorded under "token".
type tokenAnswer struct{ err error }

func (e *tokenAnswer) Error() string { return e.err.Error() }

func (e *tokenAnswer) Unwrap() error { return e.err }

// ListingAttempt is one failed request of the leftover check (spec 007 FR-002), kept in
// leftovers.json under attempts.
type ListingAttempt struct {
	Path    string `json:"path"`
	Attempt int    `json:"attempt"`
	Status  int    `json:"status,omitempty"`
	Class   string `json:"class"`
	Retried bool   `json:"retried"`
	Reason  string `json:"reason,omitempty"`
}

// Leftover is a listed resource that should not exist.
type Leftover struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	// Before marks a leftover the chain's baseline record listed (there before the run's first
	// apply, T047); it never decides whether a resource is a leftover.
	Before bool `json:"before_run,omitempty"`
}

// Report is the outcome of a leftover check: "pass" only with no leftover and no error.
type Report struct {
	Outcome   string                       `json:"outcome"`
	Leftovers []Leftover                   `json:"leftovers"`
	Errors    []string                     `json:"errors"`
	Listings  map[string][]json.RawMessage `json:"-"`
	// Listed is provider type -> the id of every resource listed (the chain's baseline record).
	Listed map[string][]string `json:"-"`
	// Attempts is every failed request of the check (spec 007 FR-002).
	Attempts []ListingAttempt `json:"attempts"`
}

const (
	tStorage = "ovh_cloud_project_storage"
	tNetwork = "ovh_cloud_project_network_private"
	tSubnet  = "ovh_cloud_project_network_private_subnet"
	tUser    = "ovh_cloud_project_user"
	tCred    = "ovh_cloud_project_user_s3_credential"
	tS3Pol   = "ovh_cloud_project_user_s3_policy"
	tClient  = "ovh_me_api_oauth2_client"
	tPolicy  = "ovh_iam_policy"
	tGroup   = "ovh_me_identity_group"
	tTags    = "ovh_iam_resource_tags"
	tAlert   = "ovh_cloud_project_alerting"
	tQuota   = "ovh_cloud_quota"
	tProject = "ovh_cloud_project"
	// tRegion records the project's region details (not a matrix kind): the evidence of which
	// regions offer Object Storage (T065).
	tRegion = "ovh_cloud_project_region"
	// tSwift is the Swift container kind (spec 003 T041): the swift-reach check's control
	// containers, which it creates with the sandbox admin through the API (research.md *S3-API
	// buckets are not on the Swift plane*). The provider has no Swift container resource, so the
	// kind is not a provider type, never in an inventory and not in matrix (research R12's provider
	// types); it is listed with them, and a container named Prefix… is a leftover.
	tSwift = "swift_container"
)

var matrix = []string{tStorage, tNetwork, tSubnet, tUser, tCred, tS3Pol, tClient, tPolicy, tGroup, tTags, tAlert, tQuota, tProject}

// MatrixTypes returns the provider types of the kind matrix.
func MatrixTypes() []string { return slices.Clone(matrix) }

// item is one listed resource; a child (subnet, S3 credential, S3 policy) points to its parent.
type item struct {
	typ, id, name string
	values        []string
	parent        *item
	project       string // the project of a nameless kind's resource (Before is one project's)
}

// apiLister is the leftover check's read-only client of the OVHcloud API: the bearer token of the
// run's credential (client-credentials grant, binding.go), GET only, redirects never followed.
// /iam/* paths exist only in APIv2 (kb/api/v2/iam.json), so they go to the v2 sibling of the v1
// base. Pagination follows apiv2.mdx *Pagination*: the cursor in X-Pagination-Cursor, the next one
// from X-Pagination-Cursor-Next, none on the last page (for v1 listings UNVERIFIED: a v1 listing
// without the header is one page).
type apiLister struct {
	api  API
	cred Credential
	tok  string
	// tokErr is the first token failure: a rejected credential is not sent again per listing.
	tokErr error
	// red, when non-nil, gets the token as soon as it is obtained, before any request carries it
	// (FR-004): every record and stream the run redacts through it hides the token from then on.
	red *Redactor
}

// quiet is a failed request's error without what Go quotes from the answer (an unparseable
// Location header, which a hostile endpoint can fill with the token or the secret): only the
// kind of failure remains. An error that is not a transport error passes unchanged (Do fails only
// with one). A listing's failed request is a *transportError, whose text is as quiet.
func quiet(what string, err error) error {
	var ue *url.Error
	switch {
	case !errors.As(err, &ue):
		return err
	case ue.Timeout():
		return fmt.Errorf("%s: timed out", what)
	case errors.Is(err, context.Canceled):
		return fmt.Errorf("%s: cancelled", what)
	}
	return fmt.Errorf("%s: request failed", what)
}

// quietToken is a failed token request's error with nothing the answer chose: binding.go's quiet
// call error (the status or transport class, e.g. "token endpoint answered 401") kept, anything
// else only named.
func quietToken(err error) error {
	var ce *callError
	if errors.As(err, &ce) {
		return err
	}
	return errors.New("token endpoint: no usable token in the answer")
}

// maxListing bounds one listing answer; a longer one is an error, never cut short.
const maxListing = 8 << 20

func newAPILister(a API, c Credential) *apiLister { return &apiLister{api: a, cred: c} }

func (l *apiLister) Get(ctx context.Context, path, cursor string) ([]byte, string, error) {
	return l.request(ctx, http.MethodGet, path, cursor)
}

// request sends one API request; any method but GET is refused before a request (token
// included). No error carries the answer body, the token or the credential.
func (l *apiLister) request(ctx context.Context, method, path, cursor string) ([]byte, string, error) {
	if method != http.MethodGet {
		return nil, "", fmt.Errorf("read-only API client: %s %s refused", method, path)
	}
	if l.api.BaseURL == "" || l.api.TokenURL == "" || !strings.HasSuffix(l.api.BaseURL, "/v1") {
		return nil, "", errors.New("read-only API client: no API endpoint")
	}
	if l.tokErr != nil {
		return nil, "", &tokenRefused{l.tokErr}
	}
	if l.tok == "" {
		tok, err := l.api.token(ctx, l.cred)
		if err != nil {
			// A transient failure is asked again by the next attempt; any other stays cached.
			err = tokenFailure(err)
			if _, retry := transientListing(err); !retry {
				l.tokErr = err
			}
			return nil, "", err
		}
		if l.red != nil {
			l.red.Add(tok)
		}
		l.tok = tok
	}
	base := l.api.BaseURL
	if strings.HasPrefix(path, "/iam/") {
		base = strings.TrimSuffix(base, "/v1") + "/v2"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return nil, "", fmt.Errorf("GET %s: %w", path, err)
	}
	req.Header.Set("Authorization", "Bearer "+l.tok)
	req.Header.Set("Accept", "application/json")
	if cursor != "" {
		req.Header.Set("X-Pagination-Cursor", cursor)
	}
	return l.send(req, path)
}

// send sends req and reads its answer. The status decides first (FR-001: only 429 and 5xx are
// retried): a non-2xx answer is a *statusError whatever happens to its body, and so is a 3xx whose
// Location Go cannot parse (Do fails on it before CheckRedirect, see statusRecorder); any other
// failure of Do or of the body read is a *transportError.
func (l *apiLister) send(req *http.Request, path string) ([]byte, string, error) {
	c := l.api.client()
	rec := &statusRecorder{next: c.Transport}
	if rec.next == nil {
		rec.next = http.DefaultTransport
	}
	c.Transport = rec
	resp, err := c.Do(req)
	switch {
	case err != nil && rec.code/100 == 3:
		return nil, "", &statusError{path: path, code: rec.code}
	case err != nil:
		return nil, "", &transportError{path: path, err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, "", &statusError{path: path, code: resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxListing+1))
	if err != nil {
		return nil, "", &transportError{path: path, err: err}
	}
	if len(body) > maxListing {
		return nil, "", fmt.Errorf("GET %s: answer longer than %d bytes", path, maxListing)
	}
	return body, resp.Header.Get("X-Pagination-Cursor-Next"), nil
}

// statusError is a non-2xx answer: its status code without the answer's text.
type statusError struct {
	path string
	code int
}

func (e *statusError) Error() string {
	return fmt.Sprintf("%s answered %d", requestName(e.path), e.code)
}

// notFound reports a 404 answer.
func notFound(err error) bool {
	var se *statusError
	return errors.As(err, &se) && se.code == http.StatusNotFound
}

// maxListingPages bounds the pages of one listing: a cursor that never ends is an error.
const maxListingPages = 1000

type lister struct {
	ctx  context.Context
	l    Lister
	errs *[]string
	rec  map[string][]json.RawMessage
	try  *listRetry
}

func (l lister) fail(format string, a ...any) { *l.errs = append(*l.errs, fmt.Sprintf(format, a...)) }

// listRetry is the leftover check's retry of a failed request (spec 007 FR-001) and its record of
// every failed attempt (FR-002). Not safe for concurrent use: the check lists one request at a
// time.
type listRetry struct {
	backoff  []time.Duration
	budget   *retryBudget
	clock    waitClock
	term     io.Writer
	red      *Redactor
	attempts []ListingAttempt
	// tokenAttempts counts the token request's failed attempts across the check: it is one
	// request, whichever listing needs it, so its attempts never spend a listing's (FR-001).
	tokenAttempts int
	// tokenErr is the token request's failure once no retry follows it; every later request gets
	// it without being sent.
	tokenErr error
	// last is the number of the attempt that answered each path last, for a failure found in the
	// answer's elements.
	last map[string]int
}

// retry is the check's listRetry, with the defaults of data-model *Fixed signatures*.
func (c LeftoverCheck) retry() *listRetry {
	t := &listRetry{backoff: c.Backoff, budget: c.Budget, clock: c.Clock, term: c.Term, red: c.Red, last: map[string]int{}}
	if t.backoff == nil {
		t.backoff = defaultListingBackoff
	}
	if t.clock == nil {
		t.clock = wallClock{}
	}
	if t.term == nil {
		t.term = io.Discard
	}
	return t
}

// lentOrOwn is the retry the runner lent the run's Sweep (T027), else a retry of the check's own.
func (c LeftoverCheck) lentOrOwn() *listRetry {
	if c.lent != nil {
		return c.lent
	}
	return c.retry()
}

// readRetryKey keys the retry a probe check's Sweep gets in its context (APIChecker.Read).
type readRetryKey struct{}

// ReadRetry is ctx carrying c's retry, with a budget ending at end (the run's reserve floor), for
// APIChecker.Read (spec 007 T027): a Sweep's reads are retried as the check's listings are. The
// runner lends a Sweep the leftover check's own retry instead (Execute's destroy), so both spend
// one budget on one clock and leftovers.json records both; tests of a Sweep build theirs here.
func (c LeftoverCheck) ReadRetry(ctx context.Context, end time.Time) context.Context {
	c.Budget = newRetryBudget(end, minRetryReserve)
	return context.WithValue(ctx, readRetryKey{}, c.retry())
}

// get is one request of the check, asked again after each transient failure the retry admits;
// it returns the last attempt's number. A first page's 404 that absentOK admits, and the cached
// token refusal (no request was sent), are returned unrecorded.
func (l lister) get(path, cursor string, absentOK bool) ([]byte, string, int, error) {
	t := l.try
	attempt := 1
	for {
		body, next, err := t.ask(l.ctx, l.l, path, cursor)
		if err == nil {
			t.last[path] = attempt
		}
		var cached *tokenRefused
		if err == nil || absentOK && notFound(err) || errors.As(err, &cached) {
			return body, next, attempt, err
		}
		token, err := t.failed(l.ctx, path, attempt, err)
		if err != nil {
			return nil, "", attempt, err
		}
		if !token {
			attempt++
		}
	}
}

// ask sends one request, unless the token request has failed for good, and keeps its duration in
// the shared budget: the slowest request so far is time kept for the next one (spec 006
// data-model *Retry budget*).
func (t *listRetry) ask(ctx context.Context, l Lister, path, cursor string) ([]byte, string, error) {
	if t.tokenErr != nil {
		return nil, "", &tokenRefused{t.tokenErr}
	}
	start := t.clock.Now()
	body, next, err := l.Get(ctx, path, cursor)
	if t.budget != nil {
		t.budget.keep(t.clock.Now().Sub(start))
	}
	return body, next, err
}

// failed records a failed attempt and, when the retry admits it, waits for the next one (err
// nil); otherwise it returns the error to report. token reports a failure of the token request,
// which counts its own attempts.
func (t *listRetry) failed(ctx context.Context, path string, attempt int, err error) (token bool, _ error) {
	a, retry := t.row(path, attempt, err)
	a.Reason = t.refusal(ctx, a.Attempt, retry)
	if a.Reason == "" {
		a.Reason = t.wait(ctx, a)
	}
	a.Retried = a.Reason == ""
	t.attempts = append(t.attempts, a)
	err = t.reported(ctx, err, a)
	token = a.Path == tokenPath
	if token && err != nil {
		t.tokenErr = err
	}
	return token, err
}

// row is the record of a failed attempt and whether its failure is transient; a failure of the
// token request is recorded under "token" with the token request's own attempt number.
func (t *listRetry) row(path string, attempt int, err error) (ListingAttempt, bool) {
	class, retry := transientListing(err)
	a := ListingAttempt{Path: path, Attempt: attempt, Class: class}
	var se *statusError
	var te *transportError
	var ta *tokenAnswer
	switch {
	case errors.As(err, &se):
		a.Status = se.code
		a.Path = tokenOr(se.path, path)
	case errors.As(err, &te):
		a.Path = tokenOr(te.path, path)
	case errors.As(err, &ta):
		a.Path = tokenPath
	}
	if a.Path == tokenPath {
		t.tokenAttempts++
		a.Attempt = t.tokenAttempts
	}
	return a, retry
}

// reported is the error a failed attempt that no retry follows reports (nil when one follows).
func (t *listRetry) reported(ctx context.Context, err error, a ListingAttempt) error {
	of := len(t.backoff) + 1
	switch a.Reason {
	case "":
		return nil
	case "permanent":
		return err
	case "cancelled":
		return fmt.Errorf("%w; the retry ended: %v", err, context.Cause(ctx))
	case "attempts":
		if a.Attempt > 1 {
			return fmt.Errorf("%w (attempt %d of %d)", err, a.Attempt, of)
		}
		return err
	}
	return fmt.Errorf("%w (attempt %d of %d, not retried: %s)", err, a.Attempt, of, a.Reason)
}

// ended is why ctx ended: "deadline" when its cause is the deadline (a timeout, or the destroy
// context's at), else "cancelled".
func ended(ctx context.Context) string {
	if errors.Is(context.Cause(ctx), context.DeadlineExceeded) {
		return "deadline"
	}
	return "cancelled"
}

// tokenOr is "token" for a failed token request, else the listing's path.
func tokenOr(errPath, path string) string {
	if errPath == tokenPath {
		return tokenPath
	}
	return path
}

// refusal is why no retry follows attempt ("" when one may): a failure that is not transient, no
// attempt left, a cancelled context, a wait reaching the context's deadline, or a wait the shared
// budget refuses (a nil budget admits none).
func (t *listRetry) refusal(ctx context.Context, attempt int, retry bool) string {
	switch {
	case !retry:
		return "permanent"
	case attempt > len(t.backoff):
		return "attempts"
	case ctx.Err() != nil:
		return ended(ctx)
	}
	wait, now := t.backoff[attempt-1], t.clock.Now()
	if end, ok := ctx.Deadline(); ok && !end.After(now.Add(wait)) {
		return "deadline"
	}
	if t.budget == nil || !t.budget.admit(now, wait) {
		return "budget"
	}
	return ""
}

// wait prints the retry line and waits before the attempt after a; the context's end ends the
// wait, also when it ends as the wait fires (no further attempt then).
func (t *listRetry) wait(ctx context.Context, a ListingAttempt) string {
	wait := t.backoff[a.Attempt-1]
	what := a.Class
	if a.Status != 0 {
		what = fmt.Sprint(a.Status)
	}
	path := a.Path
	if t.red != nil {
		path = t.red.Redact(path)
	}
	fmt.Fprintf(t.term, "LZ-LIVE retry listing %s in %v: %s (attempt %d of %d)\n", path, wait, what, a.Attempt+1, len(t.backoff)+1)
	select {
	case <-t.clock.After(wait):
		if ctx.Err() != nil {
			return ended(ctx)
		}
		return ""
	case <-ctx.Done():
		return ended(ctx)
	}
}

// refused records a failure found in an answer (one that does not decode, or a page bound): never
// retried.
func (t *listRetry) refused(path string, attempt int, class string) {
	t.attempts = append(t.attempts, ListingAttempt{Path: path, Attempt: attempt, Class: class, Reason: "permanent"})
}

// all reads every page of an array listing; a cursor that repeats or more than maxListingPages
// pages is an error.
func (l lister) all(path string) ([]json.RawMessage, bool) {
	out, ok, _ := l.pages(path, false)
	return out, ok
}

// pages is all; with absentOK, a 404 to the first page is reported as absent (no error recorded).
// A failed page is asked again at its own cursor (FR-001).
func (l lister) pages(path string, absentOK bool) ([]json.RawMessage, bool, bool) {
	var out []json.RawMessage
	cursor := ""
	seen := map[string]bool{}
	for pages := 1; ; pages++ {
		page, next, attempt, ok, absent := l.page(path, cursor, absentOK && pages == 1)
		if !ok {
			return nil, false, absent
		}
		out = append(out, page...)
		switch {
		case next == "":
			return out, true, false
		case seen[next]:
			l.try.refused(path, attempt, "bounds")
			l.fail("listing %s: the next-page cursor repeats", path)
			return nil, false, false
		case pages >= maxListingPages:
			l.try.refused(path, attempt, "bounds")
			l.fail("listing %s: more than %d pages", path, maxListingPages)
			return nil, false, false
		}
		seen[next] = true
		cursor = next
	}
}

// page is one page of an array listing and the number of the attempt that answered it; ok is
// false on an error (recorded) or an admitted absence (absent).
func (l lister) page(path, cursor string, absentOK bool) (page []json.RawMessage, next string, attempt int, ok, absent bool) {
	body, next, attempt, err := l.get(path, cursor, absentOK)
	if err != nil && absentOK && notFound(err) {
		return nil, "", attempt, false, true
	}
	if err != nil {
		l.fail("listing %s: %v", path, err)
		return nil, "", attempt, false, false
	}
	if json.Unmarshal(body, &page) != nil {
		l.try.refused(path, attempt, "decode")
		l.fail("listing %s: not a JSON array", path)
		return nil, "", attempt, false, false
	}
	return page, next, attempt, true, false
}

// under is parent/<id> with id escaped as one path segment, so a listed id cannot re-route the
// GET; an empty or dot-segment id is refused with an error naming parent.
func (l lister) under(parent, id string) (string, bool) {
	if id == "" || id == "." || id == ".." {
		l.fail("listing %s: id %q is not a path segment", parent, id)
		return "", false
	}
	return parent + "/" + url.PathEscape(id), true
}

func (l lister) object(path string, v any) bool {
	_, ok := l.objectRaw(path, v)
	return ok
}

// objectRaw is object returning the answer as well.
func (l lister) objectRaw(path string, v any) (json.RawMessage, bool) {
	body, _, attempt, err := l.get(path, "", false)
	if err != nil {
		l.fail("listing %s: %v", path, err)
		return nil, false
	}
	if len(bytes.TrimSpace(body)) == 0 || bytes.TrimSpace(body)[0] != '{' || json.Unmarshal(body, v) != nil {
		l.try.refused(path, attempt, "decode")
		l.fail("listing %s: not a JSON object", path)
		return nil, false
	}
	return json.RawMessage(bytes.TrimSpace(body)), true
}

// s3Services are the storage-s3 service names the region details of run 20261007T133803Z-aeee
// name (evidence/T087.md), and whether a region offering one answers a bucket listing. Cold Archive
// (RBX-ARCHIVE, whose only Object Storage service it is) answers the listing 404; its containers
// are not a matrix kind in this slice (research R12).
var s3Services = map[string]bool{
	"storage-s3-standard":    true,
	"storage-s3-high-perf":   true,
	"storage-s3-coldarchive": false,
}

// withoutBucketService reports whether a region detail (cloud.Region, kb/api/v1/cloud.json:67596)
// shows that the region offers no bucket service: its services (cloud.Component[], cloud.json:67651,
// 64709) are all named and none is storage-s3-standard, storage-s3-high-perf or a storage-s3 name
// not in s3Services (fail closed: a new Object Storage class counts as a bucket service until a
// capture shows otherwise); an empty services list counts as none. Only then may the region's
// storage listing answer 404 (first page). The rule decides only whether a 404 is acceptable: every
// region's storage is asked, so a 200 is never hidden. A wrong rule either turns a 404 into an
// error, or (a bucket service under a name outside storage-s3, e.g. Swift's "storage", or another
// letter case) accepts a 404 from a region that offers buckets; the captured details show neither.
func withoutBucketService(services []struct {
	Name string `json:"name"`
}) bool {
	for _, s := range services {
		if s.Name == "" {
			return false
		}
		if bucket, known := s3Services[s.Name]; bucket || !known && strings.HasPrefix(s.Name, "storage-s3") {
			return false
		}
	}
	return true
}

func (l lister) strings(path string) ([]string, bool) {
	raw, ok := l.all(path)
	if !ok {
		return nil, false
	}
	var out []string
	for _, r := range raw {
		var s string
		if json.Unmarshal(r, &s) != nil || s == "" {
			l.try.refused(path, l.try.last[path], "decode")
			l.fail("listing %s: not a list of ids", path)
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

// decode decodes each element, requiring the id field to be present.
func decode[T any](l lister, path string, raw []json.RawMessage, id func(T) string) ([]T, bool) {
	var out []T
	for _, r := range raw {
		var v T
		if json.Unmarshal(r, &v) != nil || id(v) == "" {
			l.try.refused(path, l.try.last[path], "decode")
			l.fail("listing %s: unparseable element", path)
			return nil, false
		}
		out = append(out, v)
	}
	return out, true
}

func (l lister) record(typ string, raw ...json.RawMessage) {
	if l.rec[typ] == nil {
		l.rec[typ] = []json.RawMessage{}
	}
	l.rec[typ] = append(l.rec[typ], raw...)
}

// Check lists every kind and reconciles it with inv. A listed resource is a candidate when its
// name starts with Prefix, its id, name or tag value holds RunID, its (type, id) is in the
// inventory, or its parent is a candidate; a candidate is a leftover unless it (or, for a child,
// its parent) is retained, or it is the exempt admin client or policy (by id).
func (c LeftoverCheck) Check(ctx context.Context, inv []InventoryEntry) Report {
	rep := Report{Listings: map[string][]json.RawMessage{}}
	var errs []string
	fail := func(format string, a ...any) { errs = append(errs, fmt.Sprintf(format, a...)) }
	created := map[string]bool{}
	for _, e := range inv {
		if !slices.Contains(matrix, e.Type) {
			fail("created type %s (%s) is outside the kind matrix", e.Type, e.Address)
		}
		if e.ID != "" {
			created[e.Type+"\x00"+e.ID] = true
		}
	}
	src, err := c.source()
	if err != nil {
		fail("%v", err)
	}
	if len(c.Projects) == 0 {
		fail("no project to list")
	}
	var items []*item
	// The retry's attempts are reported also when no lister could be built: a Sweep lent this
	// retry may have recorded some (T027).
	try := c.lentOrOwn()
	if src != nil {
		items = c.list(lister{ctx: ctx, l: src, errs: &errs, rec: rep.Listings, try: try})
	}
	rep.Attempts = try.attempts
	rep.Listed = map[string][]string{}
	for _, it := range items {
		rep.Listed[it.typ] = append(rep.Listed[it.typ], it.id)
	}
	retained := map[string]bool{}
	for typ, ids := range c.Retained {
		for _, id := range ids {
			retained[typ+"\x00"+id] = true
		}
	}
	candidate := func(it *item) bool { return c.candidate(it, created) }
	var isRetained func(*item) bool
	isRetained = func(it *item) bool {
		if it.parent != nil {
			return isRetained(it.parent)
		}
		return retained[it.typ+"\x00"+it.id]
	}
	for _, it := range items {
		if !candidate(it) || isRetained(it) {
			continue
		}
		if it.typ == tClient && c.Exempt.ClientID != "" && it.id == c.Exempt.ClientID {
			continue
		}
		if it.typ == tPolicy && c.Exempt.PolicyID != "" && it.id == c.Exempt.PolicyID {
			continue
		}
		rep.Leftovers = append(rep.Leftovers, Leftover{Type: it.typ, ID: it.id, Name: it.name})
	}
	rep.Errors = errs
	rep.Outcome = "pass"
	if len(errs) > 0 || len(rep.Leftovers) > 0 {
		rep.Outcome = "fail"
	}
	return rep
}

// candidate reports whether a listed resource may be the run's: its name starts with Prefix, its
// id, name or a tag value holds RunID, it is in the inventory (created), or its parent is a
// candidate. For a nameless kind for which Before has an entry (a probe's baseline, spec 007
// FR-006) an id of beforeProject absent from that entry is a candidate and one in it is not;
// Before never decides a named kind or another project's resource, and nil or no entry leaves
// that kind to the matching above.
func (c LeftoverCheck) candidate(it *item, created map[string]bool) bool {
	if c.Prefix != "" && strings.HasPrefix(it.name, c.Prefix) {
		return true
	}
	if c.RunID != "" && slices.ContainsFunc(append([]string{it.id, it.name}, it.values...), func(v string) bool { return strings.Contains(v, c.RunID) }) {
		return true
	}
	if created[it.typ+"\x00"+it.id] {
		return true
	}
	if ids, ok := c.Before[it.typ]; ok && slices.Contains(namelessKinds, it.typ) && (c.beforeProject == "" || it.project == c.beforeProject) {
		return !slices.Contains(ids, it.id)
	}
	return it.parent != nil && c.candidate(it.parent, created)
}

// listContainers lists the project's Swift containers (GET /cloud/project/{serviceName}/storage,
// cloud.storage.Container[], kb api/v1/cloud.json "Get SWIFT storage containers"; spec 003 T041).
// An element without an id or a name is unparseable: the kind has no inventory entry, so only
// its name can make it the run's (review r1).
func listContainers(l lister, base, project string, add func(*item) *item) {
	path := base + "/storage"
	raw, ok := l.all(path)
	if !ok {
		return
	}
	l.record(tSwift, raw...)
	cs, _ := decode(l, path, raw, func(c struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}) string {
		if c.Name == "" {
			return ""
		}
		return c.ID
	})
	for _, c := range cs {
		// project: a container id is believed to be per region, not per project (review r1).
		add(&item{typ: tSwift, id: c.ID, name: c.Name, project: project})
	}
}

// list lists every kind. Buckets are listed in every region of each project; the storage
// listing of a region without a bucket service answers 404 (compute regions, live run
// 20261007T125154Z-14a6, evidence/T084.md; the Cold Archive region, run 20261007T133803Z-aeee,
// evidence/T087.md), which is accepted only when the region's detail shows no bucket service
// (withoutBucketService); any other failure is an error. A region detail without
// services is an error, and so is a project in which no region's storage listing succeeded: the
// check assumes every project lists at least one region with a bucket service (standard or
// high-perf) whose listing succeeds; a Cold Archive region does not count (true of the sandbox
// project, run 20261007T133803Z-aeee: ten such regions; UNVERIFIED for a fresh project).
func (c LeftoverCheck) list(l lister) []*item {
	var items []*item
	add := func(it *item) *item { items = append(items, it); return it }
	for _, p := range c.Projects {
		base, ok := l.under("/cloud/project", p.ID)
		if !ok {
			continue
		}
		if regions, ok := l.strings(base + "/region"); ok {
			listed := 0
			for _, r := range regions {
				rp, ok := l.under(base+"/region", r)
				if !ok {
					continue
				}
				var reg struct {
					Services *[]struct {
						Name string `json:"name"`
					} `json:"services"`
				}
				detail, ok := l.objectRaw(rp, &reg)
				if !ok {
					continue
				}
				l.record(tRegion, detail)
				if reg.Services == nil {
					l.fail("listing %s: no services field", rp)
					continue
				}
				path := rp + "/storage"
				raw, ok, absent := l.pages(path, withoutBucketService(*reg.Services))
				if absent || !ok {
					continue
				}
				listed++
				l.record(tStorage, raw...)
				bs, _ := decode(l, path, raw, func(b struct {
					Name string `json:"name"`
				}) string {
					return b.Name
				})
				for _, b := range bs {
					add(&item{typ: tStorage, id: b.Name, name: b.Name})
				}
			}
			if listed == 0 {
				l.fail("listing %s: no region's bucket listing succeeded", base+"/region")
			}
		}
		type named struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if raw, ok := l.all(base + "/network/private"); ok {
			l.record(tNetwork, raw...)
			nets, _ := decode(l, base+"/network/private", raw, func(n named) string { return n.ID })
			for _, n := range nets {
				net := add(&item{typ: tNetwork, id: n.ID, name: n.Name})
				np, ok := l.under(base+"/network/private", n.ID)
				if !ok {
					continue
				}
				path := np + "/subnet"
				if raw, ok := l.all(path); ok {
					l.record(tSubnet, raw...)
					subs, _ := decode(l, path, raw, func(s struct {
						ID   string `json:"id"`
						CIDR string `json:"cidr"`
					}) string {
						return s.ID
					})
					for _, s := range subs {
						add(&item{typ: tSubnet, id: s.ID, name: s.CIDR, parent: net})
					}
				}
			}
		}
		listContainers(l, base, p.ID, add)
		if raw, ok := l.all(base + "/user"); ok {
			l.record(tUser, raw...)
			users, _ := decode(l, base+"/user", raw, func(u struct {
				ID          json.Number `json:"id"`
				Description string      `json:"description"`
				Username    string      `json:"username"`
			}) string {
				return u.ID.String()
			})
			for _, u := range users {
				name := u.Description
				if name == "" {
					name = u.Username
				}
				usr := add(&item{typ: tUser, id: u.ID.String(), name: name})
				up, ok := l.under(base+"/user", u.ID.String())
				if !ok {
					continue
				}
				path := up + "/s3Credentials"
				if raw, ok := l.all(path); ok {
					l.record(tCred, raw...)
					creds, _ := decode(l, path, raw, func(c struct {
						Access string `json:"access"`
					}) string {
						return c.Access
					})
					for _, cr := range creds {
						add(&item{typ: tCred, id: cr.Access, parent: usr})
					}
				}
				var pol struct {
					Policy *string `json:"policy"`
				}
				ppath := up + "/policy"
				if l.object(ppath, &pol) {
					if pol.Policy == nil {
						l.fail("listing %s: no policy field", ppath)
					} else if *pol.Policy != "" {
						raw, _ := json.Marshal(pol)
						l.record(tS3Pol, raw)
						add(&item{typ: tS3Pol, id: u.ID.String(), parent: usr})
					}
				}
			}
		}
		if ids, ok := l.strings(base + "/alerting"); ok {
			for _, id := range ids {
				var a named
				if ap, ok := l.under(base+"/alerting", id); ok && l.object(ap, &a) {
					raw, _ := json.Marshal(a)
					l.record(tAlert, raw)
					add(&item{typ: tAlert, id: id, name: a.Name, project: p.ID})
				}
			}
		}
		// iam.resource.Resource marks tags "required": false, "canBeNull": true
		// (kb/api/v2/iam.json:2661-2667): an absent or null tags field is no tags (the untagged
		// project of run 20261007T125154Z-14a6 answered without one). Tags of another type fail.
		var res struct {
			Tags map[string]string `json:"tags"`
		}
		if rp, ok := l.under("/iam/resource", p.URN); ok && l.object(rp, &res) {
			if res.Tags == nil {
				res.Tags = map[string]string{}
			}
			raw, _ := json.Marshal(res.Tags)
			l.record(tTags, raw)
			for k, v := range res.Tags {
				add(&item{typ: tTags, id: k, name: k, values: []string{v}})
			}
		}
	}
	if ids, ok := l.strings("/me/api/oauth2/client"); ok {
		for _, id := range ids {
			var cl struct {
				ClientID string `json:"clientId"`
				Name     string `json:"name"`
			}
			if cp, ok := l.under("/me/api/oauth2/client", id); ok && l.object(cp, &cl) {
				raw, _ := json.Marshal(cl)
				l.record(tClient, raw)
				add(&item{typ: tClient, id: id, name: cl.Name})
			}
		}
	}
	if raw, ok := l.all("/iam/policy"); ok {
		l.record(tPolicy, raw...)
		pols, _ := decode(l, "/iam/policy", raw, func(p struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}) string {
			return p.ID
		})
		for _, p := range pols {
			add(&item{typ: tPolicy, id: p.ID, name: p.Name})
		}
	}
	if names, ok := l.strings("/me/identity/group"); ok {
		for _, n := range names {
			var g struct {
				Name string `json:"name"`
			}
			if gp, ok := l.under("/me/identity/group", n); ok && l.object(gp, &g) {
				raw, _ := json.Marshal(g)
				l.record(tGroup, raw)
				add(&item{typ: tGroup, id: n, name: g.Name})
			}
		}
	}
	return items
}
