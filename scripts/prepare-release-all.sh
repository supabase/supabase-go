#!/usr/bin/env bash
# Prepare a coordinated release of everything that has something to ship: each
# module with entries under its changelog's '## Unreleased' heading, plus any
# module the operator chooses to release solely so its pinned sibling requires
# carry the new versions to consumers through one `go get`, as the root client
# exists to do. Carrying is offered, never forced: a sibling require is a floor
# that minimal version selection resolves upward in consumer builds
# (go.dev/ref/mod#minimal-version-selection), so an unchanged module keeping an
# older pin stays correct, nothing goes stale and declining releases nothing.
#
# The operator stays the release gate. The script first gathers and presents
# the full picture: which modules release for their own changes (their
# unreleased entries printed, to help grade each bump), which release only to
# carry dependency upgrades and which stay put. It then prompts for each new
# version, shows the complete plan - versions, pins and the changelog sections
# it will write - and asks for confirmation before touching anything.
#
# For a module released only to carry upgrades it writes the changelog section
# the release needs: one 'Upgrades X from version Y to version Z.' bullet per
# upgraded sibling and nothing more. The sibling's own changelog carries the
# substance, deliberately not repeated here, so readers (human or AI) follow
# the reference instead of trusting a copy.
#
# Unlike scripts/prepare-release.sh - which this script drives once per
# released module, in dependency order - this script DOES commit.
# prepare-release.sh demands a clean tree, so a multi-module sequence needs
# the intermediate commits the manual procedure has the operator make, and
# this script makes exactly those commits with exactly the messages that
# procedure instructs. It never tags and never pushes: review the branch and
# open the release PR as RELEASING.md describes.
set -euo pipefail

source "$(dirname "$0")/common.sh"

module_base="$(module_path_base)"
zero_pseudo_version="v0.0.0-00010101000000-000000000000"
workspace_modules="$(enumerate_workspace_modules | sed 's|^\./||')"

# --- helpers ----------------------------------------------------------------

