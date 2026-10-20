package live

// OVHcloud API calls of a probe check (spec 005 T100): the storage-iam probe observes, with the
// probe identities its root publishes, whether a tag-conditioned read is allowed and whether the
// tenant identity is denied GET /me and an IAM write, and its undo deletes the policy such a write
// may have created, with the sandbox admin's credential. The bootstrap's client (ovhapi.go) reads
// and deletes what the bootstrap needs; this checker is a separate client with its own gate: a
// closed set of named operations, each with its method and path fixed and its path
// parameters checked by shape before any request. Spec 003 T030 adds the seven a probe's Sweep
// needs to find and delete a run's buckets and cloud project users by listing (the region,
// bucket, object and user listings, the bulk object delete, the bucket and user deletes; kb
// api/v1/cloud.json). Spec 003 T032 adds the five the token-residual check needs: create, read,
// list by name and delete an OAuth2 client (kb api/v1/me.json oauth2.clientRequest, oauth2.client,
// oauth2.clientSecret), and the one read its policy grants, GET /cloud/project/{project}
// (publicCloudProject:apiovh:get, kb api/v1/cloud.json); a created client's secret is the one
// secret an answer keeps, for the caller's token request only. Spec 003 T034 adds the four the
// iam-urn check needs (kb api/v2/iam.json): read a policy (its probed read,
// account:apiovh:iam/policy/get), list IAM resources by name, replace a resource's tags (its
// probed write, account:apiovh:iam/resource/edit) and delete one tag key (its cleanup); a listed
// resource keeps its URN and tags, each of iam.resource.AddTag's shape. Spec 003 T041 adds the
// three the swift-reach check's Swift-native control needs: create, list and recursively delete a
// Swift container (kb api/v1/cloud.json /cloud/project/{serviceName}/storage). It never follows a redirect, caps every answer
// at maxBody, and keeps of an answer only the status, the IAM actions a refusal names (action-
// shaped tokens of `details.unauthorizedActionsByIAM` and of the message "missing IAM
// permissions: …"; kb manage-and-operate/iam/troubleshooting.mdx:21-31, T010 rows 10 and 14), a
// created policy's uuid and createdAt (T034), the ids of the listed policies of the asked name or of an id asked to
// be kept (T101), with their createdAt (T113). No error carries answer text, a token or a secret.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// APIOp is an OVHcloud API operation the checker may send.
type APIOp string

// The operations a probe check sends.
const (
	APIGetStorage   APIOp = "GetStorage"   // GET /cloud/project/{project}/region/{region}/storage/{name} (v1)
	APIGetMe        APIOp = "GetMe"        // GET /me (v1)
	APICreatePolicy APIOp = "CreatePolicy" // POST /iam/policy (v2)
	APIListPolicies APIOp = "ListPolicies" // GET /iam/policy (v2), every page, the policies of one name
	APIDeletePolicy APIOp = "DeletePolicy" // DELETE /iam/policy/{id} (v2)

	// A probe's Sweep (spec 003 T030), all v1 under /cloud/project/{project}.
	APIListRegions       APIOp = "ListRegions"       // GET …/region
	APIListBuckets       APIOp = "ListBuckets"       // GET …/region/{region}/storage
	APIListObjects       APIOp = "ListObjects"       // GET …/region/{region}/storage/{name}/object?withVersions=true
	APIBulkDeleteObjects APIOp = "BulkDeleteObjects" // POST …/region/{region}/storage/{name}/bulkDeleteObjects
	APIDeleteBucket      APIOp = "DeleteBucket"      // DELETE …/region/{region}/storage/{name}
	APIListUsers         APIOp = "ListUsers"         // GET …/user
	APIDeleteUser        APIOp = "DeleteUser"        // DELETE …/user/{id}

	// The token-residual check (spec 003 T032).
	APICreateClient APIOp = "CreateClient" // POST /me/api/oauth2/client (v1), flow CLIENT_CREDENTIALS
	APIGetClient    APIOp = "GetClient"    // GET /me/api/oauth2/client/{id} (v1)
	APIListClients  APIOp = "ListClients"  // GET /me/api/oauth2/client, then each client: those of one name
	APIDeleteClient APIOp = "DeleteClient" // DELETE /me/api/oauth2/client/{id} (v1)
	APIGetProject   APIOp = "GetProject"   // GET /cloud/project/{project} (v1)

	// The iam-urn check (spec 003 T034), all v2.
	APIGetPolicy         APIOp = "GetPolicy"         // GET /iam/policy/{id}
	APIListResources     APIOp = "ListResources"     // GET /iam/resource?resourceName={name}, every page: the resources of that name
	APIPutResourceTags   APIOp = "PutResourceTags"   // PUT /iam/resource/{urn} with {"tags": …}
	APIDeleteResourceTag APIOp = "DeleteResourceTag" // DELETE /iam/resource/{urn}/tag/{key}

	// The budget-alert check (spec 007 FR-007), v1 under /cloud/project/{project}; ID is one path
	// segment, a uuid (kb api/v1/cloud.json: GET …/alerting answers uuid[], GET …/alerting/{id}
	// cloud.alerting.Alerting, DELETE …/alerting/{id} void).
	APIListAlerts  APIOp = "ListAlerts"  // GET …/alerting
	APIGetAlert    APIOp = "GetAlert"    // GET …/alerting/{id}
	APIDeleteAlert APIOp = "DeleteAlert" // DELETE …/alerting/{id}

	// The swift-reach check's Swift-native control containers (spec 003 T041), v1 under
	// /cloud/project/{project}: kb api/v1/cloud.json /cloud/project/{serviceName}/storage, "Create/
	// Get/Delete SWIFT container" (actions publicCloudProject:apiovh:storage/create|get|delete).
	APICreateContainer APIOp = "CreateContainer" // POST …/storage, cloud.ProjectStorageCreation with archive false
	APIListContainers  APIOp = "ListContainers"  // GET …/storage, cloud.storage.Container[]
	APIDeleteContainer APIOp = "DeleteContainer" // DELETE …/storage/{containerId}?recursive=true
)

// APICall is one operation; each takes only its own parts.
type APICall struct {
	Op      APIOp
	Project string // GetStorage, GetProject and the Sweep operations: a public cloud project id
	Region  string // GetStorage, ListBuckets and the bucket operations: a region (GRA)
	// Name: GetStorage and the bucket operations: a bucket name; ListPolicies: the policy name
	// kept; CreateClient: the client's name (and description); ListClients: the client name kept.
	Name string
	ID   string // DeletePolicy: a policy uuid; DeleteUser: a user id (digits); GetClient, DeleteClient: a client id
	Body []byte // CreatePolicy: the policy, JSON
	// Keep (ListPolicies, optional): policy uuids kept whatever their name (T101 review r2).
	Keep []string
	// Objects (BulkDeleteObjects): the objects to delete, 1 to maxBulk (cloud.StorageObjectDelete).
	Objects []APIObject
	URN     string            // PutResourceTags, DeleteResourceTag: an IAM resource URN
	Tags    map[string]string // PutResourceTags: every tag the resource is to carry
	Key     string            // DeleteResourceTag: the tag key
}

