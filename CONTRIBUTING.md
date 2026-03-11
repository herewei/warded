# Contributing to Warded CLI

`warded_cli` is open source for transparency and auditability.

The project is intentionally conservative about accepting contributions. The CLI can modify network-facing behavior, write local runtime configuration, and generate or handle private key material. Because of that risk profile, every non-trivial contribution must have a clear legal chain of title and a clear technical review trail.

This file is a workflow document, not legal advice. The CLA templates in [`CLA/`](./CLA/) should be reviewed by qualified counsel before production use.

## License

This repository is distributed under the Apache License 2.0. See [`LICENSE`](./LICENSE) and [`NOTICE`](./NOTICE).

## Contribution Policy

Maintainers may reject any contribution for legal, security, product, or maintenance reasons.

External contributions are accepted only when all of the following are true:

1. The contributor has signed the required CLA.
2. The contributor can lawfully grant the required copyright and patent rights.
3. The contribution is compatible with the project architecture and product boundary.
4. The contribution passes technical and provenance review.

Small fixes such as typo fixes may still be treated as non-trivial if they touch sensitive files or embedded legal text.

## Required Agreements

### Individuals

An individual contributor must sign [`CLA/INDIVIDUAL_CLA.md`](./CLA/INDIVIDUAL_CLA.md) before any non-trivial contribution can be merged.

### Companies and employer-owned work

If a contribution is created within employment, contracting, or any arrangement where another party may own the work, the contributor's organization must sign [`CLA/CORPORATE_CLA.md`](./CLA/CORPORATE_CLA.md) before merge.

Maintainers may also require a corporate CLA whenever the contributor uses a company email address or states that the work was prepared on company time or equipment.

### Why the project requires a strong CLA

The CLA is intentionally strong because the project may later be relicensed, dual licensed, sublicensed, sold, assigned, or transferred as part of a business transaction. The project owner therefore requires rights broad enough to support:

1. open-source distribution;
2. commercial distribution;
3. proprietary relicensing;
4. sublicensing to partners, acquirers, and distributors; and
5. transfer to successors and assigns.

## How to Contribute

1. Open an issue first for anything non-trivial.
2. Confirm whether you need an individual CLA or a corporate CLA.
3. Request the correct CLA form and return the signed agreement to `ivan@warded.me`.
4. Wait for maintainer confirmation that the agreement is on file.
5. Submit a pull request with the required declarations completed.

Do not assume a pull request will be reviewed just because the code is technically correct. Provenance and security review are part of the merge gate.

## Technical Review Expectations

The project uses DDD and Hexagonal Architecture. Keep domain logic in domain and application layers. Adapters should stay focused on IO and integration concerns.

Before proposing implementation changes, read the authoritative docs in `../warded_docs/`, especially:

1. `DESIGN.md`
2. `architecture/repo-structure.md`
3. `architecture/domain-and-ports.md` and its sub-docs
4. `contracts/auth-proxy.md`
5. `contracts/local-jwt.md`
6. `contracts/local-config.md` and its sub-docs
7. `contracts/ward-lifecycle.md` and its sub-docs
8. `contracts/proxy-target-config.md`
9. `contracts/cli-commands.md`

If a contribution changes a contract, schema, or architecture decision, the corresponding document update is required in `warded_docs/`.

## Sensitive Areas

Changes in the following areas receive stricter review and may be declined even with a signed CLA:

1. key generation, storage, export, rotation, or deletion;
2. auth middleware and local JWT handling;
3. TLS issuance and certificate storage;
4. reverse proxy request handling and identity propagation;
5. local config persistence and filesystem permissions;
6. installation scripts and system service setup;
7. any code that changes network exposure, DNS behavior, or HTTPS access paths.

For these areas, maintainers may require:

1. additional tests;
2. a design note in the PR description;
3. proof that no third-party code was copied in;
4. a narrower patch; or
5. a manual security review before merge.

Security vulnerabilities should not be reported through public issues or public pull requests. Use [`SECURITY.md`](./SECURITY.md) for the disclosure process.

## Source Provenance Rules

By opening a pull request, the contributor represents that:

1. the contribution is original or lawfully reused with compatible licensing;
2. no code was copied from a source that is unavailable, proprietary, or license-incompatible with Apache-2.0;
3. any AI assistance was reviewed by a human and can be legally contributed under the CLA and the repository license;
4. the contribution does not disclose secrets, credentials, private keys, internal URLs, customer data, or confidential material; and
5. the contributor has disclosed any employer, client, or other third-party ownership interest.

Maintainers may ask for additional provenance details before review.

## Pull Request Checklist

Every pull request should:

1. describe what changed and why;
2. identify any user-visible or operator-visible behavior changes;
3. identify any files or flows that affect network behavior, auth, TLS, config, or key material;
4. include tests or a clear justification for not adding tests;
5. update docs when contracts or behavior changed; and
6. complete every declaration in the PR template truthfully.

## Development Notes

### Prerequisites

1. Go 1.21 or later
2. Make

### Common Commands

```bash
make build
make test
make test-v
```

### Architectural Constraints

1. `warded` is a CLI / Skill for OpenClaw robots, not a generic end-user CLI.
2. The project targets cloud-deployed OpenClaw only.
3. Do not add NAT traversal, FRP, local tunnel, Tailscale replacement, or generic localhost exposure features.
4. `warded serve` must remain a single-binary identity-aware reverse proxy.
5. One `ward` maps to exactly one domain and one upstream port.

## Contact

For CLA handling and provenance questions, contact `ivan@warded.me`.
