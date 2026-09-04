# envd

`envd` resolves secrets from a secret backend at process start, injects them as
environment variables, and then execs the command you give it. It is a tiny,
static, dependency-free binary designed to sit in front of another process as
its entrypoint.

Currently supported backends:

- **AWS** (`--backend=aws`, the default) — resolves identifiers against either
  **AWS Secrets Manager** or **AWS Systems Manager Parameter Store**,
  depending on the identifier's shape (see below).

## Why envd exists

`envd` was built to pair with the **Amazon Bedrock AgentCore CLI** so that an
agent's secrets are loaded into the **AgentCore Docker runtime** at container
start — without baking secret values into the image, the task definition, or
plaintext environment variables.

AgentCore runtime containers launch your agent via the image's `CMD`, receive
AWS credentials through the standard credential chain (an IAM role in the
runtime), and run on `linux/arm64`. `envd` slots in as a wrapper around that
`CMD`: on startup it uses the container's existing AWS credentials to fetch each
secret from Secrets Manager, exports them as environment variables, and then
hands off to your agent server. The secrets exist only in the running process's
environment — never in the image layers or the container spec.

This is one representative use case; `envd` is a general-purpose secret-loading
exec wrapper and works anywhere you can set a container entrypoint or run a
command (Linux servers, CI jobs, local development, etc.).

## Install

```sh
go install github.com/ruizu/envd@latest
```

