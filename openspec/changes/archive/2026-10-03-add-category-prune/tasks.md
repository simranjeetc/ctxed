# Tasks

## 1. Model transport

- [x] 1.1 Implement the model client with an OpenAI-compatible endpoint (base URL, key, model from flags/env) and a `--categorizer-cmd` override that takes precedence; verify the command path makes no network call and the endpoint path sends the prompt and returns the response
- [x] 1.2 Implement the bounded-input limit that errors instead of truncating; verify an oversized input exits non-zero, states the limit, and sends nothing
- [x] 1.3 Verify missing credentials (no endpoint, no command) exits non-zero with a clear message and produces no categories; cover it with a test using an unset environment
- [x] 1.4 Document the model/command configuration in the README; verify the documented offline path (`--categorizer-cmd`) runs with no network

## 2. Categorization

- [x] 2.1 Define the categories model (labeled categories of stable entry ids with entry counts and token totals) and parse the model's response into it; verify a fixed model response yields the expected categories
- [x] 2.2 Implement assigning every entry to exactly one category (or an explicit uncategorized group) and enforce the configured maximum category count, at least one and at most five by default; verify the bound is reported and not exceeded
- [x] 2.3 Implement stable entry ids: use the harness id when present and a deterministic fallback otherwise; verify the same session yields the same fallback ids across runs and that membership is expressed only as ids
- [x] 2.4 Implement `ctxed categorize` writing the editable categories file and printing the high-level table (label, entries, tokens); verify the file round-trips and the table shows a label and token total per category
- [x] 2.5 Verify categorize never prunes: snapshot the session before and after; assert nothing changed and no entries removed
- [x] 2.6 Document categorization and the categories file format in the README; verify the documented commands run against a committed fixture with a fixed-output command

## 3. Selection and read-back

- [x] 3.1 Implement reading an edited categories file back, honoring renamed labels and moved membership; verify an adjusted file changes the selection as written
- [x] 3.2 Implement validation on read-back: an id not present in the session is a hard error naming it; verify with a file containing a bogus id
- [x] 3.3 Implement selection by category and by explicit ids; verify each selects exactly the intended entries

## 4. Dispatch prune

- [x] 4.1 Implement the prune emitter: given a session and a prune set, emit the transcript with those entries excluded on stdout, diagnostics on stderr, stable exit codes; verify the stored session is byte-for-byte unchanged and the session id is unchanged
- [x] 4.2 Implement structural-validity resolution (a result whose call is excluded is excluded too, or the pair is retained) and report the adjustment; verify with a fixture containing a split call/result pair
- [x] 4.3 Verify determinism: emitting the same session and prune set twice yields identical output
- [x] 4.4 Verify id-based application against a reordered/outbound-shaped message list, proving the prune follows ids and not positions

## 5. Plugin contract

- [x] 5.1 Write `docs/plugin-contract.md` specifying the exact inputs a plugin provides (session source, prune set) and consumes (pruned transcript), plus the per-harness mapping notes for OpenCode and Claude Code
- [x] 5.2 Verify the contract is sufficient by writing a throwaway test that plays the plugin role: feed a session and prune set, receive the transcript, and map it for each harness shape, with no logic beyond substitution

## 6. Integration and acceptance

- [x] 6.1 End-to-end on the committed fixtures with a fixed-output command: categorize, adjust the file, prune, and confirm the excluded entries are absent and the included ones intact
- [x] 6.2 Verify a new harness is a thin caller: the test-only plugin in 5.2 contains no categorization or validity logic
- [x] 6.3 Verify the change validates with `openspec validate add-category-prune` and that the three delta specs match the implemented behavior
