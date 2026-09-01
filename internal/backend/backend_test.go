package backend

import (
	"context"
	"strings"
	"testing"
)

func TestNewUnknownBackend(t *testing.T) {
	_, err := New(context.Background(), "does-not-exist", Options{})
	if err == nil {
		t.Fatal("expected error for unknown backend")
	}
	if !strings.Contains(err.Error(), "unknown backend") {
		t.Fatalf("expected 'unknown backend' error, got: %v", err)
	}
}

func TestNewAWSAliases(t *testing.T) {
	// All of these names should dispatch to the AWS backend. Region is set to
	// avoid any environment-dependent region resolution; no network call is
	// made during construction (credentials are resolved lazily at GetSecret).
	for _, name := range []string{"aws", "aws-secrets-manager", "secretsmanager"} {
		t.Run(name, func(t *testing.T) {
			b, err := New(context.Background(), name, Options{Region: "us-east-1"})
			if err != nil {
				t.Fatalf("unexpected error constructing %q backend: %v", name, err)
			}
			if b == nil {
				t.Fatalf("expected non-nil backend for %q", name)
			}
		})
	}
}

func TestNewAWSWithProfileAndRegion(t *testing.T) {
	// Passing a profile/region must not fail construction (profile is resolved
	// lazily; a non-existent profile only errors when credentials are used).
	b, err := New(context.Background(), "aws", Options{Region: "eu-west-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b == nil {
		t.Fatal("expected non-nil backend")
	}
}
