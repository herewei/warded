#!/bin/sh

set -eu

PROGRAM="warded"

WARDED_INSTALL_VERSION="${WARDED_INSTALL_VERSION:-latest}"
WARDED_INSTALL_SOURCE="${WARDED_INSTALL_SOURCE:-auto}"
WARDED_INSTALL_SITE="${WARDED_INSTALL_SITE:-global}"
WARDED_INSTALL_DIR="${WARDED_INSTALL_DIR:-}"
WARDED_INSTALL_SYSTEMD="${WARDED_INSTALL_SYSTEMD:-auto}"
WARDED_SKIP_CHECKSUM="${WARDED_SKIP_CHECKSUM:-0}"

WARDED_DOWNLOAD_BASE_URL="${WARDED_DOWNLOAD_BASE_URL:-}"
WARDED_DOWNLOAD_BASE_URL_GLOBAL="${WARDED_DOWNLOAD_BASE_URL_GLOBAL:-https://downloads.warded.me/releases}"
WARDED_DOWNLOAD_BASE_URL_CN="${WARDED_DOWNLOAD_BASE_URL_CN:-https://downloads.warded.cn/releases}"

WARDED_GITHUB_REPO="${WARDED_GITHUB_REPO:-}"
WARDED_GITEE_REPO="${WARDED_GITEE_REPO:-}"
WARDED_GITEE_ASSET_BASE="${WARDED_GITEE_ASSET_BASE:-}"

WARDED_SYSTEM_USER="${WARDED_SYSTEM_USER:-warded}"
WARDED_SYSTEM_GROUP="${WARDED_SYSTEM_GROUP:-warded}"
WARDED_SYSTEM_UID="${WARDED_SYSTEM_UID:-}"
WARDED_SYSTEM_GID="${WARDED_SYSTEM_GID:-}"
WARDED_STATE_DIR="${WARDED_STATE_DIR:-/var/lib/warded}"
WARDED_ETC_DIR="${WARDED_ETC_DIR:-/etc/warded}"
WARDED_ENV_FILE="${WARDED_ENV_FILE:-${WARDED_ETC_DIR%/}/warded.env}"
WARDED_SYSTEMD_UNIT_DIR="${WARDED_SYSTEMD_UNIT_DIR:-/etc/systemd/system}"
WARDED_SYSTEMD_UNIT_NAME="${WARDED_SYSTEMD_UNIT_NAME:-warded.service}"

TMPDIR_ROOT="${TMPDIR:-/tmp}"
WORKDIR=""
ATTEMPTED_SOURCES=""
OS_NORMALIZED=""
INSTALL_SITE=""
SYSTEMD_SETUP_MODE=""

log() {
  printf '%s\n' "$*"
}

fail() {
  printf 'Error: %s\n' "$*" >&2
  exit 1
}

cleanup() {
  if [ -n "${WORKDIR}" ] && [ -d "${WORKDIR}" ]; then
    rm -rf "${WORKDIR}"
  fi
}

trap cleanup EXIT INT TERM

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

has_cmd() {
  command -v "$1" >/dev/null 2>&1
}

is_root() {
  [ "$(id -u)" -eq 0 ]
}

normalize_os() {
  raw_os="$(uname -s 2>/dev/null || true)"
  case "$raw_os" in
    Darwin) printf 'darwin' ;;
    Linux) printf 'linux' ;;
    *) fail "unsupported operating system: ${raw_os:-unknown}" ;;
  esac
}

normalize_arch() {
  raw_arch="$(uname -m 2>/dev/null || true)"
  case "$raw_arch" in
    x86_64|amd64) printf 'amd64' ;;
    arm64|aarch64) printf 'arm64' ;;
    *) fail "unsupported architecture: ${raw_arch:-unknown}" ;;
  esac
}

normalize_site() {
  case "$WARDED_INSTALL_SITE" in
    global|cn) printf '%s' "$WARDED_INSTALL_SITE" ;;
    *) fail "unsupported WARDED_INSTALL_SITE: $WARDED_INSTALL_SITE" ;;
  esac
}

