#!/usr/bin/env bash
# Configure a pinned SSH alias for an AMG RFID Raspberry Pi.
set -euo pipefail

SSH_DIR="${HOME}/.ssh"
CONFIG_FILE="${SSH_DIR}/config"
TMP_CONFIG=""
TMP_KNOWN_HOSTS=""
TMP_COPY_CONFIG=""
SCAN_FILE=""
FILTERED_SCAN_FILE=""

BEGIN_PREFIX="# BEGIN AMG RFID RASPBERRY SSH "
END_PREFIX="# END AMG RFID RASPBERRY SSH "

cleanup() {
  local file
  for file in "${TMP_CONFIG}" "${TMP_KNOWN_HOSTS}" "${TMP_COPY_CONFIG}" "${SCAN_FILE}" "${FILTERED_SCAN_FILE}"; do
    if [[ -n "${file}" && -e "${file}" ]]; then
      rm -f -- "${file}"
    fi
  done
}
trap cleanup EXIT

fail() {
  printf 'ERROR: %s\n' "$1" >&2
  exit 1
}

contains_control_chars() {
  [[ "$1" =~ [[:cntrl:]] ]]
}

validate_alias() {
  [[ "$1" =~ ^[A-Za-z0-9._-]{1,63}$ && "$1" != -* ]]
}

validate_user() {
  [[ "$1" =~ ^[A-Za-z_][A-Za-z0-9_-]{0,31}$ ]]
}