// APIResource is one IAM resource of a listing (iam.resource.Resource): its URN, name and tags.
type APIResource struct {
	URN, Name string
	Tags      map[string]string // the listed tags whose key is of iam.resource.AddTag's shape (a value of another shape: "")
	// Partial: the listing also showed tags of another shape, dropped here (their text never
	// leaves the checker); a write of Tags back would remove them (T034 review r1).
	Partial bool
}

// APIPolicy is one entry of a policy listing.
type APIPolicy struct {
	ID   string
	Name string
}

// APIObject is one object (version) of a bucket: its key and, when listed, its version id.
type APIObject struct {
	Key       string `json:"key"`
	VersionID string `json:"versionId,omitempty"`
}

// APIClient is an OAuth2 client: its id, name and IAM identity URN (empty when the answer's is
// not identity-shaped).
type APIClient struct {
	ID, Name, Identity string
}

// APIUser is one cloud project user of a listing: its id (digits) and description.
type APIUser struct {
	ID          string
	Description string
}

// APIAnswer is what the checker keeps of an answer.
type APIAnswer struct {
	Status   int
	Actions  []string    // a non-2xx answer's IAM actions, sorted, when it names any
	ID       string      // a 2xx CreatePolicy answer's policy id, when a uuid
	Policies []APIPolicy // a 2xx ListPolicies answer: the policies of the asked name, every page
	// CreatedAt (a 2xx ListPolicies answer, or a 2xx CreatePolicy answer for its id, T034): by id, the createdAt of each policy in Policies that has
	// a readable one (iam.json iam.policy.Response.createdAt, RFC 3339; T113).
	CreatedAt map[string]time.Time
	// Names (a 2xx ListRegions or ListBuckets answer): the region or bucket names, as listed.
	Names []string
	// NoBucketService (a ListBuckets 404): the region's detail shows no bucket service, so the
	// 404 means no bucket there (the leftover check's rule, withoutBucketService).
	NoBucketService bool
	Objects         []APIObject // a 2xx ListObjects answer: every listed object version
	Users           []APIUser   // a 2xx ListUsers answer
	// Clients: a 2xx GetClient answer (one), a 2xx ListClients answer (those of the asked name),
	// or a 2xx CreateClient answer (one, its id only).
	Clients []APIClient
	// Secret (a 2xx CreateClient answer): the created client's secret. It goes to the token
	// endpoint and nowhere else; no error or record carries it.
	Secret string
	// Resources (a 2xx ListResources answer): the listed resources of the asked name, every page.
	Resources []APIResource
	// Email (a 2xx GetMe answer): the account's own address (nichandle.Nichandle email), which
	// the budget-alert check compares an alert's recipients with; it is never printed.
	Email string
	// Alerts (a 2xx ListAlerts answer): the listed alert ids, each a uuid.
	Alerts []string
	// Alert (a 2xx GetAlert answer): the alert read.
	Alert *APIAlert
	// Containers (a 2xx ListContainers answer): every listed Swift container, its id and name as
	// listed (server text: a caller acts on, and names, only its own exact names).
	Containers []APIContainer
}

// APIContainer is one Swift container of a listing (cloud.storage.Container): its id and name. A
// non-string id or name makes the listing unreadable.
type APIContainer struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// APIAlert is a budget alert as read (cloud.alerting.Alerting): its id, monthly threshold, delay
// in seconds and recipients (email, deprecated, and emails).
type APIAlert struct {
	ID               string
	MonthlyThreshold int64
	Delay            int64
	Email            string
	Emails           []string
}

// maxPolicyBody bounds a CreatePolicy body: a probe's policy is a few hundred bytes.
const maxPolicyBody = 64 << 10

// maxBulk bounds a BulkDeleteObjects call (cloud.json lists at most 1000 objects a page).
const maxBulk = 1000

var (
	apiProject = regexp.MustCompile(`^[0-9a-f]{32}$`)
	// apiProjectSegment is the project of the budget-alert operations: one path segment of
	// letters, digits and dashes (no dot, slash or escape), as the leftover check takes the
	// project probe.env records (a live project id is 32 hex: apiProject).
	apiProjectSegment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,63}$`)
	apiRegion         = regexp.MustCompile(`^[A-Z]+[0-9]*$`)
	apiUUID           = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	// apiRegionName is a region as a project's region listing names it, dashes included
	// (RBX-ARCHIVE, evidence/T087.md of spec 005); the Sweep operations take it.
	apiRegionName = regexp.MustCompile(`^[A-Z][A-Z0-9]*(-[A-Z0-9]+)*$`)
	// apiContainerID is a Swift container id (cloud.storage.Container id, a string whose form the
	// schema leaves open; UNVERIFIED until spec 003 T042): base64-like text that does not start
	// with a dot, sent as one escaped path segment. Another shape is refused, never sent.
	apiContainerID = regexp.MustCompile(`^[A-Za-z0-9+/=_-][A-Za-z0-9+/=._-]{0,511}$`)
	// apiUserID is a cloud project user id (cloud.user.User id, a long).
	apiUserID = regexp.MustCompile(`^[0-9]{1,20}$`)
	// apiPolicyName is the policy name a listing is asked for: printable ASCII, no space.
	apiPolicyName = regexp.MustCompile(`^[\x21-\x7e]{1,256}$`)
	// apiClientID is an OAuth2 client id, a path segment (EU.0000000000000001 in the bootstrap's
	// fixture testdata/bootstrap/world.json; me.json types it a string only).
	apiClientID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	// apiIdentity is an OAuth2 client's IAM identity URN
	// (urn:v1:eu:identity:credential:<account>/oauth2-<client id>, world.json).
	apiIdentity = regexp.MustCompile(`^urn:v1:[a-z]{2,8}:identity:credential:[A-Za-z0-9._-]{1,64}/oauth2-[A-Za-z0-9._-]{1,128}$`)
	// apiResourceURN is an IAM resource URN, urn:v1:<region>:resource:<type>:<id> (provider docs
	// resources/iam_resource_tags.md); ValidResourceURN adds: no dot segment.
	apiResourceURN = regexp.MustCompile(`^urn:v1:[a-z]{2,8}:resource:[A-Za-z][A-Za-z0-9]*:[A-Za-z0-9._:/@-]{1,512}$`)
	// apiTagKey and apiTagValue are a resource tag's key and value (kb api/v2/iam.json,
	// iam.resource.AddTag: '^[a-zA-Z0-9_.:/=+@-]{1,128}' and '{0,256}').
	apiTagKey   = regexp.MustCompile(`^[a-zA-Z0-9_.:/=+@-]{1,128}$`)
	apiTagValue = regexp.MustCompile(`^[a-zA-Z0-9_.:/=+@-]{0,256}$`)
	// iamAction is an IAM action as OVHcloud names them (vps:apiovh:reboot,
	// publicCloudProject:apiovh:region/storage/get).
	iamAction = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*:apiovh:[A-Za-z0-9/_.*-]{1,200}$`)
)

