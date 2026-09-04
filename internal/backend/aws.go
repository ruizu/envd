package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

// secretsClient is the subset of the AWS Secrets Manager client that the
// backend uses. It is an interface so the backend can be tested with a fake.
type secretsClient interface {
	GetSecretValue(ctx context.Context, params *secretsmanager.GetSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
}

// parameterClient is the subset of the AWS Systems Manager client that the
// backend uses to read Parameter Store values. It is an interface so the
// backend can be tested with a fake.
type parameterClient interface {
	GetParameter(ctx context.Context, params *ssm.GetParameterInput, optFns ...func(*ssm.Options)) (*ssm.GetParameterOutput, error)
}

// awsBackend retrieves secrets from AWS Secrets Manager and parameters from
// AWS Systems Manager Parameter Store.
type awsBackend struct {
	secrets secretsClient
	params  parameterClient
}

// NewAWS constructs an AWS backend using the given options. Identifiers are
// routed to Secrets Manager or Parameter Store based on their shape; see
// Resolve.
func NewAWS(ctx context.Context, opts Options) (Backend, error) {
	loadOpts := []func(*config.LoadOptions) error{}
	if opts.Profile != "" {
		loadOpts = append(loadOpts, config.WithSharedConfigProfile(opts.Profile))
	}
	if opts.Region != "" {
		loadOpts = append(loadOpts, config.WithRegion(opts.Region))
	}

	cfg, err := config.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("loading aws config: %w", err)
	}

	return &awsBackend{
		secrets: secretsmanager.NewFromConfig(cfg),
		params:  ssm.NewFromConfig(cfg),
	}, nil
}

// secretRef holds the components of a parsed secret identifier. For a plain
// secret name (or an identifier without optional fields), only secretID is
// populated and the remaining fields are empty.
type secretRef struct {
	// secretID is the identifier passed to the Secrets Manager API. It is
	// either a plain secret name or the ARN up to and including the secret
	// name (without the optional trailing fields).
	secretID string
	// jsonKey, if set, selects a single key from a JSON-formatted secret.
	jsonKey string
	// versionStage selects a specific staging label (e.g. AWSPREVIOUS).
	versionStage string
	// versionID selects a specific version by its unique ID.
	versionID string
}

// parseSecretRef parses a secret identifier into its components. Three shapes
// are recognized:
//
//  1. A Secrets Manager ARN with optional trailing fields:
//
//     arn:aws:secretsmanager:region:account:secret:secret-name:json-key:version-stage:version-id
//
//  2. A "secretsmanager:" prefixed short form:
//
//     secretsmanager:secret-id:field-type:json-key:version-stage
//
//  3. Anything else (a plain secret name), used verbatim.
//
// Optional fields are empty by default and fall back to Secrets Manager's
// defaults (full secret contents / AWSCURRENT).
func parseSecretRef(id string) (secretRef, error) {
	switch {
	case strings.HasPrefix(id, "arn:"):
		return parseARNRef(id), nil
	case strings.HasPrefix(id, "secretsmanager:"):
		return parseShortRef(id)
	default:
		return secretRef{secretID: id}, nil
	}
}

// parseARNRef parses a Secrets Manager ARN with optional trailing
// json-key, version-stage, and version-id fields. ARNs for other services
// (or malformed ones) are returned verbatim as the secretID.
func parseARNRef(id string) secretRef {
	parts := strings.Split(id, ":")
	// A Secrets Manager ARN has the fixed prefix:
	//   arn(0):aws(1):secretsmanager(2):region(3):account(4):secret(5):name(6)
	// followed by up to three optional fields (7,8,9). Fewer than 7 parts, or
	// a non-secretsmanager service, means this is not a shape we extend.
	if len(parts) < 7 || parts[2] != "secretsmanager" || parts[5] != "secret" {
		return secretRef{secretID: id}
	}

	ref := secretRef{
		// Rejoin the fixed ARN prefix through the secret name as the SecretId.
		secretID: strings.Join(parts[:7], ":"),
	}
	if len(parts) > 7 {
		ref.jsonKey = parts[7]
	}
	if len(parts) > 8 {
		ref.versionStage = parts[8]
	}
	if len(parts) > 9 {
		ref.versionID = parts[9]
	}
	return ref
}

// parseShortRef parses the "secretsmanager:" prefixed short form:
//
//	secretsmanager:secret-id:field-type:json-key:version-stage
//
// secret-id is required and must be a plain secret name (not an ARN). The
// field-type, json-key, and version-stage fields are optional; skipped fields
// keep their colon placeholders. field-type, when present, must be
// SecretString (the only supported value).
func parseShortRef(id string) (secretRef, error) {
	// Drop the "secretsmanager:" prefix, then split the remainder. Limit the
	// split so a json-key or later field cannot be truncated by stray colons
	// beyond the fields we recognize.
	body := strings.TrimPrefix(id, "secretsmanager:")
	parts := strings.SplitN(body, ":", 4)

	name := parts[0]
	if name == "" {
		return secretRef{}, fmt.Errorf("invalid secret reference %q: missing secret id", id)
	}

	ref := secretRef{secretID: name}
	if len(parts) > 1 && parts[1] != "" && parts[1] != "SecretString" {
		return secretRef{}, fmt.Errorf("invalid secret reference %q: field-type must be SecretString, got %q", id, parts[1])
	}
	if len(parts) > 2 {
		ref.jsonKey = parts[2]
	}
	if len(parts) > 3 {
		ref.versionStage = parts[3]
	}
	return ref, nil
}

