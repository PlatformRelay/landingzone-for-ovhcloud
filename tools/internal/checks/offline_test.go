package checks

import (
	"strconv"
	"strings"
	"testing"
)

// Synthetic device identities: hostDisk and hostShm are mounts the host entry
// already has; scratch tmpfs mounts created by bwrap get fresh anonymous ids.
const (
	hostDisk = "259:2"
	hostShm  = "0:25"
)

var hostDevices = map[string]bool{hostDisk: true, hostShm: true}

type mount struct{ point, options, fstype, super, device, root string }

// row renders one /proc/self/mountinfo line for the gate's parser.
func (m mount) row(id int) string {
	device, root := m.device, m.root
	if device == "" {
		device = hostDisk
	}
	if root == "" {
		root = "/"
	}
	return strings.Join([]string{strconv.Itoa(id), "1", device, root, m.point, m.options, "-", m.fstype, "none", m.super}, " ")
}

const (
	ro = "ro,nosuid,nodev,relatime"
	rw = "rw,nosuid,nodev,relatime"
)

// admittedMounts is the layout lz-offline creates, as observed inside bwrap
// 0.11.1 on the qualified preparation.
func admittedMounts() []mount {
	mounts := []mount{
		{point: "/", options: ro, fstype: "ext4", super: "rw", root: "/home/user/rootfs"},
		{point: "/proc", options: rw, fstype: "proc", super: "rw", device: "0:60"},
		{point: "/dev", options: rw, fstype: "tmpfs", super: "rw,mode=755", device: "0:61"},
	}
	for _, device := range []string{"null", "zero", "full", "random", "urandom", "tty"} {
		mounts = append(mounts, mount{point: "/dev/" + device, options: "rw,nosuid", fstype: "devtmpfs", super: "rw", device: "0:5"})
	}
	return append(mounts,
		mount{point: "/dev/pts", options: rw, fstype: "devpts", super: "rw", device: "0:62"},
		mount{point: "/tmp", options: rw, fstype: "tmpfs", super: "rw,size=2097152k", device: "0:63"},
		mount{point: "/run", options: rw, fstype: "tmpfs", super: "rw,size=16384k", device: "0:64"},
		mount{point: "/home", options: rw, fstype: "tmpfs", super: "rw,size=65536k", device: "0:65"},
		mount{point: "/tcb", options: ro, fstype: "tmpfs", super: "rw", device: "0:66"},
		mount{point: "/tools", options: ro, fstype: "tmpfs", super: "rw", device: "0:67"},
		mount{point: "/tools/go", options: ro, fstype: "ext4", super: "rw"},
		mount{point: "/tcb/task", options: ro, fstype: "ext4", super: "rw"},
		mount{point: "/tcb/tflint", options: ro, fstype: "ext4", super: "rw"},
		mount{point: "/tcb/tofu", options: ro, fstype: "ext4", super: "rw"},
		mount{point: "/tcb/terramate", options: ro, fstype: "ext4", super: "rw"},
		mount{point: "/tcb/lz-offline", options: ro, fstype: "ext4", super: "rw"},
		mount{point: "/mirror", options: ro, fstype: "ext4", super: "rw"},
		mount{point: "/run/lz/tofurc", options: ro, fstype: "ext4", super: "rw"},
		mount{point: "/run/lz/admission.json", options: ro, fstype: "ext4", super: "rw"},
		mount{point: "/run/lz/src", options: ro, fstype: "ext4", super: "rw"},
		mount{point: "/candidate", options: rw, fstype: "tmpfs", super: "rw,size=1048576k", device: "0:68"},
	)
}

func render(mounts []mount) string {
	rows := make([]string, len(mounts))
	for i, m := range mounts {
		rows[i] = m.row(i + 1)
	}
	return strings.Join(rows, "\n") + "\n"
}

func TestRuntimeMountsAdmitted(t *testing.T) {
	if err := ValidateMounts(render(admittedMounts()), hostDevices); err != nil {
		t.Fatalf("admitted topology rejected: %v", err)
	}
	withProbe := append(admittedMounts(), mount{point: "/tcb/probe", options: ro, fstype: "ext4", super: "rw"})
	if err := ValidateMounts(render(withProbe), hostDevices); err != nil {
		t.Fatalf("admitted topology with probe rejected: %v", err)
	}
}

