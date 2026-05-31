# Warded CLI

Warded is an out-of-the-box identity access gateway for cloud-hosted AI Agent management interfaces.

The `warded` CLI prepares a protected public entry point, helps verify that the host is reachable, opens the browser setup flow, and runs the local identity-aware proxy that protects the Agent UI.

Warded is not a generic tunnel, NAT traversal tool, or "expose localhost" product. It is meant to protect supported cloud-deployed Agent control surfaces with identity, TLS, and reverse proxy behavior bundled into one local runtime.

## What You Get

- A public HTTPS entry point under `warded.me`, `warded.cn`, or your own domain.
- Browser login through the Warded platform.
- A local identity-aware reverse proxy through `warded serve`.
- Support for a long-running upstream process, such as OpenClaw Control UI.
- Support for a managed upstream command, useful for dashboards that are not always running.
- Local diagnostics for host readiness, OpenClaw safety baseline, activation status, and runtime health.

## What Warded Does Not Do

Warded does not provide NAT traversal, FRP/ngrok-style tunneling, VPN replacement, or arbitrary localhost publishing. Your host must already be reachable in the way you configure it.

One ward protects one domain and one default upstream. If you want multiple domains, use `warded new` create multiple wards.

## Install

Global site:

```bash
curl -fsSL https://warded.me/install.sh | sh
```

China site:

```bash
curl -fsSL https://warded.cn/install.sh | sh
```

Verify the installation:

```bash
warded --version
```

## Sites

Choose the site that matches the owner account and billing region:

| Site | Domain | Typical login and billing |
|---|---|---|
| `global` | `warded.me` | Global accounts, USD billing |
| `cn` | `warded.cn` | China accounts, CNY billing |

The two sites are separate. Do not expect accounts, wards, payments, or providers to carry across sites.

## Quick Start: OpenClaw

For OpenClaw, check the safety baseline before creating a ward:

```bash
warded doctor agent openclaw --baseline
```

If the output says the baseline needs repair, apply the suggested repair:

```bash
warded integrate agent openclaw --baseline --repair
```

If you need Warded to take over an old public OpenClaw port, use the suggested `--adopt-public-port` value shown by `doctor`:

```bash
warded integrate agent openclaw --baseline --repair --adopt-public-port 18789
```

After the baseline is acceptable, run a preflight check:

```bash
warded doctor preflight --site global --upstream 127.0.0.1:18789
```

Prepare the pending setup:

```bash
warded new \
  --site global \
  --spec starter \
  --domain-type platform_subdomain \
  --listen 0.0.0.0 \
  --port 443 \
  --upstream 127.0.0.1:18789
```

Review the output. When it looks correct, submit it:

```bash
warded new --commit
```

Open the setup link printed by the CLI, claim the Agent in the browser, and complete trial or payment. Then check status:

```bash
warded status
```

If the ward is active, make sure OpenClaw accepts the protected origin:

```bash
warded integrate agent openclaw --allow-origins
```

Start the local proxy:

```bash
warded serve
```

For production use, run `warded serve` under systemd or another process manager instead of leaving it attached to a terminal.

## Quick Start: Managed Upstream

Use `managed` upstream mode when Warded should start the local dashboard command itself.

Example:

```bash
warded doctor preflight \
  --site global \
  --upstream 127.0.0.1:9119 \
  --upstream-mode managed \
  --upstream-command "hermes dashboard --host 127.0.0.1 --port 9119 --no-open"
```

Then save the setup:

```bash
warded new \
  --site global \
  --spec starter \
  --domain-type platform_subdomain \
  --upstream 127.0.0.1:9119 \
  --upstream-mode managed \
  --upstream-command "hermes dashboard --host 127.0.0.1 --port 9119 --no-open"
```

Submit when the pending setup looks correct:

```bash
warded new --commit
```

In managed mode, `warded new --commit` starts the upstream only for the preflight window and cleans it up when the command exits. Later, `warded serve` starts and owns the upstream while the proxy is running.

## Ingress Modes

Warded supports two ingress modes.

### Standalone

`standalone` is the default. Warded listens on the public entry port and handles the local proxy listener itself.

Example:

```bash
warded new \
  --site global \
  --spec starter \
  --domain-type platform_subdomain \
  --ingress-mode standalone \
  --listen 0.0.0.0 \
  --port 443 \
  --upstream 127.0.0.1:18789
```

Use this when the machine can bind the public port directly and your firewall or security group allows inbound traffic.

### Behind Proxy

`behind-proxy` is for deployments where an existing reverse proxy already owns public HTTPS. In this mode, Warded listens locally over HTTP and the front proxy forwards a whole domain to it.

Example:

