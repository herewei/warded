---
name: warded
description: Use when you need to install, activate, run, or diagnose `warded` to protect the public entrypoint of an AI agent node. The integration steps in this skill currently apply to OpenClaw only.
---

# Warded For AI Agent

Use this skill to operate `warded`.

First confirm what agent you are running.

- If you are OpenClaw, use the OpenClaw workflow in this skill.
- If you are not OpenClaw, do not invent OpenClaw-specific integration steps.
- Only run `warded integrate --agent openclaw` after confirming the host agent is OpenClaw.

Warded protects the public entrypoint of an AI agent node with identity-based access control.
For OpenClaw today, that public entrypoint usually means the Control UI entrypoint.

This skill does **not** teach generic proxy engineering, firewall design, tunnel setup, or internal product architecture.

## Operator Quick Reference

Use this as the fast path:

| User intent | Primary command | Next step |
|---|---|---|
| Warded not installed | `warded --version` | install via official `install.sh` entrypoint |
| First-time protection setup | `warded activate` | user opens browser link, then run `warded doctor` |
| Continue unfinished activation | `warded status` | re-open setup link, or re-run `warded activate` to resume |
| Public entrypoint still blocked after activation | `warded doctor` | then `warded integrate --agent openclaw` |
| Apply OpenClaw config fix | `warded integrate --agent openclaw --apply` | then start or retry service |
| Check current state | `warded status` | summarize domain, activation, expiry |
| Diagnose broken access | `warded doctor` | classify failure, then choose next command |
| Start protection runtime | `systemctl enable --now warded.service` | prefer this on Linux systemd nodes after activation |

Quick rules:

1. Browser claim and activation belong to the human user.
2. Confirm the host agent before you choose an integration workflow.
3. OpenClaw integration problems usually mean `allowedOrigins` is missing the ward origin.
3. Do not say protection is running until `warded.service` or `warded serve` is actually running.

## Language

1. Reply to the end user in the user's current language.


## Use This Skill When

Use this skill if the request is about any of these:

- install `warded`
- activate protection for the public entrypoint of an AI agent node
- continue an incomplete activation
- configure OpenClaw so the protected public entrypoint works correctly
- start the local Warded service
- check status
- diagnose why protected access is not working

Do **not** use this skill for:

- generic firewall or SSH help
- generic reverse proxy setup unrelated to AI Agent
- NAT traversal, FRP, Tailscale-like exposure, or localhost tunneling
- direct manual editing of `ward.json`

## Core Operating Rules

1. Prefer `warded` commands over manual file editing.
2. Treat the browser claim/activation step as human-owned. The robot can provide links and guidance, but cannot complete browser login for the user.
3. Do not claim success before evidence:
   - activation is not complete until CLI state proves it
   - protection is not running until `warded.service` or `warded serve` is actually running
4. Keep three states separate:
   - environment ready
   - activation complete
   - local proxy running
5. Treat host-agent integration as agent-specific. Do not assume OpenClaw integration applies to every AI agent.
6. If the protected public entrypoint still fails after activation, check the host-agent integration before blaming Warded runtime.
7. On Linux nodes with `systemd`, prefer `warded.service` for steady-state runtime instead of leaving `warded serve` attached to an interactive shell.

## Command Set

Only rely on these current commands:

```bash
warded version
warded activate
warded integrate --agent openclaw
warded serve
warded status
warded doctor
warded renew-cert
```

Do not invent or recommend planned commands unless the user explicitly asks about future capabilities.

## Workflow 1: Install Warded

Use this when `warded` is missing.

First verify:

```bash
warded --version
```

If it is missing, prefer one official installer command:

- global/default:

```bash
curl -fsSL https://warded.me/install.sh | sh
```

- China / `cn` site:

```bash
curl -fsSL https://warded.cn/install.sh | sh
```

After installation, verify again:

```bash
warded --version
```

Rules:

1. Prefer the short official install entrypoint.
2. Do not send users to raw release asset URLs unless the install entrypoint is unavailable.
3. Do not say installation succeeded until `warded --version` works.

## Workflow 2: First-Time Activation

Use this when the user wants to protect OpenClaw for the first time.

First inspect the current command surface:

```bash
warded activate --help
```

Before you run activation, ask the owner to choose the product shape:

1. `starter` or `pro`
2. platform-managed subdomain or custom domain
3. if using a preferred subdomain or custom domain, which domain string to request
4. monthly or yearly billing if that choice matters in the current flow

Do not guess these choices if the owner has not made them clear.

Then run `warded activate` with the chosen flags.

Examples:

```bash
warded activate
```