// APIChecker sends a probe check's OVHcloud API calls as one OAuth2 identity.
type APIChecker struct {
	b      bearerClient
	secret string
}

// NewAPIChecker fetches the identity's token; an error names the token endpoint, never a value.
func NewAPIChecker(ctx context.Context, a API, c Credential) (*APIChecker, error) {
	b, err := newBearer(ctx, a, c)
	if err != nil {
		return nil, err
	}
	return &APIChecker{b: b, secret: c.ClientSecret}, nil
}

// TokenExpiry is a token answer's expires_in: Given when it is a JSON integer (seconds, of any
// sign); absent, null, a string or a fraction is not given.
type TokenExpiry struct {
	Seconds int64
	Given   bool
}

// NewAPICheckerExpiry is NewAPIChecker with its token answer's expires_in (spec 003 T032: one
// token, whose lifetime the check judges).
func NewAPICheckerExpiry(ctx context.Context, a API, c Credential) (*APIChecker, TokenExpiry, error) {
	b, raw, err := newBearerExpiry(ctx, a, c)
	if err != nil {
		return nil, TokenExpiry{}, err
	}
	var exp TokenExpiry
	if n, perr := strconv.ParseInt(string(bytes.TrimSpace(raw)), 10, 64); perr == nil {
		exp = TokenExpiry{Seconds: n, Given: true}
	}
	return &APIChecker{b: b, secret: c.ClientSecret}, exp, nil
}

// Carries reports whether s holds the identity's token or secret (spec 003 T034: a check that
// holds several identities filters each answer against all of them).
func (c *APIChecker) Carries(s string) bool {
	return c.b.tok != "" && strings.Contains(s, c.b.tok) || c.secret != "" && strings.Contains(s, c.secret)
}

// Do sends one call. Only a refused call or a missing or unusable answer is an error; any status
// is an answer.
func (c *APIChecker) Do(ctx context.Context, call APICall) (APIAnswer, error) {
	if err := apiGate(call); err != nil {
		return APIAnswer{}, err
	}
	var a APIAnswer
	var err error
	switch call.Op {
	case APIGetStorage:
		a, _, err = c.send(ctx, http.MethodGet, "/cloud/project/"+call.Project+"/region/"+call.Region+"/storage/"+call.Name, nil, "", nil)
	case APIGetMe:
		a, err = c.me(ctx)
	case APICreatePolicy:
		a, _, err = c.send(ctx, http.MethodPost, "/iam/policy", call.Body, "", nil)
	case APIDeletePolicy:
		a, _, err = c.send(ctx, http.MethodDelete, "/iam/policy/"+call.ID, nil, "", nil)
	case APIListPolicies:
		a, err = c.list(ctx, call.Name, call.Keep)
	case APIGetProject:
		a, _, err = c.send(ctx, http.MethodGet, "/cloud/project/"+call.Project, nil, "", nil)
	case APICreateClient, APIGetClient, APIListClients, APIDeleteClient:
		a, err = c.clientOp(ctx, call)
	case APIGetPolicy:
		a, _, err = c.send(ctx, http.MethodGet, "/iam/policy/"+call.ID, nil, "", nil)
	case APIListResources:
		a, err = c.resources(ctx, call.Name)
	case APIPutResourceTags:
		body, _ := json.Marshal(map[string]map[string]string{"tags": call.Tags})
		a, err = c.status(ctx, http.MethodPut, "/iam/resource/"+url.PathEscape(call.URN), body, "PUT /iam/resource/{urn}")
	case APIDeleteResourceTag:
		a, err = c.status(ctx, http.MethodDelete, "/iam/resource/"+url.PathEscape(call.URN)+"/tag/"+url.PathEscape(call.Key), nil, "DELETE /iam/resource/{urn}/tag/{key}")
	case APIListAlerts, APIGetAlert, APIDeleteAlert:
		a, err = c.alertOp(ctx, call)
	case APICreateContainer, APIListContainers, APIDeleteContainer:
		a, err = c.containerOp(ctx, call)
	default:
		a, err = c.sweepOp(ctx, call)
	}
	return a, err
}

// readRoutes are the reads Read retries (spec 007 T027: the budget-alert sweep's), each with the
// route its attempts are recorded under: placeholders, never the project or the alert id.
var readRoutes = map[APIOp]string{
	APIGetMe:      "/me",
	APIListAlerts: alertRoute,
	APIGetAlert:   alertRoute + "/{id}",
}

// Read sends a read as Do does, through the leftover check's retry the runner lends a probe
// check's Sweep in ctx (LeftoverCheck.ReadRetry, spec 007 T027): an answer of 429 or 500-599 and a
// transport failure are asked again with that retry's backoff, budget and clock; any other non-2xx
// answer (but a GetAlert 404, the alert gone) fails without a retry; every failed attempt is
// recorded with the check's (FR-001, FR-002). Without a retry in ctx it sends once. Only the reads
// of readRoutes are admitted; anything else is refused before a request.
func (c *APIChecker) Read(ctx context.Context, call APICall) (APIAnswer, error) {
	route, ok := readRoutes[call.Op]
	if !ok {
		return APIAnswer{}, fmt.Errorf("%s is not a retried read; not sent", call.Op)
	}
	t, _ := ctx.Value(readRetryKey{}).(*listRetry)
	if t == nil {
		return c.Do(ctx, call)
	}
	for attempt := 1; ; attempt++ {
		start := t.clock.Now()
		a, err := c.Do(ctx, call)
		if t.budget != nil {
			t.budget.keep(t.clock.Now().Sub(start))
		}
		if call.Op == APIGetAlert && answered(a, err) == http.StatusNotFound {
			return APIAnswer{Status: http.StatusNotFound}, nil // the alert gone (FR-007), body or not
		}
		failure := readFailure(route, a, err)
		if failure == nil {
			return a, err
		}
		if _, err := t.failed(ctx, route, attempt, failure); err != nil {
			return APIAnswer{}, err
		}
	}
}

