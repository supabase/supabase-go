# AI Agent Skills

## Overview

This folder holds the repository's guidance for AI coding agents as task-scoped skills.

This document has primarily been designed for consumption by human readers, explaining the conventions and philosophies used by this repository in respect of agent instructions.

There is deliberately no repository-root `AGENTS.md` defined as discovery is the skill mechanism itself, based upon the principle of **progressive disclosure** as described at [agentskills.io](https://agentskills.io/).

A skill is a unit of context loading. Agents retain every skill's `name` and `description` at all times, only loading a skill's body when the task matches and following deeper links only when needed.
Skills are therefore defined by activation moment (the kind of work being done right now), never by subject taxonomy.
This means that a new skill is only added when a genuinely new activation moment appears in day-to-day work.

## Vendor-agnostic

Nothing in this repository's tree is named for, or shaped around, any single AI vendor.
The skill format we use is as defined by [the Agent Skills Specification](https://agentskills.io/specification).
The result is that this folder is located and scanned natively by the large majority of current coding agents, being the vendor-neutral location for skills definition.

Tools that do not read this folder natively or out-of-the-box can be adapted via local user-level configuration or with untracked local symlinks, never by committing vendor-branded files or folders to this repository. At the time of writing the notable holdout is [Claude Code](https://www.anthropic.com/product/claude-code), which looks only for its own branded filenames (e.g. `CLAUDE.md` and `.claude/skills/`).

### Workaround for Claude Code

Consider adding the following to your `~/.claude/CLAUDE.md` user-level, persistent context:

```markdown
## Agent Skills in Vendor-Neutral Repositories

Some repositories contain no Claude-branded files and no root `AGENTS.md` file.
When a repository has a `.agents/skills/` directory, read the YAML frontmatter (`name` and `description`) of every `.agents/skills/*/SKILL.md` markdown file at the start of the session, then read a skill's full body before undertaking any work its description matches, exactly as native skill discovery would behave.
```

or create symlinks, bearing in mind these will need to be recreated when skills are added or removed in future:

```bash
mkdir -p .claude/skills
for skill in .agents/skills/*/; do
  ln -s "../../${skill}" ".claude/skills/$(basename "${skill}")"
done
```

## Keep it DRY

Skills route and documents explain.

Canonical knowledge should live in a single place. Examples include:

| Document | For | Defining |
| -------- | --- | -------- |
| [`standard.md`](../../standard.md) | What | Our normative Go SDK standard, defining "what good looks like". |
| [`DEVELOPMENT.md`](../../DEVELOPMENT.md) | How | Building, testing conventions, naming policy and supply-chain pinning. |
| [`decisions.md`](../../decisions.md) | Why | The What+Why record of development decisions. |
| [`scripts/`](../../scripts/) | Executable Truth | What a check does is defined by running it, so it can never drift from its documentation. |

This means that:

- A skill's body must not restate what it can point to in canonical documents.
- Skills are purely triggers that allow the agent to progress into the canonical sources.
- Only where genuinely additive, a skill might add an operational note for agents that exists nowhere else.
- The test for any line in this folder: it must say _when_ or _where_, never _what_ or _how_.
  A line that states a norm belongs in a canonical document, with a pointer from a skill hosted here.
- When guidance changes, the canonical document changes, and skills change only when the routing itself changes.

## Writing and Changing Skills

Keep them obvious, clean and simple:

- The frontmatter `description` is the routing function in third person, stating what the skill covers and precisely when to use it, naming the files, identifiers and verbs a task would actually contain.
- Descriptions should partition cleanly - if two skills could plausibly claim the same task then something is wrong.
- Names are gerund ('ing) phrases in full words, matching this repository's naming policy.
- Bodies are pointer lists of a dozen lines, nowhere near the specification's 500-line ceiling. A growing body is a sign content is leaking in that belongs in a canonical document.

Ideally avoid AI-generated skills, prefer to only document the non-inferable (that is, the AI can read the source code, so don't regurgitate) and keep "progressive disclosure" in mind when writing or modifying skills.