```bash
warded activate --spec starter --domain-type platform_subdomain
```

```bash
warded activate --spec pro --domain-type platform_subdomain --domain myrobot
```

```bash
warded activate --spec pro --domain-type custom_domain --domain robot.example.com
```

Site hint:

1. If the node is clearly on the `cn` site, prefer:

```bash
warded activate --site cn
```

2. Otherwise let the CLI use its default site behavior.

Interpret the result:

1. if preflight fails, stop and explain the exact blocker
2. if an activation URL is shown, tell the user to open it in a browser
3. tell the user to claim the OpenClaw and activate protection there
4. let `warded activate` keep waiting by default

After activation succeeds, continue with:

```bash
warded doctor
```

If OpenClaw integration is missing:

```bash
warded integrate --agent openclaw
```

If the user wants Warded to update OpenClaw config directly:

```bash
warded integrate --agent openclaw --apply
```

Then start the local service.

On Linux nodes with `systemd`, prefer:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now warded.service
```

Use foreground mode only for manual runs, non-systemd environments, or debugging:

```bash
warded serve
```

Only after `warded.service` or `warded serve` is running should you say protection is running.

## Workflow 3: Continue An Incomplete Activation

Use this when:

- the user closed the terminal
- the browser step was not finished
- activation timed out locally
- the user says "continue" or "what now"

Start with:

```bash
warded status
```

Resume hint:

1. If local state still contains a pending draft, `warded activate` may also be re-run.
2. In that case it should resume from the existing draft instead of creating a brand-new one.

Then:

1. if activation is still pending, show or repeat the activation URL and ask the user to open it
2. if activation is complete but OpenClaw integration is missing, run:

```bash
warded doctor
warded integrate --agent openclaw
```

3. if activation is complete and integration is fine, start the runtime:

- on Linux systemd nodes:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now warded.service
```

- otherwise:

```bash
warded serve
```

Do not restart from generic troubleshooting if the main problem is simply "browser-side claim not completed yet".

## Workflow 4: OpenClaw Integration

OpenClaw Control UI may still fail after activation if the ward origin is not in:

`gateway.controlUi.allowedOrigins`

Default check:

```bash
warded doctor
```

Default fix preview:

```bash
warded integrate --agent openclaw
```

Apply the change only when the user wants an actual file modification:

```bash
warded integrate --agent openclaw --apply
```

Rules:

1. Prefer showing the suggested patch first.
2. Use `--apply` only when the user wants Warded to edit `openclaw.json`.
3. Do not tell the user that `warded serve` alone guarantees Control UI will work.

## Workflow 5: Status Check

Use:

```bash
warded status
```

Summarize:

1. whether protection is usable now
2. whether activation is complete
3. which domain is active
4. expiry timing, if available

If the user asks whether the local runtime is healthy, run `warded doctor` instead of inferring too much from `warded status`.

## Workflow 6: Diagnosis

Use:

```bash
warded doctor
```

If needed, also run:

```bash
warded status
```

Classify the primary problem into one bucket:

- Warded not installed
- local proxy not started
- `warded.service` not running or failed on a Linux systemd node
- activation not complete
- OpenClaw integration missing
- upstream service not reachable
- public reachability / DNS / certificate problem
- login or session problem

If the node uses `systemd`, also check:

```bash
systemctl status warded.service
journalctl -u warded.service -n 50 --no-pager
```

Response order:

1. primary failure point
2. next action
3. supporting detail

Good examples:

- "Activation is still pending. Open the setup link and finish the browser step."
- "The ward is active, but OpenClaw still needs the protected origin added to `allowedOrigins`."
- "The local Warded proxy is not running yet. Start `warded.service` or run `warded serve` for a manual session."

Bad examples:

- "The ward_draft has not transitioned."
- "Please complete principal binding."
- "auth_code exchange failed."

## User-Facing Wording

Prefer simple phrases:

- "claim your OpenClaw and activate protection"
- "open this link in a browser"
- "start the local protection service"
- "add the protected origin to OpenClaw Control UI settings"

Avoid internal phrases:

- `ward_draft`
- `principal`
- `auth_code`

## Safety And Boundaries

1. Never edit `ward.json` manually.
2. Only edit OpenClaw config through `warded integrate --agent openclaw --apply` or explicit user-approved manual editing.
3. Do not promise webhook, renewal, payment, cron, or notification commands in the current build.
4. Do not suggest replacing Warded with a different reverse proxy stack.
5. Do not suggest exposing arbitrary local services; this skill is only for the OpenClaw Control UI behind Warded.
6. Do not treat interactive `warded serve` as the preferred steady-state deployment mode on Linux systemd nodes.
