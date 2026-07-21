# Writing in the Supabase voice

<!-- cSpell:ignore Zhang -->

Write like a confident developer who respects the reader's time. Every sentence must earn its place.

## The voice

Supabase speaks as a peer, not a vendor. Direct. Technical without jargon. Action-oriented. Honest about tradeoffs.

**Sounds like:** "Build in a weekend. Scale to millions."
**Does not sound like:** "Unlock the power of our revolutionary platform to drive scalable developer experiences."

### Voice principles

- **Conversational authority.** You know the terrain. You are the experienced colleague at the next desk, leaning over to say "here's what actually works."
- **Show, don't tell.** Code examples beat explanations. When you must explain, be brief.
- **Respect developer intelligence.** Do not explain what npm is. Do not explain what a function is. Assume baseline technical competence.
- **States opinions without hedging.** "This doesn't work" not "this might not be the most optimal approach."
- **Acknowledges tradeoffs.** If read replicas don't solve analytical queries, say so. If there's a better tool for the job, recommend it.
- **Write with humility.** Credit open-source tools for what they do for us, not what we do for them. Avoid taking credit for things we did not build.

## Core rules

- **US English.** American spelling and grammar.
- **5th-6th grade reading level.** Short sentences. Easy to understand by everyone, including people new to the English language.
- **Oxford comma.** Always (a, b, and c).
- **Sentence case for headings.** "Set up authentication" not "Set Up Authentication."
- **Present tense, active voice.** "The function returns an error" not "The function will return an error."
- **One idea per sentence.** Break up compound sentences.
- **No em dashes.** Use commas, periods, or semicolons.
- **No emoji** in any content.
- **No italics for emphasis.** Use structure and word choice.
- **No horizontal rules (separators).** Never use `---`, `***`, or `___` to divide sections. Use headings instead.
- **Contractions are fine.** Use them (they're, we've, don't) except in error messages or reference docs.
- **Numbers:** spell out zero through nine, numerals for 10+.

## Progressive disclosure

Start broad, get specific. Do not front-load complexity.

1. What it does (one sentence)
2. When to use it (use case)
3. How to implement it (code)
4. Advanced options (if needed)

## Banned words

These never appear in Supabase content:

**Marketing fluff:** easily, simply, just, powerful, robust, seamless, leverage, utilize, enable, revolutionary, game-changing, empower, cutting-edge, innovative, supercharge, synergy, holistic, paradigm, disrupt/disruptive, reimagine, elevate, streamline

**AI vocabulary:** delve, dive into (and variants), unpack, harness, unlock (metaphorical), landscape (metaphorical), ecosystem (unless literal), navigate (metaphorical), foster, facilitate, crucial, pivotal, testament, tapestry, vibrant, intricate/intricacies, underscore, showcase, garnered, enduring

**Vague language:** things, stuff, ambiguous "it," there is/are (hides the subject)

**Filler phrases:** "please note that," "it should be noted," "in order to" (use "to"), "due to the fact that" (use "because"), "it's important to note," "it's worth mentioning," "furthermore," "moreover," "in the realm of," "plays a crucial role," "serves as a reminder"

## Banned constructions

These are fatal. Their presence means the text needs rewriting.

- "This isn't X. This is Y." (contrarian flip)
- "It's not X, it's Y." (same pattern, different punctuation)
- "Not only X, but Y" / "It's not just about X, it's Y"
- "of someone who" ("the hands of someone who...")
- "the kind of person who"
- Trailing participle clauses: "making it easier than ever," "enabling developers to," "emphasizing the importance of"
- False ranges: "from X to Y, from A to B"

## AI pattern detection

Scan all output for these common AI writing tells and rewrite on sight:

- **Significance inflation.** "Marks a pivotal moment," "stands as a testament," "a vital role." Cut it. State the fact.
- **Superficial -ing clauses.** "Highlighting the importance of," "underscoring its significance," "reflecting broader trends." Delete the clause. If the point matters, make it its own sentence.
- **Copula avoidance.** "Serves as," "stands as," "functions as." Use "is."
- **Synonym cycling.** Repeating the same idea with different words across sentences. Say it once.
- **Rule of three abuse.** Not every list needs three items. Say what needs saying.
- **Generic positive conclusions.** "The future looks bright." "Exciting times lie ahead." End with a specific next step or fact.
- **Excessive hedging.** "It could potentially be argued that this might have some effect." State what you mean.
- **Boldface headers in lists.** Do not start bullet points with bolded labels followed by colons unless the style guide structure explicitly calls for it.
- **Sycophantic tone.** "Great question!" "Absolutely!" Just state the answer.

## Capitalization and product names

- **Postgres**, not PostgreSQL
- **Supabase** (capitalize except in code)
- **Product names capitalized:** Database, Auth, Storage, Edge Functions, Realtime, Vector
- **Realtime** is the product name. Use "real-time" as an adjective ("real-time updates").
- **Open source** as a noun. "Open-source" as an adjective.

## Code formatting

- Inline code for technical terms: `supabase init`
- Code blocks for multi-line examples
- All code examples must use lowercase
- SQL statements formatted one statement per line
- Use realistic but fake data: diverse names (Sidney Jones, Zhang Wei, Maria Garcia), `example.com` emails, `YOUR_API_KEY` for placeholders

## Links

- Descriptive link text: "Read about [Row Level Security](link)" not "Click [here](link)"
- Maximum 15 links per page
- Do not link from headings

## Documentation tone

- Start with what it does, then how to use it
- Code examples for every concept, tested before publishing
- Task completion over comprehension: "Set up authentication in three steps" beats "Understanding Supabase Auth architecture"
- Include troubleshooting if common errors exist

## Self-check before publishing

Read through the final text and verify:

1. No banned words or constructions
2. No AI writing patterns (significance inflation, -ing clauses, synonym cycling, generic conclusions)
3. Opening gets to the point within two sentences
4. Every paragraph earns its place
5. Sentence lengths vary naturally
6. Active voice throughout (passive only when actor is irrelevant)
7. All claims are specific (numbers, names, dates)
8. Code examples are tested and lowercase
9. Headers are sentence case
10. No em dashes, no emoji, no italics for emphasis

## When to break these rules

Grammar serves clarity. If a rule makes writing less clear, break it.

- Sentence fragments are fine if meaning is obvious
- Starting with "and" or "but" is fine for emphasis
- One-sentence paragraphs are fine for impact
- No conversational asides in reference docs

The goal is clarity and respect for developer time. Everything else is negotiable.