normalize_systemd_mode() {
  case "$WARDED_INSTALL_SYSTEMD" in
    auto|0|1|true|false|yes|no) printf '%s' "$WARDED_INSTALL_SYSTEMD" ;;
    *) fail "unsupported WARDED_INSTALL_SYSTEMD: $WARDED_INSTALL_SYSTEMD" ;;
  esac
}

artifact_name() {
  os="$1"
  arch="$2"
  printf '%s_%s_%s.tar.gz' "$PROGRAM" "$os" "$arch"
}

choose_source() {
  case "$WARDED_INSTALL_SOURCE" in
    auto) printf 'downloads' ;;
    downloads|github|gitee) printf '%s' "$WARDED_INSTALL_SOURCE" ;;
    *) fail "unsupported WARDED_INSTALL_SOURCE: $WARDED_INSTALL_SOURCE" ;;
  esac
}

resolve_version() {
  if [ "$WARDED_INSTALL_VERSION" = "latest" ]; then
    printf 'latest'
    return
  fi
  printf '%s' "$WARDED_INSTALL_VERSION"
}

version_component() {
  version="$1"
  if [ "$version" = "latest" ]; then
    printf 'latest'
    return
  fi
  printf '%s' "$version"
}

downloads_base_for_site() {
  site="$1"
  if [ -n "$WARDED_DOWNLOAD_BASE_URL" ]; then
    printf '%s' "${WARDED_DOWNLOAD_BASE_URL%/}"
    return
  fi

  case "$site" in
    global) printf '%s' "${WARDED_DOWNLOAD_BASE_URL_GLOBAL%/}" ;;
    cn) printf '%s' "${WARDED_DOWNLOAD_BASE_URL_CN%/}" ;;
    *) return 1 ;;
  esac
}

secondary_downloads_base_for_site() {
  site="$1"
  if [ -n "$WARDED_DOWNLOAD_BASE_URL" ]; then
    return 1
  fi

  case "$site" in
    global) printf '%s' "${WARDED_DOWNLOAD_BASE_URL_CN%/}" ;;
    cn) printf '%s' "${WARDED_DOWNLOAD_BASE_URL_GLOBAL%/}" ;;
    *) return 1 ;;
  esac
}

downloads_asset_url() {
  base="$1"
  version="$2"
  artifact="$3"
  printf '%s/%s/%s' "$base" "$(version_component "$version")" "$artifact"
}

downloads_checksums_url() {
  base="$1"
  version="$2"
  printf '%s/%s/checksums.txt' "$base" "$(version_component "$version")"
}

github_asset_url() {
  version="$1"
  artifact="$2"
  if [ -z "$WARDED_GITHUB_REPO" ]; then
    return 1
  fi

  if [ "$version" = "latest" ]; then
    printf 'https://github.com/%s/releases/latest/download/%s' "$WARDED_GITHUB_REPO" "$artifact"
  else
    printf 'https://github.com/%s/releases/download/%s/%s' "$WARDED_GITHUB_REPO" "$version" "$artifact"
  fi
}

github_checksums_url() {
  version="$1"
  if [ -z "$WARDED_GITHUB_REPO" ]; then
    return 1
  fi

  if [ "$version" = "latest" ]; then
    printf 'https://github.com/%s/releases/latest/download/checksums.txt' "$WARDED_GITHUB_REPO"
  else
    printf 'https://github.com/%s/releases/download/%s/checksums.txt' "$WARDED_GITHUB_REPO" "$version"
  fi
}

gitee_asset_url() {
  version="$1"
  artifact="$2"
  if [ -z "$WARDED_GITEE_ASSET_BASE" ]; then
    return 1
  fi

  base="${WARDED_GITEE_ASSET_BASE%/}"
  if [ "$version" = "latest" ]; then
    printf '%s/latest/%s' "$base" "$artifact"
  else
    printf '%s/%s/%s' "$base" "$version" "$artifact"
  fi
}

