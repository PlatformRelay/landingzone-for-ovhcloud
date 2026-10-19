package checks

// CheckUpdateLane judges the dependency-update configuration under root (.github/renovate.json)
// against the repository's pin sources and its provider schema snapshot (006 FR-015, SC-004) and
// returns one finding per breach, with the rules UPDATE_UNCOVERED, UPDATE_AUTOMERGE,
// UPDATE_RUNTIME_GROUP, UPDATE_PROVIDER_GROUP, SCHEMA_VERSION, SCHEMA_RECORD and UPDATE_SYNTAX.
// The pin sources are those of data-model *Update-lane configuration*; fixture trees (anything
// under tests/ or a fixtures/ directory, tests/check/fixtures/updatelane and
// tools/cmd/lz-offline/fixtures among them) are test data, never pin sources.
// Not implemented (T037).
func CheckUpdateLane(root string) []Finding {
	return nil
}
