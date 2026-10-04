package checks

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// RuntimeGate is placed read-only by the external launcher. It is never loaded
// from candidate data or environment. Its namespace differs from the host.
type RuntimeGate struct {
	HostNetwork string `json:"host_network"`
	// HostDevices lists the device ids of every host mount, so the gate can
	// tell a fresh private tmpfs from a bind of existing host storage.
	HostDevices []string `json:"host_devices"`
	Prepared    Prepared `json:"prepared"`
}

// MountDevices returns the device id of every row in a mountinfo listing.
func MountDevices(mountinfo string) []string {
	var devices []string
	for _, line := range strings.Split(mountinfo, "\n") {
		if fields := strings.Fields(line); len(fields) > 2 {
			devices = append(devices, fields[2])
		}
	}
	return devices
}

// Mount points lz-offline creates. Anything else, a writable trusted mount or a
// host-backed or unbounded writable area refuses admission.
var (
	readOnlyMounts = map[string]bool{"/": true, "/tcb": true, "/tools": true, "/tools/go": true, "/tcb/task": true,
		"/tcb/tofu": true, "/tcb/terramate": true, "/tcb/lz-offline": true, "/mirror": true, "/run/lz/tofurc": true,
		"/run/lz/admission.json": true, "/run/lz/src": true}
	optionalReadOnlyMounts = map[string]bool{"/tcb/probe": true}
	boundedScratchMounts   = map[string]bool{"/tmp": true, "/run": true, "/home": true, "/candidate": true}
	kernelMounts           = map[string]string{"/proc": "proc", "/dev": "tmpfs", "/dev/pts": "devpts", "/dev/null": "devtmpfs",
		"/dev/zero": "devtmpfs", "/dev/full": "devtmpfs", "/dev/random": "devtmpfs", "/dev/urandom": "devtmpfs", "/dev/tty": "devtmpfs"}
)

