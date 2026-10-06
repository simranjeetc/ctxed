# Claude Code mods API — spike findings

Status: **verified empirically** (2026-10-06) against Claude Code 2.1.288 on this
machine, plus the official mods docs and the `.d.ts` Claude Code writes for this
build. This answers the open questions in
[`live-context-prune-decisions.md`](live-context-prune-decisions.md) §6 and closes
the implementation-shaping risks in the D3b half of
`add-claude-code-live-prune`.

**Headline: D3b is real and buildable.** The decisions doc calls the mod API
"early access, v2.1.287+". It is now generally available: Claude Code shipped
**Claude Mods** in 2.1.287 (2026-10-01), enabled by default, and this machine
runs 2.1.288. The prior session deferred D3b because the API could not be
verified; it can now.

## What a mod is, concretely

A mod is a plugin directory with an entry module:

```text
mod/
├── .claude-plugin/
│   └── plugin.json        # manifest; mods add no required fields
└── hooks/
    ├── hooks.json         # { "modules": ["./register.js"] }
    └── register.js        # export function register(on) { on(event, [matcher], hook) }
```

Each hook is `async ($, e, next) => …`: `$` is the mods API, `e` the event input
as deeply frozen plain data, `next(e)` the middleware continuation. Returning an
object answers the event without calling `next`.

## `session.compact` is the message-replacing seam

From the types Claude Code 2.1.288 writes to
`.claude-plugin/types/claude-code/index.d.ts` for the installed build (this is the
authoritative contract; the public reference's one-line `{ skip: reason }` return
is incomplete):

```ts
export type SessionCompactTrigger = 'manual' | 'auto' | 'plugin' | 'precompute';

export type SessionCompactInput = {
  trigger: SessionCompactTrigger;
  agentId?: string;        // absent for the main conversation
  instructions?: string;   // the text after /compact
  messages: readonly SessionMessage[];  // the transcript, each with the engine's `handle`
};

export type SessionMessage = {
  role: 'user' | 'assistant';
  text: string;
  toolUses: ToolUseSummary[];
  toolResults?: ToolResultSummary[];
  handle?: string;         // opaque; present on messages handed to a compact hook
};

// What a hook returns / next(e) resolves to:
export type SessionCompactResult =
  | { messages: readonly SessionMessage[]; tokensBefore?: number; tokensAfter?: number; usage?: ModelUsage; skip?: undefined }
  | { skip: string; messages?: undefined };
```

The contract states the two facts D3b rests on:

- **`messages` is writable.** `next({ ...e, messages })` changes what is
  summarized; a hook may also answer with its own `{ messages }` list.
- **Handles round-trip.** "A message with the engine's `handle` stands as the
  engine has it; one without is built from its `role`, `text` and tool blocks."
  So a kept message handed back with its handle is the engine's own, whole — the
  hook does not have to rebuild messages.

`$.session.compact({ instructions })` triggers the same event with `trigger`
`plugin`, "the same call `/compact` makes, between turns"; it rejects while a
turn runs.

## Answers to the §6 open questions

- **Does `$.session.compact()` install its result live, between turns?** The
  contract says yes: a compaction that stands replaces the transcript, and a hook
  that answers without calling `next` supplies the final list. *Not yet confirmed
  end-to-end in a live interactive session* (task 3.3); the mechanism is proven
  below via the offline test kit, and the contract is explicit.
- **Does the hook run for the main conversation, or only subagents?** It runs for
  whatever is compacting. `agentId` is "absent for the main conversation (a
  subagent's or a fork's own transcript)". `e.messages` is that loop's
  transcript; `$.session.messages()` reads the main conversation. So the main
  conversation is compacted through the same hook, with `agentId` absent.
- **Does the `/compact` instruction steer retention?** (D3a) unchanged — it stays
  model-mediated; D3b is the exact path.

## Verification tooling (all offline)

| Command | What it proves |
| --- | --- |
| `claude plugin validate <dir>` | Manifest + static analysis of the module: prints the events it hooks and the `$.` calls it makes. No session. |
| `claude plugin test` | Loads the mod and fires events through it with no session, sign-in, or network. Fires `session.compact` with an engine-shaped input. |
| `claude --plugin-dir <dir> …` | Loads for one session and writes `.claude-plugin/types/` for the installed build. |

`claude plugin test` fires `session.compact` by calling `$.session.compact(input)`
with the transcript:

```ts
const r = await $.session.compact({
  trigger: 'manual',
  messages: [
    { role: 'user', text: 'keep me', toolUses: [], handle: 'h1' },
    { role: 'assistant', text: 'DROP me', toolUses: [], handle: 'h2' },
    { role: 'user', text: 'keep me too', toolUses: [], handle: 'h3' },
  ],
})
```

A probe hook that answers `{ messages: e.messages.filter(m => !m.text.includes('DROP')) }`
resolved to the two kept messages, **with `handle` `h1` and `h3` intact**:

```text
RESULT [{"role":"user","text":"keep me","toolUses":[],"handle":"h1"},
        {"role":"user","text":"keep me too","toolUses":[],"handle":"h3"}]
(pass) a hook drops a chosen message and keeps the rest with handles
```

That is the exact operation D3b needs, proven without a live session.

## Machine-specific blocker (and the workaround)

A normal session on this machine currently refuses to load the hooks module:

```text
hooks module not loaded: hooks modules are turned off for installed plugins in
this process: the rollout switch was saved off by an earlier session and is not
refreshed yet; built-in plugins load regardless
```

This is the engine's crash kill-switch (`modsOffAt`), persisted when the hooks
worker crashed in an earlier session; the GrowthBook gate itself is on
(`tengu_plugin_hooks_modules: true`). `claude plugin validate` and
`claude plugin test` are unaffected. A session loads mods when hooks run in the
same thread:

```sh
CLAUDE_CODE_HOOKS_SAME_THREAD=1 claude -p "/probe" --plugin-dir ./mod   # prints the mod's output
```

So a live load is verifiable headlessly today; the default worker mode needs the
switch refreshed (a clean session, or `/reload-plugins`).

## Impact on `add-claude-code-live-prune`

- **D3b stays as specified**, now on a GA API. Tasks 2.1, 2.2 and 2.4 are
  mechanistically verified offline (load, event contract, drop-with-handles);
  only the live-session install (2.2's last clause, 3.3) is outstanding.
- **One design decision remains — the bucket ↔ live-message mapping.** A live
  `SessionMessage` carries only `role`, `text`, `toolUses`/`toolResults` and an
  opaque `handle`; it has **no stored transcript uuid**. A categories file from
  `ctxed categorize` refers to entries by their adapter ids (the `.jsonl` uuids),
  so the mod cannot map live messages to buckets by id. The mapping must be
  content-based — re-classify the live message list into the existing bucket
  labels (e.g. `$.model.classify`), or match message text against the categorized
  entries. This is the open choice for task 2.3 and should be decided before the
  mod is written.
- **`precompute` is a real alternative.** Its result installs nothing immediately;
  it is kept for the compaction that comes. A mod may prefer to precompute the
  pruned list and let the engine's own compaction install it.
- The public mods reference (v2.1.289) under-documents the `session.compact`
  return; trust the generated `.claude-plugin/types/` for the installed build.