validate_ipv4() {
  local host="$1" octet
  local -a octets

  [[ "$host" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]] || return 1
  IFS='.' read -r -a octets <<<"${host}"
  for octet in "${octets[@]}"; do
    [[ ${#octet} -le 3 && "$octet" =~ ^[0-9]+$ ]] || return 1
    ((10#${octet} <= 255)) || return 1
  done
}

validate_ipv6() {
  # Accept canonical hexadecimal IPv6 groups; reject zones, brackets, and
  # IPv4-embedded forms rather than trying to reinterpret them.
  local host="$1" left right group
  local -a groups

  [[ "$host" == *:* && "$host" =~ ^[0-9A-Fa-f:]+$ ]] || return 1
  [[ "${host}" != *:::* ]] || return 1

  if [[ "$host" == *::* ]]; then
    left="${host%%::*}"
    right="${host#*::}"
    [[ "${right}" != *::* ]] || return 1
    groups=()
    if [[ -n "$left" ]]; then
      IFS=':' read -r -a groups <<<"${left}"
    fi
    if [[ -n "$right" ]]; then
      local -a right_groups
      IFS=':' read -r -a right_groups <<<"${right}"
      groups+=("${right_groups[@]}")
    fi
    ((${#groups[@]} <= 7)) || return 1
  else
    IFS=':' read -r -a groups <<<"${host}"
    ((${#groups[@]} == 8)) || return 1
  fi

  for group in "${groups[@]}"; do
    [[ "$group" =~ ^[0-9A-Fa-f]{1,4}$ ]] || return 1
  done
}

validate_hostname() {
  local host="$1" label
  local -a labels

  [[ ${#host} -le 253 && "$host" != .* && "$host" != *..* ]] || return 1
  host="${host%.}"
  [[ -n "$host" && "$host" != *[!0-9.]* ]] && return 1
  IFS='.' read -r -a labels <<<"${host}"
  for label in "${labels[@]}"; do
    [[ ${#label} -ge 1 && ${#label} -le 63 ]] || return 1
    [[ "$label" =~ ^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?$ ]] || return 1
  done
}

validate_host() {
  local host="$1"
  [[ -n "$host" ]] && ! contains_control_chars "$host" && [[ "$host" != *[[:space:]]* ]] || return 1
  validate_ipv4 "$host" || validate_ipv6 "$host" || validate_hostname "$host"
}

validate_port() {
  local port="$1"
  [[ "$port" =~ ^[0-9]{1,5}$ ]] || return 1
  ((10#${port} >= 1 && 10#${port} <= 65535))
}

expand_key_path() {
  local path="$1"
  # shellcheck disable=SC2088 # literal tilde is matched on purpose, then expanded
  if [[ "$path" == "~" ]]; then
    path="${HOME}"
  elif [[ "${path:0:2}" == '~/' ]]; then
    path="${HOME}/${path#~/}"
  elif [[ "$path" != /* ]]; then
    path="${PWD}/${path}"
  fi
  printf '%s' "$path"
}

mode_of() {
  stat -c '%a' -- "$1" 2>/dev/null || stat -f '%Lp' -- "$1"
}

assert_regular_file() {
  local path="$1" label="$2"
  [[ ! -L "$path" ]] || fail "${label} is a symlink (${path}); refusing to use it."
  [[ -f "$path" ]] || fail "${label} must be a regular file (${path})."
}

assert_safe_regular_file() {
  local path="$1" label="$2" mode
  assert_regular_file "$path" "$label"
  mode="$(mode_of "$path")" || fail "Cannot determine permissions for ${label} (${path})."
  [[ "$mode" =~ ^[0-7]{3,4}$ ]] || fail "Cannot validate permissions for ${label} (${path})."
  (((8#${mode} & 0077) == 0)) || fail "${label} must not be accessible by group or others (${path})."
}

public_key_material() {
  awk '
    NF >= 2 &&
    $1 ~ /^(ssh-ed25519|ssh-rsa|ecdsa-sha2-nistp(256|384|521))$/ &&
    match($2, "^[A-Za-z0-9+/]+={0,2}$") {
      print $1 " " $2
      found = 1
      exit
    }
    END { exit found ? 0 : 1 }
  '
}

validate_key_pair() {
  local private_key="$1" public_key="${1}.pub" expected derived

  assert_regular_file "$public_key" "Ed25519 public key"
  expected="$(public_key_material <"$public_key")" || fail "Ed25519 public key is malformed (${public_key})."
  derived="$(ssh-keygen -y -f "$private_key" 2>/dev/null | public_key_material)" || fail "Cannot derive a public key from existing private key (${private_key})."
  [[ "$derived" == "$expected" ]] || fail "Existing private and public keys do not correspond; refusing to use them."
}

ssh_copy_id_supports_config_file() {
  ssh-copy-id -h 2>&1 | grep -F -- '-F' >/dev/null
}

ssh_config_quote() {
  local value="$1"
  value="${value//\\/\\\\}"
  value="${value//\"/\\\"}"
  printf '"%s"' "$value"
}

validate_managed_blocks() {
  local config="$1"
  awk -v begin_prefix="${BEGIN_PREFIX}" -v end_prefix="${END_PREFIX}" '
    function valid_alias(value) { return value ~ /^[A-Za-z0-9._-]{1,63}$/ && value !~ /^-/ }
    index($0, begin_prefix) == 1 {
      alias = substr($0, length(begin_prefix) + 1)
      if (!valid_alias(alias) || active != "" || ++begins[alias] > 1) invalid = 1
      active = alias
      next
    }
    index($0, end_prefix) == 1 {
      alias = substr($0, length(end_prefix) + 1)
      if (!valid_alias(alias) || active == "" || active != alias || ++ends[alias] > 1) invalid = 1
      active = ""
      next
    }
    END {
      if (active != "") invalid = 1
      for (alias in begins) if (begins[alias] != 1 || ends[alias] != 1) invalid = 1
      for (alias in ends) if (begins[alias] != 1 || ends[alias] != 1) invalid = 1
      exit invalid ? 1 : 0
    }
  ' "$config" || fail "Existing SSH config has malformed, duplicate, or nested AMG RFID Raspberry SSH markers; refusing to modify it."
}

host_alias_exists_outside_managed_block() {
  local alias="$1" config="$2"
  awk -v target="$alias" -v begin_prefix="${BEGIN_PREFIX}" -v end_prefix="${END_PREFIX}" '
    BEGIN { target = tolower(target) }
    index($0, begin_prefix) == 1 { in_managed = 1; next }
    index($0, end_prefix) == 1 { in_managed = 0; next }
    in_managed { next }
    /^[[:space:]]*[Hh][Oo][Ss][Tt][[:space:]]+/ {
      line = $0
      sub(/^[[:space:]]*[Hh][Oo][Ss][Tt][[:space:]]+/, "", line)
      count = split(line, patterns, /[[:space:]]+/)
      for (i = 1; i <= count; i++) if (tolower(patterns[i]) == target) found = 1
    }
    END { exit found ? 0 : 1 }
  ' "$config"
}

filter_verified_scan_records() {
  local scan_file="$1" expected="$2" output="$3" known_host_alias="$4" record fingerprint
  local candidate

  : >"$output"
  while IFS= read -r record; do
    candidate="$(mktemp "${SSH_DIR}/.raspberry-scan-record.XXXXXX")"
    printf '%s\n' "$record" >"$candidate"
    fingerprint="$(ssh-keygen -lf "$candidate" 2>/dev/null | awk 'NR == 1 { print $2 }')" || true
    rm -f -- "$candidate"
    if [[ "$fingerprint" == "$expected" ]]; then
      printf '%s\n' "${known_host_alias} ${record#* }" >>"$output"
    fi
  done < <(awk '
    ($2 == "ssh-ed25519" || $2 == "ssh-rsa" || $2 ~ /^ecdsa-sha2-nistp(256|384|521)$/) &&
    $3 ~ /^[A-Za-z0-9+\/]+={0,2}$/ { print $1 " " $2 " " $3 }
  ' "$scan_file")

  [[ -s "$output" ]] || fail "Scanned host keys do not match the independently verified fingerprint; no SSH or key-copy action was attempted."
}

read -r -p "SSH alias [amg-rfid-pi]: " SSH_ALIAS
SSH_ALIAS="${SSH_ALIAS:-amg-rfid-pi}"
validate_alias "$SSH_ALIAS" || fail "SSH alias must be a single token of up to 63 letters, numbers, dots, dashes, or underscores and must not start with '-'."

read -r -p "Raspberry Pi hostname or IP: " RASPBERRY_HOST
validate_host "$RASPBERRY_HOST" || fail "Raspberry Pi host must be a valid DNS/mDNS name, IPv4 address, or raw IPv6 literal."

read -r -p "SSH user [pi]: " RASPBERRY_USER
RASPBERRY_USER="${RASPBERRY_USER:-pi}"
validate_user "$RASPBERRY_USER" || fail "SSH user must be a Linux account name (letter/underscore followed by up to 31 letters, numbers, underscores, or dashes)."

read -r -p "SSH port [22]: " RASPBERRY_PORT
RASPBERRY_PORT="${RASPBERRY_PORT:-22}"
validate_port "$RASPBERRY_PORT" || fail "SSH port must be an integer from 1 to 65535."
RASPBERRY_PORT="$((10#${RASPBERRY_PORT}))"
HOST_KEY_ALIAS="[${RASPBERRY_HOST}]:${RASPBERRY_PORT}"

DEFAULT_KEY="${SSH_DIR}/amg-rfid-raspberry-${SSH_ALIAS}_ed25519"
read -r -p "Dedicated Ed25519 private key path [${DEFAULT_KEY}]: " SSH_KEY
SSH_KEY="${SSH_KEY:-${DEFAULT_KEY}}"
SSH_KEY="$(expand_key_path "$SSH_KEY")"
if [[ -z "$SSH_KEY" ]] || contains_control_chars "$SSH_KEY"; then
  fail "Private key path must not contain control characters."
fi
[[ "$SSH_KEY" != *'%'* && "$SSH_KEY" != *'$'* ]] || fail "Private key path must not contain SSH percent tokens or environment-expansion syntax."

read -r -p "Independently verified SHA256 host-key fingerprint: " EXPECTED_FINGERPRINT
[[ "$EXPECTED_FINGERPRINT" =~ ^SHA256:[A-Za-z0-9+/]{43}$ ]] || fail "Host-key fingerprint must be an SSH SHA256 fingerprint in canonical SHA256:<base64> form."

[[ ! -L "$SSH_DIR" ]] || fail "SSH directory is a symlink (${SSH_DIR}); refusing to modify it."
mkdir -p -- "$SSH_DIR"
chmod 700 -- "$SSH_DIR"

if [[ -e "$SSH_KEY" || -L "$SSH_KEY" ]]; then
  assert_safe_regular_file "$SSH_KEY" "Existing private key"
  key_description="$(ssh-keygen -lf "$SSH_KEY" 2>/dev/null)" || fail "Existing private key cannot be read by ssh-keygen."
  [[ "$key_description" == *"(ED25519)"* ]] || fail "Existing private key is not an Ed25519 identity; refusing to change or reuse it."
else
  [[ ! -e "${SSH_KEY}.pub" && ! -L "${SSH_KEY}.pub" ]] || fail "Refusing to generate a private key because public-key path already exists (${SSH_KEY}.pub)."
  read -r -p "Key does not exist. Generate a new dedicated Ed25519 key? [y/N]: " GENERATE_KEY
  [[ "$GENERATE_KEY" =~ ^[Yy]$ ]] || fail "Cannot configure SSH without a dedicated existing or explicitly generated Ed25519 key."
  ssh-keygen -t ed25519 -a 100 -f "$SSH_KEY" -C "amg-rfid-raspberry-${SSH_ALIAS}"
  assert_safe_regular_file "$SSH_KEY" "Generated private key"
fi
validate_key_pair "$SSH_KEY"

[[ ! -L "$CONFIG_FILE" ]] || fail "SSH config is a symlink (${CONFIG_FILE}); refusing to modify it."
if [[ -e "$CONFIG_FILE" ]]; then
  [[ -f "$CONFIG_FILE" ]] || fail "SSH config must be a regular file (${CONFIG_FILE})."
  validate_managed_blocks "$CONFIG_FILE"
  host_alias_exists_outside_managed_block "$SSH_ALIAS" "$CONFIG_FILE" && fail "SSH config already contains an unmanaged Host '${SSH_ALIAS}' block; refusing to create a conflicting alias."
fi

KNOWN_HOSTS_FILE="${SSH_DIR}/amg-rfid-raspberry-${SSH_ALIAS}.known_hosts"
[[ ! -L "$KNOWN_HOSTS_FILE" ]] || fail "Helper-owned known-hosts file is a symlink (${KNOWN_HOSTS_FILE}); refusing to modify it."
if [[ -e "$KNOWN_HOSTS_FILE" ]]; then
  [[ -f "$KNOWN_HOSTS_FILE" ]] || fail "Helper-owned known-hosts path must be a regular file (${KNOWN_HOSTS_FILE})."
fi

SCAN_FILE="$(mktemp "${SSH_DIR}/.raspberry-keyscan.XXXXXX")"
FILTERED_SCAN_FILE="$(mktemp "${SSH_DIR}/.raspberry-keyscan-filtered.XXXXXX")"
ssh-keyscan -T 5 -p "$RASPBERRY_PORT" "$RASPBERRY_HOST" >"$SCAN_FILE" 2>/dev/null || fail "Could not scan a host key; verify the host, port, and network, then retry."
filter_verified_scan_records "$SCAN_FILE" "$EXPECTED_FINGERPRINT" "$FILTERED_SCAN_FILE" "$HOST_KEY_ALIAS"

TMP_KNOWN_HOSTS="$(mktemp "${SSH_DIR}/.raspberry-known-hosts.XXXXXX")"
chmod 600 -- "$TMP_KNOWN_HOSTS"
cat "$FILTERED_SCAN_FILE" >"$TMP_KNOWN_HOSTS"
mv -- "$TMP_KNOWN_HOSTS" "$KNOWN_HOSTS_FILE"
TMP_KNOWN_HOSTS=""

TMP_CONFIG="$(mktemp "${SSH_DIR}/.config.XXXXXX")"
chmod 600 -- "$TMP_CONFIG"
QUOTED_SSH_KEY="$(ssh_config_quote "$SSH_KEY")"
QUOTED_KNOWN_HOSTS="$(ssh_config_quote "$KNOWN_HOSTS_FILE")"
QUOTED_HOST_KEY_ALIAS="$(ssh_config_quote "$HOST_KEY_ALIAS")"
cat >"$TMP_CONFIG" <<EOF
${BEGIN_PREFIX}${SSH_ALIAS}
Host ${SSH_ALIAS}
  HostName ${RASPBERRY_HOST}
  User ${RASPBERRY_USER}
  Port ${RASPBERRY_PORT}
  IdentityFile ${QUOTED_SSH_KEY}
  IdentitiesOnly yes
  UserKnownHostsFile ${QUOTED_KNOWN_HOSTS}
  GlobalKnownHostsFile /dev/null
  KnownHostsCommand none
  StrictHostKeyChecking yes
  CheckHostIP no
  VerifyHostKeyDNS no
  UpdateHostKeys no
  HostKeyAlias ${QUOTED_HOST_KEY_ALIAS}
  ProxyCommand none
  ProxyJump none
  ServerAliveInterval 30
  ServerAliveCountMax 3
# Restore catch-all scope before the preserved configuration: SSH comments and
# marker lines do not terminate a preceding Host block.
Host *
${END_PREFIX}${SSH_ALIAS}
EOF
if [[ -e "$CONFIG_FILE" ]]; then
  awk -v begin="${BEGIN_PREFIX}${SSH_ALIAS}" -v end="${END_PREFIX}${SSH_ALIAS}" '
    $0 == begin { skip = 1; next }
    $0 == end { skip = 0; next }
    !skip { print }
  ' "$CONFIG_FILE" >>"$TMP_CONFIG"
fi
mv -- "$TMP_CONFIG" "$CONFIG_FILE"
TMP_CONFIG=""
chmod 600 -- "$CONFIG_FILE"

printf '\nSSH alias %s is configured with a pinned host key.\n' "$SSH_ALIAS"
printf 'Connect later with: ssh %s <command>\n' "$SSH_ALIAS"
read -r -p "Install this public key on the verified Raspberry Pi now? [y/N]: " INSTALL_KEY
if [[ "$INSTALL_KEY" =~ ^[Yy]$ ]]; then
  ssh_copy_id_supports_config_file || fail "Local ssh-copy-id does not support isolated configuration (-F); install ${SSH_KEY}.pub manually with a trusted SSH client."

  TMP_COPY_CONFIG="$(mktemp "${SSH_DIR}/.ssh-copy-id-config.XXXXXX")"
  chmod 600 -- "$TMP_COPY_CONFIG"
  cat >"$TMP_COPY_CONFIG" <<EOF
Host ${RASPBERRY_HOST}
  HostName ${RASPBERRY_HOST}
  User ${RASPBERRY_USER}
  Port ${RASPBERRY_PORT}
  IdentityFile ${QUOTED_SSH_KEY}
  IdentitiesOnly yes
  UserKnownHostsFile ${QUOTED_KNOWN_HOSTS}
  GlobalKnownHostsFile /dev/null
  KnownHostsCommand none
  StrictHostKeyChecking yes
  CheckHostIP no
  VerifyHostKeyDNS no
  UpdateHostKeys no
  HostKeyAlias ${QUOTED_HOST_KEY_ALIAS}
  ProxyCommand none
  ProxyJump none
EOF
  ssh-copy-id -F "$TMP_COPY_CONFIG" -i "${SSH_KEY}.pub" -p "$RASPBERRY_PORT" "${RASPBERRY_USER}@${RASPBERRY_HOST}"
  printf 'Public key installation completed.\n'
else
  printf 'Public-key installation skipped.\n'
fi