gitee_checksums_url() {
  version="$1"
  if [ -z "$WARDED_GITEE_ASSET_BASE" ]; then
    return 1
  fi

  base="${WARDED_GITEE_ASSET_BASE%/}"
  if [ "$version" = "latest" ]; then
    printf '%s/latest/checksums.txt' "$base"
  else
    printf '%s/%s/checksums.txt' "$base" "$version"
  fi
}

append_attempt() {
  label="$1"
  if [ -z "$ATTEMPTED_SOURCES" ]; then
    ATTEMPTED_SOURCES="$label"
  else
    ATTEMPTED_SOURCES="$ATTEMPTED_SOURCES, $label"
  fi
}

manual_release_hint() {
  if [ -n "$WARDED_GITHUB_REPO" ]; then
    printf 'https://github.com/%s/releases/latest' "$WARDED_GITHUB_REPO"
    return
  fi
  if [ -n "$WARDED_GITEE_REPO" ]; then
    printf 'https://gitee.com/%s/releases' "$WARDED_GITEE_REPO"
    return
  fi
  printf 'https://warded.me/install.sh'
}

download_file() {
  url="$1"
  out="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$out"
    return
  fi
  if command -v wget >/dev/null 2>&1; then
    wget -qO "$out" "$url"
    return
  fi
  fail "neither curl nor wget is available"
}

detect_install_dir() {
  if [ -n "$WARDED_INSTALL_DIR" ]; then
    printf '%s' "$WARDED_INSTALL_DIR"
    return
  fi

  if [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
    printf '/usr/local/bin'
    return
  fi

  if [ -z "${HOME:-}" ]; then
    fail "HOME is not set and /usr/local/bin is not writable; set WARDED_INSTALL_DIR explicitly"
  fi

  printf '%s/.local/bin' "$HOME"
}

systemd_setup_enabled() {
  case "$SYSTEMD_SETUP_MODE" in
    1|true|yes) return 0 ;;
  esac

  if [ "$SYSTEMD_SETUP_MODE" != "auto" ]; then
    return 1
  fi
  if [ "$OS_NORMALIZED" != "linux" ]; then
    return 1
  fi
  if ! is_root; then
    return 1
  fi
  if ! has_cmd systemctl; then
    return 1
  fi
  return 0
}

ensure_root_if_systemd_forced() {
  case "$SYSTEMD_SETUP_MODE" in
    1|true|yes)
      [ "$OS_NORMALIZED" = "linux" ] || fail "WARDED_INSTALL_SYSTEMD requires Linux"
      is_root || fail "WARDED_INSTALL_SYSTEMD requires running the installer as root"
      has_cmd systemctl || fail "WARDED_INSTALL_SYSTEMD requires systemctl on this host"
      ;;
  esac
}

group_exists() {
  group="$1"
  if has_cmd getent; then
    getent group "$group" >/dev/null 2>&1
    return
  fi
  grep -q "^${group}:" /etc/group 2>/dev/null
}

user_exists() {
  user="$1"
  if has_cmd getent; then
    getent passwd "$user" >/dev/null 2>&1
    return
  fi
  grep -q "^${user}:" /etc/passwd 2>/dev/null
}

ensure_system_group() {
  if group_exists "$WARDED_SYSTEM_GROUP"; then
    return
  fi

  if has_cmd groupadd; then
    if [ -n "$WARDED_SYSTEM_GID" ]; then
      groupadd --system --gid "$WARDED_SYSTEM_GID" "$WARDED_SYSTEM_GROUP"
    else
      groupadd --system "$WARDED_SYSTEM_GROUP"
    fi
    return
  fi

  fail "cannot create group '$WARDED_SYSTEM_GROUP': groupadd is required"
}

