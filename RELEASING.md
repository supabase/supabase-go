# Releasing

This repository publishes multiple Go modules. Each module is versioned, tagged and released independently. The canonical list of published modules can be found in [`go.work`](go.work).

Publishing a module is achieved by pushing a Git tag. That's all. There is no upload, account or registry step.

The [Go module proxy](https://go.dev/ref/mod#module-proxy) caches every published version immutably, so a mistake can never be withdrawn or re-tagged, only superseded by a higher version carrying a [`retract` directive](https://go.dev/ref/mod#go-mod-file-retract) for the bad one.

A release is therefore prepared by:

1. Running one or more scripts on a branch.
2. Landing that work through an ordinary pull request.
3. Release automation pushes the tags that the merged pull request declared.
4. A human dispatches the Seed Release workflow, which pulls the tagged versions through the public Go module proxy.

Steps 1, 2 and 4 are manually handled by a human operator (perhaps with AI assistance) positioning that human as the release gate where "sense checks" must be made. Only step 3 runs on its own, on every merge to `main`. While we could automate these steps more, and perhaps we might in future, the release cadence and need for flexibility right now mean that this manual handling is the preferred operations method for now in this SDK.

> [!TIP]
> Most releases touch several modules at once, so the interactive wrapper described in [Cut a coordinated release](#cut-a-coordinated-release) is usually the preferred way to run them.

## Versions and tags

A module's version lives in its Git tags.
Tags for a module in a subdirectory [are path-prefixed](https://go.dev/ref/mod#vcs-version) - module `github.com/supabase/supabase-go/auth` at `v0.1.0-alpha.1` means tag `auth/v0.1.0-alpha.1` - while `require` lines always carry the plain version, never the tag prefix, because they already include the module name.

In the tree, the newest version heading in a module's [changelog](CHANGELOG.md) declares that module's current version: `scripts/prepare-release.sh` writes it and [the Release Tags workflow](.github/workflows/release-tags.yml) reads it to know what to tag.
Choose each module's next version by hand from the entries under the `## Unreleased` heading of its changelog, which accumulate as changes merge, and from `./scripts/api-diff.sh <module>`, which lists the module's public API changes since its newest declared version.
An incompatible change in that list with no changelog entry is an entry missing.
A module whose changelog has no `## Unreleased` section has nothing to release, and the prepare script refuses to prepare it.

We treat `v1` as a long-term commitment to backward compatibility, in line with Go's own module versioning philosophy. Because Go enforces [Semantic Import Versioning](https://go.dev/doc/modules/major-version), bumping a module to `v2` requires changing its import path (appending `/v2`). This requires changes to all downstream consumers, forcing them to manually rewrite their imports, reflecting the breaking nature of the major release. To avoid this friction and ecosystem fragmentation, our goal is to evolve the SDK's APIs backward-compatibly indefinitely and never require a `v2` release.

## Dependency order

Prepare `core` first, then `auth` and `postgrest` in either order, then `supabase`.
A dependent module pins its sibling requires to the versions being released, and a pin may only name a version the sibling's changelog already declares - prepare the modules out of order and the pre-flight check below fails.
Tag pushes order themselves: the workflow derives dependency order from the sibling requires.

## Cut a release

1. :technologist: Branch off an up-to-date `main`. Any branch name works, with a `release/` prefix preferred.

2. :technologist: Run `scripts/prepare-release.sh` once per module being released, in dependency order, pinning every sibling the module requires, and commit as its output instructs. A coordinated all-module release looks like:

   ```bash
   ./scripts/prepare-release.sh core v0.1.0-alpha.1
   ./scripts/prepare-release.sh auth v0.1.0-alpha.1 core=v0.1.0-alpha.1
   ./scripts/prepare-release.sh postgrest v0.1.0-alpha.1 core=v0.1.0-alpha.1
   ./scripts/prepare-release.sh supabase v0.1.0-alpha.1 core=v0.1.0-alpha.1 auth=v0.1.0-alpha.1 postgrest=v0.1.0-alpha.1
   ```

   For each module the script refuses to run unless the tree is clean and the module's changelog has entries under `## Unreleased`, pins the sibling `require` lines to the given `dep=version` arguments (aborting if any sibling is left at the zero pseudo-version, the mark of a forgotten pin), stamps the changelog - the `## Unreleased` heading becomes ``## `<version>` (<date>)`` - then raises every non-published module's require of the module to the version being released and re-tidies them (the integration tests, the examples and `telemetrytest`) with `GOWORK=off`, so every module's recorded versions stay consistent with the new graph and CI stays green.
   The raise matters because those modules resolve the SDK through `replace` directives, which make a recorded version nominal to the build: tidy moves it only when something else in the graph demands more, and would otherwise leave an example naming a release that lacks what its code uses.
   It never commits, tags or pushes.

3. :technologist: Run the pre-flight check, which joins the dots across everything the session prepared:

   ```bash
   ./scripts/check-release-consistency.sh
   ```

   It works from committed files alone - no network and no Go module resolution, because pinned versions cannot resolve until their tags exist - and catches the anticipated failure modes: malformed or duplicate version headings, a forgotten pin, a pin naming a version its module never declared, a release prepared out of dependency order and a non-published module recording a sibling behind its newest release.
   Between the prepare commits of a multi-module release it fails on the pins not yet made - expected, and [explained in Troubleshooting](#the-consistency-check-failed-part-way-through-preparing-a-release).
   CI runs the same script on every push, and the release-tags workflow runs it once more before pushing tags.

4. :technologist: Push the branch and open the PR.
   CI must be green before merge, and review approvals are welcome but not required: by the time the PR exists, its contents are mechanical bookkeeping over code already reviewed into `main`.

5. :technologist: Merge.

6. :robot: The release-tags workflow runs on the landing commit, finds each released module's newest declared version untagged and pushes the missing `<module>/<version>` tags in dependency order.
   Its run summary lists what it tagged - or states plainly that it detected no release, which is what every ordinary merge to `main` shows.
   If the run fails or its summary surprises you, see [Troubleshooting](#troubleshooting).

7. :technologist: Seed the public Go module ecosystem: from [the GitHub Actions tab](https://github.com/supabase/supabase-go/actions), run the [Seed Release workflow](.github/workflows/seed-release.yml) on `main`, leaving its `landing-commit` input empty.
   If `main` has moved on since the merge, name the landing commit in the input.
   The run is safe to repeat at any time, and [`scripts/seed-release.sh`](scripts/seed-release.sh) runs the same locally.

8. :robot: The run verifies the tagged content, pulls it through proxy.golang.org - **the point of no return** - and fails unless the proxy serves exactly what GitHub serves, reporting the outcome and the hash lines on its summary page.
   Expect twenty minutes or so: the proxy answers 404 for freshly tagged versions for a while, and the run retries for up to two hours before giving up (see [Troubleshooting](#troubleshooting)).

## Cut a coordinated release

`scripts/prepare-release-all.sh` wraps steps 2 and 3 of [Cut a release](#cut-a-release) for the common case of releasing several modules together. It is interactive and commits as it goes - one commit per step, carrying the same messages the manual procedure instructs - but, like the script it drives, it never tags and never pushes.

From a release branch (it refuses to run on `main`), run it with no arguments:

```bash
./scripts/prepare-release-all.sh
```

It gathers every module whose changelog holds `## Unreleased` entries, then walks the rest of the plan interactively, in dependency order throughout:

1. A module with no changes of its own, requiring a sibling that is releasing, is offered - never forced - as a pins-only release. Declining is the norm and loses nothing: a `require` is a floor that [minimal version selection](https://go.dev/ref/mod#minimal-version-selection) resolves upward in consumer builds, so an older pin never goes stale and never blocks a newer sibling. Accept only when consumers should receive the new modules by upgrading that one module alone - the root `supabase` client's reason to exist, and rarely true of anything else.
2. Each released module's new version is prompted for beneath the context that grades it: its own unreleased entries, or - for a pins-only release - the upgrades it would carry, followed by its public API changes since its current version.
3. The whole plan is shown - versions, every pin movement and the changelog sections to be written - behind one final confirmation before anything is touched.

For a pins-only module the script writes the changelog section its release needs, one `Upgrades X from version Y to version Z.` bullet per upgraded sibling and nothing more: the substance lives in the sibling's own changelog, deliberately not repeated.

It finishes by running the pre-flight check, leaving the branch ready at step 4 of [Cut a release](#cut-a-release): push, open the PR and continue from there.

## Troubleshooting

The first two entries below are the expected failures and need no recovery: one met part-way through cutting a release, the other met while seeding the proxy with every tag already in place.
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

### The proxy returned 404 for a freshly tagged version

State: `seed-module-proxy.sh` fails reading a version's `.info` from proxy.golang.org with `404 Not Found` and a server response calling the freshly pushed tag an "unknown revision", even though the release-tags run succeeded, `git ls-remote --tags origin` shows every tag and `verify-release.sh` has already verified the same versions straight from GitHub. Under `seed-release.sh`, the log shows the same failure once a minute.
The release is sound: the mirror answers from a cached view of the repository that predates the tag push, and a version requested before its tag was visible can hold the stale answer in place for a while.

Fix: nothing, while the Seed Release run is still retrying - it tries again a minute apart for up to two hours, and a failed attempt seeds nothing. If it gave up, dispatch it again, naming the landing commit if `main` has moved on. By hand, re-run `./scripts/seed-module-proxy.sh` until it succeeds.
The proxy's [FAQ](https://proxy.golang.org/) expects a new version within a minute of an explicit request but allows up to 30 minutes for a cached stale answer to expire, and this repository's releases have waited about twenty minutes from tag push to a successful seed.
If the 404 outlives the two hours by hours, [file a Go issue](https://github.com/golang/go/issues) titled with the prefix "proxy.golang.org:", as that FAQ asks.

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

Confirm the end state: every declared version has its tag on the GitHub remote (fact 3 above) and the tagged content verifies and seeds, exactly as in step 7 of [Cut a release](#cut-a-release).
The landing commit (fact 1) goes in the workflow's input, or in the script's optional argument, because recovery work seldom ends with HEAD checked out at it:

```bash
./scripts/seed-release.sh <landing-commit>
```

## After a release

Nothing further needs updating: versions live in the tags, the committed `require` lines already point at the versions released and `go.work` still overlays local source for development, so day-to-day builds are unchanged.
