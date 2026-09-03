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
	// gotVersionStage and gotVersionID capture the version selectors passed in.
	gotVersionStage string
	gotVersionID    string
	// called records whether GetSecretValue was invoked at all.
	called bool
}

func (f *fakeSecretsClient) GetSecretValue(_ context.Context, in *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	f.called = true
	if in.SecretId != nil {
		f.gotID = *in.SecretId
	}
	if in.VersionStage != nil {
		f.gotVersionStage = *in.VersionStage
	}
	if in.VersionId != nil {
		f.gotVersionID = *in.VersionId
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

func TestParseSecretRef(t *testing.T) {
	const arn = "arn:aws:secretsmanager:us-east-1:123456789012:secret:appauthexample-AbCdEf"

	tests := []struct {
		name    string
		id      string
		want    secretRef
		wantErr bool
	}{
		{
			name: "plain name",
			id:   "my/secret",
			want: secretRef{secretID: "my/secret"},
		},
		{
			name: "full arn no trailing fields",
			id:   arn,
			want: secretRef{secretID: arn},
		},
		{
			name: "arn with json key",
			id:   arn + ":username1::",
			want: secretRef{secretID: arn, jsonKey: "username1"},
		},
		{
			name: "arn with version stage",
			id:   arn + "::AWSPREVIOUS:",
			want: secretRef{secretID: arn, versionStage: "AWSPREVIOUS"},
		},
		{
			name: "arn with version id",
			id:   arn + ":::9d4cb84b-ad69-40c0-a0ab-cead3EXAMPLE",
			want: secretRef{secretID: arn, versionID: "9d4cb84b-ad69-40c0-a0ab-cead3EXAMPLE"},
		},
		{
			name: "arn with json key and version stage",
			id:   arn + ":username1:AWSPREVIOUS:",
			want: secretRef{secretID: arn, jsonKey: "username1", versionStage: "AWSPREVIOUS"},
		},
		{
			name: "arn with json key and version id",
			id:   arn + ":username1::9d4cb84b-ad69-40c0-a0ab-cead3EXAMPLE",
			want: secretRef{secretID: arn, jsonKey: "username1", versionID: "9d4cb84b-ad69-40c0-a0ab-cead3EXAMPLE"},
		},
		{
			name: "non-secretsmanager arn passes through",
			id:   "arn:aws:ssm:us-east-1:123456789012:parameter/foo",
			want: secretRef{secretID: "arn:aws:ssm:us-east-1:123456789012:parameter/foo"},
		},
		// short form: secretsmanager:secret-id:field-type:json-key:version-stage
		{
			name: "resolve name only",
			id:   "secretsmanager:prod/db",
			want: secretRef{secretID: "prod/db"},
		},
		{
			name: "resolve with field-type",
			id:   "secretsmanager:prod/db:SecretString",
			want: secretRef{secretID: "prod/db"},
		},
		{
			name: "resolve with json key",
			id:   "secretsmanager:prod/db:SecretString:username",
			want: secretRef{secretID: "prod/db", jsonKey: "username"},
		},
		{
			name: "resolve with json key default field-type",
			id:   "secretsmanager:prod/db::username",
			want: secretRef{secretID: "prod/db", jsonKey: "username"},
		},
		{
			name: "resolve with json key and version stage",
			id:   "secretsmanager:prod/db:SecretString:username:AWSPREVIOUS",
			want: secretRef{secretID: "prod/db", jsonKey: "username", versionStage: "AWSPREVIOUS"},
		},
		{
			name: "resolve with version stage only",
			id:   "secretsmanager:prod/db:::AWSPREVIOUS",
			want: secretRef{secretID: "prod/db", versionStage: "AWSPREVIOUS"},
		},
		{
			name: "resolve json key containing colons preserved",
			id:   "secretsmanager:prod/db:SecretString:a:b:c",
			want: secretRef{secretID: "prod/db", jsonKey: "a", versionStage: "b:c"},
		},
		{
			name:    "resolve missing secret id",
			id:      "secretsmanager:",
			wantErr: true,
		},
		{
			name:    "resolve invalid field-type",
			id:      "secretsmanager:prod/db:Binary",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSecretRef(tt.id)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseSecretRef(%q) = %+v, want error", tt.id, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSecretRef(%q) unexpected error: %v", tt.id, err)
			}
			if got != tt.want {
				t.Fatalf("parseSecretRef(%q) = %+v, want %+v", tt.id, got, tt.want)
			}
		})
	}
}

