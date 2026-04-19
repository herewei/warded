# Warded CLI Release Notes

## v0.2.1

### Features

#### Activation Enhancements

- **`activate --port`** - Allow specifying the local HTTP listener port during activation
  - Useful when the default port is already in use or when running multiple instances
  - Integrated into preflight checks

- **Preflight Ingress Probe** - Added proactive ingress reachability check before requesting activation
  - Detects firewall/port blocking issues early
  - Provides clear warnings if port 443 is unreachable
  - Reduces activation failures due to network misconfiguration

### Fixes

- **Expired Draft Auto-Cleanup** - Fixed issue where expired drafts blocked re-activation
  - Automatically deletes expired drafts before creating new ones
  - Users no longer need to manually clean up stale drafts to get a new activation link

### Improvements

- **Unified `--data-dir` Flag** - Renamed `--config-dir` to `--data-dir` across all commands
  - Affects: `activate`, `doctor`, `integrate`, `renew-cert`, `status`, `serve`
  - Updated all tests, install scripts, and systemd service files accordingly
  - More accurately reflects that the directory stores both configuration and runtime state

- **Install Script** - Improved non-root installation output with clear next-step instructions

- **E2E Test Stability** - Automatic port reservation in tests to prevent collisions

- **Release Pipeline** - Improved CI/CD release workflow and GoReleaser configuration

---

## v0.2.0

### Features

#### Core Commands

- **`activate`** - Activate protection for the current OpenClaw
  - Interactive ward activation workflow
  - Support for both `cn` and `global` sites
  - Automatic domain provisioning (subdomain or custom domain)
  - JWT signing secret generation and local storage
  - Platform API integration for ward draft creation and activation
  - Configurable upstream port for local service proxy
  - Billing mode selection (trial/paid)

- **`serve`** - Run the identity-aware reverse proxy
  - TLS termination with CertMagic automatic certificate management
  - JWT-based authentication middleware
  - Local JWT validation (passive auth)
  - `/_ward/callback` endpoint for authentication callbacks
  - Logout and health check endpoints
  - `X-Forwarded-User` header injection for OpenClaw trusted-proxy-auth
  - Support for multiple TLS providers (ACME/Platform)

- **`status`** - Show current ward and runtime status
  - Local configuration display
  - Platform API connectivity check
  - Runtime state inspection (JWT secret, domain, upstream)
  - Ward lifecycle status (draft, pending_dns, active, etc.)

- **`doctor`** - Run interactive diagnostics for the current node
  - Ward runtime configuration validation
  - Systemd service status check
  - TLS certificate validation
  - DNS resolution check
  - Platform API reachability test
  - Colored output with status indicators

- **`integrate`** - Inspect or apply local agent integration patches
  - OpenClaw integration support
  - Configuration file patching
  - Preview mode (dry-run) and apply mode
  - Automatic Caddyfile/OpenClaw config updates

- **`renew-cert`** - Renew TLS certificates manually
  - Force certificate renewal
  - Platform TLS provider integration

- **`version`** - Display CLI version information

### Site Support

- **warded.cn** (China site)
  - WeChat and Email login
  - WeChat Pay
  - CNY billing

- **warded.me** (Global site)
  - Google, GitHub, and Email login
  - Paddle payment
  - USD billing

### Platform Support

- macOS (Intel & Apple Silicon)
- Linux (x86_64 & ARM64)

### Configuration

- Config directory (优先级递减):
  - Linux root (if `/var/lib/warded` exists): `/var/lib/warded`
  - User config: `~/.config/warded` (via `os.UserConfigDir`)
  - Fallback: `.warded`
- Runtime state: `ward.json`
- ACME state (when using local TLS): `certmagic/` subdirectory
- Ward configuration: `ward.json`

---

## Installation

```bash
# macOS/Linux
curl -fsSL https://warded.me/install.sh | sh

# Or download directly
curl -LO https://github.com/herewei/warded/releases/download/v0.2.0/warded_v0.2.0_$(uname -s)_$(uname -m).tar.gz
tar -xzf warded_v0.2.0_$(uname -s)_$(uname -m).tar.gz
sudo mv warded /usr/local/bin/
```

## Quick Start

```bash
# Activate a new ward
warded activate --site global --upstream-port 3000

# Start the reverse proxy
warded serve

# Check status
warded status

# Run diagnostics
warded doctor
```

---

*For detailed documentation, see [warded_docs](https://docs.warded.me/)*
