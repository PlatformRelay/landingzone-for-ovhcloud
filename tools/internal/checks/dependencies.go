package checks

// Layers a directory can belong to (ADR-0002).
const (
	LayerLibrary  = "library"
	LayerStage    = "stage"
	LayerInstance = "instance"
	LayerExample  = "example"
	LayerTest     = "test"
)

// DependencyRules lists every rule ScanDependencies can report.
var DependencyRules = []string{"CYCLE", "LAYER_VIOLATION", "PARSE_ERROR", "UNCLASSIFIED", "UNREADABLE", "UNRESOLVED_REFERENCE"}

// DependencyGraph is the classified module graph of a repository. Keys are
// directories relative to the repository root.
type DependencyGraph struct {
	Layers   map[string]string
	Uses     map[string][]string
	External map[string][]string
	Configs  map[string][]string
}

// Selection is the set of directories a change requires to be checked.
type Selection struct {
	Full   bool
	Dirs   []string
	Reason string
}

func ScanDependencies(root string) (DependencyGraph, []Finding) { return DependencyGraph{}, nil }

func SelectChanged(g DependencyGraph, changed []string) Selection { return Selection{} }
