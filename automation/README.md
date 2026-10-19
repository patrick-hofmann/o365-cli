# External mail automation

This package owns mail ingestion, conversation grouping, source-bound analysis,
attachment extraction and the bounded knowledge recipe. It is delivered from the
mail-tool repository and must not be bundled into OpenApe Pods. The desktop only
provides generic program execution, assigned files, model calls and shared data.

Build with Node 24 or newer: `npm ci && npm run build && npm test`. The package
is an unpublished implementation candidate. Installation and live recipe
activation are separate rollout steps.

Use `runMailKnowledge(context, options)` from an independently pinned dependency.
Options select the mail application, parser application, exact mailbox/folders,
attachment/history scope, and the parser worker path/digest. The recipe invokes
`o365-cli pods read` through `context.tools.invoke`; output is a bounded hashed
artifact, never an oversized service frame. It uses no `context.mail` or custom
approval API. The model receives untrusted evidence and no tools. Findings must
cite exact retained text; unsupported material remains a gap.

The separately assigned parser is built under `dist/parser`. After placing it
in its permanent owner-controlled installation directory, run
`node scripts/parser-runtime.mjs` and assign the resulting `o365-extract` with
its `runtime.json`. The runtime fixes the heap to 192 MiB. Calls request temporary
isolation, pin the sole source file, have no network or Pod identity, and stop
after 15 seconds. The parser verifies the source and PDF worker hashes and bounds
output to 256 KiB. Assign the parser directory read-only. A new parser build
requires new file bindings; do not rewrite an existing assigned installation.

Knowledge is stored separately in `workspace/mail-knowledge/knowledge.sqlite`
and immutable hashed blobs. Scope/binding changes select new processing receipts
without destroying history. Export/import of existing desktop knowledge must be
rehearsed before activating this recipe. Historical archive decisions are handled
by the Go tool's `workflow archive-import` and shared mailbox journal, never by
this analysis package. Do not infer archival permission from a classification.

The preserved ingestion/knowledge/parser tests use synthetic data. Native
execution, output limits and sandbox confinement require the companion ape-shell
and desktop tests; unit success alone does not prove a complete rollout.

## Historical migration and reconciliation

With the installed desktop stopped for the final cutover (read-only rehearsal can
run beforehand), export schema 52 with `node scripts/migrate.mjs export
CANONICAL_PROFILE FRESH_EXPORT`. Inspect `manifest.json`; keep its exact SHA-256.
Import each reviewed account file with `o365-cli workflow archive-import --account
ACCOUNT --source FILE --source-sha256 SHA256 --journal SHARED_PRIVATE_DIRECTORY`.
Import each selected knowledge namespace with `node scripts/migrate.mjs
import-knowledge EXPORT SNAPSHOT_SHA256 POD_ID FRESH_DESTINATION`. Place that
completed standalone store at the assigned recipe workspace only at rollout.
Imports preserve original rows, revisions, sources and citations. They neither
activate recipes nor create execution authority. Failed imports stay unactivated.

`workflow archive-inspect --account ACCOUNT --journal DIRECTORY` shows original
receipts and their canonical digests. Explicit reconciliation uses `workflow
archive-reconcile --account ACCOUNT --journal DIRECTORY --message MESSAGE
--source SOURCE --operation ORIGINAL_OPERATION --receipt-sha256 DIGEST --outcome
archived|notApplied --evidence OBSERVATION`. The IdP requires a fresh exact once
decision. The CLI appends the observation, preserving the original receipt.
It performs no provider call. An old operation is never replayed; confirmed
non-application only permits a new operation with a separate authorization.
Every unresolved or consumed historical hold for that message must be reviewed.
Journal schema 4 is refused by older binaries; retain the pre-cutover backup
instead of opening a new journal with an old tool.

## Delivering the candidate

`npm run build` also produces `dist/recipe/entry.mjs` and its SHA-256 manifest.
Copy that immutable bundle into a reviewed Pod script draft; no npm publication
or mail implementation in the desktop is needed. Configure `mailbox`, `folders`
(JSON array), `attachments` (`on` or `off`), optional `since`, `mail-application`,
`parser-application`, `parser-worker` and `parser-worker-sha256`. Assign the mail
CLI, parser and exact directories before validating the draft. Validate without
provider calls, then activate only under the separately approved rollout.

The protected archive command requires the matching registry adapter and IdP
single-use enforcement before any existing raw-move recipe is enabled. Do not
activate against an older registry or accept an untested executable digest as
release evidence. Record the built CLI SHA-256 in the registry's tested digests,
exercise `review-archive`, refusal of raw move and exact once authorization with
synthetic fixtures, and pin the CLI, adapter, recipe and parser manifests together.
The provider's final preflight and move are separate requests; this preserves the
existing change/folder checks but does not promise an atomic provider compare-and-move.

## Shared label decisions

Independent recipes use `workflow label-snapshot`, perform their assessment, then
call `workflow decide-label` with a stable retained operation ID and a declared
category dimension and eligibility gate. All writers and protected archivers
must receive the exact same private journal directory. Label intent, provider
readback and explicit uncertainty reconciliation live beside historical archive
receipts in journal schema 4. Old raw `workflow categorize` and `workflow move`
commands refuse execution. Personal policies, prompts and recipe state remain
outside this product repository.

For thread assessment, `workflow thread-evidence` returns immutable full-body
pages with exact Sent Items identity and draft status. Follow all continuation
pages before calling the evidence complete. Neither paging nor the local lock
makes external Outlook changes atomic. See the CLI README for the request,
gate, receipt and operator reconciliation contracts.
