package checks

// Prepared describes synthetic capture admission inputs. T003 must implement
// their validation and bind qualified isolation evidence before real capture.
type Prepared struct {
	Versions map[string]string
	Artifacts map[string]string
	Image string
	IsolationProof string
}

// AdmitCapture is a compiling, deliberately permissive boundary stub.
func AdmitCapture(_ Prepared) error { return nil }

// Capture exposes observable pre-invocation and publication ordering to tests.
func Capture(p Prepared, invoke, publish func() error) error {
	if err := AdmitCapture(p); err != nil { return err }
	if err := invoke(); err != nil { return err }
	return publish()
}