// readFailure is a read's failure as the leftover check classifies it (transientListing), judged
// by the status first (FR-001): a non-2xx answer is a *statusError, also when its body could not be
// read, was over the bound or its redirect not parsed; a request without an answer, or a 2xx whose
// body broke off, a *transportError; any other error stays as it is (not retried). nil for a 2xx
// answer, which the caller judges (Read handles a GetAlert 404 before).
func readFailure(route string, a APIAnswer, err error) error {
	var sf *sendFailed
	switch {
	case errors.As(err, &sf) && sf.status != 0 && (sf.status < 200 || sf.status > 299):
		return &statusError{path: route, code: sf.status}
	case errors.As(err, &sf) && !sf.large:
		return &transportError{path: route, err: sf.cause}
	case err != nil:
		return err
	case a.Status >= 200 && a.Status <= 299:
		return nil
	}
	return &statusError{path: route, code: a.Status}
}

// answered is the status a read got: the answer's, or the one a failed exchange saw (0: none).
func answered(a APIAnswer, err error) int {
	var sf *sendFailed
	if errors.As(err, &sf) {
		return sf.status
	}
	return a.Status
}

// sendFailed is a request that got no usable answer (exchange: the send or the body read failed,
// or the answer was over maxBody, large): its text names the request only; cause is what failed,
// for transportError's kind; status is the answer's status when one came (a body that broke off
// or was too long, a redirect Go could not parse), else 0. It does not unwrap to cause, so a
// caller sees what it saw before (quiet's text, nothing beneath it).
type sendFailed struct {
	text   string
	cause  error
	status int
	large  bool
}

func (e *sendFailed) Error() string { return e.text }

