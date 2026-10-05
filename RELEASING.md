# Releasing

This repository publishes multiple Go modules. Each module is versioned, tagged and released independently. The canonical list of published modules can be found in [`go.work`](go.work).

Publishing a module is achieved by pushing a Git tag. That's all. There is no upload, account or registry step.

The [Go module proxy](https://go.dev/ref/mod#module-proxy) caches every published version immutably, so a mistake can never be withdrawn or re-tagged, only superseded by a higher version carrying a [`retract` directive](https://go.dev/ref/mod#go-mod-file-retract) for the bad one.

A release is therefore prepared by:

1. Running one or more scripts on a branch.
2. Landing that work through an ordinary pull request.
3. Release automation pushes the tags that the merged pull request declared.

Steps 1 and 2 are manually handled by a human operator (perhaps with AI assistance) positioning that human as the release gate where "sense checks" must be made. While we could automate these steps more, and perhaps we might in future, the release cadence and need for flexibility right now mean that this manual handling is the preferred operations method for now in this SDK.

## Versions and tags

A module's version lives in its Git tags.
Tags for a module in a subdirectory [are path-prefixed](https://go.dev/ref/mod#vcs-version) - module `github.com/supabase/supabase-go/auth` at `v0.1.0-alpha.1` means tag `auth/v0.1.0-alpha.1` - while `require` lines always carry the plain version, never the tag prefix, because they already include the module name.

In the tree, the newest version heading in a module's [changelog](CHANGELOG.md) declares that module's current version: `scripts/prepare-release.sh` writes it and [the Release Tags workflow](.github/workflows/release-tags.yml) reads it to know what to tag.
Choose each module's next version by hand from the entries under the `## Unreleased` heading of its changelog, which accumulate as changes merge.
A module whose changelog has no `## Unreleased` section has nothing to release, and the prepare script refuses to prepare it.

We treat `v1` as a long-term commitment to backward compatibility, in line with Go's own module versioning philosophy. Because Go enforces [Semantic Import Versioning](https://go.dev/doc/modules/major-version), bumping a module to `v2` requires changing its import path (appending `/v2`). This requires changes to all downstream consumers, forcing them to manually rewrite their imports, reflecting the breaking nature of the major release. To avoid this friction and ecosystem fragmentation, our goal is to evolve the SDK's APIs backward-compatibly indefinitely and never require a `v2` release.

## Dependency order

Prepare `core` first, then `auth` and `postgrest` in either order, then `supabase`.
A dependent module pins its sibling requires to the versions being released, and a pin may only name a version the sibling's changelog already declares - prepare the modules out of order and the pre-flight check below fails.
Tag pushes order themselves: the workflow derives dependency order from the sibling requires.

## Cut a release

1. Branch off an up-to-date `main`. Any branch name works, with a `release/` prefix preferred.

2. Run `scripts/prepare-release.sh` once per module being released, in dependency order, pinning every sibling the module requires, and commit as its output instructs. A coordinated all-module release looks like:

   ```bash
   ./scripts/prepare-release.sh core v0.1.0-alpha.1
   ./scripts/prepare-release.sh auth v0.1.0-alpha.1 core=v0.1.0-alpha.1
   ./scripts/prepare-release.sh postgrest v0.1.0-alpha.1 core=v0.1.0-alpha.1
   ./scripts/prepare-release.sh supabase v0.1.0-alpha.1 core=v0.1.0-alpha.1 auth=v0.1.0-alpha.1 postgrest=v0.1.0-alpha.1
   ```

   For each module the script refuses to run unless the tree is clean and the module's changelog has entries under `## Unreleased`, pins the sibling `require` lines to the given `dep=version` arguments (aborting if any sibling is left at the zero pseudo-version, the mark of a forgotten pin), stamps the changelog - the `## Unreleased` heading becomes ``## `<version>` (<date>)`` - and re-tidies the non-published modules (the integration tests, the examples and `telemetrytest`) with `GOWORK=off`, so every module's recorded versions stay consistent with the new graph and CI stays green.
   It never commits, tags or pushes.

3. Run the pre-flight check, which joins the dots across everything the session prepared:

   ```bash
   ./scripts/check-release-consistency.sh
   ```

   It works from committed files alone - no network and no Go module resolution, because pinned versions cannot resolve until their tags exist - and catches the anticipated failure modes: malformed or duplicate version headings, a forgotten pin, a pin naming a version its module never declared and a release prepared out of dependency order.
   Between the prepare commits of a multi-module release it fails on the pins not yet made - expected, and [explained in Troubleshooting](#the-consistency-check-failed-part-way-through-preparing-a-release).
   CI runs the same script on every push, and the release-tags workflow runs it once more before pushing tags.

4. Push the branch and open the PR.
   CI must be green before merge, and review approvals are welcome but not required: by the time the PR exists, its contents are mechanical bookkeeping over code already reviewed into `main`.

5. Merge.
   The release-tags workflow runs on the landing commit, finds each released module's newest declared version untagged and pushes the missing `<module>/<version>` tags in dependency order.
   Its run summary lists what it tagged - or states plainly that it detected no release, which is what every ordinary merge to `main` shows.
   If the run fails or its summary surprises you, see [Troubleshooting](#troubleshooting).

6. Verify the tagged content:

   ```bash
   ./scripts/verify-release.sh [<landing-commit>]
   ```

   It learns what was released from origin's tags pointing at the landing commit - HEAD when the argument is omitted, for the common case of running from a checkout of it - then downloads each version into a throwaway module cache under `/tmp` - straight from GitHub, touching neither the public module proxy nor the checksum database, so nothing is cached anywhere ahead of the next step - confirms a `LICENSE` sits in each module zip and prints the content hashes.

   Keep the hash lines.

7. Seed the public Go module ecosystem:

   ```bash
   ./scripts/seed-module-proxy.sh [<landing-commit>]
   ```

   It pulls the same tag-derived versions through [proxy.golang.org](https://proxy.golang.org/), which fetches each one from GitHub and caches it immutably - **the point of no return** - records its hashes in the [sum.golang.org](https://sum.golang.org) checksum database and leads [pkg.go.dev](https://pkg.go.dev/github.com/supabase/supabase-go/supabase) to build the documentation pages minutes later.

   Its hash lines must match step 6's byte for byte, proving the proxy serves exactly what GitHub serves.

   The tags carry everything both scripts need, so any release stays reachable from the current checkout - including one whose landing commit predates the scripts themselves, which no checkout requirement could ever serve.

## Troubleshooting

The first entry below is the one expected failure, met part-way through cutting a release, and needs no recovery.
Every other entry concerns a merged release PR, which puts `main` into a state that cannot regress: its landing commit permanently declares the release, through each released module's newest changelog heading and pinned `require` lines.

Tags are the only artifact that can be missing.
Every recovery below therefore drives at one end state: for each released module, a tag `<module>/<version>` exists on the GitHub remote, pointing at the landing commit.
No recovery path modifies `main`, none touches an existing tag and the tag script skips whatever already exists, so each path is safe to attempt and safe to repeat.
A consumer who fetches while only some of the tags exist sees a transient resolution failure, healed the moment the remaining tags land.

### The consistency check failed part-way through preparing a release

State: between the per-module commits of a multi-module release, `check-release-consistency.sh` - run directly or through `check-fast.sh` - fails with "a forgotten dep=version pin" complaints against the modules not yet prepared.
The check joins the dots across the whole repository, and mid-sequence the dots genuinely do not join: a prepared module's changelog already declares the new version while a dependent yet to be prepared still carries the zero pseudo-version placeholder.
At that moment a forgotten pin and a pin not yet made are indistinguishable, and stopping a half-released state from reaching `main` unnoticed is this check's whole purpose.

Fix: nothing needs recovering - finish the sequence.
Prepare and commit the remaining modules, and the last pin written turns the check green, which is why [Cut a release](#cut-a-release) places the pre-flight after every module is prepared.
CI never sees the intermediate states: the branch is pushed once carrying all its commits, CI checks its tip and the release-tags workflow checks the landing commit on `main`.
The intermediate commits stay inconsistent in history, visible only to a `git bisect` that runs the check across the release PR's commits, an inherent and accepted cost of preparing each module in its own commit.

### Establish the state

Gather three facts before choosing a fix.

1. The landing commit: named on the merged release PR as "merged commit `<sha>` into `main`", and immediately after the merge it is also `git log --merges -1 origin/main`.
2. What the release-tags workflow did: the repository's Actions tab lists its run against the landing commit, and that run's summary page states what it tagged, what it skipped and what it never reached.
3. Which tags exist on the GitHub remote, independent of any log or summary:

   ```bash
   git ls-remote --tags origin | grep <version>
   ```

The gap between the declared versions and fact 3 is the entire problem, and each fix below closes it in a different circumstance.

### The workflow ran and failed

State: the Actions tab shows a failed release-tags run at the landing commit.
Tags pushed before the failing step exist on the GitHub remote and the run's log names them, while the rest are missing.

Fix: open the failed run's page and choose "Re-run all jobs".
A re-run executes at the landing commit, reports the already-pushed tags as already tagged and pushes the missing ones.
Transient causes - a dropped connection, a runner lost mid-job - need nothing more.

### The workflow never started

State: the Actions tab shows no release-tags run for the landing commit, so no tags have been pushed.
The usual cause is a GitHub platform incident (check GitHub's status page) delaying or, rarely, dropping the push event.

Preferred fix: wait for the GitHub incident to resolve.
A delayed run starts on its own once GitHub Actions recovers and needs no intervention.
Waiting costs little, because the same incident typically also degrades everything a rushed release needs anyway: the Go module proxy and every consumer's `go get` fetch this repository from GitHub too.

Fix when the release is urgent and `git push` to GitHub is working: run the same script the workflow runs, from a clean checkout of the landing commit.
It needs `go` and `jq` on the machine plus tag push rights, and reports what it did the same way the workflow summary does:

```bash
git fetch origin
git checkout <landing-commit>
./scripts/push-release-tags.sh
```

If GitHub recovers and no run ever appears, the push event was dropped and will not replay: use the same three commands.

### The script stopped at a stale local tag

State: a local run of `push-release-tags.sh` exited with "Local tag `<module>/<version>` exists but does not point at HEAD".
Your clone already carried that tag name pointing at some other commit, and pushing it would have published the wrong content, so the script refused.
Tags it pushed before reaching this module exist on the GitHub remote, while this module's tag and any after it are still missing.

Fix: delete the stale local tag, then re-run the script from the same checkout:

```bash
git tag -d <module>/<version>
./scripts/push-release-tags.sh
```

### Pushing a tag by hand

When `go` or `jq` is unavailable on the machine at hand, or the script itself is misbehaving, plain git does the same work minus the checks.
For each tag still missing from the GitHub remote, in dependency order (`core`, then `auth` and `postgrest`, then `supabase`):

```bash
git tag auth/v0.1.0-alpha.1 <landing-commit>
git push origin auth/v0.1.0-alpha.1
```

### The summary reports already tagged for a version you meant to release

State: the release-tags run succeeded, but a module you meant to release is reported as already tagged, and `git ls-remote --tags origin` shows its tag pointing at a commit that is not the landing commit.
The module's newest changelog heading names a version that was released before.
`prepare-release.sh` refuses to stamp a version its changelog already declares, so a heading was written or edited by hand somewhere.

Fix: leave the existing tag exactly where it is.
A published version is immutable, and the Go module proxy may have cached it the moment it appeared.
Correct the changelog and release the module's next version through a fresh release PR, and the workflow tags that PR's landing commit.

### The consistency check failed on the landing commit

State: the release-tags run is red at its `check-release-consistency.sh` step, which runs before any tagging, so this run pushed nothing.
`main`'s release bookkeeping violates an invariant despite the same check gating the release PR, which can happen when two PRs merge in quick succession and their combination breaks what neither broke alone.

Fix: restore consistency with an ordinary PR.
The release PR's declarations are already on `main` and stay put.
The workflow run for that fix's landing commit pushes the release tags, which then point at the fixed commit - the content a consumer should get.

### After any recovery

Confirm the end state: every declared version has its tag on the GitHub remote (fact 3 above) and the tagged content verifies, exactly as in step 6 of [Cut a release](#cut-a-release).
The argument is step 6's optional one, filled with the landing commit (fact 1) because recovery work seldom ends with HEAD checked out at it:

```bash
./scripts/verify-release.sh <landing-commit>
```

## After a release

Nothing further needs updating: versions live in the tags, the committed `require` lines already point at the versions released and `go.work` still overlays local source for development, so day-to-day builds are unchanged.
