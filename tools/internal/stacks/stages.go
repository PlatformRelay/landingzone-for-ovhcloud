// This file is the stage table (data-model *Stage table*, ADR-0004): the stages a manifest row may
// name, the scope each is instantiated at, the producers it depends on and how its state is held.
// TestStageTableMatchesDataModel reads data-model.md and keeps the two equal.
package stacks

// Stage is one row of the stage table.
type Stage struct {
	Name        string
	Scope       string   // one of the Scope constants; empty for a reserved stage
	Data        []string // stages whose outputs.json it reads (data edges)
	Authority   []string // stages whose principal or state access it runs under (authority edges)
	Principal   string   // who applies it: bootstrap | platform | tenant
	Backend     string   // local | s3
	Bucket      string   // the s3 backend's bucket: account | tenant; empty for local
	Chain       string   // live chain: retained | ephemeral
	Slots       bool     // a row may carry a slot (runtime only)
	Implemented bool     // false for the reserved stages, refused with STAGE_NOT_IMPLEMENTED
}

// stageTable lists every stage of the manifest's stage enum, in the enum's order. The reserved
// stages (research R10) keep their names in the enum and nothing else.
var stageTable = []Stage{
	{Name: "account-admin"},
	{Name: "bootstrap", Scope: ScopeAccount, Principal: "bootstrap", Backend: "local", Chain: "retained", Implemented: true},
	{Name: "tenant-state", Scope: ScopeAccountTenant, Authority: []string{"bootstrap"}, Principal: "bootstrap",
		Backend: "s3", Bucket: "account", Chain: "retained", Implemented: true},
	{Name: "account-governance", Scope: ScopeAccount, Authority: []string{"bootstrap"}, Principal: "bootstrap",
		Backend: "s3", Bucket: "account", Chain: "retained", Implemented: true},
	{Name: "account-fabric"},
	{Name: "project", Scope: ScopeEnvironment, Authority: []string{"account-governance", "tenant-state"}, Principal: "platform",
		Backend: "s3", Bucket: "tenant", Chain: "retained", Implemented: true},
	{Name: "project-network", Scope: ScopeRegion, Data: []string{"project"}, Authority: []string{"account-governance", "tenant-state"},
		Principal: "tenant", Backend: "s3", Bucket: "tenant", Chain: "ephemeral", Implemented: true},
	{Name: "runtime", Scope: ScopeRegion, Data: []string{"project"}, Authority: []string{"account-governance", "tenant-state"},
		Principal: "tenant", Backend: "s3", Bucket: "tenant", Chain: "ephemeral", Slots: true, Implemented: true},
	{Name: "observability"},
}

// stageNamed returns the table row of a stage.
func stageNamed(name string) (Stage, bool) {
	for _, s := range stageTable {
		if s.Name == name {
			return s, true
		}
	}
	return Stage{}, false
}

// stageNames lists the stage enum in order.
func stageNames() []string {
	names := make([]string, 0, len(stageTable))
	for _, s := range stageTable {
		names = append(names, s.Name)
	}
	return names
}

// StageOf returns the stage table row of a stage name; the live lane reads its principal and chain.
func StageOf(name string) (Stage, bool) { return stageNamed(name) }
