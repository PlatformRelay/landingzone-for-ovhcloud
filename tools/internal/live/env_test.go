package live

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Child environment G12 (FR-010, research R6 *Child environment*): a child starts from scratch
// with PATH, HOME and the selected authority's variables; nothing ambient reaches it.

// ambient is the caller's environment, as a .envrc exporting the admin credential would leave
// it. Every value starts with "ambient-" so a leak is visible under any name.
var ambient = map[string]string{
	"OVH_ENDPOINT":            "ambient-endpoint",
	"OVH_CLIENT_ID":           "ambient-admin-client-id",
	"OVH_CLIENT_SECRET":       "ambient-admin-client-secret",
	"OVH_APPLICATION_KEY":     "ambient-application-key",
	"OVH_APPLICATION_SECRET":  "ambient-application-secret",
	"OVH_CONSUMER_KEY":        "ambient-consumer-key",
	"AWS_ACCESS_KEY_ID":       "ambient-aws-access-key",
	"AWS_SECRET_ACCESS_KEY":   "ambient-aws-secret-key",
	"AWS_SESSION_TOKEN":       "ambient-aws-session-token",
	"AWS_PROFILE":             "ambient-aws-profile",
	"TF_CLI_ARGS":             "ambient-tf-cli-args",
	"TF_VAR_state_passphrase": "ambient-state-passphrase",
	"LZ_ENV_CANARY":           "ambient-canary",
	// No OVH_/AWS_/TF_/LZ_ prefix: a prefix filter over the caller's environment keeps them.
	"OS_PASSWORD":   "ambient-openstack-password",
	"SSH_AUTH_SOCK": "ambient-ssh-agent",
}

func TestEnvChildFromScratch(t *testing.T) {
	for k, v := range ambient {
		t.Setenv(k, v)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range []Authority{AuthorityTenant, AuthorityPlatform, AuthorityBootstrap} {
		t.Run(string(a), func(t *testing.T) {
			creds := readEnvFile(t, filepath.Join("testdata", "env", string(a)+".env"))
			out := filepath.Join(t.TempDir(), "child.env")
			cmd := Command(a, creds, self, fakeChildArg, out)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("child: %v: %s", err, output)
			}
			raw, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			child := map[string]string{}
			for _, line := range strings.Split(string(raw), "\n") {
				if k, v, ok := strings.Cut(line, "="); ok {
					child[k] = v
				}
			}
			for k, v := range child {
				if strings.Contains(v, "ambient-") {
					t.Errorf("caller value reached the child: %s=%s", k, v)
				}
			}
			for k := range ambient {
				if _, own := creds[k]; !own {
					if _, ok := child[k]; ok {
						t.Errorf("caller variable %s reached the child", k)
					}
				}
			}
			// From scratch, not filtered: every key is PATH, HOME, PWD (set by os/exec), a
			// TF_* variable lz-live sets itself, or one of the authority's own (R6).
			for k := range child {
				_, own := creds[k]
				if !own && k != "PATH" && k != "HOME" && k != "PWD" && !strings.HasPrefix(k, "TF_") {
					t.Errorf("child carries %s, which is neither PATH, HOME, TF_* nor a %s credential", k, a)
				}
			}
			for k, v := range creds {
				if child[k] != v {
					t.Errorf("child %s = %q, want the %s authority's value", k, child[k], a)
				}
			}
			for _, k := range []string{"PATH", "HOME"} {
				if child[k] == "" {
					t.Errorf("child has no %s", k)
				}
			}
		})
	}
}
