// Package backend defines the secret backend interface and its implementations.
package backend

import (
	"context"
	"fmt"
)

// Backend retrieves secret values by their identifier.
type Backend interface {
	// GetSecret returns the secret value for the given secret identifier.
	GetSecret(ctx context.Context, secretID string) (string, error)
}

// Options configures the construction of a Backend.
type Options struct {
	// Profile is the named credentials/config profile to use (backend-specific).
	Profile string
	// Region overrides the default region (backend-specific, optional).
	Region string
}

// New returns a Backend for the given name.
func New(ctx context.Context, name string, opts Options) (Backend, error) {
	switch name {
	case "aws", "aws-secrets-manager", "secretsmanager":
		return NewAWS(ctx, opts)
	default:
		return nil, fmt.Errorf("unknown backend %q", name)
	}
}
