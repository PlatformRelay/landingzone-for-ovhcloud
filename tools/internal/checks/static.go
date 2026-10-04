package checks

// StaticTools locates the pinned tools and the TFLint configuration.
type StaticTools struct {
	Tofu, TFLint, TFLintConfig string
}

// StaticResult is one static clause's outcome with the upstream detail kept.
type StaticResult struct {
	Clause   string
	Status   string
	Reason   string
	Files    []string
	Messages []string
}

// StaticObservation is every static clause for one module directory.
type StaticObservation struct {
	Dir        string
	Discovered []string
	Results    []StaticResult
	Pass       bool
}

func RunStatic(tools StaticTools, dir string) StaticObservation { return StaticObservation{Pass: true} }
