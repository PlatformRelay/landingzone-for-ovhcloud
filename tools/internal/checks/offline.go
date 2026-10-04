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
	HostNetwork string   `json:"host_network"`
	Prepared    Prepared `json:"prepared"`
}

func RuntimePrepared() (Prepared, error) {
	data, err := os.ReadFile("/run/lz/admission.json")
	if err != nil {
		return Prepared{}, fmt.Errorf("ISOLATION_GATE: %w", err)
	}
	var gate RuntimeGate
	if err := json.Unmarshal(data, &gate); err != nil {
		return Prepared{}, fmt.Errorf("ISOLATION_GATE: %w", err)
	}
	network, err := os.Readlink("/proc/self/ns/net")
	if err != nil || gate.HostNetwork == "" || network == gate.HostNetwork {
		return Prepared{}, fmt.Errorf("ISOLATION_GATE: network namespace unchanged")
	}
	// An isolated namespace has no usable nonloopback interface. No outbound
	// request, DNS, timeout or candidate assertion establishes this gate.
	interfaces, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return Prepared{}, fmt.Errorf("ISOLATION_GATE: %w", err)
	}
	for _, line := range strings.Split(string(interfaces), "\n") {
		if name, _, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(name) != "lo" {
			return Prepared{}, fmt.Errorf("ISOLATION_GATE: unexpected network interface")
		}
	}
	mounts, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return Prepared{}, fmt.Errorf("ISOLATION_GATE: %w", err)
	}
	rootRO, gateRO := false, false
	for _, line := range strings.Split(string(mounts), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		readonly := strings.Contains(","+fields[5]+",", ",ro,")
		if fields[4] == "/" {
			rootRO = readonly
		}
		if fields[4] == "/run/lz/admission.json" {
			gateRO = readonly
		}
	}
	if !rootRO || !gateRO {
		return Prepared{}, fmt.Errorf("ISOLATION_GATE: read-only root and admission mount required")
	}
	gate.Prepared.IsolationProof = "runtime-qualified"
	if err := AdmitCapture(gate.Prepared); err != nil {
		return Prepared{}, err
	}
	return gate.Prepared, nil
}

// VerifyRuntime admits capture before invoking the pinned tools. Publication
// is owned by the external entry after exact observations and private cleanup.
func VerifyRuntime() error {
	p, err := RuntimePrepared()
	if err != nil {
		return err
	}
	return Capture(p, func() error {
		commands := []struct {
			name, path string
			args       []string
			expected   string
		}{
			{"go", "/tools/go/bin/go", []string{"version"}, "go version go1.27.1 linux/amd64"},
			{"tofu", "/tcb/tofu", []string{"version", "-json"}, "1.13.0"},
			{"terramate", "/tcb/terramate", []string{"version"}, "0.17.3"},
		}
		for _, tool := range commands {
			cmd := exec.Command(tool.path, tool.args...)
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
