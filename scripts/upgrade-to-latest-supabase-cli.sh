#!/usr/bin/env bash
# Upgrade the pinned Supabase CLI to the latest release: discover the newest
# version tag on github.com/supabase/cli, fetch that release's checksums.txt
# and rewrite SUPABASE_CLI_VERSION plus the four per-platform cli_sha256
# values in the pin file in lockstep, each checksum looked up by its archive
# name so no checksums.txt line ordering is assumed. Idempotent: when the pin
# already matches the latest release, nothing is modified and the script says
# so. Probes live origins (GitHub tags and release assets), so it needs
# network access. Requires git and curl. Run from the repository root, then
# run ./scripts/integration-test.sh to prove the new pin fetches, verifies
# and starts the stack.
set -euo pipefail

source "$(dirname "$0")/common.sh"

pin_file="$(supabase_cli_pin_file)"

echo "Upgrade to Latest Supabase CLI..."

pinned_version="$(pinned_supabase_cli_version)"
if [ -z "${pinned_version}" ]; then
  echo "${pin_file} carries no SUPABASE_CLI_VERSION line; align this script with it by hand." >&2
  exit 1
fi

latest_version="$(latest_supabase_cli_version)"
if [ -z "${latest_version}" ]; then
  echo "No release versions found among the version tags on github.com/supabase/cli." >&2
  exit 1
fi

if [ "${pinned_version}" = "${latest_version}" ]; then
  echo "✅ Nothing to do: the pinned Supabase CLI ${pinned_version} is already the latest release."
  exit 0
fi

echo "==> upgrading the pin from ${pinned_version} to ${latest_version}"

checksums_url="https://github.com/supabase/cli/releases/download/v${latest_version}/checksums.txt"
echo "==> fetching ${checksums_url}"
checksums="$(curl -fsSL "${checksums_url}")"

# Print the release's SHA-256 for one platform's archive, looked up by name
# in the fetched checksums.txt ('<sha256>  <archive>' lines). Fails unless
# exactly one well-formed checksum matches. $1 is the <os>_<arch> platform.
checksum_for() {
  local archive="supabase_${latest_version}_$1.tar.gz"
  local sha256
  sha256="$(printf '%s\n' "${checksums}" | awk -v archive="${archive}" '$2 == archive { print $1 }')"
  if ! [[ "${sha256}" =~ ^[0-9a-f]{64}$ ]]; then
    echo "checksums.txt for v${latest_version} does not carry exactly one SHA-256 for ${archive}." >&2
    return 1
  fi
  printf '%s\n' "${sha256}"
}

darwin_amd64_sha256="$(checksum_for darwin_amd64)"
darwin_arm64_sha256="$(checksum_for darwin_arm64)"
linux_amd64_sha256="$(checksum_for linux_amd64)"
linux_arm64_sha256="$(checksum_for linux_arm64)"

# BSD and GNU sed disagree on -i, so rewrite via a temporary file.
sed -E \
  -e "s|^SUPABASE_CLI_VERSION=\"[0-9.]+\"$|SUPABASE_CLI_VERSION=\"${latest_version}\"|" \
  -e "s|^(  darwin_amd64\) cli_sha256=\")[0-9a-f]{64}|\1${darwin_amd64_sha256}|" \
  -e "s|^(  darwin_arm64\) cli_sha256=\")[0-9a-f]{64}|\1${darwin_arm64_sha256}|" \
  -e "s|^(  linux_amd64\) cli_sha256=\")[0-9a-f]{64}|\1${linux_amd64_sha256}|" \
  -e "s|^(  linux_arm64\) cli_sha256=\")[0-9a-f]{64}|\1${linux_arm64_sha256}|" \
  "${pin_file}" > "${pin_file}.tmp"

# Prove every rewrite landed before the temporary replaces the pin file, so a
# pin file whose shape has drifted from the sed patterns above fails loudly
# with the original untouched. $1 is one expected whole line.
verify_line() {
  if ! grep -qxF "$1" "${pin_file}.tmp"; then
    echo "Rewrite failed: ${pin_file} no longer matches the shape this script expects. Missing line:" >&2
    echo "$1" >&2
    rm -f "${pin_file}.tmp"
    exit 1
  fi
}
verify_line "SUPABASE_CLI_VERSION=\"${latest_version}\""
verify_line "  darwin_amd64) cli_sha256=\"${darwin_amd64_sha256}\" ;;"
verify_line "  darwin_arm64) cli_sha256=\"${darwin_arm64_sha256}\" ;;"
verify_line "  linux_amd64) cli_sha256=\"${linux_amd64_sha256}\" ;;"
verify_line "  linux_arm64) cli_sha256=\"${linux_arm64_sha256}\" ;;"

# mv installs the temporary wholesale, dropping the executable bit the
# temporary never had, so restore it.
mv "${pin_file}.tmp" "${pin_file}"
chmod +x "${pin_file}"

echo "✅ Pinned Supabase CLI upgraded from ${pinned_version} to ${latest_version}."
echo "Release notes: https://github.com/supabase/cli/releases"
echo "Now run ./scripts/integration-test.sh to prove the new pin fetches, verifies and starts the stack."
