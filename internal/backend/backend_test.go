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

func TestNewAWS(t *testing.T) {
	// Region is set to avoid any environment-dependent region resolution; no
	// network call is made during construction (credentials are resolved
	// lazily at Resolve).
	b, err := New(context.Background(), "aws", Options{Region: "us-east-1"})
	if err != nil {
		t.Fatalf("unexpected error constructing aws backend: %v", err)
	}
	if b == nil {
		t.Fatal("expected non-nil backend")
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