ensure_system_user() {
  if user_exists "$WARDED_SYSTEM_USER"; then
    return
  fi

  if has_cmd useradd; then
    shell_path="/usr/sbin/nologin"
    if [ ! -x "$shell_path" ]; then
      shell_path="/sbin/nologin"
    fi
    if [ ! -x "$shell_path" ]; then
      shell_path="/usr/bin/false"
    fi
    if [ -n "$WARDED_SYSTEM_UID" ]; then
      useradd --system --uid "$WARDED_SYSTEM_UID" --home-dir "$WARDED_STATE_DIR" --create-home --gid "$WARDED_SYSTEM_GROUP" --shell "$shell_path" "$WARDED_SYSTEM_USER"
    else
      useradd --system --home-dir "$WARDED_STATE_DIR" --create-home --gid "$WARDED_SYSTEM_GROUP" --shell "$shell_path" "$WARDED_SYSTEM_USER"
    fi
    return
  fi

  fail "cannot create user '$WARDED_SYSTEM_USER': useradd is required"
}

ensure_dir_owned() {
  dir="$1"
  owner="$2"
  group="$3"
  mode="$4"
  mkdir -p "$dir"
  chmod "$mode" "$dir"
  chown "$owner:$group" "$dir"
}

ensure_env_file() {
  env_dir="$WARDED_ETC_DIR"
  env_file="$WARDED_ENV_FILE"
  owner="$WARDED_SYSTEM_USER"
  group="$WARDED_SYSTEM_GROUP"

  mkdir -p "$env_dir"
  chmod 0755 "$env_dir"
  chown "${owner}:${group}" "$env_dir"

  if [ ! -f "$env_file" ]; then
    cat > "$env_file" <<EOF
# Warded environment for systemd.
# Example:
# WARDED_BASE_DOMAIN=dev.warded.me
# WARDED_SITE=global
EOF
    chmod 0640 "$env_file"
    chown "${owner}:${group}" "$env_file"
  fi
}

read_binary_version() {
  target="$1"
  if [ ! -x "$target" ]; then
    return 1
  fi
  "$target" --version 2>/dev/null || true
}

write_systemd_unit() {
  installed_path="$1"
  unit_dir="$WARDED_SYSTEMD_UNIT_DIR"
  unit_file="${unit_dir%/}/$WARDED_SYSTEMD_UNIT_NAME"
  tmp_file="$WORKDIR/$WARDED_SYSTEMD_UNIT_NAME.tmp"

  mkdir -p "$unit_dir"

  cat > "$tmp_file" <<EOF
[Unit]
Description=Warded OpenClaw Protection Proxy
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$WARDED_SYSTEM_USER
Group=$WARDED_SYSTEM_GROUP
EnvironmentFile=-$WARDED_ENV_FILE
WorkingDirectory=$WARDED_STATE_DIR
ExecStart=$installed_path serve --data-dir $WARDED_STATE_DIR
Restart=always
RestartSec=5
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
NoNewPrivileges=true
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

  if [ -f "$unit_file" ] && has_cmd cmp && cmp -s "$tmp_file" "$unit_file"; then
    return
  fi

  mv "$tmp_file" "$unit_file"
  chmod 0644 "$unit_file"
}

setup_system_service_layout() {
  installed_path="$1"

  if ! systemd_setup_enabled; then
    return
  fi

  ensure_system_group
  ensure_system_user
  ensure_dir_owned "$WARDED_STATE_DIR" "$WARDED_SYSTEM_USER" "$WARDED_SYSTEM_GROUP" 0750
  ensure_env_file
  write_systemd_unit "$installed_path"
}

verify_checksum() {
  artifact="$1"
  checksums_file="$2"
  artifact_path="$3"

  if [ "$WARDED_SKIP_CHECKSUM" = "1" ]; then
    log "Skipping checksum verification because WARDED_SKIP_CHECKSUM=1"
    return
  fi

  expected="$(awk -v name="$artifact" '$2 == name || $2 == "*" name { print $1 }' "$checksums_file")"
  [ -n "$expected" ] || fail "checksum entry not found for $artifact"

  if command -v shasum >/dev/null 2>&1; then
    actual="$(shasum -a 256 "$artifact_path" | awk '{print $1}')"
  elif command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "$artifact_path" | awk '{print $1}')"
  else
    fail "neither shasum nor sha256sum is available for checksum verification"
  fi

  [ "$expected" = "$actual" ] || fail "checksum verification failed for $artifact"
}

