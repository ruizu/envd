package cli

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseEnv(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    SecretMapping
		wantErr bool
	}{
		{
			name:  "simple mapping",
			value: "SECRET_VAR1=aws_secret_id1",
			want:  SecretMapping{EnvVar: "SECRET_VAR1", SecretID: "aws_secret_id1"},
		},
		{
			name:  "value containing equals signs",
			value: "TOKEN=a=b=c",
			want:  SecretMapping{EnvVar: "TOKEN", SecretID: "a=b=c"},
		},
		{
			name:  "underscore-prefixed name",
			value: "_HIDDEN=id",
			want:  SecretMapping{EnvVar: "_HIDDEN", SecretID: "id"},
		},
		{
			name:    "no equals sign",
			value:   "SECRET_VAR1",
			wantErr: true,
		},
		{
			name:    "empty name",
			value:   "=id",
			wantErr: true,
		},
		{
			name:    "invalid name with dash",
			value:   "not-a-var=id",
			wantErr: true,
		},
		{
			name:    "name starting with digit",
			value:   "1VAR=id",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseEnv(tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got none", tt.value)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseEnvs(t *testing.T) {
	got, err := ParseEnvs([]string{"A=1", "B=2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []SecretMapping{{EnvVar: "A", SecretID: "1"}, {EnvVar: "B", SecretID: "2"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}

	if _, err := ParseEnvs([]string{"A=1", "bad-name=2"}); err == nil {
		t.Fatal("expected error when one mapping is invalid")
	}
}

func TestParseEnvsRejectsDuplicates(t *testing.T) {
	_, err := ParseEnvs([]string{"A=id1", "B=id2", "A=id3"})
	if err == nil {
		t.Fatal("expected error for duplicate --env variable name")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected 'duplicate' error, got: %v", err)
	}

	// Distinct names must still succeed.
	if _, err := ParseEnvs([]string{"A=id1", "B=id2"}); err != nil {
		t.Fatalf("unexpected error for distinct names: %v", err)
	}
}

func TestIsValidEnvName(t *testing.T) {
	valid := []string{"A", "_", "ABC", "abc_123", "_x1"}
	invalid := []string{"", "1abc", "a-b", "a.b", "a b"}

	for _, s := range valid {
		if !isValidEnvName(s) {
			t.Errorf("expected %q to be valid", s)
		}
	}
	for _, s := range invalid {
		if isValidEnvName(s) {
			t.Errorf("expected %q to be invalid", s)
		}
	}
}
