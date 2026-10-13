#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="${ROOT_DIR}/scripts/setup-raspberry-pi-ssh-access.sh"
FIXTURE_FINGERPRINT="SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
TEST_ROOT="$(mktemp -d)"
CURRENT_HOME=""
CURRENT_BIN=""

cleanup() {
  rm -rf -- "${TEST_ROOT}"
}
trap cleanup EXIT

fail() {
  printf 'FAIL: %s\n' "$1" >&2
  exit 1
}

assert_contains() {
  local needle="$1" file="$2"
  grep -F -- "$needle" "$file" >/dev/null || fail "expected ${file} to contain: ${needle}"
}

assert_not_contains() {
  local needle="$1" file="$2"
  if grep -F -- "$needle" "$file" >/dev/null; then
    fail "expected ${file} not to contain: ${needle}"
  fi
}

assert_matches() {
  local expression="$1" file="$2"
  grep -E -- "$expression" "$file" >/dev/null || fail "expected ${file} to match: ${expression}"
}

assert_exact_text() {
  local expected="$1" actual="$2" label="$3"
  [[ "$actual" == "$expected" ]] || fail "expected exact ${label} sequence"
}

assert_exact_file_content() {
  local expected="$1" file="$2"
  if ! cmp -s <(printf '%s\n' "$expected") "$file"; then
    fail "expected exact file content in ${file}"
  fi
}

assert_file_absent() {
  [[ ! -e "$1" && ! -L "$1" ]] || fail "expected path to be absent: $1"
}

