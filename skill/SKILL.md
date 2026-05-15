---
name: warded
description: Use when you need to install, create, run, or diagnose `warded` for OpenClaw public entrypoint protection.
---

# Warded For OpenClaw

This is the official skill for letting OpenClaw operate `warded`.

Warded protects the public entrypoint of a cloud-hosted OpenClaw node with identity-based access control.
For OpenClaw today, that public entrypoint usually means the Control UI entrypoint.

Use this skill when the robot needs to check and repair the OpenClaw security baseline first, then prepare setup, submit setup, continue an unfinished activation, diagnose protected access, or keep the local Warded runtime healthy.

The robot owns the operational path:

- collect the right setup choices from the owner
- run `warded` commands in the supported order
- explain current state and choose the next supported action
- diagnose why the protected public entrypoint is not working

The human owner still owns the browser claim boundary:

- open the setup link
- sign in in the browser
- confirm which identity owns the service
- start trial or complete payment when required

This skill does **not** teach generic proxy engineering, firewall design, tunnel setup, or internal product architecture.
It is intentionally narrow: agent-operated setup and runtime workflow for Warded + OpenClaw.

## Operator Quick Reference

Use this as the fast path:

| User intent | Primary command | Next step |
|---|---|---|
| Warded not installed | `warded --version` | install via official `install.sh` entrypoint |
| Check OpenClaw security baseline | `warded doctor` | confirm the Control UI is not still directly exposed before running `warded new` |
| Repair OpenClaw security baseline | `warded integrate --agent openclaw --baseline` | preview the baseline fix; then re-run with `--apply` before running `warded new` |
| First-time protection setup | `warded new --help` | ask setup questions, then run `warded new --site ... --spec ... --port ...` |
| Change an unactivated setup | `warded new ...` | re-run with the new values; then run `warded new --commit` again to sync the draft |
| Submit pending setup | `warded new --commit` | user opens browser link, then run `warded doctor` |
| Continue unfinished setup | `warded status` | re-open setup link, wait for activation, or start service if ready |
| Public entrypoint still blocked after activation | `warded doctor` | then `warded integrate --agent openclaw` |
| Apply OpenClaw config fix | `warded integrate --agent openclaw --apply` | then start or retry service |
| Check current state | `warded status` | summarize domain, activation, expiry |
| Diagnose broken access | `warded doctor` | classify failure, then choose next command |
| Start protection runtime | `systemctl enable --now warded.service` | use systemd when root is available; otherwise prefer `systemctl --user`, then `tmux/screen`, then `nohup` |

Quick rules:

1. Browser claim and activation belong to the human user.
2. OpenClaw integration problems usually mean `allowedOrigins` is missing the ward origin.
3. Do not say protection is running until `warded.service` or `warded serve` is actually running.
4. Before first-time setup, inspect `warded new --help` and then pass explicit core flags such as `--site`, `--spec`, and `--port`; do not rely on implicit defaults.
5. Before browser activation is completed, all setup fields remain editable. Re-run `warded new ...` with the corrected values, then run `warded new --commit` again to sync the unactivated draft.
6. After installation, do not jump straight to `warded new`. Run `warded doctor` first and treat the OpenClaw security baseline as a hard gate.
7. If the baseline is unsafe, repair that first. Do not create or commit a setup draft until the OpenClaw Control UI is no longer directly exposed in the old topology.
8. Browser users authenticate through the platform login flow. Agent, Bot, script, or CI clients may use platform-issued Agent Bearer Tokens; those are managed on the platform, not by editing `ward.json`.

## Language

1. Reply to the end user in the user's current language.


## Use This Skill When

Use this skill if the request is about any of these:

- install `warded`
- prepare protection for the public entrypoint of an OpenClaw node
- submit or continue an incomplete setup/activation
- configure OpenClaw so the protected public entrypoint works correctly
- start the local Warded service
- check status
- diagnose why protected access is not working

Do **not** use this skill for:

- generic firewall or SSH help
- generic reverse proxy setup unrelated to OpenClaw
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
5. If the protected public entrypoint still fails after activation, check the OpenClaw integration before blaming Warded runtime.
6. On Linux nodes with `systemd`, prefer a managed service for steady-state runtime instead of leaving `warded serve` attached to an interactive shell.
7. Without root, prefer `systemctl --user`; if user-level systemd is unavailable, prefer `tmux` or `screen`; use `nohup` only as the final fallback.
8. Treat `warded status` as local discovery first. It should inspect local `data-dir` state, not enumerate every ward from the platform account.
9. Keep `.pending` local setup, submitted drafts, and claimed ward runtimes separate when explaining status.
10. Treat Agent Bearer Token support as a runtime auth path in `warded serve`: the CLI verifies platform-signed RS256 tokens with cached platform public keys and the heartbeat-provided valid JTI set.

