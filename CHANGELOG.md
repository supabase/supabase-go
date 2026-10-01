# Supabase Go SDK Changelog

This is a multi-module repository containing discrete Go modules, which in aggregate form the Supabase Go SDK.
Each module defines its own changelog file.
The format of each of those changelog files is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), with modules adhering to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

The canonical list of modules published to form this SDK is defined in [`go.work`](go.work).
For convenience, here are links to each of their changelog files:

- [`auth`](auth/CHANGELOG.md)
- [`core`](core/CHANGELOG.md)
- [`postgrest`](postgrest/CHANGELOG.md)
- [`supabase`](supabase/CHANGELOG.md)

## Deviations from Keep a Changelog

### Avoid

Our changelog files **DO NOT**:

- format the unreleased heading with enclosing square brackets, nor do they define a reference-style link for `unreleased` at the bottom of the file. The unreleased heading is always simple, unformatted text in the form:
  ```markdown
  ## Unreleased
  ```
- embed links into any headings, as this is confusing in the GitHub viewing context, prompting the reader to wonder if a click would link to that heading or the underlying diff view.
- include links to GitHub-rendered diffs, and thus do not embed reliance on GitHub's `org/repo/compare/X...Y` endpoints.

### Humans and Machines

Our changelog files are also not _just_ designed for humans. Keep a Changelog [states](https://keepachangelog.com/en/1.1.0/#who):

> **Who needs a changelog?**  
> People do. Whether consumers or developers, the end users of software are human beings who care about what's in the software. When the software changes, people want to know why and how.

We completely agree, however we also understand that our changelog files are increasingly read by and relied upon by AI builders, as a source of truth and aggregate index in the spirit of [progressive disclosure](https://agentskills.io/specification#progressive-disclosure). This is also true.

Therefore we aim to write our changelog entries so that they can be parsed by humans and machines alike, using natural language, avoiding overly mechanized content formatting or going into too much detail that ends up presenting information that could already be gleaned relatively quickly from other source files. A succinct and snappy synopsis that contains links to canonical sources of more detailed information is the sweet spot.