# Print a module's sibling requires as 'directory version' lines. $1 is the
# module directory.
sibling_requires() {
  go mod edit -json "$1/go.mod" \
    | jq -r '.Require[]? | "\(.Path) \(.Version)"' \
    | while read -r path version; do
        case "${path}" in
          "${module_base}"/*) printf '%s %s\n' "${path#"${module_base}"/}" "${version}" ;;
        esac
      done
}

# Succeed when $1 equals one of the remaining arguments.
in_list() {
  local needle="$1" candidate
  shift
  for candidate in "$@"; do
    [ "${candidate}" = "${needle}" ] && return 0
  done
  return 1
}

# Print the version chosen for module $1, from the parallel arrays the
# prompting phase fills.
chosen_version() {
  local index
  for index in "${!chosen_modules[@]}"; do
    if [ "${chosen_modules[${index}]}" = "$1" ]; then
      printf '%s\n' "${chosen_versions[${index}]}"
      return 0
    fi
  done
  return 1
}

# Print the entry lines under a module's '## Unreleased' heading, trimmed of
# leading and trailing blank lines so they read cleanly at a prompt. $1 is
# the module directory.
print_unreleased_entries() {
  awk '
    $0 == "## Unreleased" { in_section = 1; next }
    /^## / { in_section = 0 }
    in_section { print }
  ' "$1/CHANGELOG.md" | awk '
    { lines[NR] = $0 }
    END {
      start = 1
      while (start <= NR && lines[start] ~ /^[[:space:]]*$/) start++
      end = NR
      while (end >= start && lines[end] ~ /^[[:space:]]*$/) end--
      for (i = start; i <= end; i++) print lines[i]
    }
  '
}

# --- guards ------------------------------------------------------------------

branch="$(git rev-parse --abbrev-ref HEAD)"
if [ "${branch}" = "main" ]; then
  echo "Refusing to prepare a release on main. Create a release branch first (a 'release/' prefix is preferred)." >&2
  exit 1
fi

if [ -n "$(git status --porcelain --untracked-files=no)" ]; then
  echo "The working tree has staged or unstaged changes; commit or stash them first." >&2
  exit 1
fi

# --- gather ------------------------------------------------------------------

# Modules releasing for their own changes: a changelog with a well-formed,
# non-empty '## Unreleased' section. An empty section is a malformed state
# this script refuses to paper over, because prepare-release.sh would refuse
# it too, only later and part-way through a sequence.
direct_modules=()
for module in ${workspace_modules}; do
  headings="$(grep -cx '## Unreleased' "${module}/CHANGELOG.md" || true)"
  if [ "${headings}" -gt 1 ]; then
    echo "${module}/CHANGELOG.md has ${headings} '## Unreleased' sections; it needs at most one." >&2
    exit 1
  fi
  if [ "${headings}" -eq 1 ] && [ "$(unreleased_entry_lines "${module}")" -eq 0 ]; then
    echo "${module}/CHANGELOG.md has an empty '## Unreleased' section; fill it or remove it, then rerun." >&2
    exit 1
  fi
  if [ "$(unreleased_entry_lines "${module}")" -gt 0 ]; then
    direct_modules+=("${module}")
  fi
done

if [ "${#direct_modules[@]}" -eq 0 ]; then
  echo "Every module's changelog is quiet - nothing to release."
  exit 0
fi

# Dependency order over every workspace module, derived from the sibling
# requires rather than hard-coded, as the release-tags workflow derives it.
remaining=()
for module in ${workspace_modules}; do
  remaining+=("${module}")
done
all_ordered=()
while [ "${#remaining[@]}" -gt 0 ]; do
  progressed=0
  for index in "${!remaining[@]}"; do
    module="${remaining[${index}]}"
    ready=1
    for dependency in $(sibling_requires "${module}" | awk '{ print $1 }'); do
      if in_list "${dependency}" "${remaining[@]}"; then
        ready=0
        break
      fi
    done
    if [ "${ready}" -eq 1 ]; then
      all_ordered+=("${module}")
      unset 'remaining[index]'
      remaining=(${remaining[@]+"${remaining[@]}"})
      progressed=1
      break
    fi
  done
  if [ "${progressed}" -eq 0 ]; then
    echo "The sibling requires of (${remaining[*]}) form a cycle; modules must order." >&2
    exit 1
  fi
done

# A module requiring a releasing sibling may join to carry the upgraded pins,
# by the operator's choice and never out of necessity (see the header: floors,
# not freshness). Candidates are asked in dependency order, so declining one
# also stops any cascade it would have caused, and each prompt spells out what
# accepting ships so caution steers toward No, not toward a no-op release.
release_modules=("${direct_modules[@]}")
for module in "${all_ordered[@]}"; do
  in_list "${module}" "${release_modules[@]}" && continue
  triggers=""
  for dependency in $(sibling_requires "${module}" | awk '{ print $1 }'); do
    if in_list "${dependency}" "${release_modules[@]}"; then
      [ -n "${triggers}" ] && triggers="${triggers}, "
      triggers="${triggers}${dependency}"
    fi
  done
  [ -n "${triggers}" ] || continue
  echo
  echo "==> ${module} has nothing of its own to release, but modules it requires"
  echo "    (${triggers}) release this run."
  echo "    Skipping is safe. A pin is only a minimum: consumer builds still get the"
  echo "    newer versions whenever anything in the build asks for them, and an old"
  echo "    pin in ${module} never blocks that."
  echo "    Releasing ${module} changes nothing except its pins. That is worth doing"
  echo "    only if consumers should receive the new modules by upgrading ${module}"
  echo "    alone - true for the root client, rarely for anything else."
  if ! read -r -p "    Release ${module} for that reason? [y/N] " reply; then
    echo
    echo "Aborted at end of input - nothing changed." >&2
    exit 1
  fi
  case "${reply}" in
    y | Y) release_modules+=("${module}") ;;
    *) echo "    Skipping ${module}: dependents will pin it at its current version." ;;
  esac
done

ordered_modules=()
for module in "${all_ordered[@]}"; do
  in_list "${module}" "${release_modules[@]}" && ordered_modules+=("${module}")
done

# --- present -----------------------------------------------------------------

echo "Releasing for changes of their own:"
for module in "${ordered_modules[@]}"; do
  in_list "${module}" "${direct_modules[@]}" || continue
  current="$(declared_module_versions "${module}" | head -n 1)"
  echo "  ${module} (current ${current:-unreleased})"
done

echo
echo "Releasing only to carry dependency upgrades:"
transitive_named=0
for module in "${ordered_modules[@]}"; do
  in_list "${module}" "${direct_modules[@]}" && continue
  triggers=""
  for dependency in $(sibling_requires "${module}" | awk '{ print $1 }'); do
    in_list "${dependency}" "${release_modules[@]}" && triggers="${triggers} ${dependency}"
  done
  echo "  ${module} (requires:${triggers})"
  transitive_named=1
done
[ "${transitive_named}" -eq 0 ] && echo "  (none)"

echo
echo "Nothing to release:"
quiet_named=0
for module in ${workspace_modules}; do
  if ! in_list "${module}" "${release_modules[@]}"; then
    echo "  ${module} (stays $(declared_module_versions "${module}" | head -n 1))"
    quiet_named=1
  fi
done
[ "${quiet_named}" -eq 0 ] && echo "  (none)"

# --- choose versions ---------------------------------------------------------

# Each module's context prints at its own prompt rather than up front, so the
# operator grades one bump at a time without scrolling: unreleased entries for
# a module releasing its own changes, the upgrade lines being carried for a
# module releasing only to carry them (its dependencies' versions are already
# chosen, since prompting follows dependency order).
chosen_modules=()
chosen_versions=()
for module in "${ordered_modules[@]}"; do
  current="$(declared_module_versions "${module}" | head -n 1)"
  echo
  if in_list "${module}" "${direct_modules[@]}"; then
    echo "==> ${module} (current ${current:-unreleased}), '## Unreleased' holds:"
    print_unreleased_entries "${module}" | sed 's/^./    &/'
  else
    echo "==> ${module} (current ${current:-unreleased}) releases only to carry:"
    while read -r dependency dependency_current; do
      in_list "${dependency}" "${release_modules[@]}" || continue
      echo "    ${dependency} ${dependency_current} -> $(chosen_version "${dependency}")"
    done < <(sibling_requires "${module}")
  fi
  while :; do
    if ! read -r -p "New version for ${module} (current ${current:-unreleased}): " candidate; then
      echo
      echo "Aborted at end of input - nothing changed." >&2
      exit 1
    fi
    if ! printf '%s' "${candidate}" | grep -Eq "$(semver_version_pattern)"; then
      echo "  '${candidate}' is not a SemVer tag version (want vX.Y.Z or vX.Y.Z-pre)." >&2
      continue
    fi
    if declared_module_versions "${module}" | grep -qx "${candidate}"; then
      echo "  ${module}/CHANGELOG.md already declares ${candidate}." >&2
      continue
    fi
    break
  done
  chosen_modules+=("${module}")
  chosen_versions+=("${candidate}")
done

# --- confirm -----------------------------------------------------------------

echo
echo "The plan, in dependency order:"
for module in "${ordered_modules[@]}"; do
  new_version="$(chosen_version "${module}")"
  current="$(declared_module_versions "${module}" | head -n 1)"
  echo
  echo "==> ${module}: ${current:-unreleased} -> ${new_version}"
  while read -r dependency dependency_current; do
    if in_list "${dependency}" "${release_modules[@]}"; then
      echo "    pins ${dependency} ${dependency_current} -> $(chosen_version "${dependency}")"
    else
      echo "    pins ${dependency} ${dependency_current} (unchanged)"
    fi
  done < <(sibling_requires "${module}")
  if ! in_list "${module}" "${direct_modules[@]}"; then
    echo "    writes ${module}/CHANGELOG.md '## Unreleased' section:"
    while read -r dependency dependency_current; do
      in_list "${dependency}" "${release_modules[@]}" || continue
      if [ "${dependency_current}" = "${zero_pseudo_version}" ]; then
        echo "      - Upgrades \`${dependency}\` to \`$(chosen_version "${dependency}")\`."
      else
        echo "      - Upgrades \`${dependency}\` from \`${dependency_current}\` to \`$(chosen_version "${dependency}")\`."
      fi
    done < <(sibling_requires "${module}")
  fi
done

echo
if ! read -r -p "Proceed? This writes, runs prepare-release.sh per module and commits on '${branch}'. [y/N] " reply; then
  echo
  echo "Aborted at end of input - nothing changed." >&2
  exit 1
fi
case "${reply}" in
  y | Y) ;;
  *)
    echo "Aborted - nothing changed."
    exit 1
    ;;
esac

# --- write the dependency-upgrade changelogs ---------------------------------

# One commit for all of them, ahead of the prepare sequence, because
# prepare-release.sh (rightly) refuses a dirty tree.
upgraded_changelogs=()
for module in "${ordered_modules[@]}"; do
  in_list "${module}" "${direct_modules[@]}" && continue
  section_file="$(mktemp)"
  {
    echo '## Unreleased'
    echo
    echo '### Changed'
    echo
    while read -r dependency dependency_current; do
      in_list "${dependency}" "${release_modules[@]}" || continue
      if [ "${dependency_current}" = "${zero_pseudo_version}" ]; then
        echo "- Upgrades \`${dependency}\` to \`$(chosen_version "${dependency}")\`."
      else
        echo "- Upgrades \`${dependency}\` from \`${dependency_current}\` to \`$(chosen_version "${dependency}")\`."
      fi
    done < <(sibling_requires "${module}")
  } > "${section_file}"

  changelog="${module}/CHANGELOG.md"
  awk -v section_file="${section_file}" '
    !inserted && /^## / {
      while ((getline line < section_file) > 0) print line
      print ""
      inserted = 1
    }
    { print }
  ' "${changelog}" > "${changelog}.tmp"
  mv "${changelog}.tmp" "${changelog}"
  rm -f "${section_file}"

  if [ "$(grep -cx '## Unreleased' "${changelog}")" -ne 1 ]; then
    echo "Writing the upgrade section left ${changelog} without exactly one '## Unreleased' heading; inspect the file." >&2
    exit 1
  fi
  echo "==> wrote dependency-upgrade entries to ${changelog}"
  upgraded_changelogs+=("${changelog}")
done

if [ "${#upgraded_changelogs[@]}" -gt 0 ]; then
  git add -- "${upgraded_changelogs[@]}"
  git commit --quiet --message="Add dependency upgrade changelog entries for the release."
  echo "==> committed: Add dependency upgrade changelog entries for the release."
fi

# --- prepare each module, committing as the manual procedure instructs --------

for module in "${ordered_modules[@]}"; do
  new_version="$(chosen_version "${module}")"
  pin_arguments=()
  while read -r dependency dependency_current; do
    if in_list "${dependency}" "${release_modules[@]}"; then
      pin_arguments+=("${dependency}=$(chosen_version "${dependency}")")
    else
      pin_arguments+=("${dependency}=${dependency_current}")
    fi
  done < <(sibling_requires "${module}")

  echo
  # Suppress prepare-release.sh's next-steps guidance: this script performs
  # those steps itself, immediately below.
  PREPARE_RELEASE_SUPPRESS_NEXT_STEPS=1 \
    ./scripts/prepare-release.sh "${module}" "${new_version}" ${pin_arguments[@]+"${pin_arguments[@]}"}
  git add -A
  git commit --quiet --message="Prepare release ${module}/${new_version}."
  echo "==> committed: Prepare release ${module}/${new_version}."
done

# --- pre-flight, as RELEASING.md's next step ----------------------------------

echo
./scripts/check-release-consistency.sh

echo
echo "✅ Prepared $(printf '%s ' "${ordered_modules[@]}")on '${branch}', one commit per step."
echo
echo "Review the commits, push the branch and open the release PR."
echo "When it merges, the release-tags workflow pushes the tags in dependency order."
