package profile

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const profilesDirName = "profiles"

// Profile represents a permission profile that restricts CLI operations.
type Profile struct {
	Name        string   `yaml:"-"`
	Description string   `yaml:"description,omitempty"`
	Enforce     bool     `yaml:"enforce"`
	Allow       []string `yaml:"allow"`
}

// IsAllowed checks whether the given permission is granted by this profile.
// A nil profile allows everything (no profile = no restrictions).
func (p *Profile) IsAllowed(permission string) bool {
	if p == nil {
		return true
	}
	for _, perm := range p.Allow {
		if perm == permission {
			return true
		}
	}
	return false
}

// ProfilesDir returns the path to ~/.o365-cli/profiles/
func ProfilesDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".o365-cli", profilesDirName)
}

// LoadProfile loads a profile by name from the profiles directory.
func LoadProfile(name string) (*Profile, error) {
	path := filepath.Join(ProfilesDir(), name+".yaml")
	return loadProfileFromPath(path, name)
}

// FindEnforcedProfile scans the profiles directory for a profile with enforce: true.
// Returns nil if no enforced profile exists.
func FindEnforcedProfile() (*Profile, error) {
	dir := ProfilesDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read profiles directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}

		name := entry.Name()[:len(entry.Name())-len(".yaml")]
		p, err := loadProfileFromPath(filepath.Join(dir, entry.Name()), name)
		if err != nil {
			continue
		}
		if p.Enforce {
			return p, nil
		}
	}

	return nil, nil
}

// ResolveProfile determines the active profile.
// Priority: enforced profile > --profile flag > no profile.
func ResolveProfile(flagValue string) (*Profile, error) {
	enforced, err := FindEnforcedProfile()
	if err != nil {
		return nil, err
	}
	if enforced != nil {
		return enforced, nil
	}

	if flagValue == "" {
		return nil, nil
	}

	return LoadProfile(flagValue)
}

func loadProfileFromPath(path, name string) (*Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read profile %q: %w", name, err)
	}

	var p Profile
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("failed to parse profile %q: %w", name, err)
	}
	p.Name = name

	return &p, nil
}