Prebuilt binaries (including `linux/arm64` for AgentCore) are attached to each
[GitHub Release](#building-and-releasing).

## Usage

```
envd [flags] run command [args...]
```

Each `--env VAR=secret_id` flag resolves `secret_id` against the backend and
exports the result as the environment variable `VAR`. The flag is repeatable.
The `run` subcommand takes the command to execute followed by its arguments;
everything after the command name is passed through to it verbatim (including
the command's own flags).

Run `envd version` to print the version, commit, build date, and Go version.

### Flags

| Flag             | Default | Description                                   |
|------------------|---------|-----------------------------------------------|
| `--env`, `-e`    | (none)  | Secret mapping `VAR=secret_id` (repeatable)   |
| `--backend`      | `aws`   | Secret backend to use                         |
| `--profile`      | (none)  | Backend credentials/config profile (optional) |
| `--region`       | (none)  | Backend region (optional)                     |

### Examples

```sh
# Single secret, default (aws) backend
envd run --env SECRET_VAR1=aws_secret_id1 cli-to-run

# Multiple secrets and command arguments
envd run --env SECRET_VAR1=aws_secret_id1 --env SECRET_VAR2=aws_secret_id2 cli-to-run parameter1 parameter2

# Using a named AWS profile
envd run --backend=aws --profile=test_profile --env SECRET_VAR1=aws_secret_id1 cli-to-run
```

The child command's own flags pass through untouched:

```sh
envd run --env DB_PASSWORD=prod/db migrate --verbose --steps 3
```

### Signals

`envd` forwards `SIGINT` (Ctrl-C) and `SIGTERM` to the child process as
`SIGTERM`, giving it a chance to shut down gracefully. If the child does not
exit within a short grace period it is force-killed. On a signal-triggered
shutdown, `envd` exits with code 130. This lets a wrapped server drain
connections when the runtime stops the container.

## Using envd with AgentCore

Add the `envd` binary to your agent image and make it the entrypoint that wraps
the server `CMD`. Because AgentCore runtimes are `linux/arm64`, use the
`linux/arm64` build.

```dockerfile
FROM --platform=linux/arm64 ghcr.io/astral-sh/uv:python3.11-bookworm-slim

WORKDIR /app

# Install envd (linux/arm64) into the image.
ADD https://github.com/ruizu/envd/releases/download/v0.0.1/envd_0.0.1_linux_arm64.tar.gz /tmp/envd.tar.gz
RUN tar -xzf /tmp/envd.tar.gz -C /usr/local/bin envd && rm /tmp/envd.tar.gz

COPY pyproject.toml uv.lock ./
RUN uv sync --frozen --no-cache
COPY agent.py ./

EXPOSE 8080

# envd resolves the secrets using the runtime's IAM credentials, exports them,
# and then launches the agent server. Secret values never live in the image.
CMD ["envd", "run", \
     "--env", "OPENAI_API_KEY=prod/agent/openai", \
     "--env", "DB_PASSWORD=prod/agent/db", \
     "uv", "run", "uvicorn", "agent:app", "--host", "0.0.0.0", "--port", "8080"]
```

At container start, `envd`:

1. Reads the AWS credentials already available in the runtime (IAM role).
2. Fetches `prod/agent/openai` and `prod/agent/db` from Secrets Manager.
3. Exports them as `OPENAI_API_KEY` and `DB_PASSWORD`.
4. Execs `uv run uvicorn agent:app ...`, which inherits those variables.

The agent process reads its secrets from the environment exactly as it would
locally, with no SDK calls or secret-management code of its own. Signals from
the runtime are forwarded so the server shuts down gracefully.

## AWS credentials

The AWS backend uses the standard AWS SDK credential chain (environment
variables, shared config/credentials files, IAM roles, etc.). In an AgentCore
runtime this is the task's IAM role, so no credentials need to be passed to
`envd` explicitly. Use `--profile` to select a named profile and `--region` to
override the region when running locally.

Secret identifiers may be either the secret's name or its full ARN. Both
`SecretString` and `SecretBinary` values are supported.

The IAM role (or profile) that `envd` runs under must be allowed to call
`secretsmanager:GetSecretValue` for identifiers resolved against Secrets
Manager, and `ssm:GetParameter` for identifiers resolved against Parameter
Store.

### Secret ARNs and specific keys/versions

Like ECS `secrets.valueFrom`, an ARN identifier may append optional fields to
select a single JSON key and/or a specific version of the secret:

```
arn:aws:secretsmanager:region:aws_account_id:secret:secret-name:json-key:version-stage:version-id
```

All three trailing fields are optional, but you must keep the colon separators
as placeholders for any field you skip:

- `json-key` — return a single key from a JSON-object secret instead of the
  whole document. Non-string values (numbers, booleans) are returned as their
  JSON text.
- `version-stage` — a staging label such as `AWSPREVIOUS`. Defaults to
  `AWSCURRENT`. Cannot be combined with `version-id`.
- `version-id` — a specific version ID. Cannot be combined with `version-stage`.

Examples:

```sh
# Whole secret (name or bare ARN)
envd run --env DB=prod/db cli-to-run
envd run --env DB=arn:aws:secretsmanager:us-east-1:123456789012:secret:prod/db-AbCdEf cli-to-run

# A single JSON key ("username1") from the secret
envd run --env USER=arn:aws:secretsmanager:us-east-1:123456789012:secret:appauth-AbCdEf:username1:: cli-to-run

# A specific version stage
envd run --env DB=arn:aws:secretsmanager:us-east-1:123456789012:secret:prod/db-AbCdEf::AWSPREVIOUS: cli-to-run

# A JSON key from a specific version ID
envd run --env USER=arn:aws:secretsmanager:us-east-1:123456789012:secret:appauth-AbCdEf:username1::9d4cb84b-... cli-to-run
```

Plain secret names and non–Secrets Manager identifiers are used verbatim, so
this only affects Secrets Manager ARNs.

### Resolve short form

As a shorthand, `envd` also accepts the Secrets Manager dynamic-reference body
(the contents of a `{{resolve:...}}` reference, without the `resolve:` prefix):

```
secretsmanager:secret-id:field-type:json-key:version-stage
```

- `secret-id` — required, the secret's name (not an ARN in this form).
- `field-type` — optional, defaults to and must be `SecretString`.
- `json-key` — optional, selects a single key from a JSON-object secret.
- `version-stage` — optional staging label, defaults to `AWSCURRENT`.

Skipped fields keep their colon placeholders. Examples:

```sh
# Whole secret
envd run --env DB=secretsmanager:prod/db cli-to-run

# A single JSON key
envd run --env PW=secretsmanager:prod/db:SecretString:password cli-to-run

# JSON key with the default field-type
envd run --env PW=secretsmanager:prod/db::password cli-to-run

# A specific version stage of a JSON key
envd run --env PW=secretsmanager:prod/db:SecretString:password:AWSPREVIOUS cli-to-run
```

Unlike the ARN form, this short form has no `version-id` field; use a
`version-stage` (or the full ARN) to pin a version.

### Systems Manager Parameter Store

An identifier is resolved against Parameter Store instead of Secrets Manager
when it is a Parameter Store ARN or uses the `ssm:` prefixed short form:

```
arn:aws:ssm:region:aws_account_id:parameter/parameter-name
ssm:parameter-name
```

Unlike the Secrets Manager forms above, neither of these supports a JSON key
or a version/label field — Parameter Store parameters hold a single value.
To read a specific version or label, include it directly in the parameter
name as SSM itself expects (`parameter-name:label` or
`parameter-name:version`), since `GetParameter` interprets that suffix
natively.

`SecureString` parameters are automatically decrypted.

Examples:

```sh
# By parameter name
envd run --env DB_HOST=ssm:/prod/db/host cli-to-run

# By full ARN
envd run --env DB_HOST=arn:aws:ssm:us-east-1:123456789012:parameter/prod/db/host cli-to-run

# A specific parameter version or label
envd run --env DB_HOST=ssm:/prod/db/host:3 cli-to-run
envd run --env DB_HOST=ssm:/prod/db/host:prod cli-to-run
```

## Backends

Add a new backend by implementing the `Backend` interface in
`internal/backend` and registering it in the `New` factory function.

## Building and releasing

Build and test locally:

```sh
go build ./...
go test ./...
```

Version metadata (`envd version`) is derived from Go's build info by default —
the module version and, once the repo has commits, the VCS revision and time.
For release builds you can stamp it explicitly:

```sh
go build -ldflags "\
  -X github.com/ruizu/envd/internal/cli.version=v1.0.0 \
  -X github.com/ruizu/envd/internal/cli.commit=$(git rev-parse --short HEAD) \
  -X github.com/ruizu/envd/internal/cli.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
```

Releases are automated with [GoReleaser](https://goreleaser.com) via GitHub
Actions (`.github/workflows/release.yml`). Pushing a semver tag builds
cross-compiled binaries and publishes them to GitHub Releases. The workflow
stamps the tag into the binary's version metadata, builds `.tar.gz` archives
(`.zip` for Windows), and uploads them with a `checksums.txt` file.

Prebuilt archives are produced for:

| OS      | amd64 | arm64 |
|---------|-------|-------|
| Linux   | ✅    | ✅     |
| macOS   | ✅    | ✅     |
| Windows | ✅    | —     |

The `linux/arm64` archive is the one to use for AgentCore runtime images.

To cut a release:

```sh
git tag v1.0.0
git push origin v1.0.0
```

To dry-run the build locally without publishing (requires GoReleaser
installed):

```sh
goreleaser release --snapshot --clean
```

## License

[MIT](LICENSE)