extract_archive() {
  archive="$1"
  dest="$2"
  mkdir -p "$dest"
  tar -xzf "$archive" -C "$dest"
}

find_binary() {
  root="$1"
  found="$(find "$root" -type f -name "$PROGRAM" | head -n 1 || true)"
  [ -n "$found" ] || fail "binary '$PROGRAM' not found in archive"
  printf '%s' "$found"
}

install_binary() {
  src="$1"
  bin_dir="$2"
  target="$bin_dir/$PROGRAM"

  mkdir -p "$bin_dir"

  tmp_target="$target.tmp.$$"
  cp "$src" "$tmp_target"
  chmod +x "$tmp_target"
  mv "$tmp_target" "$target"
  printf '%s' "$target"
}

verify_install() {
  target="$1"
  version_output="$("$target" --version 2>/dev/null || true)"
  [ -n "$version_output" ] || fail "installed binary verification failed: '$target --version' returned no output"
  printf '%s' "$version_output"
}

ordered_sources() {
  source="$1"
  site="$2"

  case "$source" in
    downloads)
      if [ "$site" = "cn" ]; then
        printf '%s\n' 'downloads_primary downloads_secondary gitee github'
      else
        printf '%s\n' 'downloads_primary downloads_secondary github gitee'
      fi
      ;;
    github)
      printf '%s\n' 'github downloads_primary downloads_secondary gitee'
      ;;
    gitee)
      printf '%s\n' 'gitee downloads_primary downloads_secondary github'
      ;;
    *)
      fail "unsupported source: $source"
      ;;
  esac
}

resolve_source_urls() {
  token="$1"
  version="$2"
  artifact="$3"

  SOURCE_LABEL=""
  SOURCE_ASSET_URL=""
  SOURCE_CHECKSUMS_URL=""

  case "$token" in
    downloads_primary)
      base="$(downloads_base_for_site "$INSTALL_SITE" || true)"
      [ -n "$base" ] || return 1
      SOURCE_LABEL="$base"
      SOURCE_ASSET_URL="$(downloads_asset_url "$base" "$version" "$artifact")"
      SOURCE_CHECKSUMS_URL="$(downloads_checksums_url "$base" "$version")"
      ;;
    downloads_secondary)
      base="$(secondary_downloads_base_for_site "$INSTALL_SITE" || true)"
      [ -n "$base" ] || return 1
      SOURCE_LABEL="$base"
      SOURCE_ASSET_URL="$(downloads_asset_url "$base" "$version" "$artifact")"
      SOURCE_CHECKSUMS_URL="$(downloads_checksums_url "$base" "$version")"
      ;;
    github)
      SOURCE_ASSET_URL="$(github_asset_url "$version" "$artifact" || true)"
      SOURCE_CHECKSUMS_URL="$(github_checksums_url "$version" || true)"
      [ -n "$SOURCE_ASSET_URL" ] || return 1
      [ -n "$SOURCE_CHECKSUMS_URL" ] || return 1
      SOURCE_LABEL="GitHub Releases"
      ;;
    gitee)
      SOURCE_ASSET_URL="$(gitee_asset_url "$version" "$artifact" || true)"
      SOURCE_CHECKSUMS_URL="$(gitee_checksums_url "$version" || true)"
      [ -n "$SOURCE_ASSET_URL" ] || return 1
      [ -n "$SOURCE_CHECKSUMS_URL" ] || return 1
      SOURCE_LABEL="Gitee mirror"
      ;;
    *)
      return 1
      ;;
  esac

  return 0
}

