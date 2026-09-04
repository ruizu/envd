// Package backend defines the secret backend interface and its implementations.
package backend

import (
	"context"
	"fmt"
)

// Backend resolves values (secrets, parameters, etc.) by their identifier.
type Backend interface {
	// Resolve returns the value for the given identifier.
	Resolve(ctx context.Context, id string) (string, error)
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
	case "aws":
		return NewAWS(ctx, opts)
	default:
		return nil, fmt.Errorf("unknown backend %q", name)
	}
}
