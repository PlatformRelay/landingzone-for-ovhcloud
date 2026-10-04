// Package bundle admits a prepared runtime bundle: the manifest the entry was
// built for, the pinned tools, the fixed launcher and the identity of every
// prepared resource. The entry admits its bundle with it before every run,
// and lz-pack admits a bundle with it before packing, so an image is made only
// from a bundle the entry accepts.
package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/PlatformRelay/landingzone-for-ovhcloud/tools/internal/checks"
)

// Launcher is the only launcher path a manifest may name.
const Launcher = "/usr/bin/bwrap"

// Identity is one prepared resource: its kind, its content digest or link
// target, and its permission bits.
type Identity struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
	Mode  uint32 `json:"mode"`
}

// Resources is resources.json.
type Resources struct {
	Prepared checks.Prepared     `json:"prepared"`
	Files    map[string]Identity `json:"files"`
	Bwrap    string              `json:"bwrap"`
	BwrapSHA string              `json:"bwrap_sha256"`
}

func Hash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

// RealDirectory requires an absolute clean path whose every component is a
// directory, not a link.
func RealDirectory(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("INPUT_PATH: absolute clean directory required")
	}
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("INPUT_PATH: real directory required: %s", current)
		}
		if current == "/" {
			break
		}
	}
	return nil
}

// Regular reads a single-link regular file in a real directory.
func Regular(path string) ([]byte, error) {
	if err := RealDirectory(filepath.Dir(path)); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("INPUT_FILE: regular non-symlink file required: %s", path)
	}
	if metadata, ok := info.Sys().(*syscall.Stat_t); !ok || metadata.Nlink != 1 {
		return nil, fmt.Errorf("INPUT_FILE: single link required: %s", path)
	}
	return os.ReadFile(path)
}

// Inventory returns the identity of every entry under root/resources, keyed
// by its path relative to root. Links are admitted only inside
// resources/rootfs and must resolve inside it, an absolute target counting
// from the rootfs; other links and special files are refused.
func Inventory(root string) (map[string]Identity, error) {
	actual := map[string]Identity{}
	err := filepath.WalkDir(filepath.Join(root, "resources"), func(path string, entry os.DirEntry, incoming error) error {
		if incoming != nil {
			return incoming
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		value := Identity{Mode: uint32(info.Mode().Perm())}
		switch {
		case entry.IsDir():
			value.Kind = "directory"
		case info.Mode().IsRegular():
			bytes, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			value.Kind = "file"
			value.Value = Hash(bytes)
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			// Prepared rootfs links are allowed only inside its root; candidate
			// and helper links are never admitted.
			imageRoot := filepath.Join(root, "resources/rootfs")
			if !strings.HasPrefix(path, imageRoot+"/") {
				return fmt.Errorf("RESOURCE_LINK: %s", relative)
			}
			resolved := filepath.Clean(filepath.Join(filepath.Dir(path), target))
			if filepath.IsAbs(target) {
				resolved = filepath.Join(imageRoot, target)
			}
			if resolved != imageRoot && !strings.HasPrefix(resolved, imageRoot+"/") {
				return fmt.Errorf("RESOURCE_LINK: escaping link")
			}
			value.Kind = "symlink"
			value.Value = target
		default:
			return fmt.Errorf("RESOURCE_TYPE: %s", relative)
		}
		actual[relative] = value
		return nil
	})
	return actual, err
}

// Admit accepts root only if resources.json has the digest manifestSHA, names
// exactly the pinned tools and the fixed launcher, the file at launcher has
// the manifest's launcher digest, and the resources under root are exactly
// the manifest's. The entry passes the fixed launcher path itself; lz-pack
// passes the launcher it is about to pack.
func Admit(root, manifestSHA, launcher string) (Resources, error) {
	var r Resources
	data, err := Regular(filepath.Join(root, "resources.json"))
	if err != nil {
		return r, err
	}
	// An entry built without a digest has an empty one, which no hash equals.
	if Hash(data) != manifestSHA {
		return r, fmt.Errorf("RESOURCE_MANIFEST: approved digest differs")
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&r); err != nil {
		return r, fmt.Errorf("RESOURCE_MANIFEST: %w", err)
	}
	if err := checks.AdmitPins(r.Prepared); err != nil {
		return r, err
	}
	if r.Bwrap != Launcher {
		return r, fmt.Errorf("RESOURCE_HELPER: fixed launcher required")
	}
	helper, err := Regular(launcher)
	if err != nil || Hash(helper) != r.BwrapSHA {
		return r, fmt.Errorf("RESOURCE_HELPER: launcher differs")
	}
	actual, err := Inventory(root)
	if err != nil {
		return r, err
	}
	for relative, value := range actual {
		if expected, ok := r.Files[relative]; !ok || expected != value {
			return r, fmt.Errorf("RESOURCE_IDENTITY: %s", relative)
		}
	}
	if len(actual) != len(r.Files) {
		return r, fmt.Errorf("RESOURCE_IDENTITY: missing prepared resource")
	}
	return r, nil
}
