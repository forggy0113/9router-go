### Added — Per-model custom request body for OpenRouter custom models

- A custom model can now carry an `extraBody` JSON object (Add Custom Model
  modal → "Custom request body (JSON, optional)", shown for OpenRouter only).
  The gateway merges it into every OpenAI-format request it sends to
  `openrouter` for that model, so operator routing knobs (`provider.order`,
  `reasoning`, `transforms`, ...) apply without the client sending them.
- Merge rule: the operator's value wins on a shared top-level key; only the
  `provider` object is deep-merged; `reasoning` and arrays are replaced whole.
- Save is refused with `400 {"code":"INVALID_EXTRA_BODY"}` when the row's
  provider does not resolve to `openrouter`, the value is not a JSON object,
  it exceeds 8 KB, or it sets a reserved key, compared case-insensitively
  (`model`, `models`, `messages`, `input`, `prompt`, `stream`,
  `stream_options`, `tools`, `tool_choice`, `functions`, `function_call`,
  `response_format`). The same denylist is re-applied at merge time, so an
  imported row cannot override them either. The stored extraBody keeps the
  request's exact bytes, so large integers (e.g. a 17-digit `seed`) are not
  rounded.
- The lookup is an in-memory index built from the `customModels` kv scope and
  invalidated (generation counter, so a racing build is never cached) on every
  custom-model write, provider-node rename/delete and database import — no
  SQLite read per request; a failed read is retried, not cached as empty.
  A client body the merge cannot decode is forwarded unchanged. Other
  providers, including a sibling with the same model id, are untouched (§3
  provider isolation).
- Divergence: no merged `decolua/9router` equivalent exists; this is a
  9router-go addition.
- Verified by db round-trip, dashboard validation, merge unit tests and an
  `integration` test that asserts the fake OpenRouter upstream receives the
  merged body (direct and through a combo) while a sibling provider does not.
- Every custom model row on a provider page now has an Edit button that
  reopens the Add Custom Model modal in edit mode (id and type locked,
  fields prefilled, extraBody pretty-printed). Save re-posts the full stored
  row under the same key, so `name` and other fields survive; blanking the
  request body field sends `extraBody: null`, which clears it. Compatible-node
  rows use a separate list and are not editable this way.
- Fix: deleting a custom model (row X, trash, or "Delete selected") now removes
  it instead of moving it to "Disabled models"; the id the delete endpoint
  parks in `disabledModels` is pruned. Built-in models keep disable semantics.
  Pre-existing (X button since 4f4b4aeaf, endpoint disable-append in 13cce967).