// sweepOp sends one of the Sweep operations (T030) and reads its answer: a listing keeps only the
// names, object keys and version ids, or user ids and descriptions, and an answer of another shape
// is an error; a delete keeps its status. A ListBuckets 404 asks the region's detail whether the
// region offers a bucket service, as the leftover check does.
func (c *APIChecker) sweepOp(ctx context.Context, call APICall) (APIAnswer, error) {
	project := "/cloud/project/" + call.Project
	bucket := project + "/region/" + call.Region + "/storage/" + call.Name
	method, path := http.MethodGet, ""
	var body []byte
	switch call.Op {
	case APIListRegions:
		path = project + "/region"
	case APIListBuckets:
		path = project + "/region/" + call.Region + "/storage"
	case APIListObjects:
		path = bucket + "/object?withVersions=true"
	case APIBulkDeleteObjects:
		method, path = http.MethodPost, bucket+"/bulkDeleteObjects"
		body, _ = json.Marshal(map[string][]APIObject{"objects": call.Objects})
	case APIDeleteBucket:
		method, path = http.MethodDelete, bucket
	case APIListUsers:
		path = project + "/user"
	case APIDeleteUser:
		method, path = http.MethodDelete, project+"/user/"+call.ID
	}
	what := method + " " + path
	status, _, raw, err := c.exchange(ctx, method, path, body, "")
	if err != nil {
		return APIAnswer{}, err
	}
	a := APIAnswer{Status: status}
	if status < 200 || status > 299 {
		a.Actions = refusedActions(raw, c.b.tok, c.secret)
		if call.Op == APIListBuckets && status == http.StatusNotFound {
			a.NoBucketService, err = c.noBucketService(ctx, project+"/region/"+call.Region)
		}
		return a, err
	}
	if method != http.MethodGet {
		return a, nil
	}
	bad := fmt.Errorf("%s: the answer is not a %s listing", what, call.Op)
	if trimmed := bytes.TrimSpace(raw); len(trimmed) == 0 || trimmed[0] != '[' {
		return APIAnswer{}, bad
	}
	switch call.Op {
	case APIListRegions:
		var names []string
		// Region names are passed on to ListBuckets and may be printed: only region-shaped ones
		// (review r1: a listing echoing a token is not a listing).
		if json.Unmarshal(raw, &names) != nil || slices.ContainsFunc(names, func(n string) bool { return !apiRegionName.MatchString(n) }) {
			return APIAnswer{}, bad
		}
		a.Names = names
	case APIListBuckets:
		type bucketEntry struct {
			Name string `json:"name"`
		}
		var entries []*bucketEntry
		if json.Unmarshal(raw, &entries) != nil || slices.ContainsFunc(entries, func(e *bucketEntry) bool { return e == nil || e.Name == "" }) {
			return APIAnswer{}, bad
		}
		a.Names = []string{}
		for _, e := range entries {
			a.Names = append(a.Names, e.Name)
		}
	case APIListObjects:
		var entries []*APIObject
		if json.Unmarshal(raw, &entries) != nil || slices.ContainsFunc(entries, func(e *APIObject) bool {
			return e == nil || validObject(*e) != nil
		}) {
			return APIAnswer{}, bad
		}
		a.Objects = []APIObject{}
		for _, e := range entries {
			a.Objects = append(a.Objects, *e)
		}
	case APIListUsers:
		var entries []*struct {
			ID          json.Number `json:"id"`
			Description string      `json:"description"`
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if dec.Decode(&entries) != nil {
			return APIAnswer{}, bad
		}
		a.Users = []APIUser{}
		for _, e := range entries {
			if e == nil || !apiUserID.MatchString(e.ID.String()) {
				return APIAnswer{}, bad
			}
			a.Users = append(a.Users, APIUser{ID: e.ID.String(), Description: e.Description})
		}
	}
	return a, nil
}

// me sends GET /me and keeps, of a 2xx answer, the account's address (Email; "" when the answer
// has none); a non-2xx answer keeps its status and the actions it names, as send does.
func (c *APIChecker) me(ctx context.Context) (APIAnswer, error) {
	status, _, raw, err := c.exchange(ctx, http.MethodGet, "/me", nil, "")
	if err != nil {
		return APIAnswer{}, err
	}
	a := APIAnswer{Status: status}
	if status < 200 || status > 299 {
		a.Actions = refusedActions(raw, c.b.tok, c.secret)
		return a, nil
	}
	var me struct {
		Email string `json:"email"`
	}
	if json.Unmarshal(raw, &me) == nil {
		a.Email = me.Email
	}
	return a, nil
}

// alertOp sends one of the budget-alert operations (spec 007 FR-007) and reads its answer: a
// listing keeps the alert ids, each a uuid (another shape is an error: it could be neither read
// nor deleted); a read keeps the alert, whose id must be the asked one and whose threshold and
// delay must be present; a delete keeps its status. A non-2xx answer keeps its status and the
// actions it names. Errors name the route (alertRoute, {id}), never the project or the alert id
// (spec 007 T027, as containerOp).
func (c *APIChecker) alertOp(ctx context.Context, call APICall) (APIAnswer, error) {
	method, path, route := http.MethodGet, "/cloud/project/"+call.Project+"/alerting", alertRoute
	if call.ID != "" {
		path, route = path+"/"+call.ID, route+"/{id}"
	}
	if call.Op == APIDeleteAlert {
		method = http.MethodDelete
	}
	what := method + " " + route
	status, _, raw, err := c.exchange(ctx, method, path, nil, "")
	var sf *sendFailed
	switch {
	case errors.As(err, &sf):
		// exchange's failure with the route in place of the path ("timed out", "unreadable answer").
		routed := *sf
		routed.text = what + strings.TrimPrefix(sf.text, method+" "+path)
		return APIAnswer{}, &routed
	case err != nil:
		// exchange's text with the route in place of the path ("bad request", "answer larger …").
		return APIAnswer{}, fmt.Errorf("%s%s", what, strings.TrimPrefix(err.Error(), method+" "+path))
	}
	a := APIAnswer{Status: status}
	if status < 200 || status > 299 {
		a.Actions = refusedActions(raw, c.b.tok, c.secret)
		return a, nil
	}
	switch call.Op {
	case APIListAlerts:
		a.Alerts, err = alertIDs(raw)
	case APIGetAlert:
		a.Alert, err = alertRead(raw, call.ID)
	}
	if err != nil {
		return APIAnswer{}, fmt.Errorf("%s: %w", what, err)
	}
	return a, nil
}

// containerOp sends one of the Swift container operations (spec 003 T041). The create sends
// cloud.ProjectStorageCreation with "archive": false (the flag is required; true makes a Cloud
// Archive container, not a Swift-native one) and keeps the status; the delete is always recursive
// (a container the check wrote to holds an object; how the API answers a non-recursive delete of a
// non-empty one is UNVERIFIED) and keeps the status; a listing keeps each container's id and name,
// and an element that is not an object with both is an error. A non-2xx answer keeps its status
// and the actions it names. Errors name the route, never the id (a listed id is server text;
// review r1).
func (c *APIChecker) containerOp(ctx context.Context, call APICall) (APIAnswer, error) {
	method, path := http.MethodGet, "/cloud/project/"+call.Project+"/storage"
	what := method + " " + path
	var body []byte
	switch call.Op {
	case APICreateContainer:
		method = http.MethodPost
		what = method + " " + path
		body, _ = json.Marshal(map[string]any{"archive": false, "containerName": call.Name, "region": call.Region})
	case APIDeleteContainer:
		what = http.MethodDelete + " " + path + "/{containerId}"
		method, path = http.MethodDelete, path+"/"+url.PathEscape(call.ID)+"?recursive=true"
	}
	status, _, raw, err := c.exchange(ctx, method, path, body, "")
	if err != nil {
		return APIAnswer{}, fmt.Errorf("%s: no answer", what)
	}
	a := APIAnswer{Status: status}
	if status < 200 || status > 299 {
		a.Actions = refusedActions(raw, c.b.tok, c.secret)
		return a, nil
	}
	if call.Op != APIListContainers {
		return a, nil
	}
	var entries []*APIContainer
	if trimmed := bytes.TrimSpace(raw); len(trimmed) == 0 || trimmed[0] != '[' || json.Unmarshal(trimmed, &entries) != nil ||
		slices.ContainsFunc(entries, func(e *APIContainer) bool { return e == nil || e.ID == "" || e.Name == "" }) {
		return APIAnswer{}, fmt.Errorf("%s: the answer is not a container listing", what)
	}
	a.Containers = []APIContainer{}
	for _, e := range entries {
		a.Containers = append(a.Containers, *e)
	}
	return a, nil
}

// alertRoute is the alert listing's route as errors and attempt records name it.
const alertRoute = "/cloud/project/{serviceName}/alerting"

// alertIDs reads a GET …/alerting answer: a JSON array of uuids.
func alertIDs(raw []byte) ([]string, error) {
	var ids []string
	if trimmed := bytes.TrimSpace(raw); len(trimmed) == 0 || trimmed[0] != '[' || json.Unmarshal(trimmed, &ids) != nil {
		return nil, fmt.Errorf("the answer is not an alert listing")
	}
	if slices.ContainsFunc(ids, func(id string) bool { return !apiUUID.MatchString(id) }) {
		return nil, fmt.Errorf("a listed alert id is not a uuid")
	}
	return ids, nil
}

// alertRead reads a GET …/alerting/{id} answer (cloud.alerting.Alerting).
func alertRead(raw []byte, id string) (*APIAlert, error) {
	var e struct {
		ID               string   `json:"id"`
		MonthlyThreshold *int64   `json:"monthlyThreshold"`
		Delay            *int64   `json:"delay"`
		Email            string   `json:"email"`
		Emails           []string `json:"emails"`
	}
	if json.Unmarshal(raw, &e) != nil || e.ID != id || e.MonthlyThreshold == nil || e.Delay == nil {
		return nil, fmt.Errorf("the answer is not the asked alert with its threshold and delay")
	}
	return &APIAlert{ID: e.ID, MonthlyThreshold: *e.MonthlyThreshold, Delay: *e.Delay, Email: e.Email, Emails: e.Emails}, nil
}

// clientPath is the path of the OAuth2 clients.
const clientPath = "/me/api/oauth2/client"

// maxClients bounds the clients ListClients reads one by one.
const maxClients = 1000

// clientOp sends one of the OAuth2 client operations (T032). A created client keeps its id and
// secret; a client read keeps its id, name and identity (when identity-shaped); a listing reads
// each listed client and keeps those of the asked name. Any non-2xx status is the answer.
func (c *APIChecker) clientOp(ctx context.Context, call APICall) (APIAnswer, error) {
	switch call.Op {
	case APICreateClient:
		body, _ := json.Marshal(map[string]any{"callbackUrls": []string{}, "description": call.Name, "flow": "CLIENT_CREDENTIALS", "name": call.Name})
		status, _, raw, err := c.exchange(ctx, http.MethodPost, clientPath, body, "")
		if err != nil {
			return APIAnswer{}, err
		}
		a := APIAnswer{Status: status}
		if status < 200 || status > 299 {
			a.Actions = refusedActions(raw, c.b.tok, c.secret)
			return a, nil
		}
		var sec struct {
			ClientID     string `json:"clientId"`
			ClientSecret string `json:"clientSecret"`
		}
		if json.Unmarshal(raw, &sec) != nil || !apiClientID.MatchString(sec.ClientID) || sec.ClientSecret == "" {
			return a, fmt.Errorf("POST %s: the answer names no usable client and secret", clientPath)
		}
		a.Clients, a.Secret = []APIClient{{ID: sec.ClientID}}, sec.ClientSecret
		return a, nil
	case APIDeleteClient:
		status, _, raw, err := c.exchange(ctx, http.MethodDelete, clientPath+"/"+call.ID, nil, "")
		if err != nil {
			return APIAnswer{}, fmt.Errorf("DELETE %s/{id}: no answer", clientPath)
		}
		a := APIAnswer{Status: status}
		if status < 200 || status > 299 {
			a.Actions = refusedActions(raw, c.b.tok, c.secret)
		}
		return a, nil
	case APIGetClient:
		a, cl, err := c.client(ctx, call.ID)
		if err == nil && a.Status >= 200 && a.Status <= 299 {
			a.Clients = []APIClient{cl}
		}
		return a, err
	}
	status, _, raw, err := c.exchange(ctx, http.MethodGet, clientPath, nil, "")
	if err != nil {
		return APIAnswer{}, err
	}
	if status < 200 || status > 299 {
		return APIAnswer{Status: status, Actions: refusedActions(raw, c.b.tok, c.secret)}, nil
	}
	var ids []string
	if trimmed := bytes.TrimSpace(raw); len(trimmed) == 0 || trimmed[0] != '[' || json.Unmarshal(trimmed, &ids) != nil {
		return APIAnswer{}, fmt.Errorf("GET %s: the answer is not a client listing", clientPath)
	}
	if len(ids) > maxClients {
		return APIAnswer{}, fmt.Errorf("GET %s: more than %d clients", clientPath, maxClients)
	}
	out := APIAnswer{Status: status, Clients: []APIClient{}}
	for _, id := range ids {
		// A listed id is server text: one that is not a client id is an error, never sent or named.
		if !apiClientID.MatchString(id) {
			return APIAnswer{}, fmt.Errorf("GET %s: a listed client id is not a client id", clientPath)
		}
		a, cl, err := c.client(ctx, id)
		switch {
		case err != nil:
			return APIAnswer{}, err
		case a.Status == http.StatusNotFound:
			// Deleted since the listing.
		case a.Status < 200 || a.Status > 299:
			return a, nil
		case cl.Name == call.Name:
			out.Clients = append(out.Clients, cl)
		}
	}
	return out, nil
}

// client reads one OAuth2 client (oauth2.client); a 2xx answer that is not that client is an
// error. Errors name the route, never the id (a listed id is server text).
func (c *APIChecker) client(ctx context.Context, id string) (APIAnswer, APIClient, error) {
	what := "GET " + clientPath + "/{id}"
	status, _, raw, err := c.exchange(ctx, http.MethodGet, clientPath+"/"+id, nil, "")
	if err != nil {
		return APIAnswer{}, APIClient{}, fmt.Errorf("%s: no answer", what)
	}
	a := APIAnswer{Status: status}
	if status < 200 || status > 299 {
		a.Actions = refusedActions(raw, c.b.tok, c.secret)
		return a, APIClient{}, nil
	}
	var cl struct {
		ClientID string `json:"clientId"`
		Name     string `json:"name"`
		Identity string `json:"identity"`
	}
	if trimmed := bytes.TrimSpace(raw); len(trimmed) == 0 || trimmed[0] != '{' || json.Unmarshal(trimmed, &cl) != nil || cl.ClientID != id {
		return APIAnswer{}, APIClient{}, fmt.Errorf("%s: the answer is not that client", what)
	}
	out := APIClient{ID: id, Name: cl.Name}
	if apiIdentity.MatchString(cl.Identity) {
		out.Identity = cl.Identity
	}
	return a, out, nil
}

// noBucketService reads a region's detail (cloud.Region) and reports whether it offers no bucket
// service (withoutBucketService); a detail it cannot read, or without services, is an error.
func (c *APIChecker) noBucketService(ctx context.Context, path string) (bool, error) {
	status, _, raw, err := c.exchange(ctx, http.MethodGet, path, nil, "")
	if err != nil {
		return false, err
	}
	var reg struct {
		Services *[]struct {
			Name string `json:"name"`
		} `json:"services"`
	}
	if status < 200 || status > 299 || json.Unmarshal(raw, &reg) != nil || reg.Services == nil {
		return false, fmt.Errorf("GET %s: no readable region detail (answered %d)", path, status)
	}
	return withoutBucketService(*reg.Services), nil
}

// validObject: a key of 1 to 1024 bytes of valid UTF-8 without control characters, and a version
// id, when there is one, as the S3 checker admits it.
func validObject(o APIObject) error {
	switch {
	case o.Key == "" || len(o.Key) > 1024 || !utf8.ValidString(o.Key) || strings.ContainsFunc(o.Key, unicode.IsControl):
		return fmt.Errorf("an object key is empty, too long or not printable")
	case o.VersionID != "" && !validVersionID(o.VersionID):
		return fmt.Errorf("an object version id is not printable ASCII")
	}
	return nil
}

// ValidResourceURN reports whether u is an IAM resource URN a check may name in a path: of
// apiResourceURN's shape (no wildcard, query or space) with no "." or ".." segment.
func ValidResourceURN(u string) bool {
	if !apiResourceURN.MatchString(u) {
		return false
	}
	for _, seg := range strings.FieldsFunc(u, func(r rune) bool { return r == '/' || r == ':' }) {
		if seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// validTags: at most maxTags tags, each key and value of iam.resource.AddTag's shape.
func validTags(tags map[string]string) bool {
	if len(tags) > maxTags {
		return false
	}
	for k, v := range tags {
		if !apiTagKey.MatchString(k) || !apiTagValue.MatchString(v) {
			return false
		}
	}
	return true
}

// maxTags bounds the tags of one resource the checker reads or writes.
const maxTags = 1000

// status sends one request whose answer is its status (and, refused, the actions it names).
func (c *APIChecker) status(ctx context.Context, method, path string, body []byte, what string) (APIAnswer, error) {
	st, _, raw, err := c.exchange(ctx, method, path, body, "")
	if err != nil {
		return APIAnswer{}, fmt.Errorf("%s: no answer", what)
	}
	a := APIAnswer{Status: st}
	if st < 200 || st > 299 {
		a.Actions = refusedActions(raw, c.b.tok, c.secret)
	}
	return a, nil
}

// resources reads every page of GET /iam/resource?resourceName=<name> (v2 pagination, as list)
// and keeps the resources of exactly that name: their URN and tags, each of its shape, or the
// listing is an error. Resources of other names are skipped whatever their shape. A refused page
// is the answer.
func (c *APIChecker) resources(ctx context.Context, name string) (APIAnswer, error) {
	const what = "GET /iam/resource"
	path := "/iam/resource?resourceName=" + url.QueryEscape(name)
	out := APIAnswer{Resources: []APIResource{}}
	cursor := ""
	for range maxPages {
		st, next, raw, err := c.exchange(ctx, http.MethodGet, path, nil, cursor)
		if err != nil {
			return APIAnswer{}, fmt.Errorf("%s: no answer", what)
		}
		if st < 200 || st > 299 {
			return APIAnswer{Status: st, Actions: refusedActions(raw, c.b.tok, c.secret)}, nil
		}
		var entries []*struct {
			URN  string            `json:"urn"`
			Name string            `json:"name"`
			Tags map[string]string `json:"tags"`
		}
		if trimmed := bytes.TrimSpace(raw); len(trimmed) == 0 || trimmed[0] != '[' || json.Unmarshal(trimmed, &entries) != nil {
			return APIAnswer{}, fmt.Errorf("%s: the answer is not a resource listing", what)
		}
		for _, e := range entries {
			switch {
			case e == nil:
				return APIAnswer{}, fmt.Errorf("%s: a listed entry is not a resource", what)
			case e.Name != name:
				continue
			case !ValidResourceURN(e.URN):
				return APIAnswer{}, fmt.Errorf("%s: a listed resource of the asked name has no usable urn", what)
			case len(e.Tags) > maxTags:
				return APIAnswer{}, fmt.Errorf("%s: a listed resource of the asked name has more than %d tags", what, maxTags)
			}
			res := APIResource{URN: e.URN, Name: e.Name, Tags: map[string]string{}}
			for k, v := range e.Tags {
				switch {
				case apiTagKey.MatchString(k) && apiTagValue.MatchString(v):
					res.Tags[k] = v
				case apiTagKey.MatchString(k):
					// The key is kept with an empty value, so a cleanup still sees and removes it
					// (review r2); the value's text stays here.
					res.Tags[k], res.Partial = "", true
				default:
					res.Partial = true
				}
			}
			out.Resources = append(out.Resources, res)
		}
		out.Status = st
		if cursor = next; cursor == "" {
			return out, nil
		}
	}
	return APIAnswer{}, fmt.Errorf("%s: more than %d pages", what, maxPages)
}

// apiGate refuses an operation outside the named twenty-seven, a part it does not take, or a part of the wrong
// shape, before any request. A refusal names the operation and the part, never a value.
func apiGate(call APICall) error {
	type parts struct{ project, region, name, id, body, objects, urn, tags, key bool }
	var want parts
	switch call.Op {
	case APIGetStorage, APIListObjects, APIDeleteBucket, APICreateContainer:
		want = parts{project: true, region: true, name: true}
	case APIGetMe:
	case APIListPolicies:
		want = parts{name: true}
	case APICreatePolicy:
		want = parts{body: true}
	case APIDeletePolicy:
		want = parts{id: true}
	case APIListRegions, APIListUsers:
		want = parts{project: true}
	case APIListBuckets:
		want = parts{project: true, region: true}
	case APIBulkDeleteObjects:
		want = parts{project: true, region: true, name: true, objects: true}
	case APIDeleteUser, APIDeleteContainer:
		want = parts{project: true, id: true}
	case APIGetProject, APIListAlerts, APIListContainers:
		want = parts{project: true}
	case APIGetAlert, APIDeleteAlert:
		want = parts{project: true, id: true}
	case APICreateClient, APIListClients:
		want = parts{name: true}
	case APIGetClient, APIDeleteClient, APIGetPolicy:
		want = parts{id: true}
	case APIListResources:
		want = parts{name: true}
	case APIPutResourceTags:
		want = parts{urn: true, tags: true}
	case APIDeleteResourceTag:
		want = parts{urn: true, key: true}
	default:
		return fmt.Errorf("API check: operation %q refused", call.Op)
	}
	got := parts{call.Project != "", call.Region != "", call.Name != "", call.ID != "", call.Body != nil, call.Objects != nil, call.URN != "", call.Tags != nil, call.Key != ""}
	if got != want || call.Keep != nil && call.Op != APIListPolicies {
		return fmt.Errorf("API check: %s with the wrong parts", call.Op)
	}
	if want.objects && (len(call.Objects) == 0 || len(call.Objects) > maxBulk) {
		return fmt.Errorf("API check: %s: not 1 to %d objects", call.Op, maxBulk)
	}
	for _, o := range call.Objects {
		if err := validObject(o); err != nil {
			return fmt.Errorf("API check: %s: %w", call.Op, err)
		}
	}
	region := apiRegion
	if call.Op != APIGetStorage {
		region = apiRegionName
	}
	id, idKind := apiUUID, "uuid"
	switch call.Op {
	case APIDeleteUser:
		id, idKind = apiUserID, "user id"
	case APIGetClient, APIDeleteClient:
		id, idKind = apiClientID, "client id"
	case APIDeleteContainer:
		id, idKind = apiContainerID, "container id"
	}
	clientName := call.Op == APICreateClient || call.Op == APIListClients
	for _, id := range call.Keep {
		if !apiUUID.MatchString(id) {
			return fmt.Errorf("API check: %s: a kept id is not a uuid", call.Op)
		}
	}
	switch {
	case want.project && !projectShape(call.Op).MatchString(call.Project):
		return fmt.Errorf("API check: %s: the project is not a project id", call.Op)
	case want.region && !region.MatchString(call.Region):
		return fmt.Errorf("API check: %s: the region is not a region name", call.Op)
	case want.name && call.Op != APIListPolicies && !clientName && !s3Bucket.MatchString(call.Name):
		return fmt.Errorf("API check: %s: the name is not a bucket name", call.Op)
	case call.Op == APIListPolicies && !apiPolicyName.MatchString(call.Name):
		return fmt.Errorf("API check: %s: the name is not a policy name", call.Op)
	case clientName && !apiPolicyName.MatchString(call.Name):
		return fmt.Errorf("API check: %s: the name is not a client name", call.Op)
	case want.id && !id.MatchString(call.ID):
		return fmt.Errorf("API check: %s: the id is not a %s", call.Op, idKind)
	case want.body && (len(call.Body) > maxPolicyBody || !json.Valid(call.Body)):
		return fmt.Errorf("API check: %s: the body is not JSON of at most %d bytes", call.Op, maxPolicyBody)
	case want.urn && !ValidResourceURN(call.URN):
		return fmt.Errorf("API check: %s: the urn is not a resource urn", call.Op)
	case want.key && (!apiTagKey.MatchString(call.Key) || strings.Contains(call.Key, "/") || strings.Trim(call.Key, ".") == ""):
		return fmt.Errorf("API check: %s: the key is not a tag key usable in a path", call.Op)
	case want.tags && !validTags(call.Tags):
		return fmt.Errorf("API check: %s: a tag key or value is not of iam.resource.AddTag's shape", call.Op)
	}
	return nil
}

// projectShape is the shape apiGate holds an operation's project to.
func projectShape(op APIOp) *regexp.Regexp {
	switch op {
	case APIListAlerts, APIGetAlert, APIDeleteAlert:
		return apiProjectSegment
	}
	return apiProject
}

// policyPage collects, over the pages of a listing, the policies named name or with an id in keep.
type policyPage struct {
	name    string
	keep    []string
	found   []APIPolicy
	created map[string]time.Time
}

// send makes one request and reads its answer: a non-2xx answer keeps its status and the actions
// it names; a 2xx CreatePolicy answer its id; a 2xx listing page (page non-nil) adds the policies
// named page.name to page.found and returns its next page's cursor as next.
func (c *APIChecker) send(ctx context.Context, method, path string, body []byte, cursor string, page *policyPage) (a APIAnswer, next string, err error) {
	what := method + " " + path
	status, cursorNext, raw, err := c.exchange(ctx, method, path, body, cursor)
	if err != nil {
		return APIAnswer{}, "", err
	}
	a = APIAnswer{Status: status}
	if status < 200 || status > 299 {
		a.Actions = refusedActions(raw, c.b.tok, c.secret)
		return a, "", nil
	}
	switch {
	case method == http.MethodPost:
		var created struct {
			ID        string `json:"id"`
			CreatedAt string `json:"createdAt"`
		}
		if json.Unmarshal(raw, &created) == nil && apiUUID.MatchString(created.ID) {
			a.ID = created.ID
			// The created policy's createdAt (iam.policy.Response); spec 003 T034 times its
			// calls from it.
			if at, err := time.Parse(time.RFC3339Nano, created.CreatedAt); err == nil {
				a.CreatedAt = map[string]time.Time{created.ID: at}
			}
		}
	case page != nil:
		// An array of policy objects (review r1: `null` would read as an empty listing). Policies
		// of other names are skipped whatever their shape (iam.json puts no pattern on a name);
		// one of the asked name without a uuid could not be deleted, so it is an error.
		var entries []*struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			CreatedAt string `json:"createdAt"`
		}
		if trimmed := bytes.TrimSpace(raw); len(trimmed) == 0 || trimmed[0] != '[' || json.Unmarshal(trimmed, &entries) != nil {
			return APIAnswer{}, "", fmt.Errorf("%s: the answer is not a policy listing", what)
		}
		for _, e := range entries {
			switch {
			case e == nil:
				return APIAnswer{}, "", fmt.Errorf("%s: a listed entry is not a policy", what)
			case slices.Contains(page.keep, e.ID):
				// A kept id (a uuid, gated) whatever its name: the run's own write (T101 review r2).
				page.found = append(page.found, APIPolicy{ID: e.ID, Name: e.Name})
			case e.Name != page.name:
				continue
			case !apiUUID.MatchString(e.ID):
				return APIAnswer{}, "", fmt.Errorf("%s: a listed policy of the asked name has no usable id", what)
			default:
				page.found = append(page.found, APIPolicy{ID: e.ID, Name: e.Name})
			}
			if at, err := time.Parse(time.RFC3339Nano, e.CreatedAt); err == nil {
				page.created[e.ID] = at
			}
		}
		next = cursorNext
	}
	return a, next, nil
}

// exchange makes one request as the identity and returns the status, the next page's cursor
// (X-Pagination-Cursor-Next) and the capped answer; an error names the request only.
func (c *APIChecker) exchange(ctx context.Context, method, path string, body []byte, cursor string) (status int, next string, raw []byte, err error) {
	what := method + " " + path
	var rd io.Reader = http.NoBody
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, apiBase(c.b.api, path)+path, rd)
	if err != nil {
		return 0, "", nil, fmt.Errorf("%s: bad request", what)
	}
	req.Header.Set("Authorization", "Bearer "+c.b.tok)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cursor != "" {
		req.Header.Set("X-Pagination-Cursor", cursor)
	}
	cl := c.b.api.client()
	rec := &statusRecorder{next: cl.Transport}
	if rec.next == nil {
		rec.next = http.DefaultTransport
	}
	cl.Transport = rec
	resp, err := cl.Do(req)
	if err != nil {
		// A 3xx whose Location Go cannot parse fails Do after the answer came: rec has its status.
		return 0, "", nil, &sendFailed{text: quiet(what, err).Error(), cause: err, status: rec.code}
	}
	defer resp.Body.Close()
	raw, err = io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return 0, "", nil, &sendFailed{text: what + ": unreadable answer", cause: err, status: resp.StatusCode}
	}
	if len(raw) > maxBody {
		return 0, "", nil, &sendFailed{text: fmt.Sprintf("%s: answer larger than %d bytes", what, maxBody), status: resp.StatusCode, large: true}
	}
	return resp.StatusCode, resp.Header.Get("X-Pagination-Cursor-Next"), raw, nil
}

