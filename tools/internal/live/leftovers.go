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
)

// Leftover check G9 (FR-011, research R12 *Leftover kind matrix*): after destroy, independent
// listings of every resource type the slice or its probes create are reconciled against the
// inventory, the retained instances' states and the admin exemption. It fails closed: a listing
// error, a missing API client, an unparseable listing or a created type outside the matrix is
// `fail`. It lists through lz-live's own read-only API client (T084): ovhcloud 0.15.0 has no `api`
// command (premise P18 refuted, evidence/T009.md).
// The parser follows the API response schemas in kb/api (synthetic listings, T054); T065 qualified
// it on the captured listings of the plan-only run 20261007T125154Z-14a6
// (tests/fixtures/ovhcloud/captured/; its provenance.json names the kinds still uncaptured).

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
}

// Leftover is a listed resource that should not exist.
type Leftover struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// Report is the outcome of a leftover check: "pass" only with no leftover and no error.
type Report struct {
	Outcome   string                       `json:"outcome"`
	Leftovers []Leftover                   `json:"leftovers"`
	Errors    []string                     `json:"errors"`
	Listings  map[string][]json.RawMessage `json:"-"`
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
)

var matrix = []string{tStorage, tNetwork, tSubnet, tUser, tCred, tS3Pol, tClient, tPolicy, tGroup, tTags, tAlert, tQuota, tProject}

// MatrixTypes returns the provider types of the kind matrix.
func MatrixTypes() []string { return slices.Clone(matrix) }

// item is one listed resource; a child (subnet, S3 credential, S3 policy) points to its parent.
type item struct {
	typ, id, name string
	values        []string
	parent        *item
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
}

// quiet is a failed request's error without what Go quotes from the answer (an unparseable
// Location header, which a hostile endpoint can fill with the token or the secret): only the
// kind of failure remains. An error that is not a transport error passes unchanged (a listing's
// Do fails only with one; the token request goes through quietToken).
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

