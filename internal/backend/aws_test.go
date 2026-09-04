package backend

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"
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

// fakeParameterClient is a test double for the AWS Systems Manager client.
type fakeParameterClient struct {
	out *ssm.GetParameterOutput
	err error
	// gotName captures the Name passed in, to assert it is forwarded.
	gotName string
	// gotWithDecryption captures whether decryption was requested.
	gotWithDecryption bool
	// called records whether GetParameter was invoked at all.
	called bool
}

func (f *fakeParameterClient) GetParameter(_ context.Context, in *ssm.GetParameterInput, _ ...func(*ssm.Options)) (*ssm.GetParameterOutput, error) {
	f.called = true
	if in.Name != nil {
		f.gotName = *in.Name
	}
	if in.WithDecryption != nil {
		f.gotWithDecryption = *in.WithDecryption
	}
	return f.out, f.err
}

func TestResolveString(t *testing.T) {
	fake := &fakeSecretsClient{
		out: &secretsmanager.GetSecretValueOutput{SecretString: aws.String("plain-secret")},
	}
	b := &awsBackend{secrets: fake}

	got, err := b.Resolve(context.Background(), "my/secret")
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

func TestResolveBinary(t *testing.T) {
	fake := &fakeSecretsClient{
		out: &secretsmanager.GetSecretValueOutput{SecretBinary: []byte("binary-secret")},
	}
	b := &awsBackend{secrets: fake}

	got, err := b.Resolve(context.Background(), "id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "binary-secret" {
		t.Fatalf("got %q, want %q", got, "binary-secret")
	}
}

func TestResolvePrefersStringOverBinary(t *testing.T) {
	// When both are set, SecretString takes precedence.
	fake := &fakeSecretsClient{
		out: &secretsmanager.GetSecretValueOutput{
			SecretString: aws.String("the-string"),
			SecretBinary: []byte("the-binary"),
		},
	}
	b := &awsBackend{secrets: fake}

	got, err := b.Resolve(context.Background(), "id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "the-string" {
		t.Fatalf("expected SecretString to win, got %q", got)
	}
}

func TestResolveEmptyString(t *testing.T) {
	// A present-but-empty SecretString is a valid value, not an error.
	fake := &fakeSecretsClient{
		out: &secretsmanager.GetSecretValueOutput{SecretString: aws.String("")},
	}
	b := &awsBackend{secrets: fake}

	got, err := b.Resolve(context.Background(), "id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty string value, got %q", got)
	}
}

func TestResolveNoValue(t *testing.T) {
	// Neither SecretString nor SecretBinary set -> error.
	fake := &fakeSecretsClient{out: &secretsmanager.GetSecretValueOutput{}}
	b := &awsBackend{secrets: fake}

	_, err := b.Resolve(context.Background(), "id")
	if err == nil {
		t.Fatal("expected error when secret has no value")
	}
	if !strings.Contains(err.Error(), "no value") {
		t.Fatalf("expected 'no value' error, got: %v", err)
	}
}

