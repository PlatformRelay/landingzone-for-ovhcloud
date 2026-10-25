package configure

import (
	"encoding/json"
	"strings"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/stacks"
)

// This file is the derivation and the decoder gate (FR-002, FR-003, data-model.md *Derivation*):
// the answers become a deployments.yaml in the key order data-model gives, and the bytes leave
// Export only when stacks.DecodeManifest, the decoder behind stacks:check, accepts them.
//
// The output is the manifest's JSON-compatible flow form (research R6). Key order comes from the
// field order of the types below, rows from the answers' order, so the same answers give the same
// bytes; no map is marshalled.

// The stage source the derivation fixes, and two defaults an answer overrides (data-model.md).
const (
	manifestStageLocal = "local"
	defaultStateRef    = "STATE"
	defaultProjectMode = "reference"
)

type exportManifest struct {
	APIVersion string         `json:"apiVersion"`
	Kind       string         `json:"kind"`
	Metadata   exportMetadata `json:"metadata"`
	Spec       exportSpecDoc  `json:"spec"`
}

type exportMetadata struct {
	Name string `json:"name"`
}

type exportSpecDoc struct {
	Scope       string           `json:"scope"`
	Org         string           `json:"org"`
	Forge       string           `json:"forge"`
	StageSource exportKind       `json:"stage_source"`
	Sandbox     *exportSandbox   `json:"sandbox,omitempty"` // only when true: the decoder refuses a false block
	State       exportState      `json:"state"`
	Tenants     []exportTenant   `json:"tenants"`
	Instances   []exportInstance `json:"instances"`
}

type exportKind struct {
	Kind string `json:"kind"`
}

type exportSandbox struct {
	SharedStateProject bool `json:"shared_state_project"`
}

type exportState struct {
	Project  exportRef `json:"project"`
	Region   string    `json:"region"`
	Endpoint string    `json:"endpoint"`
}

type exportRef struct {
	Ref string `json:"ref"`
}

type exportTenant struct {
	Name         string              `json:"name"`
	Environments []exportEnvironment `json:"environments"`
}

type exportEnvironment struct {
	Name        string         `json:"name"`
	Project     exportProject  `json:"project"`
	Regions     []exportRegion `json:"regions"`
	BudgetAlert exportToggle   `json:"budget_alert"`
	QuotaGuard  exportToggle   `json:"quota_guard"`
}

type exportProject struct {
	Mode string `json:"mode"`
	Ref  string `json:"ref"`
}

type exportRegion struct {
	Name    string         `json:"name"`
	Network *exportNetwork `json:"network,omitempty"` // only when the answers give a cidr
}

type exportNetwork struct {
	CIDR   string `json:"cidr"`
	VlanID int    `json:"vlan_id"`
}

type exportToggle struct {
	Enabled bool `json:"enabled"`
}

type exportInstance struct {
	ID          string `json:"id"`
	Stage       string `json:"stage"`
	Tenant      string `json:"tenant,omitempty"`
	Environment string `json:"environment,omitempty"`
	Region      string `json:"region,omitempty"`
}

// Export derives the manifest of a decoded answers file (FR-002, data-model.md *Derivation*) and
// passes it through stacks.DecodeManifest (FR-003). It returns the bytes only when the decoder
// accepts them; on a refusal it returns nil and the decoder's *stacks.ManifestError.
func Export(a Answers) ([]byte, error) {
	data, err := json.MarshalIndent(derive(a), "", "  ")
	if err != nil {
		return nil, err // unreachable: the types hold only strings, booleans, ints and pointers to them
	}
	data = append(data, '\n')
	if _, err := stacks.DecodeManifest(data); err != nil {
		return nil, err
	}
	return data, nil
}

// derive applies data-model.md *Derivation*: the defaults, the tenants in answers order and the
// instance rows, a tenant's rows contiguous.
func derive(a Answers) exportManifest {
	spec := exportSpecDoc{
		Scope: stacks.ManifestScopePlatform, Org: a.Org, Forge: a.Forge, StageSource: exportKind{Kind: manifestStageLocal},
		State: exportState{
			Project:  exportRef{Ref: orDefault(a.StateRef, defaultStateRef)},
			Region:   a.StateRegion,
			Endpoint: orDefault(a.StateEndpoint, "https://s3."+a.StateRegion+".io.cloud.ovh.net"),
		},
		Tenants: []exportTenant{},
		Instances: []exportInstance{
			{ID: "account-bootstrap", Stage: "bootstrap"},
			{ID: "account-governance", Stage: "account-governance"},
		},
	}
	if a.SharedStateProject {
		spec.Sandbox = &exportSandbox{SharedStateProject: true}
	}
	for _, t := range a.Tenants {
		tenant := exportTenant{Name: t.Name, Environments: []exportEnvironment{}}
		spec.Instances = append(spec.Instances, exportInstance{ID: t.Name + "-state", Stage: "tenant-state", Tenant: t.Name})
		for _, e := range t.Environments {
			env, rows := deriveEnvironment(t.Name, e)
			tenant.Environments = append(tenant.Environments, env)
			spec.Instances = append(spec.Instances, rows...)
		}
		spec.Tenants = append(spec.Tenants, tenant)
	}
	return exportManifest{APIVersion: stacks.ManifestAPIVersion, Kind: stacks.ManifestKind, Metadata: exportMetadata{Name: a.Name}, Spec: spec}
}

// deriveEnvironment is one environment of tenant and its rows: the project row, then per region
// its network row (only with a cidr) and its runtime row.
func deriveEnvironment(tenant string, e Environment) (exportEnvironment, []exportInstance) {
	env := exportEnvironment{
		Name: e.Name,
		Project: exportProject{
			Mode: orDefault(e.ProjectMode, defaultProjectMode),
			Ref:  orDefault(e.ProjectRef, strings.ToUpper(tenant)+"_"+strings.ToUpper(e.Name)),
		},
		Regions:     []exportRegion{},
		BudgetAlert: exportToggle{Enabled: e.BudgetAlert},
		QuotaGuard:  exportToggle{Enabled: e.QuotaGuard},
	}
	prefix := tenant + "-" + e.Name
	rows := []exportInstance{{ID: prefix + "-project", Stage: "project", Tenant: tenant, Environment: e.Name}}
	for _, r := range e.Regions {
		region := exportRegion{Name: r.Name}
		id := prefix + "-" + strings.ToLower(r.Name)
		if r.CIDR != "" {
			region.Network = &exportNetwork{CIDR: r.CIDR}
			if r.VlanID != nil {
				region.Network.VlanID = *r.VlanID
			}
			rows = append(rows, exportInstance{ID: id + "-network", Stage: "project-network", Tenant: tenant, Environment: e.Name, Region: r.Name})
		}
		rows = append(rows, exportInstance{ID: id + "-runtime", Stage: "runtime", Tenant: tenant, Environment: e.Name, Region: r.Name})
		env.Regions = append(env.Regions, region)
	}
	return env, rows
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
