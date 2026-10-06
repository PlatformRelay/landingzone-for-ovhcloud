package live

import "os/exec"

// Authority is the credential authority an instance runs under (research R6).
type Authority string

const (
	AuthorityBootstrap Authority = "bootstrap"
	AuthorityPlatform  Authority = "platform"
	AuthorityTenant    Authority = "tenant"
)

// Command returns a child process (tofu, ovhcloud) whose environment is built from scratch:
// PATH and HOME, and the selected authority's credential variables in creds. Nothing else of the
// caller's environment is inherited (FR-010, guard G12).
//
// Stub (T052): inherits the caller's environment. T053 implements it.
func Command(a Authority, creds map[string]string, name string, arg ...string) *exec.Cmd {
	return exec.Command(name, arg...)
}