func TestResolveAPIError(t *testing.T) {
	sentinel := errors.New("access denied")
	fake := &fakeSecretsClient{err: sentinel}
	b := &awsBackend{secrets: fake}

	_, err := b.Resolve(context.Background(), "id")
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
			name: "short form name only",
			id:   "secretsmanager:prod/db",
			want: secretRef{secretID: "prod/db"},
		},
		{
			name: "short form with field-type",
			id:   "secretsmanager:prod/db:SecretString",
			want: secretRef{secretID: "prod/db"},
		},
		{
			name: "short form with json key",
			id:   "secretsmanager:prod/db:SecretString:username",
			want: secretRef{secretID: "prod/db", jsonKey: "username"},
		},
		{
			name: "short form with json key default field-type",
			id:   "secretsmanager:prod/db::username",
			want: secretRef{secretID: "prod/db", jsonKey: "username"},
		},
		{
			name: "short form with json key and version stage",
			id:   "secretsmanager:prod/db:SecretString:username:AWSPREVIOUS",
			want: secretRef{secretID: "prod/db", jsonKey: "username", versionStage: "AWSPREVIOUS"},
		},
		{
			name: "short form with version stage only",
			id:   "secretsmanager:prod/db:::AWSPREVIOUS",
			want: secretRef{secretID: "prod/db", versionStage: "AWSPREVIOUS"},
		},
		{
			name: "short form json key containing colons preserved",
			id:   "secretsmanager:prod/db:SecretString:a:b:c",
			want: secretRef{secretID: "prod/db", jsonKey: "a", versionStage: "b:c"},
		},
		{
			name:    "short form missing secret id",
			id:      "secretsmanager:",
			wantErr: true,
		},
		{
			name:    "short form invalid field-type",
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

func TestResolveARNStripsTrailingFields(t *testing.T) {
	const arn = "arn:aws:secretsmanager:us-east-1:123456789012:secret:appauthexample-AbCdEf"
	fake := &fakeSecretsClient{
		out: &secretsmanager.GetSecretValueOutput{SecretString: aws.String("plain")},
	}
	b := &awsBackend{secrets: fake}

	if _, err := b.Resolve(context.Background(), arn+":::"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.gotID != arn {
		t.Fatalf("SecretId not stripped of trailing fields: got %q, want %q", fake.gotID, arn)
	}
}

func TestResolveJSONKey(t *testing.T) {
	const arn = "arn:aws:secretsmanager:us-east-1:123456789012:secret:appauthexample-AbCdEf"
	fake := &fakeSecretsClient{
		out: &secretsmanager.GetSecretValueOutput{
			SecretString: aws.String(`{"username1":"password1","username2":"password2"}`),
		},
	}
	b := &awsBackend{secrets: fake}

	got, err := b.Resolve(context.Background(), arn+":username2::")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "password2" {
		t.Fatalf("got %q, want %q", got, "password2")
	}
}

func TestResolveJSONKeyNumericValue(t *testing.T) {
	// Non-string JSON values are rendered as their JSON text.
	const arn = "arn:aws:secretsmanager:us-east-1:123456789012:secret:cfg-AbCdEf"
	fake := &fakeSecretsClient{
		out: &secretsmanager.GetSecretValueOutput{
			SecretString: aws.String(`{"port":5432}`),
		},
	}
	b := &awsBackend{secrets: fake}

	got, err := b.Resolve(context.Background(), arn+":port::")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "5432" {
		t.Fatalf("got %q, want %q", got, "5432")
	}
}

func TestResolveJSONKeyMissing(t *testing.T) {
	const arn = "arn:aws:secretsmanager:us-east-1:123456789012:secret:appauthexample-AbCdEf"
	fake := &fakeSecretsClient{
		out: &secretsmanager.GetSecretValueOutput{
			SecretString: aws.String(`{"username1":"password1"}`),
		},
	}
	b := &awsBackend{secrets: fake}

	_, err := b.Resolve(context.Background(), arn+":nope::")
	if err == nil {
		t.Fatal("expected error for missing JSON key")
	}
	if !strings.Contains(err.Error(), "no JSON key") {
		t.Fatalf("expected 'no JSON key' error, got: %v", err)
	}
}

func TestResolveJSONKeyNotJSON(t *testing.T) {
	const arn = "arn:aws:secretsmanager:us-east-1:123456789012:secret:appauthexample-AbCdEf"
	fake := &fakeSecretsClient{
		out: &secretsmanager.GetSecretValueOutput{
			SecretString: aws.String("not-json"),
		},
	}
	b := &awsBackend{secrets: fake}

	_, err := b.Resolve(context.Background(), arn+":key::")
	if err == nil {
		t.Fatal("expected error when secret is not a JSON object")
	}
	if !strings.Contains(err.Error(), "not a JSON object") {
		t.Fatalf("expected 'not a JSON object' error, got: %v", err)
	}
}

func TestResolveVersionSelectorsForwarded(t *testing.T) {
	const arn = "arn:aws:secretsmanager:us-east-1:123456789012:secret:appauthexample-AbCdEf"
	fake := &fakeSecretsClient{
		out: &secretsmanager.GetSecretValueOutput{SecretString: aws.String("v")},
	}
	b := &awsBackend{secrets: fake}

	// version-stage
	if _, err := b.Resolve(context.Background(), arn+"::AWSPREVIOUS:"); err != nil {
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
	if _, err := b.Resolve(context.Background(), arn+":::abc-123"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.gotVersionID != "abc-123" {
		t.Fatalf("version id not forwarded: got %q", fake.gotVersionID)
	}
	if fake.gotVersionStage != "" {
		t.Fatalf("unexpected version stage forwarded: %q", fake.gotVersionStage)
	}
}

func TestResolveShortForm(t *testing.T) {
	fake := &fakeSecretsClient{
		out: &secretsmanager.GetSecretValueOutput{
			SecretString: aws.String(`{"username":"alice","password":"s3cr3t"}`),
		},
	}
	b := &awsBackend{secrets: fake}

	got, err := b.Resolve(context.Background(), "secretsmanager:prod/db:SecretString:password:AWSPREVIOUS")
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

func TestResolveShortFormWholeValue(t *testing.T) {
	// The short form with no json-key returns the whole secret verbatim.
	fake := &fakeSecretsClient{
		out: &secretsmanager.GetSecretValueOutput{SecretString: aws.String(`{"a":"b"}`)},
	}
	b := &awsBackend{secrets: fake}

	got, err := b.Resolve(context.Background(), "secretsmanager:prod/db")
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

func TestResolveParseErrorSkipsClient(t *testing.T) {
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
			b := &awsBackend{secrets: fake}

			if _, err := b.Resolve(context.Background(), id); err == nil {
				t.Fatalf("expected parse error for %q", id)
			}
			if fake.called {
				t.Fatalf("client should not be called on parse error for %q", id)
			}
		})
	}
}

func TestResolveJSONKeyFromBinary(t *testing.T) {
	// A json-key can be extracted from a JSON document stored as SecretBinary.
	const arn = "arn:aws:secretsmanager:us-east-1:123456789012:secret:cfg-AbCdEf"
	fake := &fakeSecretsClient{
		out: &secretsmanager.GetSecretValueOutput{
			SecretBinary: []byte(`{"token":"abc123"}`),
		},
	}
	b := &awsBackend{secrets: fake}

	got, err := b.Resolve(context.Background(), arn+":token::")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "abc123" {
		t.Fatalf("got %q, want %q", got, "abc123")
	}
}

func TestResolveJSONKeyBooleanValue(t *testing.T) {
	// Boolean values are rendered as their JSON text.
	const arn = "arn:aws:secretsmanager:us-east-1:123456789012:secret:cfg-AbCdEf"
	fake := &fakeSecretsClient{
		out: &secretsmanager.GetSecretValueOutput{
			SecretString: aws.String(`{"enabled":true}`),
		},
	}
	b := &awsBackend{secrets: fake}

	got, err := b.Resolve(context.Background(), arn+":enabled::")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "true" {
		t.Fatalf("got %q, want %q", got, "true")
	}
}

func TestResolveJSONKeyNestedValue(t *testing.T) {
	// Object/array values are returned as their raw JSON text.
	const arn = "arn:aws:secretsmanager:us-east-1:123456789012:secret:cfg-AbCdEf"
	fake := &fakeSecretsClient{
		out: &secretsmanager.GetSecretValueOutput{
			SecretString: aws.String(`{"nested":{"k":"v"},"list":[1,2]}`),
		},
	}
	b := &awsBackend{secrets: fake}

	gotObj, err := b.Resolve(context.Background(), arn+":nested::")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotObj != `{"k":"v"}` {
		t.Fatalf("nested object: got %q, want %q", gotObj, `{"k":"v"}`)
	}

	gotList, err := b.Resolve(context.Background(), arn+":list::")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotList != `[1,2]` {
		t.Fatalf("nested list: got %q, want %q", gotList, `[1,2]`)
	}
}

func TestResolveARNJSONKeyWithVersionID(t *testing.T) {
	// End-to-end: ARN with both a json-key and a version-id.
	const arn = "arn:aws:secretsmanager:us-east-1:123456789012:secret:appauthexample-AbCdEf"
	fake := &fakeSecretsClient{
		out: &secretsmanager.GetSecretValueOutput{
			SecretString: aws.String(`{"username1":"alice"}`),
		},
	}
	b := &awsBackend{secrets: fake}

	got, err := b.Resolve(context.Background(), arn+":username1::9d4cb84b-ad69-40c0-a0ab-cead3EXAMPLE")
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

func TestParseParameterRef(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		wantName string
		wantOK   bool
	}{
		{
			name:   "plain name is not a parameter ref",
			id:     "my/param",
			wantOK: false,
		},
		{
			name:   "secrets manager arn is not a parameter ref",
			id:     "arn:aws:secretsmanager:us-east-1:123456789012:secret:appauthexample-AbCdEf",
			wantOK: false,
		},
		{
			name:     "ssm parameter arn",
			id:       "arn:aws:ssm:us-east-1:123456789012:parameter/my/param",
			wantName: "arn:aws:ssm:us-east-1:123456789012:parameter/my/param",
			wantOK:   true,
		},
		{
			name:   "non-ssm arn is not a parameter ref",
			id:     "arn:aws:iam::123456789012:role/my-role",
			wantOK: false,
		},
		{
			name:     "ssm short form",
			id:       "ssm:/my/param",
			wantName: "/my/param",
			wantOK:   true,
		},
		{
			name:     "ssm short form preserves label/version selectors",
			id:       "ssm:/my/param:2",
			wantName: "/my/param:2",
			wantOK:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, gotOK := parseParameterRef(tt.id)
			if gotOK != tt.wantOK {
				t.Fatalf("parseParameterRef(%q) ok = %v, want %v", tt.id, gotOK, tt.wantOK)
			}
			if gotOK && gotName != tt.wantName {
				t.Fatalf("parseParameterRef(%q) name = %q, want %q", tt.id, gotName, tt.wantName)
			}
		})
	}
}

func TestResolveParameterStoreShortForm(t *testing.T) {
	fake := &fakeParameterClient{
		out: &ssm.GetParameterOutput{
			Parameter: &types.Parameter{Value: aws.String("param-value")},
		},
	}
	b := &awsBackend{params: fake}

	got, err := b.Resolve(context.Background(), "ssm:/my/param")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "param-value" {
		t.Fatalf("got %q, want %q", got, "param-value")
	}
	if fake.gotName != "/my/param" {
		t.Fatalf("parameter name: got %q, want %q", fake.gotName, "/my/param")
	}
	if !fake.gotWithDecryption {
		t.Fatal("expected WithDecryption to be requested")
	}
}

func TestResolveParameterStoreARN(t *testing.T) {
	const arn = "arn:aws:ssm:us-east-1:123456789012:parameter/my/param"
	fake := &fakeParameterClient{
		out: &ssm.GetParameterOutput{
			Parameter: &types.Parameter{Value: aws.String("param-value")},
		},
	}
	b := &awsBackend{params: fake}

	got, err := b.Resolve(context.Background(), arn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "param-value" {
		t.Fatalf("got %q, want %q", got, "param-value")
	}
	if fake.gotName != arn {
		t.Fatalf("parameter name: got %q, want %q (ARN forwarded unchanged)", fake.gotName, arn)
	}
}

func TestResolveParameterStoreNoValue(t *testing.T) {
	fake := &fakeParameterClient{out: &ssm.GetParameterOutput{}}
	b := &awsBackend{params: fake}

	_, err := b.Resolve(context.Background(), "ssm:/my/param")
	if err == nil {
		t.Fatal("expected error when parameter has no value")
	}
	if !strings.Contains(err.Error(), "no value") {
		t.Fatalf("expected 'no value' error, got: %v", err)
	}
}

func TestResolveParameterStoreAPIError(t *testing.T) {
	sentinel := errors.New("parameter not found")
	fake := &fakeParameterClient{err: sentinel}
	b := &awsBackend{params: fake}

	_, err := b.Resolve(context.Background(), "ssm:/my/param")
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected API error to propagate, got: %v", err)
	}
	if !strings.Contains(err.Error(), "getting parameter") {
		t.Fatalf("expected wrapped context, got: %v", err)
	}
}

func TestResolveParameterStoreDoesNotCallSecretsClient(t *testing.T) {
	// A parameter reference must never reach the Secrets Manager client.
	fakeSecrets := &fakeSecretsClient{out: &secretsmanager.GetSecretValueOutput{SecretString: aws.String("x")}}
	fakeParams := &fakeParameterClient{
		out: &ssm.GetParameterOutput{Parameter: &types.Parameter{Value: aws.String("v")}},
	}
	b := &awsBackend{secrets: fakeSecrets, params: fakeParams}

	if _, err := b.Resolve(context.Background(), "ssm:/my/param"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fakeSecrets.called {
		t.Fatal("secrets manager client should not be called for a parameter reference")
	}
}
