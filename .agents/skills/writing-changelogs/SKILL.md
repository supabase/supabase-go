---
name: writing-changelogs
description: Guides the writing and editing of changelog entries in the per-module CHANGELOG.md files. Use when a user-facing change needs an entry, when drafting or reshaping an Unreleased section, or when reviewing entries at release time. Covers the type headings, the entry shape and the succinctness bar - what an entry says and what it leaves to the docs.
---

The following document will be helpful:

| Document | For |
| -------- | --- |
| [`CHANGELOG.md`](../../../CHANGELOG.md) | The repository's changelog conventions: the deviations from Keep a Changelog, the unreleased-heading rules, the humans-and-machines philosophy and the entry shape with its exemplar. |

Each published module owns a `CHANGELOG.md` beside its code, and a user-facing change adds its entry alongside the change it describes.

The entry speaks to a consumer deciding whether and how to upgrade. One lead line names the surface, terse bullets name the exported identifiers, and everything discoverable from the identifiers themselves - signatures, field lists, wire behavior, doc-comment content - stays out. When more depth genuinely helps, link the canonical source rather than restating it.