func TestGetSecretARNStripsTrailingFields(t *testing.T) {
	const arn = "arn:aws:secretsmanager:us-east-1:123456789012:secret:appauthexample-AbCdEf"
	fake := &fakeSecretsClient{
		out: &secretsmanager.GetSecretValueOutput{SecretString: aws.String("plain")},
	}
	b := &awsBackend{client: fake}

	if _, err := b.GetSecret(context.Background(), arn+":::"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.gotID != arn {
		t.Fatalf("SecretId not stripped of trailing fields: got %q, want %q", fake.gotID, arn)
	}
}

func TestGetSecretJSONKey(t *testing.T) {
	const arn = "arn:aws:secretsmanager:us-east-1:123456789012:secret:appauthexample-AbCdEf"
	fake := &fakeSecretsClient{
		out: &secretsmanager.GetSecretValueOutput{
			SecretString: aws.String(`{"username1":"password1","username2":"password2"}`),
		},
	}
	b := &awsBackend{client: fake}

	got, err := b.GetSecret(context.Background(), arn+":username2::")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "password2" {
		t.Fatalf("got %q, want %q", got, "password2")
	}
}

func TestGetSecretJSONKeyNumericValue(t *testing.T) {
	// Non-string JSON values are rendered as their JSON text.
	const arn = "arn:aws:secretsmanager:us-east-1:123456789012:secret:cfg-AbCdEf"
	fake := &fakeSecretsClient{
		out: &secretsmanager.GetSecretValueOutput{
			SecretString: aws.String(`{"port":5432}`),
		},
	}
	b := &awsBackend{client: fake}

	got, err := b.GetSecret(context.Background(), arn+":port::")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "5432" {
		t.Fatalf("got %q, want %q", got, "5432")
	}
}

func TestGetSecretJSONKeyMissing(t *testing.T) {
	const arn = "arn:aws:secretsmanager:us-east-1:123456789012:secret:appauthexample-AbCdEf"
	fake := &fakeSecretsClient{
		out: &secretsmanager.GetSecretValueOutput{
			SecretString: aws.String(`{"username1":"password1"}`),
		},
	}
	b := &awsBackend{client: fake}

	_, err := b.GetSecret(context.Background(), arn+":nope::")
	if err == nil {
		t.Fatal("expected error for missing JSON key")
	}
	if !strings.Contains(err.Error(), "no JSON key") {
		t.Fatalf("expected 'no JSON key' error, got: %v", err)
	}
}

func TestGetSecretJSONKeyNotJSON(t *testing.T) {
	const arn = "arn:aws:secretsmanager:us-east-1:123456789012:secret:appauthexample-AbCdEf"
	fake := &fakeSecretsClient{
		out: &secretsmanager.GetSecretValueOutput{
			SecretString: aws.String("not-json"),
		},
	}
	b := &awsBackend{client: fake}

	_, err := b.GetSecret(context.Background(), arn+":key::")
	if err == nil {
		t.Fatal("expected error when secret is not a JSON object")
	}
	if !strings.Contains(err.Error(), "not a JSON object") {
		t.Fatalf("expected 'not a JSON object' error, got: %v", err)
	}
}

func TestGetSecretVersionSelectorsForwarded(t *testing.T) {
	const arn = "arn:aws:secretsmanager:us-east-1:123456789012:secret:appauthexample-AbCdEf"
	fake := &fakeSecretsClient{
		out: &secretsmanager.GetSecretValueOutput{SecretString: aws.String("v")},
	}
	b := &awsBackend{client: fake}

	// version-stage
	if _, err := b.GetSecret(context.Background(), arn+"::AWSPREVIOUS:"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.gotVersionStage != "AWSPREVIOUS" {
		t.Fatalf("version stage not forwarded: got %q", fake.gotVersionStage)
	}
	if fake.gotVersionID != "" {
		t.Fatalf("unexpected version id forwarded: %q", fake.gotVersionID)
	}

	// version-id
	fake.gotVersionStage = ""
	if _, err := b.GetSecret(context.Background(), arn+":::abc-123"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.gotVersionID != "abc-123" {
		t.Fatalf("version id not forwarded: got %q", fake.gotVersionID)
	}
	if fake.gotVersionStage != "" {
		t.Fatalf("unexpected version stage forwarded: %q", fake.gotVersionStage)
	}
}

func TestGetSecretResolveForm(t *testing.T) {
	fake := &fakeSecretsClient{
		out: &secretsmanager.GetSecretValueOutput{
			SecretString: aws.String(`{"username":"alice","password":"s3cr3t"}`),
		},
	}
	b := &awsBackend{client: fake}

	got, err := b.GetSecret(context.Background(), "secretsmanager:prod/db:SecretString:password:AWSPREVIOUS")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "s3cr3t" {
		t.Fatalf("got %q, want %q", got, "s3cr3t")
	}
	if fake.gotID != "prod/db" {
		t.Fatalf("secret id: got %q, want %q", fake.gotID, "prod/db")
	}
	if fake.gotVersionStage != "AWSPREVIOUS" {
		t.Fatalf("version stage: got %q, want %q", fake.gotVersionStage, "AWSPREVIOUS")
	}
}

func TestGetSecretResolveWholeValue(t *testing.T) {
	// The short form with no json-key returns the whole secret verbatim.
	fake := &fakeSecretsClient{
		out: &secretsmanager.GetSecretValueOutput{SecretString: aws.String(`{"a":"b"}`)},
	}
	b := &awsBackend{client: fake}

	got, err := b.GetSecret(context.Background(), "secretsmanager:prod/db")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != `{"a":"b"}` {
		t.Fatalf("got %q, want whole secret", got)
	}
	if fake.gotID != "prod/db" {
		t.Fatalf("secret id: got %q, want %q", fake.gotID, "prod/db")
	}
	if fake.gotVersionStage != "" || fake.gotVersionID != "" {
		t.Fatalf("no version selectors expected, got stage=%q id=%q", fake.gotVersionStage, fake.gotVersionID)
	}
}

func TestGetSecretParseErrorSkipsClient(t *testing.T) {
	// An invalid short-form reference must fail before any API call is made.
	tests := []string{
		"secretsmanager:",               // missing secret id
		"secretsmanager:prod/db:Binary", // unsupported field-type
	}
	for _, id := range tests {
		t.Run(id, func(t *testing.T) {
			fake := &fakeSecretsClient{
				out: &secretsmanager.GetSecretValueOutput{SecretString: aws.String("x")},
			}
			b := &awsBackend{client: fake}

			if _, err := b.GetSecret(context.Background(), id); err == nil {
				t.Fatalf("expected parse error for %q", id)
			}
			if fake.called {
				t.Fatalf("client should not be called on parse error for %q", id)
			}
		})
	}
}

func TestGetSecretJSONKeyFromBinary(t *testing.T) {
	// A json-key can be extracted from a JSON document stored as SecretBinary.
	const arn = "arn:aws:secretsmanager:us-east-1:123456789012:secret:cfg-AbCdEf"
	fake := &fakeSecretsClient{
		out: &secretsmanager.GetSecretValueOutput{
			SecretBinary: []byte(`{"token":"abc123"}`),
		},
	}
	b := &awsBackend{client: fake}

	got, err := b.GetSecret(context.Background(), arn+":token::")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "abc123" {
		t.Fatalf("got %q, want %q", got, "abc123")
	}
}

func TestGetSecretJSONKeyBooleanValue(t *testing.T) {
	// Boolean values are rendered as their JSON text.
	const arn = "arn:aws:secretsmanager:us-east-1:123456789012:secret:cfg-AbCdEf"
	fake := &fakeSecretsClient{
		out: &secretsmanager.GetSecretValueOutput{
			SecretString: aws.String(`{"enabled":true}`),
		},
	}
	b := &awsBackend{client: fake}

	got, err := b.GetSecret(context.Background(), arn+":enabled::")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "true" {
		t.Fatalf("got %q, want %q", got, "true")
	}
}

func TestGetSecretJSONKeyNestedValue(t *testing.T) {
	// Object/array values are returned as their raw JSON text.
	const arn = "arn:aws:secretsmanager:us-east-1:123456789012:secret:cfg-AbCdEf"
	fake := &fakeSecretsClient{
		out: &secretsmanager.GetSecretValueOutput{
			SecretString: aws.String(`{"nested":{"k":"v"},"list":[1,2]}`),
		},
	}
	b := &awsBackend{client: fake}

	gotObj, err := b.GetSecret(context.Background(), arn+":nested::")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotObj != `{"k":"v"}` {
		t.Fatalf("nested object: got %q, want %q", gotObj, `{"k":"v"}`)
	}

	gotList, err := b.GetSecret(context.Background(), arn+":list::")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotList != `[1,2]` {
		t.Fatalf("nested list: got %q, want %q", gotList, `[1,2]`)
	}
}

func TestGetSecretARNJSONKeyWithVersionID(t *testing.T) {
	// End-to-end: ARN with both a json-key and a version-id.
	const arn = "arn:aws:secretsmanager:us-east-1:123456789012:secret:appauthexample-AbCdEf"
	fake := &fakeSecretsClient{
		out: &secretsmanager.GetSecretValueOutput{
			SecretString: aws.String(`{"username1":"alice"}`),
		},
	}
	b := &awsBackend{client: fake}

	got, err := b.GetSecret(context.Background(), arn+":username1::9d4cb84b-ad69-40c0-a0ab-cead3EXAMPLE")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "alice" {
		t.Fatalf("got %q, want %q", got, "alice")
	}
	if fake.gotID != arn {
		t.Fatalf("secret id: got %q, want %q", fake.gotID, arn)
	}
	if fake.gotVersionID != "9d4cb84b-ad69-40c0-a0ab-cead3EXAMPLE" {
		t.Fatalf("version id: got %q", fake.gotVersionID)
	}
	if fake.gotVersionStage != "" {
		t.Fatalf("unexpected version stage: %q", fake.gotVersionStage)
	}
}

func TestExtractJSONKeyStringWithEscapes(t *testing.T) {
	// A JSON string value is unwrapped (quotes/escapes decoded).
	got, err := extractJSONKey("id", "k", `{"k":"line1\nline2"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "line1\nline2" {
		t.Fatalf("got %q, want unescaped newline", got)
	}
}