func TestRuntimeMountsRejected(t *testing.T) {
	change := func(point string, edit func(*mount)) []mount {
		mounts := admittedMounts()
		for i := range mounts {
			if mounts[i].point == point {
				edit(&mounts[i])
			}
		}
		return mounts
	}
	remove := func(point string) []mount {
		var mounts []mount
		for _, m := range admittedMounts() {
			if m.point != point {
				mounts = append(mounts, m)
			}
		}
		return mounts
	}
	writable := func(m *mount) { m.options = rw }
	cases := map[string][]mount{
		"credential-directory":   append(admittedMounts(), mount{point: "/home/offline/.config/ovh", options: ro, fstype: "ext4", super: "rw"}),
		"runtime-socket":         append(admittedMounts(), mount{point: "/run/docker.sock", options: rw, fstype: "ext4", super: "rw"}),
		"shared-cache":           append(admittedMounts(), mount{point: "/tmp/go-cache", options: rw, fstype: "ext4", super: "rw"}),
		"writable-root":          change("/", writable),
		"writable-tool":          change("/tcb/task", writable),
		"writable-linter":        change("/tcb/tflint", writable),
		"writable-tool-dir":      change("/tcb", writable),
		"writable-source":        change("/run/lz/src", writable),
		"writable-admission":     change("/run/lz/admission.json", writable),
		"host-backed-candidate":  change("/candidate", func(m *mount) { m.fstype, m.device = "ext4", hostDisk }),
		"host-tmpfs-bind":        change("/candidate", func(m *mount) { m.device = hostShm }),
		"host-tmpfs-subtree":     change("/candidate", func(m *mount) { m.root = "/lz-cache" }),
		"host-tmpfs-tmp":         change("/tmp", func(m *mount) { m.device = hostShm }),
		"host-tmpfs-dev":         change("/dev", func(m *mount) { m.device = hostShm }),
		"host-tmpfs-dev-subtree": change("/dev", func(m *mount) { m.root = "/lz-dev" }),
		"unbounded-candidate":    change("/candidate", func(m *mount) { m.super = "rw,mode=755" }),
		"unbounded-tmp":          change("/tmp", func(m *mount) { m.super = "rw,mode=755" }),
		"missing-admission":      remove("/run/lz/admission.json"),
		"missing-source":         remove("/run/lz/src"),
		"duplicate-mount":        append(admittedMounts(), mount{point: "/candidate", options: rw, fstype: "tmpfs", super: "rw,size=1k", device: "0:70"}),
	}
	for name, mounts := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateMounts(render(mounts), hostDevices); err == nil {
				t.Errorf("BEHAVIORAL_RED: %s admitted", name)
			}
		})
	}
	t.Run("malformed-row", func(t *testing.T) {
		if err := ValidateMounts(render(admittedMounts())+"1 2 3\n", hostDevices); err == nil {
			t.Error("BEHAVIORAL_RED: malformed row admitted")
		}
	})
	t.Run("no-host-inventory", func(t *testing.T) {
		if err := ValidateMounts(render(admittedMounts()), nil); err == nil {
			t.Error("BEHAVIORAL_RED: scratch provenance admitted without a host device inventory")
		}
	})
}

func admittedEnvironment() []string {
	return []string{"PATH=/tcb:/tools/go/bin:/usr/bin:/bin", "HOME=/home/offline", "TF_CLI_CONFIG_FILE=/run/lz/tofurc",
		"GOTOOLCHAIN=local", "GOENV=off", "GOWORK=off", "GOPROXY=off", "GOSUMDB=off", "GOCACHE=/tmp/go-cache",
		"GOPATH=/tmp/go-path", "CGO_ENABLED=0", "PWD=/candidate"}
}

func TestRuntimeEnvironment(t *testing.T) {
	if err := ValidateEnvironment(admittedEnvironment()); err != nil {
		t.Fatalf("admitted environment rejected: %v", err)
	}
	probe := append(admittedEnvironment(), "LZ_PROBE=1", "LZ_PROBE_BINARY=/tcb/probe", "LZ_HOST=/tmp/h", "LZ_URL=http://10.0.0.1:1")
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
		"malformed-entry": "NOEQUALS",
		"duplicate":       "HOME=/home/offline",
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateEnvironment(append(admittedEnvironment(), extra)); err == nil {
				t.Errorf("BEHAVIORAL_RED: environment with %s admitted", name)
			}
		})
	}
	// Changed values replace the admitted entry, so each one exercises the value
	// check rather than duplicate rejection.
	for name, changed := range map[string]string{
		"changed-path":   "PATH=/candidate/bin:/tcb",
		"changed-tofurc": "TF_CLI_CONFIG_FILE=/candidate/tofurc",
		"proxy-on":       "GOPROXY=https://proxy.golang.org",
		"host-pwd":       "PWD=/home/user/checkout",
		"changed-cache":  "GOCACHE=/candidate/.cache",
	} {
		t.Run(name, func(t *testing.T) {
			key, _, _ := strings.Cut(changed, "=")
			env := admittedEnvironment()
			for i, entry := range env {
				if strings.HasPrefix(entry, key+"=") {
					env[i] = changed
				}
			}
			if err := ValidateEnvironment(env); err == nil {
				t.Errorf("BEHAVIORAL_RED: environment with %s admitted", name)
			}
		})
	}
}
