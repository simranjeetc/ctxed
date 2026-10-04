# Tasks

## 1. ctxed inputs the workflow needs

- [x] 1.1 Implement `prune --ids-only`, printing a JSON object whose `droppedIds`
      is the resolved drop set after orphan resolution; verify with a test that a
      category selection, an explicit id selection, and an orphan-resolved pair
      each yield the expected ids
- [x] 1.2 Verify the empty case and determinism: no selection yields an empty
      array, and two runs are identical
- [ ] 1.3 Accept a transcript on stdin for `categorize` (and `prune`), so the
      plugin can categorize the live messages without exporting the session;
      verify with a test that stdin and a file path produce the same result
- [ ] 1.4 Document `--ids-only` and the stdin transcript path in the README and
      `docs/plugin-contract.md`; verify the documented invocations match the
      implemented flags

## 2. Plugin: in-session categorize and select

- [ ] 2.1 Scaffold `plugin/opencode/` with a pinned `@opencode/plugin` (the v2
      API that exposes `session.hook` and command registration); verify OpenCode
      loads it without error
- [ ] 2.2 Register an in-session command that serializes the live messages,
      hands them to ctxed on stdin, gets the buckets, and presents them with
      labels; verify with a stub ctxed
- [ ] 2.3 Record the user's bucket selection as the session's active selection
      (ids remain internal); verify the selection is stored and readable
- [ ] 2.4 Implement fail-open on the command path: a ctxed failure leaves the
      session unchanged and reports the error; verify with a stub

## 3. Plugin: apply the selection at dispatch

- [ ] 3.1 Implement the hook body: re-derive the dropped set over the live
      transcript via ctxed in id-only mode with the active selection, and parse
      `droppedIds`; verify with a stub ctxed
- [ ] 3.2 Filter by id over the outbound messages, preserving order and
      unrelated messages; verify dropped ids are removed and others are untouched
- [ ] 3.3 Implement fail-open on the dispatch path: on a stub that exits
      non-zero, hangs, or prints invalid JSON, the transcript is unchanged and
      the error is reported; verify each case
- [ ] 3.4 Cache the dropped set keyed by selection and session revision; verify a
      second dispatch does not re-invoke ctxed and a changed selection does
- [ ] 3.5 Implement configuration (ctxed path, categorizer model/transport,
      selection) from config/env, and document it; verify the documented config
      drives the plugin in the stub test
- [ ] 3.6 Add the concrete OpenCode example to `docs/plugin-contract.md`; verify
      it matches the implemented hook, command, and flags
- [ ] 3.7 Verify the plugin carries no policy: assert its source contains no
      categorization, bucket-assignment, selection-resolution, or
      tool-call/result validity logic

## 4. Functional verification (the gate)

- [ ] 4.1 `scripts/verify-functionally.sh --opencode` creates a real session via
      `opencode run`, runs the in-session categorize+select, dispatches, and
      observes the outgoing request
- [ ] 4.2 Assert the selected bucket is absent from the request and the stored
      session is unchanged
- [ ] 4.3 Assert a message added after selection in a dropped bucket is also
      absent (the anti-drift requirement)
- [ ] 4.4 Assert live message ids equal the ids ctxed categorizes over (the id
      parity risk); fail loudly if not, and switch to content/tool-id matching
- [ ] 4.5 Verify `openspec validate add-opencode-dispatch-plugin --strict` passes
      and the delta specs match the implemented behavior
