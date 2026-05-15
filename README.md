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

Current runtime maps one `ward` to one domain and one default upstream. Planned Ward Routes may add path-based upstream routes under the same ward domain; multiple domains still require multiple wards.

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

### `warded status` Local Discovery

`warded status` uses the local `data-dir` as its discovery source. It does not enumerate all wards from the platform account.

The local state layout is:

1. `.pending/ward.json`: local setup choices not submitted to the platform yet
2. `<ward_draft_id>/ward.json`: submitted setup draft waiting for browser claim or activation
3. `<ward_id>/ward.json`: claimed ward runtime

If exactly one local runtime is found, `warded status` shows its detail and refreshes that target from the platform unless `--local` is set. If multiple local runtimes are found, `warded status` should show a local index list instead of failing; users can inspect one entry by index, ward id, draft id, or domain. Multi-runtime listing is local-only and should not refresh every platform object.

### `warded serve` Auth Paths

`warded serve` is an identity-aware proxy with two independent ingress authentication paths:

1. browser access uses the `warded_session` cookie issued through the platform login flow and verified with the local `jwt_signing_secret`
2. Agent, Bot, script, or CI access can use `Authorization: Bearer <Ward Access Token>`, a platform-signed RS256 JWT

Ward Access Tokens are created and revoked on the platform. The CLI stores only platform JWT public keys in `ward.json`. During heartbeat, `warded serve` refreshes those public keys and an in-memory positive set of currently valid Ward Access Token JTIs. The positive set is not persisted; after process restart, Bearer access becomes available again after the next successful heartbeat.

The Bearer path does not fall back to the browser login page. Invalid Bearer requests receive a JSON `401`, and successful Bearer requests have the original `Authorization` header stripped before proxying upstream.

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