// Resolve returns the secret or parameter value for the given identifier.
//
// The identifier may be:
//
//   - A plain secret name, a Secrets Manager ARN that appends optional
//     :json-key:version-stage:version-id fields, or a "secretsmanager:"
//     prefixed short form (secretsmanager:secret-id:field-type:json-key:version-stage)
//     to select a specific JSON key and/or secret version — resolved
//     against Secrets Manager.
//   - A Systems Manager parameter ARN, or an "ssm:" prefixed short form
//     (ssm:parameter-name) — resolved against Parameter Store. Neither form
//     supports a JSON key or version selector.
func (b *awsBackend) Resolve(ctx context.Context, id string) (string, error) {
	if name, ok := parseParameterRef(id); ok {
		return b.getParameter(ctx, id, name)
	}

	ref, err := parseSecretRef(id)
	if err != nil {
		return "", err
	}

	in := &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(ref.secretID),
	}
	if ref.versionStage != "" {
		in.VersionStage = aws.String(ref.versionStage)
	}
	if ref.versionID != "" {
		in.VersionId = aws.String(ref.versionID)
	}

	out, err := b.secrets.GetSecretValue(ctx, in)
	if err != nil {
		return "", fmt.Errorf("getting secret %q: %w", id, err)
	}

	value, err := secretValue(id, out)
	if err != nil {
		return "", err
	}

	// When a JSON key is requested, the secret value must be a JSON object and
	// the selected key's value is returned instead of the whole document.
	if ref.jsonKey != "" {
		return extractJSONKey(id, ref.jsonKey, value)
	}
	return value, nil
}

// parseParameterRef reports whether id targets Systems Manager Parameter
// Store, returning the value to pass as GetParameter's Name field. Two shapes
// are recognized:
//
//  1. A parameter ARN: arn:aws:ssm:region:account:parameter/name. It is
//     passed through unchanged; GetParameter accepts ARNs directly.
//  2. An "ssm:" prefixed short form: ssm:parameter-name.
//
// Unlike Secrets Manager identifiers, no optional trailing fields (JSON key,
// version, or label) are supported here.
func parseParameterRef(id string) (string, bool) {
	if strings.HasPrefix(id, "arn:") {
		parts := strings.Split(id, ":")
		// A Systems Manager parameter ARN has the fixed prefix:
		//   arn(0):aws(1):ssm(2):region(3):account(4):parameter/name(5)
		if len(parts) >= 6 && parts[2] == "ssm" && strings.HasPrefix(parts[5], "parameter") {
			return id, true
		}
		return "", false
	}
	if rest, ok := strings.CutPrefix(id, "ssm:"); ok {
		return rest, true
	}
	return "", false
}

// getParameter fetches a value from Parameter Store. Secure string parameters
// are decrypted; the WithDecryption flag has no effect on other parameter
// types.
func (b *awsBackend) getParameter(ctx context.Context, id, name string) (string, error) {
	out, err := b.params.GetParameter(ctx, &ssm.GetParameterInput{
		Name:           aws.String(name),
		WithDecryption: aws.Bool(true),
	})
	if err != nil {
		return "", fmt.Errorf("getting parameter %q: %w", id, err)
	}
	if out.Parameter == nil || out.Parameter.Value == nil {
		return "", fmt.Errorf("parameter %q has no value", id)
	}
	return *out.Parameter.Value, nil
}

// secretValue extracts the string value from a GetSecretValue response,
// preferring SecretString over SecretBinary. A present-but-empty SecretString
// is a valid value; only the absence of both fields is an error.
func secretValue(secretID string, out *secretsmanager.GetSecretValueOutput) (string, error) {
	switch {
	case out.SecretString != nil:
		return *out.SecretString, nil
	case out.SecretBinary != nil:
		return string(out.SecretBinary), nil
	default:
		return "", fmt.Errorf("secret %q has no value", secretID)
	}
}

// extractJSONKey parses value as a JSON object and returns the string value of
// key. Non-string JSON values (numbers, booleans) are rendered as their JSON
// text so numeric secrets remain usable.
func extractJSONKey(secretID, key, value string) (string, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(value), &obj); err != nil {
		return "", fmt.Errorf("secret %q is not a JSON object; cannot extract key %q: %w", secretID, key, err)
	}
	raw, ok := obj[key]
	if !ok {
		return "", fmt.Errorf("secret %q has no JSON key %q", secretID, key)
	}

	// Prefer decoding as a string so quotes/escapes are unwrapped; fall back to
	// the raw JSON text for non-string values.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}
	return string(raw), nil
}
