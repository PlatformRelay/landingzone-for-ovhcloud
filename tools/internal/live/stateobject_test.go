package live

import (
	"strings"
	"testing"
)

// T100: StateObjectEncrypted is the chain's state-object rule (classifyStateObject, observe.go)
// for a probe check: an OpenTofu encryption envelope holds; a plaintext state, an object without
// the envelope and one that is no JSON do not. The detail is fixed text, nothing of the object.
func TestStateObjectEncrypted(t *testing.T) {
	cases := []struct {
		name, raw string
		want      bool
	}{
		// The shape of capture tests/fixtures/tofu-probes/captures/p4-encryption.txt.
		{"envelope", `{"serial":1,"lineage":"l","meta":{"key_provider.pbkdf2.main":"x"},"encrypted_data":"c2VjcmV0","encryption_version":"v0"}`, true},
		{"plaintext", `{"version":4,"terraform_version":"1.13.0","serial":1,"lineage":"l","resources":[{"type":"ovh_cloud_project_storage"}]}`, false},
		{"envelope-and-resources", `{"encrypted_data":"c2VjcmV0","resources":[]}`, false},
		{"empty-envelope", `{"encrypted_data":""}`, false},
		{"envelope-not-a-string", `{"encrypted_data":1}`, false},
		{"no-json", "\x00\x01ciphertext", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, detail := StateObjectEncrypted([]byte(tc.raw))
			if got != tc.want {
				t.Errorf("StateObjectEncrypted = %v (%s), want %v", got, detail, tc.want)
			}
			if detail == "" || len(tc.raw) > 4 && strings.Contains(detail, tc.raw) {
				t.Errorf("detail %q: want fixed text naming the reason, nothing of the object", detail)
			}
		})
	}
}