try_source() {
  token="$1"
  version="$2"
  artifact="$3"
  archive_path="$4"
  checksums_path="$5"

  if ! resolve_source_urls "$token" "$version" "$artifact"; then
    return 1
  fi

  append_attempt "$SOURCE_LABEL"
  log "Trying source: $SOURCE_LABEL"

  rm -f "$archive_path" "$checksums_path"

  if ! download_file "$SOURCE_ASSET_URL" "$archive_path"; then
    log "Source failed while downloading artifact: $SOURCE_LABEL"
    return 1
  fi
  if ! download_file "$SOURCE_CHECKSUMS_URL" "$checksums_path"; then
    log "Source failed while downloading checksums: $SOURCE_LABEL"
    return 1
  fi

  return 0
}

main() {
  need_cmd uname
  need_cmd id
  need_cmd tar
  need_cmd awk
  need_cmd find
  need_cmd cp
  need_cmd mv
  need_cmd chmod
  need_cmd mktemp

  INSTALL_SITE="$(normalize_site)"
  OS_NORMALIZED="$(normalize_os)"
  SYSTEMD_SETUP_MODE="$(normalize_systemd_mode)"
  ensure_root_if_systemd_forced

  os="$OS_NORMALIZED"
  arch="$(normalize_arch)"
  version="$(resolve_version)"
  source="$(choose_source)"
  artifact="$(artifact_name "$os" "$arch")"

  log "Installing $PROGRAM"
  log "Selected platform artifact: $artifact"
  log "Install site: $INSTALL_SITE"

  WORKDIR="$(mktemp -d "${TMPDIR_ROOT%/}/warded-install.XXXXXX")"
  archive_path="$WORKDIR/$artifact"
  checksums_path="$WORKDIR/checksums.txt"
  extract_dir="$WORKDIR/extracted"

  source_list="$(ordered_sources "$source" "$INSTALL_SITE")"
  selected_label=""
  for token in $source_list; do
    if try_source "$token" "$version" "$artifact" "$archive_path" "$checksums_path"; then
      selected_label="$SOURCE_LABEL"
      break
    fi
  done

  if [ -z "$selected_label" ]; then
    fail "unable to download $artifact; attempted: ${ATTEMPTED_SOURCES:-none}; manual fallback: $(manual_release_hint)"
  fi

  log "Download source: $selected_label"
  verify_checksum "$artifact" "$checksums_path" "$archive_path"

  extract_archive "$archive_path" "$extract_dir"
  binary_path="$(find_binary "$extract_dir")"
  archive_version="$(verify_install "$binary_path")"

  bin_dir="$(detect_install_dir)"
  target_path="${bin_dir%/}/$PROGRAM"
  current_version="$(read_binary_version "$target_path" || true)"

  if [ -n "$current_version" ] && [ "$current_version" = "$archive_version" ]; then
    installed_path="$target_path"
    installed_version="$current_version"
    log "Version already installed at $installed_path"
  else
    installed_path="$(install_binary "$binary_path" "$bin_dir")"
    installed_version="$(verify_install "$installed_path")"
  fi

  setup_system_service_layout "$installed_path"

  log "Warded installed successfully."
  log "Version: $installed_version"
  log "Path: $installed_path"

  case ":$PATH:" in
    *":$bin_dir:"*) ;;
    *)
      log "Note: $bin_dir is not in PATH."
      ;;
  esac

  log "Next: run \`warded activate\`"

  if systemd_setup_enabled; then
    log "systemd user: $WARDED_SYSTEM_USER"
    log "State directory: $WARDED_STATE_DIR"
    log "Environment file: $WARDED_ENV_FILE"
    log "Unit file: ${WARDED_SYSTEMD_UNIT_DIR%/}/$WARDED_SYSTEMD_UNIT_NAME"
    log "After activation, run:"
    log "  sudo -u $WARDED_SYSTEM_USER $installed_path activate --data-dir $WARDED_STATE_DIR ..."
    log "  systemctl daemon-reload"
    log "  systemctl enable --now $WARDED_SYSTEMD_UNIT_NAME"
  else
    log "Note: systemd service was not set up because this is a non-root install."
    log "  Run the following to activate:"
    log "  $installed_path activate"
    log "  Data will be stored under ~/.config/warded by default."
  fi
}

main "$@"
