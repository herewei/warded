# Warded CLI

`warded` is the CLI and skill-facing runtime for protecting the OpenClaw management UI behind an identity-aware HTTPS entrypoint.

This repository is open sourced for transparency and auditability. The CLI can affect network-facing behavior, write local runtime state, request or serve TLS material, and generate or handle local key material. Publishing the code allows operators, security reviewers, and future integrators to inspect how those paths work.

## What Warded Is

`warded` is not a general-purpose reverse proxy and not a generic "expose localhost" tunnel tool.

The intended product boundary is narrow:

1. it is built for OpenClaw robots;
2. it targets cloud-deployed OpenClaw instances;
3. it protects the OpenClaw management UI and its HTTPS access path; and
4. `warded serve` runs as a single-binary identity-aware reverse proxy with built-in TLS, auth middleware, and upstream proxying.

## What Warded Does Not Do

This project does not aim to be:

1. a NAT traversal product;
2. an FRP, ngrok, or Tailscale replacement;
3. a generic localhost publishing tool; or
4. a multi-tenant reverse proxy for arbitrary unrelated services.

One `ward` maps to one domain and one upstream port.

## Why This Repository Is Public

This repository is public so that people can inspect:

1. how the CLI changes local configuration;
2. how auth and proxy boundaries are enforced;
3. how TLS and local session material are handled; and
4. what the installer and service setup actually do.

The repository is not opened primarily to maximize drive-by contributions. Governance is intentionally conservative because mistakes in this code can affect real network exposure and private key handling.

## Core Commands

Current command surface:

1. `warded version`
2. `warded new`
3. `warded integrate`
4. `warded serve`
5. `warded status`
6. `warded doctor`
7. `warded renew-cert`

For the current command contract, see the shared docs in `warded_docs/contracts/cli-commands.md`.

## Install

The long-term public install entrypoints are:

```bash
curl -fsSL https://warded.me/install.sh | sh
```

```bash
curl -fsSL https://warded.cn/install.sh | sh
```

If you are building from source:

```bash
make build
./bin/warded --version
```

## Development

Prerequisites:

1. Go 1.21 or later
2. Make

Common commands:

```bash
make build
make test
make test-v
make lint
```

Run locally:

```bash
make run ARGS="version"
```

## Security-Sensitive Areas

The most sensitive parts of this repository include:

1. private key generation, storage, export, rotation, or deletion;
2. auth middleware and local JWT handling;
3. TLS issuance, certificate storage, and HTTPS behavior;
4. reverse proxy request handling and identity propagation;
5. local config persistence and filesystem permissions; and
6. installer, service unit, and deployment scripts.

If you are reviewing the code, start there.

## Reporting Security Issues

Do not report unpatched vulnerabilities in a public issue or pull request.

See [SECURITY.md](./SECURITY.md) for the private disclosure process.

## Contributing

External contributions are reviewed conservatively.

Before any non-trivial contribution can be merged, contributors must satisfy the repository's CLA and provenance requirements. See:

1. [CONTRIBUTING.md](./CONTRIBUTING.md)
2. [SECURITY.md](./SECURITY.md)

## License

Licensed under the Apache License 2.0. See [LICENSE](./LICENSE) and [NOTICE](./NOTICE).