## Command Set

Only rely on these current commands:

```bash
warded version
warded new
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

Immediately after installation, check the baseline before any setup work:

```bash
warded doctor
```

Then:

1. if the OpenClaw security baseline is acceptable, continue to setup preparation
2. if the baseline is unsafe, stop and repair it first
3. if the baseline is unsafe, prefer the explicit repair path:

```bash
warded integrate --agent openclaw --baseline
```

4. if the node must keep an old public OpenClaw port as the future Warded entrypoint, use:

```bash
warded integrate --agent openclaw --baseline --adopt-public-port=<old_public_port>
```

5. add `--apply` only when the owner wants Warded to actually rewrite `openclaw.json`
6. if `--adopt-public-port` was used with `--apply`, restart the OpenClaw gateway before continuing
7. do not run `warded new` or `warded new --commit` while the old unsafe exposure path is still in place

## Workflow 2: OpenClaw Security Baseline

Use this before first-time setup, and also before reusing a node that previously exposed OpenClaw directly.

Start with:

```bash
warded doctor
```

What you are checking:

1. whether OpenClaw is still directly reachable without Warded in front of it
2. whether the current OpenClaw bind / exposure shape violates the expected Warded safety baseline
3. whether the node needs a topology repair before setup can continue

If repair is needed, preview the supported fix:

```bash
warded integrate --agent openclaw --baseline
```

Rules:

1. Prefer explicit baseline repair flows over manual guessing.
2. If the user wants to preserve an old public OpenClaw port as the future Warded entrypoint, use `--adopt-public-port=<old_public_port>`.
3. Only add `--apply` when the owner approves the actual rewrite of `openclaw.json`.
4. If `--adopt-public-port` was applied, restart the OpenClaw gateway before continuing.
5. Only continue after the owner understands the old public port / bind shape and agrees to the repair.

## Workflow 3: First-Time Setup And Commit

Use this when the user wants to protect OpenClaw for the first time.

Do this only after Workflow 2 confirms the security baseline is acceptable.

First inspect the current command surface:

```bash
warded new --help
```

Before you run setup, ask the owner to choose the product shape:

1. Site:
   - China site: `cn` / `warded.cn`, WeChat login and WeChat Pay, CNY billing
   - International site: `global` / `warded.me`, Google/GitHub/email login, USD billing
2. Spec:
   - `starter`: platform-managed subdomain only; simplest path
   - `pro`: platform-managed preferred subdomain or custom domain
3. Domain type:
   - `platform_subdomain`: Warded reserves an entrypoint under the platform domain
   - `custom_domain`: the owner brings their own domain and must prepare DNS
4. Domain string:
   - `starter`: do not ask for a domain; Warded assigns one
   - `pro + platform_subdomain`: ask for the preferred subdomain label
   - `pro + custom_domain`: ask for the full domain, for example `robot.example.com`
5. Local ports:
   - `--port`: the public Warded listen port, usually `443`
   - `--upstream-port`: the local OpenClaw Control UI port, usually auto-detected or `18789`
6. Billing mode:
   - ask monthly or yearly when pricing or checkout is relevant

Do not guess these choices if the owner has not made them clear.
Even if the CLI has safe defaults, the robot must explain the choices and pass explicit flags for `--site`, `--spec`, and the relevant domain/port fields before commit.

First prepare or update local pending configuration with `warded new`.

Examples:

```bash
warded new --site cn --spec starter --domain-type platform_subdomain --port 443
```

```bash
warded new --site global --spec starter --domain-type platform_subdomain --port 443
```

```bash
warded new --site global --spec pro --domain-type platform_subdomain --domain myrobot --port 443
```

```bash
warded new --site cn --spec pro --domain-type custom_domain --domain robot.example.com --port 443
```

Important rule:

1. `warded new` by itself does **not** call the platform.
2. It updates the local pending setup stored under `.pending`.
3. The user or robot can run it multiple times to refine choices without re-entering every flag.
4. Only `warded new --commit` actually performs prechecks, creates the setup draft, and prints the browser link.
5. `--site` has no safe default. Always ask and pass it explicitly.
6. `--spec` may have a CLI default, but the robot must still ask and pass it explicitly so the owner understands the `starter` / `pro` choice.
7. Do not run `warded new --commit` until the owner has confirmed the site, spec, domain type, relevant domain, and ports.

After the choices are settled, submit them:

```bash
warded new --commit
```

Interpret the result:

1. if `warded new` fails, it usually means a local immediate blocker such as invalid flags, unwritable data dir, or an explicitly requested listen port that cannot be bound
2. if `warded new --commit` fails, stop and explain the exact blocker
3. if a setup link is shown, tell the user to open it in a browser
4. tell the user to claim the OpenClaw and activate protection there
5. do not wait inside `warded new --commit`; use `warded status` to check progress

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

Preferred runtime order:

1. Linux + root + systemd:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now warded.service
```

