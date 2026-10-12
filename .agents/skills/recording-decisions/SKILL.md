---
name: recording-decisions
description: Guides when a development decision earns a decisions.md entry and how entries are proposed and written. Use when a design choice is made, changed or reverted during any task. Never write decisions.md unprompted - a candidate entry is proposed in chat through the three-part falsification test (maintainer question, answer-from-source attempt, why the answer fails) and only a commissioned entry is written. Covers the false-trigger list and the What/Why authoring rules.
---

## Who Holds the Pen

Never write to [`decisions.md`](../../../decisions.md) on your own initiative.
When you believe an entry is earned, present the falsification test's three parts in chat and stop.
The user commissions the entry or dismisses it, and only a commissioned entry is written, landing atomically with the change it records.

## The Falsification Test

Your default is No Entry: most changes, including large refactors and corrections, warrant nothing.
A proposal is exactly three parts:

1. **The question**: the precise "why?" a future maintainer would ask of this change.
2. **The answer attempt**: your best good-faith answer using only the finished diff - identifiers, types, package boundaries and comments.
3. **The failure**: why that answer is wrong or incomplete.

Run the test only against finished code, never at planning time - a plan may say "evaluate against the finished diff", never "will gain an entry".
If the answer attempt is even halfway coherent, there is no entry and nothing to propose.
An entry earns its place only by capturing what source cannot show, typically the alternative that lost and why it lost.

## False Triggers

None of these is evidence an entry is earned, however strongly it feels otherwise:

- hard deliberation, or a reversal along the way
- a mistake corrected
- a principle or policy articulated during review
- strong reviewer emotion
- a plan that promised an entry

## Authoring a Commissioned Entry

Read the `decisions.md` preamble for the What/Why format, the present-tense-only rule and atomic landing. Existing entries establish the pattern.

An entry states only what it uniquely owns: code is the canonical record of signatures, mechanics and behavior, and neighboring entries tell their own stories - never restate the former, never re-tell nor borrow from the latter.
If two entries need the same sentence, they are one decision or the sentence belongs to one of them.
Record, don't perform: no clauses admiring the design, no coined aphorisms, and the why stated once - what deliberately does not exist earns at most the one line that forbids it.
Prefer one representative example over enumeration, avoiding "every" and "all" unless totality is itself the decision.
The heading names the decision, the body carries it, and no layer repeats the layer below.