// ValidateMounts checks /proc/self/mountinfo against the admitted topology.
// Writable scratch must be a whole, fresh tmpfs: mounted at its root and on a
// device the host did not already have.
func ValidateMounts(mountinfo string, hostDevices map[string]bool) error {
	if len(hostDevices) == 0 {
		return fmt.Errorf("ISOLATION_MOUNT: host device inventory required")
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(mountinfo, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		left, right, ok := strings.Cut(line, " - ")
		mount, super := strings.Fields(left), strings.Fields(right)
		if !ok || len(mount) < 6 || len(super) < 3 {
			return fmt.Errorf("ISOLATION_MOUNT: malformed mountinfo row")
		}
		point, options, fstype := mount[4], ","+mount[5]+",", super[0]
		if seen[point] {
			return fmt.Errorf("ISOLATION_MOUNT: duplicate mount %s", point)
		}
		seen[point] = true
		readOnly := strings.Contains(options, ",ro,")
		switch {
		case readOnlyMounts[point] || optionalReadOnlyMounts[point]:
			if !readOnly {
				return fmt.Errorf("ISOLATION_MOUNT: %s must be read-only", point)
			}
		case boundedScratchMounts[point]:
			if fstype != "tmpfs" || !strings.Contains(","+super[2]+",", ",size=") {
				return fmt.Errorf("ISOLATION_MOUNT: %s must be a size-bounded tmpfs", point)
			}
			if mount[3] != "/" || hostDevices[mount[2]] {
				return fmt.Errorf("ISOLATION_MOUNT: %s must be a private tmpfs, not host storage", point)
			}
		case kernelMounts[point] != "":
			if fstype != kernelMounts[point] {
				return fmt.Errorf("ISOLATION_MOUNT: %s has unexpected type %s", point, fstype)
			}
		default:
			return fmt.Errorf("ISOLATION_MOUNT: unexpected mount %s", point)
		}
	}
	for _, required := range []map[string]bool{readOnlyMounts, boundedScratchMounts} {
		for point := range required {
			if !seen[point] {
				return fmt.Errorf("ISOLATION_MOUNT: missing mount %s", point)
			}
		}
	}
	return nil
}

// Environment the launcher sets. Fixed values must match; the probe values are
// synthetic test metadata and select nothing.
var (
	fixedEnvironment = map[string]string{"PATH": "/tcb:/tools/go/bin:/usr/bin:/bin", "HOME": "/home/offline",
		"TF_CLI_CONFIG_FILE": "/run/lz/tofurc", "GOTOOLCHAIN": "local", "GOENV": "off", "GOWORK": "off", "GOPROXY": "off",
		"GOSUMDB": "off", "GOCACHE": "/tmp/go-cache", "GOPATH": "/tmp/go-path", "CGO_ENABLED": "0",
		"LZ_PROBE": "1", "LZ_PROBE_BINARY": "/tcb/probe", "PWD": "/candidate"}
	childPath        = "/tcb:/usr/bin:/bin"
	probeEnvironment = map[string]bool{"LZ_HOST": true, "LZ_URL": true}
)

// ValidateEnvironment refuses any variable the launcher did not set, so leaked
// credentials, agent sockets or proxy settings stop admission.
func ValidateEnvironment(environment []string) error {
	seen := map[string]bool{}
	for _, entry := range environment {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || seen[name] {
			return fmt.Errorf("ISOLATION_ENV: malformed or duplicate variable")
		}
		seen[name] = true
		expected, fixed := fixedEnvironment[name]
		switch {
		case fixed && (value == expected || name == "PATH" && value == childPath):
		case probeEnvironment[name]:
		default:
			return fmt.Errorf("ISOLATION_ENV: unexpected variable %s", name)
		}
	}
	return nil
}

// RuntimePrepared verifies the namespace, interfaces, mounts and environment
// actually in force and only then issues isolation evidence.
func RuntimePrepared() (Prepared, Isolation, error) {
	data, err := os.ReadFile("/run/lz/admission.json")
	if err != nil {
		return Prepared{}, Isolation{}, fmt.Errorf("ISOLATION_GATE: %w", err)
	}
	var gate RuntimeGate
	if err := json.Unmarshal(data, &gate); err != nil {
		return Prepared{}, Isolation{}, fmt.Errorf("ISOLATION_GATE: %w", err)
	}
	network, err := os.Readlink("/proc/self/ns/net")
	if err != nil || gate.HostNetwork == "" || network == gate.HostNetwork {
		return Prepared{}, Isolation{}, fmt.Errorf("ISOLATION_GATE: network namespace unchanged")
	}
	// An isolated namespace has no usable nonloopback interface. No outbound
	// request, DNS, timeout or candidate assertion establishes this gate.
	interfaces, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return Prepared{}, Isolation{}, fmt.Errorf("ISOLATION_GATE: %w", err)
	}
	for _, line := range strings.Split(string(interfaces), "\n") {
		if name, _, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(name) != "lo" {
			return Prepared{}, Isolation{}, fmt.Errorf("ISOLATION_GATE: unexpected network interface")
		}
	}
	mounts, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return Prepared{}, Isolation{}, fmt.Errorf("ISOLATION_GATE: %w", err)
	}
	hostDevices := map[string]bool{}
	for _, device := range gate.HostDevices {
		hostDevices[device] = true
	}
	if err := ValidateMounts(string(mounts), hostDevices); err != nil {
		return Prepared{}, Isolation{}, err
	}
	if err := ValidateEnvironment(os.Environ()); err != nil {
		return Prepared{}, Isolation{}, err
	}
	if err := AdmitPins(gate.Prepared); err != nil {
		return Prepared{}, Isolation{}, err
	}
	return gate.Prepared, Isolation{network: network}, nil
}

// VerifyRuntime admits capture before invoking the pinned tools. Publication
// is owned by the external entry after exact observations and private cleanup.
func VerifyRuntime() error {
	p, isolation, err := RuntimePrepared()
	if err != nil {
		return err
	}
	return Capture(p, isolation, func() error {
		commands := []struct {
			name, path string
			args       []string
			expected   string
		}{
			{"go", "/tools/go/bin/go", []string{"version"}, "go version go1.27.1 linux/amd64"},
			{"tofu", "/tcb/tofu", []string{"version", "-json"}, "1.13.0"},
			{"terramate", "/tcb/terramate", []string{"version"}, "0.17.3"},
			{"task", "/tcb/task", []string{"--version"}, "3.53.1"},
		}
		for _, tool := range commands {
			cmd := exec.Command(tool.path, tool.args...)
			cmd.Dir = "/tmp"
			cmd.Env = []string{"PATH=/tcb:/tools/go/bin:/usr/bin:/bin", "HOME=/home/offline", "TF_CLI_CONFIG_FILE=/run/lz/tofurc", "GOTOOLCHAIN=local", "GOENV=off", "GOWORK=off", "GOPROXY=off", "GOSUMDB=off"}
			output, err := cmd.Output()
			if err != nil {
				return fmt.Errorf("PIN_TOOL: %s: %w", tool.name, err)
			}
			observed := strings.TrimSpace(string(output))
			if tool.name == "tofu" {
				var version struct {
					Version string `json:"terraform_version"`
				}
				if err := json.Unmarshal(output, &version); err != nil {
					return fmt.Errorf("PIN_TOOL: malformed tofu version")
				}
				observed = version.Version
			}
			if observed != tool.expected {
				return fmt.Errorf("PIN_VERSION: %s", tool.name)
			}
		}
		return nil
	}, func() error { return nil })
}
