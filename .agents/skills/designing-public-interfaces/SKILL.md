---
name: designing-public-interfaces
description: Guides design of this SDK's public API surface. Use when adding, renaming, removing or reshaping any exported identifier in any module published as end-user, consumer-facing interface - whether that module be existing or new to the published surface area. Includes functional options, domain client accessors, error model, sentinel errors, error struct types, exported fields, the accessor pattern, constructor signatures and naming policy.
---

The following documents will be helpful:

| Document | For |
| -------- | --- |
| [`standard.md`](../../../standard.md) | The canonical definition of "what good looks like" including authoritative guidance on encapsulation, immutability, API stability, third-party dependencies, idiomatic design, goroutines, channels, context, functional options pattern (FOP), background work ownership (close/shutdown requirements), mockability, error hygiene, logging, observability, zero values, documentation and examples. |
| [`decisions.md`](../../../decisions.md) | Deep dive commentaries with both _What_ and _Why_ defined for key architectural decisions that shape the structure of this codebase, including how we design our public APIs such as our error model, doc-comment syntax, the HTTP customization seam (`HTTPClient`) and shared modules. |
| [`DEVELOPMENT.md`](../../../DEVELOPMENT.md) | Includes a comprehensive section on Naming. |

We must aim for consistency so patterns that already exist in this codebase should be considered for similarity and thus applicability for any new API design.

The interior counterpart of this skill is `designing-internal-boundaries`, which governs how the types behind the exported surface keep their contracts compiler-enforced.

A new or changed convention should also trigger the `recording-decisions` skill.