// quietToken is a failed token request's error with nothing the answer chose: a transport error
// as quiet, binding.go's status error ("token endpoint answered <code>") kept, anything else (a
// body that cannot be read or decoded, whose error may quote the answer) only named.
func quietToken(err error) error {
	var ue *url.Error
	var code int
	if errors.As(err, &ue) {
		return quiet("token endpoint", err)
	}
	if n, _ := fmt.Sscanf(err.Error(), "token endpoint answered %d", &code); n == 1 && err.Error() == fmt.Sprintf("token endpoint answered %d", code) {
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
		return nil, "", l.tokErr
	}
	if l.tok == "" {
		tok, err := l.api.token(ctx, l.cred)
		if err != nil {
			l.tokErr = quietToken(err)
			return nil, "", l.tokErr
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
	resp, err := l.api.client().Do(req)
	if err != nil {
		return nil, "", quiet("GET "+path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxListing+1))
	if err != nil {
		return nil, "", fmt.Errorf("GET %s: reading the answer failed", path)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, "", fmt.Errorf("GET %s answered %d", path, resp.StatusCode)
	}
	if len(body) > maxListing {
		return nil, "", fmt.Errorf("GET %s: answer longer than %d bytes", path, maxListing)
	}
	return body, resp.Header.Get("X-Pagination-Cursor-Next"), nil
}

// maxListingPages bounds the pages of one listing: a cursor that never ends is an error.
const maxListingPages = 1000

type lister struct {
	ctx  context.Context
	l    Lister
	errs *[]string
	rec  map[string][]json.RawMessage
}

func (l lister) fail(format string, a ...any) { *l.errs = append(*l.errs, fmt.Sprintf(format, a...)) }

// all reads every page of an array listing; a cursor that repeats or more than maxListingPages
// pages is an error.
func (l lister) all(path string) ([]json.RawMessage, bool) {
	var out []json.RawMessage
	cursor := ""
	seen := map[string]bool{}
	for pages := 1; ; pages++ {
		if pages > maxListingPages {
			l.fail("listing %s: more than %d pages", path, maxListingPages)
			return nil, false
		}
		body, next, err := l.l.Get(l.ctx, path, cursor)
		if err != nil {
			l.fail("listing %s: %v", path, err)
			return nil, false
		}
		var page []json.RawMessage
		if err := json.Unmarshal(body, &page); err != nil {
			l.fail("listing %s: not a JSON array", path)
			return nil, false
		}
		out = append(out, page...)
		if next == "" {
			return out, true
		}
		if seen[next] {
			l.fail("listing %s: the next-page cursor repeats", path)
			return nil, false
		}
		seen[next] = true
		cursor = next
	}
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
	body, _, err := l.l.Get(l.ctx, path, "")
	if err != nil {
		l.fail("listing %s: %v", path, err)
		return nil, false
	}
	if len(bytes.TrimSpace(body)) == 0 || bytes.TrimSpace(body)[0] != '{' || json.Unmarshal(body, v) != nil {
		l.fail("listing %s: not a JSON object", path)
		return nil, false
	}
	return json.RawMessage(bytes.TrimSpace(body)), true
}

// offersObjectStorage reports whether a region detail (cloud.Region, kb/api/v1/cloud.json:67596)
// lists an S3-compatible Object Storage service among its services (cloud.Component[],
// cloud.json:67651, 64709). The schema does not enumerate the service names: the "storage-s3"
// prefix (storage-s3-standard, storage-s3-high-perf) is UNVERIFIED until a live run records the
// region details (T065). A service that is DOWN still counts: its listing then fails, closed.
func offersObjectStorage(services []struct {
	Name string `json:"name"`
}) bool {
	for _, s := range services {
		if strings.HasPrefix(s.Name, "storage-s3") {
			return true
		}
	}
	return false
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
	var src Lister = c.Lister
	if src == nil {
		if c.API.BaseURL == "" {
			fail("no API client")
		} else {
			src = newAPILister(c.API, c.Cred)
		}
	}
	if len(c.Projects) == 0 {
		fail("no project to list")
	}
	var items []*item
	if src != nil {
		l := lister{ctx: ctx, l: src, errs: &errs, rec: rep.Listings}
		items = c.list(l)
	}
	retained := map[string]bool{}
	for typ, ids := range c.Retained {
		for _, id := range ids {
			retained[typ+"\x00"+id] = true
		}
	}
	var candidate func(*item) bool
	candidate = func(it *item) bool {
		if c.Prefix != "" && strings.HasPrefix(it.name, c.Prefix) {
			return true
		}
		if c.RunID != "" {
			for _, v := range append([]string{it.id, it.name}, it.values...) {
				if strings.Contains(v, c.RunID) {
					return true
				}
			}
		}
		if created[it.typ+"\x00"+it.id] {
			return true
		}
		return it.parent != nil && candidate(it.parent)
	}
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

// list lists every kind. Buckets are listed per region, and only in the regions whose detail
// offers Object Storage: the storage listing of a compute region answers 404 (live run
// 20261007T125154Z-14a6, evidence/T084.md). A region detail without services is an error, and so
// is a check in which no region offers Object Storage: every project of the slice holds buckets
// (state, runtime), and a wrong service-name rule must not silently hide every bucket.
func (c LeftoverCheck) list(l lister) []*item {
	var items []*item
	add := func(it *item) *item { items = append(items, it); return it }
	storageRegions := 0
	for _, p := range c.Projects {
		base, ok := l.under("/cloud/project", p.ID)
		if !ok {
			continue
		}
		if regions, ok := l.strings(base + "/region"); ok {
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
				if !offersObjectStorage(*reg.Services) {
					continue
				}
				storageRegions++
				path := rp + "/storage"
				raw, ok := l.all(path)
				if !ok {
					continue
				}
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
					add(&item{typ: tAlert, id: id, name: a.Name})
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
	if storageRegions == 0 && len(c.Projects) > 0 {
		l.fail("listing buckets: no region of the projects offers Object Storage (a storage-s3 service)")
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
