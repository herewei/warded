# Security Policy

`warded_cli` can affect network exposure, TLS behavior, authentication flow, local configuration, and private key handling. Because of that, suspected vulnerabilities should be reported privately first.

## Reporting a Vulnerability

Do not open a public issue or pull request for an unpatched security vulnerability.

Current private intake: `security@warded.me`

This is acceptable if it is monitored reliably. A dedicated alias such as `security@warded.me` is cleaner long term and can forward to the same inbox.

When reporting, include:

1. affected version or commit;
2. reproduction steps;
3. impact assessment;
4. logs or traces with secrets removed; and
5. whether the issue may expose credentials, private keys, customer data, or public network surfaces.

## Disclosure Expectations

Maintainers may:

1. acknowledge receipt privately;
2. request a reduced test case;
3. prepare a fix before public discussion; and
4. delay disclosure until affected users have a reasonable chance to upgrade.

No bounty program is offered unless maintainers explicitly announce one.

## Safe Handling

Do not send live credentials, private keys, raw production configs, or customer data in vulnerability reports.
