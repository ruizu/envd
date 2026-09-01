package backend

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// fakeSecretsClient is a test double for the AWS Secrets Manager client.
type fakeSecretsClient struct {
	out *secretsmanager.GetSecretValueOutput
	err error
	// gotID captures the SecretId passed in, to assert it is forwarded.
	gotID string
}

func (f *fakeSecretsClient) GetSecretValue(_ context.Context, in *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	if in.SecretId != nil {
		f.gotID = *in.SecretId
	}
	return f.out, f.err
}

func TestGetSecretString(t *testing.T) {
	fake := &fakeSecretsClient{
		out: &secretsmanager.GetSecretValueOutput{SecretString: aws.String("plain-secret")},
	}
	b := &awsBackend{client: fake}

	got, err := b.GetSecret(context.Background(), "my/secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "plain-secret" {
		t.Fatalf("got %q, want %q", got, "plain-secret")
	}
	if fake.gotID != "my/secret" {
		t.Fatalf("secret id not forwarded: got %q", fake.gotID)
	}
}

func TestGetSecretBinary(t *testing.T) {
	fake := &fakeSecretsClient{
		out: &secretsmanager.GetSecretValueOutput{SecretBinary: []byte("binary-secret")},
	}
	b := &awsBackend{client: fake}

	got, err := b.GetSecret(context.Background(), "id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "binary-secret" {
		t.Fatalf("got %q, want %q", got, "binary-secret")
	}
}

func TestGetSecretPrefersStringOverBinary(t *testing.T) {
	// When both are set, SecretString takes precedence.
	fake := &fakeSecretsClient{
		out: &secretsmanager.GetSecretValueOutput{
			SecretString: aws.String("the-string"),
			SecretBinary: []byte("the-binary"),
		},
	}
	b := &awsBackend{client: fake}

	got, err := b.GetSecret(context.Background(), "id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "the-string" {
		t.Fatalf("expected SecretString to win, got %q", got)
	}
}

func TestGetSecretEmptyString(t *testing.T) {
	// A present-but-empty SecretString is a valid value, not an error.
	fake := &fakeSecretsClient{
		out: &secretsmanager.GetSecretValueOutput{SecretString: aws.String("")},
	}
	b := &awsBackend{client: fake}

	got, err := b.GetSecret(context.Background(), "id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty string value, got %q", got)
	}
}

func TestGetSecretNoValue(t *testing.T) {
	// Neither SecretString nor SecretBinary set -> error.
	fake := &fakeSecretsClient{out: &secretsmanager.GetSecretValueOutput{}}
	b := &awsBackend{client: fake}

	_, err := b.GetSecret(context.Background(), "id")
	if err == nil {
		t.Fatal("expected error when secret has no value")
	}
	if !strings.Contains(err.Error(), "no value") {
		t.Fatalf("expected 'no value' error, got: %v", err)
	}
}

func TestGetSecretAPIError(t *testing.T) {
	sentinel := errors.New("access denied")
	fake := &fakeSecretsClient{err: sentinel}
	b := &awsBackend{client: fake}

	_, err := b.GetSecret(context.Background(), "id")
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected API error to propagate, got: %v", err)
	}
	if !strings.Contains(err.Error(), "getting secret") {
		t.Fatalf("expected wrapped context, got: %v", err)
	}
}
