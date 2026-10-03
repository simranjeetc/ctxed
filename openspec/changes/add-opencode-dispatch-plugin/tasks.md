# Tasks

## 1. ctxed id-only output

- [ ] 1.1 Implement `prune --ids-only`, printing a JSON object whose `droppedIds` is the resolved drop set after orphan resolution; verify with a test that a category selection, an explicit id selection, and an orphan-resolved pair each yield the expected ids
- [ ] 1.2 Verify the empty case and determinism: no selection yields an empty array, and two runs are identical
- [ ] 1.3 Document `--ids-only` in the README and `docs/plugin-contract.md`; verify the documented invocation matches the implemented flag

## 2. Plugin

- [ ] 2.1 Scaffold `plugin/opencode/` with a pinned `@opencode-ai/plugin`, exporting a plugin that registers the context hook; verify OpenCode loads it without error
- [ ] 2.2 Implement the hook body: invoke ctxed in id-only mode with the configured source and selection, and parse `droppedIds`; verify with a stub ctxed binary that the parsed set matches
- [ ] 2.3 Implement filtering by id over the outbound messages, preserving order and unrelated messages; verify dropped ids are removed and others are untouched
- [ ] 2.4 Implement fail-open: on a stub that exits non-zero, hangs, or prints invalid JSON, the transcript is unchanged and the error is reported; verify each case with a test
- [ ] 2.5 Implement caching keyed by selection and session revision; verify a second dispatch does not re-invoke ctxed and a changed selection does
- [ ] 2.6 Implement configuration (ctxed path, session export, categories file, selection) from config/env, and document it; verify the documented config drives the plugin in the stub test
- [ ] 2.7 Add the concrete OpenCode example to `docs/plugin-contract.md`; verify it matches the implemented hook and flags

## 3. Integration and acceptance

- [ ] 3.1 End-to-end against a real OpenCode dispatch: configure a selection that drops a known message id, dispatch, and verify that message is absent from the request while the stored session is unchanged
- [ ] 3.2 Verify the plugin carries no policy: assert its source contains no category, selection, or tool-call/result validity logic
- [ ] 3.3 Verify `openspec validate add-opencode-dispatch-plugin --strict` passes and the two delta specs (new capability and the `context-dispatch-prune` addition) match the implemented behavior
