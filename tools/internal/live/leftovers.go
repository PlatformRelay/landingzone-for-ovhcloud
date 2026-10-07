package live

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Leftover check G9 (FR-011, research R12 *Leftover kind matrix*): after destroy, independent
// listings of every resource type the slice or its probes create are reconciled against the
// inventory, the retained instances' states and the admin exemption. It fails closed: a listing
// error, a missing binary, an unparseable listing or a created type outside the matrix is `fail`.
// The parser follows the API response schemas in kb/api (synthetic listings, T054); T065 qualifies
// it on T009's captured listings before the first resource-creating probe (T010).

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
	Ovhcloud string // the ovhcloud executable, used when Lister is nil
	Lister   Lister
	Projects []Project
	Prefix   string              // name prefix of what the run creates ("<org>-", "lzprobe-")
	RunID    string              // the run id; a name, id or tag value holding it matches
	Retained map[string][]string // provider type -> ids held in retained instances' states
	Exempt   Exemption
	// API and Cred are lz-live's own API client and the credential the run binds: when Lister is
	// nil the check lists through them, GET only (T083 pins it, T084 implements it).
	API  API
	Cred Credential

	child *childEnv // set by the runner: the environment ovhcloud runs with
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

// ovhcloudLister lists through the ovhcloud CLI with the run's child environment (scratch HOME,
// the authority's credentials). Its argv (`ovhcloud api get <path>`) and whether the CLI follows
// pagination itself are UNVERIFIED for 0.15.0 (premise P18): a wrong argv fails closed as a
// listing error; T065 qualifies both on T009's captures before any probe creates a resource.
type ovhcloudLister struct {
	bin   string
	child *childEnv
}

func (o ovhcloudLister) Get(ctx context.Context, path, cursor string) ([]byte, string, error) {
	if o.child == nil {
		return nil, "", errors.New("ovhcloud: no child environment")
	}
	cmd := o.child.command(ctx, o.bin, "api", "get", path)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return nil, "", fmt.Errorf("ovhcloud %s: %v", path, err)
	}
	return out.Bytes(), "", nil
}

// apiLister is the leftover check's read-only client of the OVHcloud API (T083 pins it; stub
// until T084).
type apiLister struct {
	api  API
	cred Credential
}

func newAPILister(a API, c Credential) *apiLister { return &apiLister{api: a, cred: c} }

func (l *apiLister) Get(ctx context.Context, path, cursor string) ([]byte, string, error) {
	return l.request(ctx, http.MethodGet, path, cursor)
}

// request sends one API request; any method but GET is refused before a request.
func (l *apiLister) request(ctx context.Context, method, path, cursor string) ([]byte, string, error) {
	return nil, "", errors.New("read-only API lister: not implemented")
}

type lister struct {
	ctx  context.Context
	l    Lister
	errs *[]string
	rec  map[string][]json.RawMessage
}

func (l lister) fail(format string, a ...any) { *l.errs = append(*l.errs, fmt.Sprintf(format, a...)) }

// all reads every page of an array listing.
func (l lister) all(path string) ([]json.RawMessage, bool) {
	var out []json.RawMessage
	cursor := ""
	for {
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
		cursor = next
	}
}

func (l lister) object(path string, v any) bool {
	body, _, err := l.l.Get(l.ctx, path, "")
	if err != nil {
		l.fail("listing %s: %v", path, err)
		return false
	}
	if len(bytes.TrimSpace(body)) == 0 || bytes.TrimSpace(body)[0] != '{' || json.Unmarshal(body, v) != nil {
		l.fail("listing %s: not a JSON object", path)
		return false
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
		if c.Ovhcloud == "" {
			fail("no ovhcloud binary")
		} else if fi, err := os.Stat(c.Ovhcloud); err != nil || fi.IsDir() {
			fail("ovhcloud binary %s missing", c.Ovhcloud)
		} else {
			src = ovhcloudLister{bin: c.Ovhcloud, child: c.child}
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

func (c LeftoverCheck) list(l lister) []*item {
	var items []*item
	add := func(it *item) *item { items = append(items, it); return it }
	for _, p := range c.Projects {
		base := "/cloud/project/" + p.ID
		if regions, ok := l.strings(base + "/region"); ok {
			for _, r := range regions {
				path := base + "/region/" + r + "/storage"
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
				path := base + "/network/private/" + n.ID + "/subnet"
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
				path := base + "/user/" + u.ID.String() + "/s3Credentials"
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
				ppath := base + "/user/" + u.ID.String() + "/policy"
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
				if l.object(base+"/alerting/"+id, &a) {
					raw, _ := json.Marshal(a)
					l.record(tAlert, raw)
					add(&item{typ: tAlert, id: id, name: a.Name})
				}
			}
		}
		var res struct {
			Tags *map[string]string `json:"tags"`
		}
		if l.object("/iam/resource/"+p.URN, &res) {
			if res.Tags == nil {
				l.fail("listing /iam/resource/%s: no tags field", p.URN)
			} else {
				raw, _ := json.Marshal(res.Tags)
				l.record(tTags, raw)
				for k, v := range *res.Tags {
					add(&item{typ: tTags, id: k, name: k, values: []string{v}})
				}
			}
		}
	}
	if ids, ok := l.strings("/me/api/oauth2/client"); ok {
		for _, id := range ids {
			var cl struct {
				ClientID string `json:"clientId"`
				Name     string `json:"name"`
			}
			if l.object("/me/api/oauth2/client/"+id, &cl) {
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
			if l.object("/me/identity/group/"+n, &g) {
				raw, _ := json.Marshal(g)
				l.record(tGroup, raw)
				add(&item{typ: tGroup, id: n, name: g.Name})
			}
		}
	}
	return items
}
