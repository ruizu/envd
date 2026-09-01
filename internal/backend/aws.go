package backend

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// secretsClient is the subset of the AWS Secrets Manager client that the
// backend uses. It is an interface so the backend can be tested with a fake.
type secretsClient interface {
	GetSecretValue(ctx context.Context, params *secretsmanager.GetSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
}

// awsBackend retrieves secrets from AWS Secrets Manager.
type awsBackend struct {
	client secretsClient
}

// NewAWS constructs an AWS Secrets Manager backend using the given options.
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

	return &awsBackend{client: secretsmanager.NewFromConfig(cfg)}, nil
}

// GetSecret returns the secret string for the given secret identifier (name or ARN).
func (b *awsBackend) GetSecret(ctx context.Context, secretID string) (string, error) {
	out, err := b.client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(secretID),
	})
	if err != nil {
		return "", fmt.Errorf("getting secret %q: %w", secretID, err)
	}

	if out.SecretString != nil {
		return *out.SecretString, nil
	}
	if out.SecretBinary != nil {
		return string(out.SecretBinary), nil
	}
	return "", fmt.Errorf("secret %q has no value", secretID)
}
