---
name: writing-commentary
description: Guides the content and style of all comments - Go doc comments on exported or internal identifiers, package overviews in doc.go, inline comments and comments in scripts, workflows, SQL and configuration files. Use when writing, editing or reviewing any comment on any surface, including comments authored in passing while implementing something else. Covers audience focus (API consumer versus codebase maintainer), contract-first phrasing and where rationale, history and cross-component notes belong instead.
---

The following documents will be helpful:

| Document | For |
| -------- | --- |
| [`DEVELOPMENT.md`](../../../DEVELOPMENT.md) | The Commentary section defining the normative rules for what any comment may say. For example: contract not rationale, self-contained at its boundary, no language tutoring, no roadmap narration, each fact stated once at the site that owns it. |
| [`decisions.md`](../../../decisions.md) | The destination for the "why" that comments must not carry: rationale, alternatives considered and history live there as What+Why records. |
| [`standard.md`](../../../standard.md) | The requirement that every exported identifier carries a Go-idiomatic doc comment with runnable examples where apt - the coverage rule that the Commentary section's content rules complement. |

When removing why-content from an existing comment, check whether [`decisions.md`](../../../decisions.md) already records it and move it there in the same change if not.

## Supabase Writing Skill

Defines the writing style to use, sometimes referred to as the "Supabase voice".

Available as a shared, internal skill (at the time of writing this, it is not open source or otherwise public).
Defines style guide rules, bans AI writing patterns, and sets the documentation tone.

If this is available to you then you must use it.

### Arbitrary Limits

The writing skill might define a maximum quantity of some concept in the writing, for example:

- the number of links allowed on a 'page'
- the number of bullets allowed in a list
- the number of characters allowed in a 'post'

These limits are usually arbitrary and subjective, applying to other content types rather than API commentary. Also, often the concept of a 'page' is difficult or impossible to define in this commentary writing context. For this reason, when you come across such limits, ignore them.