// list reads every page of GET /iam/policy (v2 pagination, kb manage-and-operate/api/apiv2.mdx: the
// next page's cursor comes in X-Pagination-Cursor-Next and goes back in X-Pagination-Cursor; its
// absence marks the last page) and keeps the policies named name or with an id in keep. A refused
// page is the answer.
func (c *APIChecker) list(ctx context.Context, name string, keep []string) (APIAnswer, error) {
	page := &policyPage{name: name, keep: keep, found: []APIPolicy{}, created: map[string]time.Time{}}
	cursor := ""
	for range maxPages {
		a, next, err := c.send(ctx, http.MethodGet, "/iam/policy", nil, cursor, page)
		if err != nil || a.Status < 200 || a.Status > 299 {
			return a, err
		}
		if cursor = next; cursor == "" {
			return APIAnswer{Status: a.Status, Policies: page.found, CreatedAt: page.created}, nil
		}
	}
	return APIAnswer{}, fmt.Errorf("GET /iam/policy: more than %d pages", maxPages)
}

// refusedActions are the IAM actions a refusal names: the action-shaped tokens of
// details.unauthorizedActionsByIAM and of a message "missing IAM permissions: …", sorted and
// without duplicates; a token holding the request's token or secret is dropped.
func refusedActions(raw []byte, keys ...string) []string {
	var e struct {
		Message string `json:"message"`
		Details struct {
			ByIAM string `json:"unauthorizedActionsByIAM"`
		} `json:"details"`
	}
	if json.Unmarshal(raw, &e) != nil {
		return nil
	}
	text := e.Details.ByIAM
	if rest, ok := strings.CutPrefix(e.Message, "missing IAM permissions:"); ok {
		text += " " + rest
	}
	var out []string
	for _, tok := range strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' }) {
		if iamAction.MatchString(tok) && !slices.ContainsFunc(keys, func(k string) bool { return k != "" && strings.Contains(tok, k) }) && !slices.Contains(out, tok) {
			out = append(out, tok)
		}
	}
	slices.Sort(out)
	return out
}
