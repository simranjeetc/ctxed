# Proposal

## Why

A long working session mixes several workstreams — an afternoon on the Claude
adapter, then the same session turns to Codex. Once the Claude work is finished,
its hundreds of messages are dead weight in every future turn, but they are far
too many to find and remove by hand. `ctxed` can already show and drop individual
entries; that does not scale to "drop the Claude part" without reading it.

What is missing is a high-level view. The tool should say *"this session is about
these three things"* and let you drop a whole thing by selecting it, not by
hunting indices. And it must not cost you the session: the point is to continue
the **same** session, in place, with a slimmer context — not to start a new one.
The harness already prunes automatically (DCP) but as a black box you cannot
steer. This change makes the choice legible and steerable: a model proposes the
categories, you adjust and pick, and the prune is applied at dispatch without
altering stored history.

## What Changes

- `ctxed categorize <session> [--model M]`: a model groups the session's entries
  into 2–5 high-level categories with short labels and stable entry ids; ctxed
  writes an editable categories file and prints a high-level table.
- You adjust that file (rename a category, move an entry, mark entries to keep)
  and select the categories to drop.
- `ctxed prune <session> …`: emits the pruned transcript for the outbound request
  on stdout, without touching the stored session.
- Model access through an OpenAI-compatible endpoint or a named command override,
  so a harness whose models are reachable only through a CLI still works and
  tests stay offline.
- A minimal, documented **plugin contract**: a harness plugin calls ctxed to get
  the pruned transcript and substitutes it at dispatch. All categorization,
  selection, and pruning logic lives in ctxed, so the same utility serves any
  harness and each plugin stays thin.
- Prune references use **stable message ids** (OpenCode `msg_…`, Claude `uuid`),
  not indices, so the dispatch hook filters the outbound transcript reliably.

Still deferred, unchanged: compression/summarization, and any concrete harness
plugin (the next change).

## Capabilities

### New Capabilities

- `context-model-transport`: reach a model for categorization through an
  OpenAI-compatible endpoint or a named command, with a deterministic offline
  path.
- `context-categorization`: group a session's entries into a small set of
  labeled, high-level categories keyed by stable entry ids, published as an
  editable file and read back after adjustment.
- `context-dispatch-prune`: given a session and a prune set, emit a structurally
  valid pruned transcript for the outbound request without modifying stored
  history.

### Modified Capabilities

_None. `add-ctxed-cli` is the unarchived file-based foundation; these are new
capabilities layered on it._

## Impact

- New internal packages: a model client (endpoint + command), a categorizer, and
  a dispatch-prune emitter; new `categorize` and `prune` subcommands.
- Runtime dependency: outbound HTTP to the configured model endpoint, only when
  categorizing without a command override.
- New `docs/plugin-contract.md`: the minimal call a harness plugin makes.
- No change to existing `inspect`/`drop` behavior or specs; they remain the
  file-based foundation this builds on.
- Out of scope: compression/summarization, automatic (unattended) pruning, a
  concrete harness plugin, and any mutation of a harness's session store.
