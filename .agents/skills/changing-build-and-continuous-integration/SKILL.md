---
name: changing-build-and-continuous-integration
description: Guides changes to this repository's build, module and CI machinery rather than the SDK's own code. Use when touching any go.mod, the go.work workspace, the scripts folder, GitHub Actions workflows, the pinned tool modules under tools or the spell-check configuration - including adding a new module, taking on a new dependency or bumping a Go or tool version. Covers workspace wiring for unpublished sibling modules, the published Go version floor versus the CI toolchain, digest pinning, the first-party-actions-only policy and the rule that CI jobs run the same scripts developers run locally.
---

The following documents will be helpful:

| Document | For |
| -------- | --- |
| [`DEVELOPMENT.md`](../../../DEVELOPMENT.md) | How to build, test, lint and spell-check locally with the same scripts CI runs, the one-time npm tooling setup and the supply-chain commit-SHA/checksum pinning approach. |
| [`decisions.md`](../../../decisions.md) | Deep dive commentaries with both _What_ and _Why_ defined for key decisions that shape the structure of this codebase, including the build and verification machinery. Presented as a series of lightweight Architectural Decisions Records (ADRs). |
| [`standard.md`](../../../standard.md) | The canonical definition of "what good looks like" including authoritative guidance on dependencies, licensing, minimum Go version, module versioning and distribution. |

Continuous Integration (CI) runs on GitHub's platform - inspect [`.github/workflows/`](../../../.github/workflows/) for details, including how commands are directed via [`scripts/`](../../../scripts/) so that local developers can run exactly what runs in CI.

A new or changed convention should also trigger the `recording-decisions` skill.
