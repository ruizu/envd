// Package cli implements the envd command-line interface.
package cli

import (
	"fmt"
	"strings"
)

// SecretMapping maps an environment variable name to a backend secret identifier.
type SecretMapping struct {
	EnvVar   string
	SecretID string
}

// ParseEnv parses a single --env value of the form VAR=secret_id into a
// SecretMapping, validating that VAR is a legal environment variable name.
func ParseEnv(value string) (SecretMapping, error) {
	eq := strings.IndexByte(value, '=')
	if eq <= 0 {
		return SecretMapping{}, fmt.Errorf("invalid --env %q: expected VAR=secret_id", value)
	}
	name := value[:eq]
	if !isValidEnvName(name) {
		return SecretMapping{}, fmt.Errorf("invalid --env %q: %q is not a valid environment variable name", value, name)
	}
	return SecretMapping{EnvVar: name, SecretID: value[eq+1:]}, nil
}

// ParseEnvs parses a slice of --env values into secret mappings. It rejects
// duplicate environment variable names to avoid a silent last-wins override.
func ParseEnvs(values []string) ([]SecretMapping, error) {
	mappings := make([]SecretMapping, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, v := range values {
		m, err := ParseEnv(v)
		if err != nil {
			return nil, err
		}
		if _, dup := seen[m.EnvVar]; dup {
			return nil, fmt.Errorf("duplicate --env variable %q", m.EnvVar)
		}
		seen[m.EnvVar] = struct{}{}
		mappings = append(mappings, m)
	}
	return mappings, nil
}

// isValidEnvName reports whether name is a valid environment variable name:
// starts with a letter or underscore, followed by letters, digits, or underscores.
func isValidEnvName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		switch {
		case r == '_':
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}
