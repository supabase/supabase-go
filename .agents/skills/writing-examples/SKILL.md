---
name: writing-examples
description: Guides the creation, update, merging and deletion of examples - the runnable Example functions that render on pkg.go.dev, the code snippets inside doc comments and the example programs under examples/. Use when adding an example, reworking one or deciding whether an existing example is replaced, upgraded, folded into a sibling or removed outright. Covers the removal test and when an example earns its place.
---

The following documents will be helpful:

| Document | For |
| -------- | --- |
| [`standard.md`](../../../standard.md) | The coverage requirement: runnable Example functions accompany doc comments where apt, compiled and verified by go test. |
| [`DEVELOPMENT.md`](../../../DEVELOPMENT.md) | The Commentary section's exception letting a runnable example frame a convention that signatures cannot show. |
| [`decisions.md`](../../../decisions.md) | The records "Placeholder credentials in rendered examples name the real key type" and "Example programs are non-published modules run by the integration tier". |

The doc comment on an example is a comment like any other, owned by the [`writing-commentary`](../writing-commentary/SKILL.md) skill.

## The Removal Test

An example is removed, as the codebase evolves and other examples come in, when it does one of these:

- **Scaffolding**: It shows only that construction succeeds, which every other example in its package does as its first line.
- **Test-shaped**: It asserts behavior that the doc comment already states and a unit test already covers, and nothing in it shows a caller what to do.
- **Superseded**: A later, more mature example shows the same thing better.
- **Near-duplicate**: Its code matches a neighbor's except for one call.

An example stays when it shows a distinct decision, a convention that signatures cannot show or the basic read on one of the two entry routes.

## Replacing and Upgrading

- Fold a near-duplicate into the sibling a reader reaches first, which keeps that sibling's name.
- When a removal leaves one suffixed example standing alone, the suffix distinguishes nothing and the survivor takes the plain name.
- go vet requires an example's name to map to a declared identifier, and an example's explanation sits in its doc comment rather than its body, because pkg.go.dev renders the doc comment as prose above the code.
