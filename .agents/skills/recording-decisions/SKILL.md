---
name: recording-decisions
description: Guides capturing development decisions. Use when a design choice is made, changed or reverted during any task, especially choices that diverge from convention or that a future maintainer is likely to ask "why?" about. Covers when an entry is warranted, the What/Why entry format, the present-tense-only rule and the expectation that entries land atomically with the change they record.
---

## Recording Location and Format

[`decisions.md`](../../../decisions.md) is the record:
Read its preamble for the What/Why format, the present-tense-only rule and the expectation that decision entries land atomically with the change they describe.
Existing decision entries establish the pattern to follow.

## Authoring Guidelines

Decision entries are records, not essays.
They must only state what they uniquely own, otherwise trusting the code as the canonical record of behavior, and trusting neighboring entries to tell their own stories.

Never re-tell a neighboring entry, never name it and never borrow its rationale.
If two entries need the same sentence, either the sentence belongs to one of them or the two entries are one decision.

The source code is the canonical record of what the implementation does and what APIs offer.
Never restate signatures, mechanics or behavior that is readable at source.
Decision entries record the choice and why it beat the alternative, nothing more.

Record, don't perform. No trailing clauses that admire the design, no coined aphorisms, no imagined reader journeys.
If a sentence still informs after its 'which proves / enables / pays off' clause is deleted, then delete that clause!

State the why once and stop: no rationale for the rationale, and what deliberately does not exist earns at most the one line that forbids it.

Prefer one representative example over an exhaustive enumeration, and avoid 'every' and 'all' unless totality is itself the decision.
This is very important for maintainability as the codebase grows, otherwise entries end up needing to be expanded in later unrelated iterations to fulfil a false sense of completeness.

Decision entry headings name the decision, and the body carries it.
Progressive disclosure applies to prose: the heading lets a reader decide whether to read the entry, the entry lets them decide whether to open the code, and no layer repeats the layer below.
