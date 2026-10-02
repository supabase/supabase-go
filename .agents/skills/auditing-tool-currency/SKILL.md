---
name: auditing-tool-currency
description: "Guides auditing the repository's externally sourced, version-pinned tooling against canonical origins - the GitHub Actions pins, the Go tool module under tools/go, the npm tool module under tools/node, the pinned Supabase CLI and the Go consumer floor. Use when asked which tools could be upgraded, whether pins are stale or to prepare an upgrade report or plan. Audit and report only: applying a bump is the changing-build-and-continuous-integration skill's territory."
---

Run [`scripts/tools-audit.sh`](../../../scripts/tools-audit.sh) first: it is the executable inventory of every pin, reporting current against latest from each canonical origin with the bump route and release-notes link for each, and it never modifies anything.

The following documents will be helpful:

| Document | For |
| -------- | --- |
| [`DEVELOPMENT.md`](../../../DEVELOPMENT.md) | The audit script's scope and the pins it cannot see, the supply-chain pinning approach and per-tool upgrade procedures such as the pinned Supabase CLI bump. |
| [`decisions.md`](../../../decisions.md) | Why the update cadence is manual rather than Dependabot-driven and why everything executed from outside the repository is digest-pinned. |

When asked for an upgrade report or plan, follow the release-notes links the script prints for each candidate and summarize what the newer version offers (features, fixes, security advisories) and any breaking changes, placing the script's bump route alongside. Recommend only: never apply an upgrade as part of this activity.
