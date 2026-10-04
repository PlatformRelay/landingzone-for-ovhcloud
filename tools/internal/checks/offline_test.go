package checks

import (
	"strconv"
	"strings"
	"testing"
)

// mountRow renders one /proc/self/mountinfo line for the gate's parser.
func mountRow(id int, point, options, fstype, super string) string {
	return strings.Join([]string{strconv.Itoa(id), "1", "0:1", "/", point, options, "-", fstype, "none", super}, " ")
}

// admittedTopology is the mount layout lz-offline creates, as observed inside
// bwrap 0.11.1 on the qualified preparation.
func admittedTopology() []string {
	ro, rw := "ro,nosuid,nodev,relatime", "rw,nosuid,nodev,relatime"
	rows := []string{
		mountRow(1, "/", ro, "ext4", "rw"),
		mountRow(2, "/proc", rw, "proc", "rw"),
		mountRow(3, "/dev", rw, "tmpfs", "rw,mode=755"),
	}
	for i, device := range []string{"null", "zero", "full", "random", "urandom", "tty"} {
		rows = append(rows, mountRow(10+i, "/dev/"+device, "rw,nosuid", "devtmpfs", "rw"))
	}
	rows = append(rows,
		mountRow(20, "/dev/pts", rw, "devpts", "rw"),
		mountRow(21, "/tmp", rw, "tmpfs", "rw,size=2097152k"),
		mountRow(22, "/run", rw, "tmpfs", "rw,size=16384k"),
		mountRow(23, "/home", rw, "tmpfs", "rw,size=65536k"),
		mountRow(24, "/tcb", ro, "tmpfs", "rw"),
		mountRow(25, "/tools", ro, "tmpfs", "rw"),
		mountRow(26, "/tools/go", ro, "ext4", "rw"),
		mountRow(27, "/tcb/task", ro, "ext4", "rw"),
		mountRow(28, "/tcb/tofu", ro, "ext4", "rw"),
		mountRow(29, "/tcb/terramate", ro, "ext4", "rw"),
		mountRow(30, "/tcb/lz-offline", ro, "ext4", "rw"),
		mountRow(31, "/mirror", ro, "ext4", "rw"),
		mountRow(32, "/run/lz/tofurc", ro, "ext4", "rw"),
		mountRow(33, "/run/lz/admission.json", ro, "ext4", "rw"),
		mountRow(34, "/run/lz/src", ro, "ext4", "rw"),
		mountRow(35, "/candidate", rw, "tmpfs", "rw,size=1048576k"),
	)
	return rows
}

func TestRuntimeMountsAdmitted(t *testing.T) {
	if err := ValidateMounts(strings.Join(admittedTopology(), "\n") + "\n"); err != nil {
		t.Fatalf("admitted topology rejected: %v", err)
	}
	withProbe := append(admittedTopology(), mountRow(40, "/tcb/probe", "ro,nosuid,nodev,relatime", "ext4", "rw"))
	if err := ValidateMounts(strings.Join(withProbe, "\n")); err != nil {
		t.Fatalf("admitted topology with probe rejected: %v", err)
	}
}

func TestRuntimeMountsRejected(t *testing.T) {
	ro, rw := "ro,nosuid,nodev,relatime", "rw,nosuid,nodev,relatime"
	replace := func(point, row string) []string {
		rows := admittedTopology()
		for i, line := range rows {
			if strings.Fields(line)[4] == point {
				rows[i] = row
			}
		}
		return rows
	}
	remove := func(point string) []string {
		var rows []string
		for _, line := range admittedTopology() {
			if strings.Fields(line)[4] != point {
				rows = append(rows, line)
			}
		}
		return rows
	}
	cases := map[string][]string{
		"credential-directory":  append(admittedTopology(), mountRow(50, "/home/offline/.config/ovh", ro, "ext4", "rw")),
		"runtime-socket":        append(admittedTopology(), mountRow(51, "/run/docker.sock", rw, "ext4", "rw")),
		"shared-cache":          append(admittedTopology(), mountRow(52, "/tmp/go-cache", rw, "ext4", "rw")),
		"writable-root":         replace("/", mountRow(1, "/", rw, "ext4", "rw")),
		"writable-tool":         replace("/tcb/task", mountRow(27, "/tcb/task", rw, "ext4", "rw")),
		"writable-tool-dir":     replace("/tcb", mountRow(24, "/tcb", rw, "tmpfs", "rw")),
		"writable-source":       replace("/run/lz/src", mountRow(34, "/run/lz/src", rw, "ext4", "rw")),
		"writable-admission":    replace("/run/lz/admission.json", mountRow(33, "/run/lz/admission.json", rw, "ext4", "rw")),
		"host-backed-candidate": replace("/candidate", mountRow(35, "/candidate", rw, "ext4", "rw")),
		"unbounded-candidate":   replace("/candidate", mountRow(35, "/candidate", rw, "tmpfs", "rw,mode=755")),
		"unbounded-tmp":         replace("/tmp", mountRow(21, "/tmp", rw, "tmpfs", "rw,mode=755")),
		"missing-admission":     remove("/run/lz/admission.json"),
		"missing-source":        remove("/run/lz/src"),
		"duplicate-mount":       append(admittedTopology(), mountRow(53, "/candidate", rw, "tmpfs", "rw,size=1k")),
		"malformed-row":         append(admittedTopology(), "1 2 3"),
	}
	for name, rows := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateMounts(strings.Join(rows, "\n")); err == nil {
				t.Errorf("BEHAVIORAL_RED: %s admitted", name)
			}
		})
	}
}

func TestRuntimeEnvironment(t *testing.T) {
	admitted := []string{"PATH=/tcb:/tools/go/bin:/usr/bin:/bin", "HOME=/home/offline", "TF_CLI_CONFIG_FILE=/run/lz/tofurc",
		"GOTOOLCHAIN=local", "GOENV=off", "GOWORK=off", "GOPROXY=off", "GOSUMDB=off", "GOCACHE=/tmp/go-cache",
		"GOPATH=/tmp/go-path", "CGO_ENABLED=0"}
	if err := ValidateEnvironment(admitted); err != nil {
		t.Fatalf("admitted environment rejected: %v", err)
	}
	probe := append(append([]string{}, admitted...), "LZ_PROBE=1", "LZ_PROBE_BINARY=/tcb/probe", "LZ_HOST=/tmp/h", "LZ_URL=http://10.0.0.1:1")
	if err := ValidateEnvironment(probe); err != nil {
		t.Fatalf("admitted probe environment rejected: %v", err)
	}
	for name, extra := range map[string]string{
		"credential":      "OVH_CLIENT_SECRET=x",
		"token":           "AUTH_TOKEN=x",
		"aws":             "AWS_SECRET_ACCESS_KEY=x",
		"ssh-agent":       "SSH_AUTH_SOCK=/run/user/1000/ssh",
		"docker-host":     "DOCKER_HOST=unix:///run/docker.sock",
		"tf-variable":     "TF_VAR_password=x",
		"changed-path":    "PATH=/candidate/bin:/tcb",
		"changed-tofurc":  "TF_CLI_CONFIG_FILE=/candidate/tofurc",
		"proxy-on":        "GOPROXY=https://proxy.golang.org",
		"malformed-entry": "NOEQUALS",
		"host-pwd":        "PWD=/home/user/checkout",
	} {
		t.Run(name, func(t *testing.T) {
			env := append(append([]string{}, admitted...), extra)
			if err := ValidateEnvironment(env); err == nil {
				t.Errorf("BEHAVIORAL_RED: environment with %s admitted", name)
			}
		})
	}
}