2. Linux + no root + user-level systemd available:

```bash
systemctl --user daemon-reload
systemctl --user enable --now warded.service
```

3. No usable systemd, but `tmux` is available:

```bash
tmux new-session -d -s warded 'warded serve'
```

4. No usable systemd and no `tmux`, but `screen` is available:

```bash
screen -dmS warded warded serve
```

5. Final fallback when none of the above are available:

```bash
mkdir -p ~/.config/warded/state
nohup warded serve > ~/.config/warded/state/serve.log 2>&1 &
echo $! > ~/.config/warded/state/serve.pid
```

Notes:

1. `systemctl --user` is the preferred non-root steady-state mode.
2. If user services must survive logout, the host may need `loginctl enable-linger <user>` once.
3. `nohup` is only a fallback. It detaches the blocking foreground process, but does not provide real supervision or auto-restart.
4. When using `nohup`, keep runtime artifacts centralized under `~/.config/warded/state/` rather than scattering pid/log files in the home directory.

Use plain foreground mode only for manual runs or debugging:

```bash
warded serve
```

Only after `warded.service` or `warded serve` is running should you say protection is running.

Runtime auth behavior:

1. Browser traffic uses the `warded_session` cookie created by the platform login flow.
2. Agent, Bot, script, and CI traffic may use `Authorization: Bearer <Agent Bearer Token>`.
3. Agent Bearer Tokens are platform-issued credentials. Do not ask users to create or edit them in `ward.json`.
4. `warded serve` stores platform JWT public keys in local runtime state and refreshes them during heartbeat.
5. `warded serve` keeps valid Agent Token JTIs in memory from heartbeat. These are not persisted.
6. If Bearer access fails immediately after a service restart, run `warded status` or wait for heartbeat, then retry after platform connectivity is confirmed.
7. Bearer auth failures return JSON `401`; they do not redirect to the browser login page.

## Workflow 4: Continue An Incomplete Setup Or Activation

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

1. If local state still contains only pending local choices, `warded new` may be run again to refine them.
2. If local state already contains a submitted setup draft, use `warded status` to refresh progress and show the setup link or entrypoint.
3. If `warded status` shows the setup draft is expired or the setup link is no longer valid, run `warded new --commit` to create a fresh setup draft from the current pending configuration.
4. If activation is already complete but the local proxy is not running, choose a runtime mode in this order:
   - root systemd
   - user-level systemd
   - `tmux` / `screen`
   - `nohup`

Then:

1. if setup or activation is still pending, show or repeat the setup link and ask the user to open it
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

## Workflow 5: OpenClaw Integration

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

## Workflow 6: Status Check

Use:

```bash
warded status
```

Discovery rules:

1. `warded status` searches the local `data-dir` first.
2. It should include `.pending/ward.json` as a local pending config if present.
3. It should include submitted draft directories such as `<ward_draft_id>/ward.json`.
4. It should include claimed ward runtime directories such as `<ward_id>/ward.json`.
5. If multiple local entries exist, treat the default output as a local index list and ask the user which entry to inspect.
6. Only refresh platform status after a single entry has been selected, unless `--local` was requested.
7. Do not infer that all platform-side wards are manageable on this node just because the account may own them.

Summarize:

1. whether protection is usable now
2. whether activation is complete
3. which domain is active
4. expiry timing, if available

If the user asks whether the local runtime is healthy, run `warded doctor` instead of inferring too much from `warded status`.

## Workflow 7: Diagnosis

Use:

```bash
warded doctor
```

When diagnosing Agent Bearer Token access:

1. confirm `warded serve` or `warded.service` is running
2. confirm the local runtime has `platform_jwt_public_keys` from activation, status refresh, or heartbeat
3. confirm the platform is reachable so heartbeat can refresh the in-memory `valid_agent_tokens`
4. distinguish browser cookie failures from Bearer failures; Bearer failures should return JSON `401`, not the browser login page
5. do not paste full Bearer tokens into logs or chat; use the token name, `token_prefix`, or `jti` when correlating with platform logs

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

- "Setup is still pending. Open the setup link and finish the browser step."
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
7. Do not hide unsafe OpenClaw exposure behind a normal `warded new` flow; baseline safety problems must be identified and explained first.
