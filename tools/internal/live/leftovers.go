package live

import (
	"context"
	"encoding/json"
)

// Leftover check G9 (FR-011, research R12 *Leftover kind matrix*): after destroy, independent
// listings of every resource type the slice or its probes create are reconciled against the
// inventory, the retained instances' states and the admin exemption. It fails closed: a listing
// error, a missing binary, an unparseable listing or a created type outside the matrix is `fail`.
//
// STUB (T054): the tests pin the behaviour; T055 implements it (T065 qualifies the parser on
// T009's captured listings).

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
// <configRoot>/accounts/<account>/account.env (credential files: private, regular, own).
func LoadExemption(configRoot, account string) (Exemption, error) { return Exemption{}, nil }

// LeftoverCheck lists every matrix kind.
type LeftoverCheck struct {
	Ovhcloud string // the ovhcloud executable, used when Lister is nil
	Lister   Lister
	Projects []Project
	Prefix   string              // slice name prefix ("<org>-")
	RunID    string              // the run id; a name, id or tag value holding it matches
	Retained map[string][]string // provider type -> ids held in retained instances' states
	Exempt   Exemption
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
	Listings  map[string][]json.RawMessage `json:"-"` // per provider type, for listings/<type>.json
}

// MatrixTypes returns the provider types of the kind matrix.
func MatrixTypes() []string { return nil }

// Check lists every kind and reconciles it with inv.
func (c LeftoverCheck) Check(ctx context.Context, inv []InventoryEntry) Report {
	return Report{Outcome: "pass"}
}