```bash
warded new \
  --site global \
  --spec pro \
  --domain-type custom_domain \
  --domain admin.example.com \
  --ingress-mode behind-proxy \
  --listen 127.0.0.1 \
  --port 6678 \
  --public-port 443 \
  --upstream 127.0.0.1:18789
```

Notes:

- `behind-proxy` requires `custom_domain`.
- It does not support path-prefix mounting such as `/admin`; use a dedicated domain such as `admin.example.com`.
- Your front proxy must preserve the Host header and forward `/_ward/*` paths to Warded.

## The Setup Flow

`warded new` uses a two-step flow.

1. `warded new ...flags` saves or updates a local pending setup.
2. `warded new --commit` performs preflight checks, asks the platform to reserve the entry point, and prints a browser setup link.

Run this any time to review the current pending setup:

```bash
warded new
```

or:

```bash
warded new --show
```

The pending setup output includes the current site, spec, domain, setup state, listener, upstream address, upstream mode, optional upstream command, and billing mode.

## Status And Activation

Use `status` to see what Warded knows locally and, unless `--local` is set, refresh the selected ward from the platform:

```bash
warded status
```

If multiple local entries exist, Warded prints a local list. Inspect one entry by index, ward id, draft id, or domain:

```bash
warded status 2
warded status --ward-id wrd_abc123
warded status --draft-id d_abc123
warded status --domain robot.example.com
```

Use local-only mode when you do not want a platform call:

```bash
warded status --local
```

`status` is also the command to run after you finish the browser setup. If activation completed, it can claim the final local runtime credentials and update `ward.json`.

## Running Warded

Manual foreground run:

```bash
warded serve
```

Run all committed wards in the current data directory when they share the same listener group:

```bash
warded serve --all
```

Run selected wards:

```bash
warded serve --ward-id wrd_abc123 --ward-id wrd_def456
```

When serving multiple wards in one process, all selected wards must share the same listener address, port, IP family, and ingress mode. Warded selects the target ward by HTTPS SNI or HTTP Host.

## Recommended Service Modes

Linux with root systemd:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now warded.service
```

Linux user-level systemd:

```bash
systemctl --user daemon-reload
systemctl --user enable --now warded.service
```

If user services must survive logout:

```bash
sudo loginctl enable-linger <user>
```

Fallback with `tmux`:

```bash
tmux new-session -d -s warded 'warded serve'
```

Final fallback with `nohup`:

```bash
mkdir -p ~/.config/warded/state
nohup warded serve > ~/.config/warded/state/serve.log 2>&1 &
echo $! > ~/.config/warded/state/serve.pid
```

`nohup` only detaches the process. It does not provide supervision or automatic restart.

## Diagnostics

Check whether the host can run Warded before setup:

```bash
warded doctor preflight --site global
```

Check OpenClaw baseline:

```bash
warded doctor agent openclaw --baseline
```

Check an existing local runtime:

```bash
warded doctor
```

or:

```bash
warded doctor runtime
```

For multiple local wards:

```bash
warded doctor runtime --ward-id wrd_abc123
```

Common outcomes:

- If preflight fails on the listener, fix port permissions or choose another listener.
- If preflight fails on upstream readiness, start the upstream service or use managed upstream mode.
- If ingress probe fails, check DNS, firewall, security group, public port forwarding, and front proxy rules.
- If OpenClaw integration fails after activation, run `warded integrate agent openclaw --allow-origins`.

## Whitelist Rules

Whitelist rules skip Warded authentication for specific paths. Use them only for endpoints that have their own security model, such as webhooks.

Add a rule:

```bash
warded whitelist add --exact /webhook
warded whitelist add --prefix /callbacks/
```

List rules:

```bash
warded whitelist list
```

Remove a rule:

```bash
warded whitelist remove --exact /webhook
```

There are no default whitelist rules.

## JSON Output

Most commands support machine-readable output:

```bash
warded --format json status
warded --format json doctor preflight --site global
warded --format json new --show
```

`warded serve --format json` prints one startup event to stdout and then keeps running. Runtime logs do not become JSON command output.

JSON output is intended for agents and automation. It does not include secrets, TLS private keys, auth codes, or raw tokens.

## Certificate Refresh

For standalone platform-subdomain wards, `serve` can fetch platform TLS material at startup and refresh it in the background. You can also refresh explicitly:

```bash
warded renew-cert
```

This does not apply to `behind-proxy` deployments because the front proxy owns public TLS.

## Security Notes

- Do not edit `ward.json` manually unless Warded support explicitly asks you to.
- Do not paste `ward_secret`, local JWT signing secrets, TLS private keys, browser auth codes, or bearer tokens into issues, chat, or logs.
- Do not report unpatched vulnerabilities in public issues or pull requests.

For private security disclosure, see [SECURITY.md](./SECURITY.md).

## License

Licensed under the Apache License 2.0. See [LICENSE](./LICENSE) and [NOTICE](./NOTICE).
