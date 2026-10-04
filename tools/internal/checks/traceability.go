package checks

// Task is one entry of a feature's tasks.md.
type Task struct {
	ID           string
	Paths        []string
	Requirements []string
	ADRs         []string
	// Verify holds the verify text; for a docs-only exemption it holds the
	// exemption's reason.
	Verify    string
	Exempt    bool
	Evidence  string
	Rationale map[string]string
}

const (
	KindBehavioral = "behavioral"
	KindDocs       = "docs"
)

// Requirement records the ADRs relevant to one requirement.
type Requirement struct {
	ADRs []string `json:"adrs"`
}

// CheckDefinition is one entry of the check registry.
type CheckDefinition struct {
	ID           string   `json:"id"`
	Requirements []string `json:"requirements"`
	Command      string   `json:"command"`
	Creator      string   `json:"creator"`
	Kind         string   `json:"kind"`
	Scope        []string `json:"scope"`
	Review       bool     `json:"review,omitempty"`
}

// Registry is harness/checks.yaml.
type Registry struct {
	Requirements map[string]Requirement `json:"requirements"`
	Checks       []CheckDefinition      `json:"checks"`
}

// Trace is everything the trace rules read.
type Trace struct {
	Requirements []string
	Tasks        []Task
	Registry     Registry
	ADRs         []string
}

// Finding is one broken trace link.
type Finding struct {
	Rule    string
	Subject string
	Detail  string
}

// TraceRules lists every rule CheckTrace can report.
var TraceRules = []string{
	"BLANKET_ADR_LIST", "DUPLICATE_TASK", "INCOMPLETE_RATIONALE", "INVALID_EXEMPTION",
	"MISSING_EVIDENCE", "MISSING_PATHS", "MISSING_VERIFY", "UNCHECKED_REQUIREMENT",
	"UNKNOWN_ADR", "UNKNOWN_CREATOR", "UNKNOWN_REQUIREMENT", "UNMAPPED_REQUIREMENT",
	"UNMAPPED_TASK", "UNREGISTERED_REQUIREMENT", "UNRELATED_ADR",
}

const (
	StatusPass           = "pass"
	StatusFail           = "fail"
	StatusBlocked        = "blocked"
	StatusNotRun         = "not-run"
	StatusReviewRequired = "review-required"
	StatusExempt         = "exempt"
)

// Evidence is the recorded observation of one check.
type Evidence struct {
	Check       string `json:"check"`
	Status      string `json:"status"`
	InputDigest string `json:"input_digest"`
	Discovered  int    `json:"discovered"`
	Failed      int    `json:"failed"`
	Reviewer    string `json:"reviewer,omitempty"`
}

// Item is one check's DoD status.
type Item struct {
	Check  string
	Status string
	Reason string
}

// DoD is the definition-of-done verdict for one path.
type DoD struct {
	Items []Item
	Pass  bool
}

func ParseSpec(text string) []string { return nil }

func ParseTasks(text string) ([]Task, error) { return nil, nil }

func ParseRegistry(data []byte) (Registry, error) { return Registry{}, nil }

func CheckTrace(t Trace) []Finding { return nil }

func Digest(root string, scope []string) (string, error) { return "", nil }

func EvaluateDoD(reg Registry, root, path string, evidence map[string]Evidence) DoD {
	return DoD{Pass: true}
}