new_environment() {
  local name="$1"
  CURRENT_HOME="${TEST_ROOT}/${name}/home"
  CURRENT_BIN="${TEST_ROOT}/${name}/bin"
  mkdir -p "${CURRENT_HOME}/.ssh" "${CURRENT_BIN}"

  cat >"${CURRENT_BIN}/ssh-keygen" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
if [[ "$1" == "-lf" ]]; then
  candidate="$2"
  if grep -F -- "${STUB_MATCH_KEY:-QUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFB}" "$candidate" >/dev/null; then
    printf '256 %s fixture (ED25519)\n' "${STUB_FINGERPRINT}"
  else
    printf '256 %s fixture (ED25519)\n' "${STUB_NONMATCH_FINGERPRINT:-SHA256:BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB}"
  fi
  exit 0
fi
if [[ "$1" == "-y" ]]; then
  printf '%s\n' "${STUB_DERIVED_PUBLIC}"
  exit 0
fi
if [[ "$1" == "-t" ]]; then
  while [[ $# -gt 0 ]]; do
    if [[ "$1" == "-f" ]]; then
      key="$2"
      break
    fi
    shift
  done
  printf 'PRIVATE-KEY-SHOULD-NOT-PRINT\n' > "$key"
  printf 'ssh-ed25519 QUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFB fixture\n' > "${key}.pub"
  chmod 600 "$key"
  exit 0
fi
exit 64
STUB
  cat >"${CURRENT_BIN}/ssh-keyscan" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$@" >> "${STUB_SCAN_LOG}"
printf '%s\n' "${STUB_SCAN_RECORD}"
STUB
  cat >"${CURRENT_BIN}/ssh-copy-id" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
if [[ "$1" == "-h" ]]; then
  if [[ "${STUB_COPY_ID_SUPPORTS_CONFIG:-yes}" == yes ]]; then
    printf 'usage: ssh-copy-id [-F ssh_config] [-i identity_file] [-p port] [user@]hostname\n'
  else
    printf 'usage: ssh-copy-id [-i identity_file] [-p port] [user@]hostname\n'
  fi
  exit 0
fi
printf '%s\n' "$@" >> "${STUB_COPY_LOG}"
while [[ $# -gt 0 ]]; do
  if [[ "$1" == "-F" ]]; then
    cp -- "$2" "${STUB_COPY_CONFIG_LOG}"
    break
  fi
  shift
done
STUB
  chmod +x "${CURRENT_BIN}/ssh-keygen" "${CURRENT_BIN}/ssh-keyscan" "${CURRENT_BIN}/ssh-copy-id"
}

run_helper() {
  local input="$1" output="$2"
  printf '%b\n' "$input" | env \
    HOME="${CURRENT_HOME}" \
    PATH="${CURRENT_BIN}:${PATH}" \
    STUB_FINGERPRINT="${STUB_FINGERPRINT:-${FIXTURE_FINGERPRINT}}" \
    STUB_DERIVED_PUBLIC="${STUB_DERIVED_PUBLIC:-ssh-ed25519 QUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFB}" \
    STUB_SCAN_RECORD="${STUB_SCAN_RECORD:-raspberrypi.local ssh-ed25519 QUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFB}" \
    STUB_MATCH_KEY="${STUB_MATCH_KEY:-QUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFB}" \
    STUB_NONMATCH_FINGERPRINT="${STUB_NONMATCH_FINGERPRINT:-SHA256:BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB}" \
    STUB_COPY_ID_SUPPORTS_CONFIG="${STUB_COPY_ID_SUPPORTS_CONFIG:-yes}" \
    STUB_SCAN_LOG="${CURRENT_HOME}/scan.log" \
    STUB_COPY_LOG="${CURRENT_HOME}/copy.log" \
    STUB_COPY_CONFIG_LOG="${CURRENT_HOME}/copy-config" \
    bash "${SCRIPT}" >"$output" 2>&1
}

expect_failure() {
  local input="$1" output="$2"
  if run_helper "$input" "$output"; then
    fail "helper unexpectedly succeeded; output: $(<"$output")"
  fi
}

seed_key() {
  local alias="$1" key
  key="${CURRENT_HOME}/.ssh/amg-rfid-raspberry-${alias}_ed25519"
  printf 'existing private fixture\n' >"$key"
  printf 'ssh-ed25519 QUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFB fixture\n' >"${key}.pub"
  chmod 600 "$key"
}

base_input() {
  local alias="$1" host="$2" user="$3" port="$4" fingerprint="$5" generate="$6" install="$7"
  base_input_with_key "$alias" "$host" "$user" "$port" '' "$fingerprint" "$generate" "$install"
}

base_input_with_key() {
  local alias="$1" host="$2" user="$3" port="$4" key="$5" fingerprint="$6" generate="$7" install="$8"
  printf '%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n' "$alias" "$host" "$user" "$port" "$key" "$fingerprint" "$generate" "$install"
}

assert_effective_ssh_configuration() {
  local config="$1" known_hosts="$2" effective="${TEST_ROOT}/effective-ssh-config.out"

  if ! command -v ssh >/dev/null 2>&1; then
    printf 'SKIP: local ssh client unavailable; effective-config assertions not run\n'
    return 0
  fi

  # -T is nonconnecting and suppresses the harmless pseudo-terminal warning
  # that some SSH clients emit when ssh -G inherits a non-terminal stdin.
  ssh -T -G -F "$config" unrelated-host >"$effective"
  assert_contains 'user workstation' "$effective"

  ssh -T -G -F "$config" pi-gateway >"$effective"
  assert_contains 'hostname raspberrypi.local' "$effective"
  assert_contains 'user pi' "$effective"
  assert_not_contains 'user workstation' "$effective"
  assert_contains 'port 22' "$effective"
  assert_contains "identityfile ${CURRENT_HOME}/.ssh/amg-rfid-raspberry-pi-gateway_ed25519" "$effective"
  assert_contains 'identitiesonly yes' "$effective"
  assert_contains "userknownhostsfile ${known_hosts}" "$effective"
  assert_contains 'globalknownhostsfile /dev/null' "$effective"
  if grep -E '^knownhostscommand ' "$effective" >/dev/null; then
    assert_matches '^knownhostscommand none$' "$effective"
  fi
  assert_matches '^stricthostkeychecking (yes|true)$' "$effective"
  assert_contains 'checkhostip no' "$effective"
  assert_contains 'verifyhostkeydns false' "$effective"
  assert_contains 'updatehostkeys false' "$effective"
  assert_contains 'hostkeyalias [raspberrypi.local]:22' "$effective"
  # OpenSSH omits disabled proxy options from ssh -G output. Any emitted
  # ProxyCommand or ProxyJump value therefore has to be the disabled value.
  if grep -E '^proxy(command|jump) ' "$effective" | grep -Ev '^proxy(command|jump) none$' >/dev/null; then
    fail 'managed alias has an active proxy route in ssh -G output'
  fi
}

test_success_idempotence_and_secrecy() {
  local output="${TEST_ROOT}/success.out" config known_hosts
  new_environment success
  printf 'User workstation\nHost *\n  KnownHostsCommand /usr/bin/alternate-known-hosts\n# unrelated workstation setting\nHost unrelated\n  User existing\n' >"${CURRENT_HOME}/.ssh/config"
  run_helper "$(base_input pi-gateway raspberrypi.local pi 22 "${FIXTURE_FINGERPRINT}" y n)" "$output" || {
    cat "$output" >&2
    fail 'initial successful setup failed'
  }

  config="${CURRENT_HOME}/.ssh/config"
  known_hosts="${CURRENT_HOME}/.ssh/amg-rfid-raspberry-pi-gateway.known_hosts"
  [[ "$(head -n 1 "$config")" == '# BEGIN AMG RFID RASPBERRY SSH pi-gateway' ]] || fail 'managed block was not placed before preserved SSH configuration'
  assert_exact_text "$(
    cat <<EOF
# BEGIN AMG RFID RASPBERRY SSH pi-gateway
Host pi-gateway
  HostName raspberrypi.local
  User pi
  Port 22
  IdentityFile "${CURRENT_HOME}/.ssh/amg-rfid-raspberry-pi-gateway_ed25519"
  IdentitiesOnly yes
  UserKnownHostsFile "${known_hosts}"
  GlobalKnownHostsFile /dev/null
  KnownHostsCommand none
  StrictHostKeyChecking yes
  CheckHostIP no
  VerifyHostKeyDNS no
  UpdateHostKeys no
  HostKeyAlias "[raspberrypi.local]:22"
  ProxyCommand none
  ProxyJump none
  ServerAliveInterval 30
  ServerAliveCountMax 3
# Restore catch-all scope before the preserved configuration: SSH comments and
# marker lines do not terminate a preceding Host block.
Host *
# END AMG RFID RASPBERRY SSH pi-gateway
EOF
  )" "$(awk -v begin='# BEGIN AMG RFID RASPBERRY SSH pi-gateway' -v end='# END AMG RFID RASPBERRY SSH pi-gateway' '
  $0 == begin { managed = 1 }
  managed { print }
  $0 == end { exit }
' "$config")" 'helper-owned managed block'
  assert_contains 'Host *' "$config"
  assert_contains '# unrelated workstation setting' "$config"
  assert_contains 'Host pi-gateway' "$config"
  assert_contains 'HostName raspberrypi.local' "$config"
  assert_contains 'IdentitiesOnly yes' "$config"
  assert_contains "UserKnownHostsFile \"${known_hosts}\"" "$config"
  assert_contains 'GlobalKnownHostsFile /dev/null' "$config"
  assert_contains 'KnownHostsCommand none' "$config"
  assert_contains 'StrictHostKeyChecking yes' "$config"
  assert_contains 'CheckHostIP no' "$config"
  assert_contains 'VerifyHostKeyDNS no' "$config"
  assert_contains 'UpdateHostKeys no' "$config"
  assert_contains 'HostKeyAlias "[raspberrypi.local]:22"' "$config"
  assert_contains 'ProxyCommand none' "$config"
  assert_contains 'ProxyJump none' "$config"
  assert_contains 'ServerAliveInterval 30' "$config"
  assert_contains 'ServerAliveCountMax 3' "$config"
  assert_not_contains 'StrictHostKeyChecking no' "$config"
  assert_not_contains 'accept-new' "$config"
  assert_contains 'ssh-ed25519' "$known_hosts"
  assert_not_contains 'PRIVATE-KEY-SHOULD-NOT-PRINT' "$output"
  assert_file_absent "${CURRENT_HOME}/copy.log"
  assert_exact_file_content $'-T\n5\n-p\n22\nraspberrypi.local' "${CURRENT_HOME}/scan.log"
  assert_effective_ssh_configuration "$config" "$known_hosts"

  run_helper "$(base_input pi-gateway raspberrypi.local pi 22 "${FIXTURE_FINGERPRINT}" n n)" "${TEST_ROOT}/idempotence.out" || {
    cat "${TEST_ROOT}/idempotence.out" >&2
    fail 'idempotent setup failed'
  }
  [[ "$(grep -c '^# BEGIN AMG RFID RASPBERRY SSH pi-gateway$' "$config")" == '1' ]] || fail 'managed block was not replaced idempotently'
  [[ "$(grep -c '^# END AMG RFID RASPBERRY SSH pi-gateway$' "$config")" == '1' ]] || fail 'managed block end marker was not replaced idempotently'
  [[ "$(awk -v begin='# BEGIN AMG RFID RASPBERRY SSH pi-gateway' -v end='# END AMG RFID RASPBERRY SSH pi-gateway' '
    $0 == begin { managed = 1; next }
    $0 == end { managed = 0; next }
    managed && $0 == "Host *" { count++ }
    END { print count + 0 }
  ' "$config")" == '1' ]] || fail 'second run accumulated helper-owned scope reset boundaries'
  assert_contains 'Host unrelated' "$config"
}

test_unsafe_boundaries() {
  local output="${TEST_ROOT}/invalid.out"

  new_environment invalid-port
  expect_failure "$(base_input pi-gateway raspberrypi.local pi 999999 "${FIXTURE_FINGERPRINT}" n n)" "$output"
  assert_contains 'SSH port must be an integer' "$output"

  new_environment invalid-ipv4
  expect_failure "$(base_input pi-gateway 999.999.999.999 pi 22 "${FIXTURE_FINGERPRINT}" n n)" "$output"
  assert_contains 'Raspberry Pi host must be a valid' "$output"

  new_environment invalid-ipv6
  expect_failure "$(base_input pi-gateway '2001:db8:::1' pi 22 "${FIXTURE_FINGERPRINT}" n n)" "$output"
  assert_contains 'Raspberry Pi host must be a valid' "$output"

  new_environment invalid-user
  expect_failure "$(base_input pi-gateway raspberrypi.local 'pi user' 22 "${FIXTURE_FINGERPRINT}" n n)" "$output"
  assert_contains 'SSH user must be a Linux account name' "$output"
}

test_config_and_marker_refusals() {
  local output="${TEST_ROOT}/refusal.out" config

  new_environment config-symlink
  ln -s /dev/null "${CURRENT_HOME}/.ssh/config"
  seed_key pi-gateway
  expect_failure "$(base_input pi-gateway raspberrypi.local pi 22 "${FIXTURE_FINGERPRINT}" n n)" "$output"
  assert_contains 'SSH config is a symlink' "$output"

  new_environment malformed-marker
  config="${CURRENT_HOME}/.ssh/config"
  printf '# BEGIN AMG RFID RASPBERRY SSH pi-gateway\nHost pi-gateway\n' >"$config"
  seed_key pi-gateway
  expect_failure "$(base_input pi-gateway raspberrypi.local pi 22 "${FIXTURE_FINGERPRINT}" n n)" "$output"
  assert_contains 'malformed, duplicate, or nested' "$output"

  new_environment nested-marker
  config="${CURRENT_HOME}/.ssh/config"
  printf '# BEGIN AMG RFID RASPBERRY SSH one\n# BEGIN AMG RFID RASPBERRY SSH two\n# END AMG RFID RASPBERRY SSH two\n# END AMG RFID RASPBERRY SSH one\n' >"$config"
  seed_key pi-gateway
  expect_failure "$(base_input pi-gateway raspberrypi.local pi 22 "${FIXTURE_FINGERPRINT}" n n)" "$output"
  assert_contains 'malformed, duplicate, or nested' "$output"

  new_environment duplicate-marker
  config="${CURRENT_HOME}/.ssh/config"
  printf '# BEGIN AMG RFID RASPBERRY SSH one\n# END AMG RFID RASPBERRY SSH one\n# BEGIN AMG RFID RASPBERRY SSH one\n# END AMG RFID RASPBERRY SSH one\n' >"$config"
  seed_key pi-gateway
  expect_failure "$(base_input pi-gateway raspberrypi.local pi 22 "${FIXTURE_FINGERPRINT}" n n)" "$output"
  assert_contains 'malformed, duplicate, or nested' "$output"

  new_environment unmanaged-alias
  config="${CURRENT_HOME}/.ssh/config"
  printf 'Host pi-gateway\n  HostName unrelated.example\n' >"$config"
  seed_key pi-gateway
  expect_failure "$(base_input pi-gateway raspberrypi.local pi 22 "${FIXTURE_FINGERPRINT}" n n)" "$output"
  assert_contains 'unmanaged Host' "$output"
}

test_verified_install_and_mismatch() {
  local output="${TEST_ROOT}/verified.out"

  new_environment verified-install
  run_helper "$(base_input pi-gateway raspberrypi.local pi 22 "${FIXTURE_FINGERPRINT}" y y)" "$output" || {
    cat "$output" >&2
    fail 'verified key installation setup failed'
  }
  assert_contains '-F' "${CURRENT_HOME}/copy.log"
  assert_contains '-i' "${CURRENT_HOME}/copy.log"
  assert_contains "${CURRENT_HOME}/.ssh/amg-rfid-raspberry-pi-gateway_ed25519.pub" "${CURRENT_HOME}/copy.log"
  assert_contains '-p' "${CURRENT_HOME}/copy.log"
  assert_contains '22' "${CURRENT_HOME}/copy.log"
  assert_contains 'pi@raspberrypi.local' "${CURRENT_HOME}/copy.log"
  assert_exact_file_content $'-T\n5\n-p\n22\nraspberrypi.local' "${CURRENT_HOME}/scan.log"
  assert_contains 'Host raspberrypi.local' "${CURRENT_HOME}/copy-config"
  assert_contains 'HostName raspberrypi.local' "${CURRENT_HOME}/copy-config"
  assert_contains 'User pi' "${CURRENT_HOME}/copy-config"
  assert_contains 'Port 22' "${CURRENT_HOME}/copy-config"
  assert_contains "IdentityFile \"${CURRENT_HOME}/.ssh/amg-rfid-raspberry-pi-gateway_ed25519\"" "${CURRENT_HOME}/copy-config"
  assert_contains "UserKnownHostsFile \"${CURRENT_HOME}/.ssh/amg-rfid-raspberry-pi-gateway.known_hosts\"" "${CURRENT_HOME}/copy-config"
  assert_contains 'GlobalKnownHostsFile /dev/null' "${CURRENT_HOME}/copy-config"
  assert_contains 'KnownHostsCommand none' "${CURRENT_HOME}/copy-config"
  assert_contains 'StrictHostKeyChecking yes' "${CURRENT_HOME}/copy-config"
  assert_contains 'CheckHostIP no' "${CURRENT_HOME}/copy-config"
  assert_contains 'VerifyHostKeyDNS no' "${CURRENT_HOME}/copy-config"
  assert_contains 'UpdateHostKeys no' "${CURRENT_HOME}/copy-config"
  assert_contains 'HostKeyAlias "[raspberrypi.local]:22"' "${CURRENT_HOME}/copy-config"
  assert_contains 'ProxyCommand none' "${CURRENT_HOME}/copy-config"
  assert_contains 'ProxyJump none' "${CURRENT_HOME}/copy-config"

  new_environment fingerprint-mismatch
  STUB_FINGERPRINT='SHA256:BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB'
  expect_failure "$(base_input pi-gateway raspberrypi.local pi 22 "${FIXTURE_FINGERPRINT}" y y)" "$output"
  assert_contains 'do not match the independently verified fingerprint' "$output"
  assert_file_absent "${CURRENT_HOME}/copy.log"
  assert_file_absent "${CURRENT_HOME}/.ssh/config"
  unset STUB_FINGERPRINT
}

test_scan_target_and_record_pinning() {
  local output="${TEST_ROOT}/scan-target.out" known_hosts

  new_environment ipv4-nonstandard-port
  run_helper "$(base_input pi-ipv4 192.0.2.44 pi 2202 "${FIXTURE_FINGERPRINT}" y n)" "$output" || {
    cat "$output" >&2
    fail 'IPv4 nonstandard-port setup failed'
  }
  known_hosts="${CURRENT_HOME}/.ssh/amg-rfid-raspberry-pi-ipv4.known_hosts"
  assert_exact_file_content $'-T\n5\n-p\n2202\n192.0.2.44' "${CURRENT_HOME}/scan.log"
  assert_contains '[192.0.2.44]:2202 ssh-ed25519' "$known_hosts"

  new_environment ipv6-nonstandard-port
  run_helper "$(base_input pi-ipv6 2001:db8::44 pi 2202 "${FIXTURE_FINGERPRINT}" y n)" "$output" || {
    cat "$output" >&2
    fail 'IPv6 nonstandard-port setup failed'
  }
  known_hosts="${CURRENT_HOME}/.ssh/amg-rfid-raspberry-pi-ipv6.known_hosts"
  assert_exact_file_content $'-T\n5\n-p\n2202\n2001:db8::44' "${CURRENT_HOME}/scan.log"
  assert_contains '[2001:db8::44]:2202 ssh-ed25519' "$known_hosts"

  new_environment multi-algorithm-filter
  STUB_SCAN_RECORD=$'raspberrypi.local ssh-ed25519 QUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFB\nraspberrypi.local ssh-rsa QkJCQkJCQkJCQkJCQkJCQkJCQkJCQkJCQkJCQkJCQkJCQkJCQkJCQkJCQkJCQkJCQkJC'
  STUB_MATCH_KEY='QUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFB'
  run_helper "$(base_input pi-multi raspberrypi.local pi 22 "${FIXTURE_FINGERPRINT}" y n)" "$output" || {
    cat "$output" >&2
    fail 'multi-algorithm setup failed'
  }
  known_hosts="${CURRENT_HOME}/.ssh/amg-rfid-raspberry-pi-multi.known_hosts"
  assert_contains '[raspberrypi.local]:22 ssh-ed25519' "$known_hosts"
  assert_not_contains 'ssh-rsa' "$known_hosts"
  unset STUB_SCAN_RECORD STUB_MATCH_KEY
}

test_ssh_copy_id_requires_config_file_support() {
  local output="${TEST_ROOT}/copy-id-no-config-support.out"

  new_environment copy-id-no-config-support
  STUB_COPY_ID_SUPPORTS_CONFIG=no
  expect_failure "$(base_input pi-gateway raspberrypi.local pi 22 "${FIXTURE_FINGERPRINT}" y y)" "$output"
  assert_contains 'does not support isolated configuration (-F)' "$output"
  assert_file_absent "${CURRENT_HOME}/copy.log"
  unset STUB_COPY_ID_SUPPORTS_CONFIG
}

test_key_safety_refusals() {
  local output="${TEST_ROOT}/key-safety.out" key

  new_environment orphan-public-key
  key="${CURRENT_HOME}/.ssh/amg-rfid-raspberry-pi-gateway_ed25519"
  printf 'orphan public key\n' >"${key}.pub"
  expect_failure "$(base_input pi-gateway raspberrypi.local pi 22 "${FIXTURE_FINGERPRINT}" y n)" "$output"
  assert_contains 'public-key path already exists' "$output"
  assert_contains 'orphan public key' "${key}.pub"

  new_environment symlink-public-key
  key="${CURRENT_HOME}/.ssh/amg-rfid-raspberry-pi-gateway_ed25519"
  ln -s /dev/null "${key}.pub"
  expect_failure "$(base_input pi-gateway raspberrypi.local pi 22 "${FIXTURE_FINGERPRINT}" y n)" "$output"
  assert_contains 'public-key path already exists' "$output"

  new_environment mismatched-keypair
  seed_key pi-gateway
  STUB_DERIVED_PUBLIC='ssh-ed25519 QkJCQkJCQkJCQkJCQkJCQkJCQkJCQkJCQkJCQkJCQkJCQkJCQkJCQkJCQkJCQkJC fixture'
  expect_failure "$(base_input pi-gateway raspberrypi.local pi 22 "${FIXTURE_FINGERPRINT}" n y)" "$output"
  assert_contains 'do not correspond' "$output"
  assert_file_absent "${CURRENT_HOME}/copy.log"
  assert_file_absent "${CURRENT_HOME}/.ssh/config"
  unset STUB_DERIVED_PUBLIC
}

test_private_key_path_expansion_refusals() {
  local output="${TEST_ROOT}/path-expansion.out"

  new_environment percent-token
  expect_failure "$(base_input_with_key pi-gateway raspberrypi.local pi 22 '%h/key' "${FIXTURE_FINGERPRINT}" n n)" "$output"
  assert_contains 'must not contain SSH percent tokens or environment-expansion syntax' "$output"

  new_environment environment-expansion
  expect_failure "$(base_input_with_key pi-gateway raspberrypi.local pi 22 '${HOME}/key' "${FIXTURE_FINGERPRINT}" n n)" "$output"
  assert_contains 'must not contain SSH percent tokens or environment-expansion syntax' "$output"
}

test_success_idempotence_and_secrecy
test_unsafe_boundaries
test_config_and_marker_refusals
test_verified_install_and_mismatch
test_scan_target_and_record_pinning
test_ssh_copy_id_requires_config_file_support
test_key_safety_refusals
test_private_key_path_expansion_refusals
printf 'PASS: setup-raspberry-pi-ssh-access hermetic tests\n'
