package live

import (
	"os"
	"os/exec"
	"sort"
)

// Authority is the credential authority an instance runs under (research R6).
type Authority string

const (
	AuthorityBootstrap Authority = "bootstrap"
	AuthorityPlatform  Authority = "platform"
	AuthorityTenant    Authority = "tenant"
)

// Command returns a child process (tofu, ovhcloud) whose environment is built from scratch:
// PATH and HOME, and the selected authority's credential variables in creds. Nothing else of the
// caller's environment is inherited (FR-010, guard G12). A creds entry cannot replace PATH or
// HOME (a key that is not a plain variable name, such as "PATH=x", is dropped too: os/exec keeps
// the last value of a duplicate key). Other keys are passed as given: the credential files the
// caller reads them from are written only by files.go.
// a names the authority creds belong to; refusing an authority/credential mismatch (FR-010) is the
// credential selection's job (T058), which builds creds from that authority's files only.
func Command(a Authority, creds map[string]string, name string, arg ...string) *exec.Cmd {
	cmd := exec.Command(name, arg...)
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")}
	keys := make([]string, 0, len(creds))
	for k := range creds {
		if envKey.MatchString(k) && k != "PATH" && k != "HOME" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		env = append(env, k+"="+creds[k])
	}
	cmd.Env = env
	return cmd
}
