---
name: code-review
description: Repo-specific checklist for reviewing argo-rollouts changes — reconciler correctness, API/CRD compatibility, generated-code hygiene, test coverage expectations, and provider parity. Use when reviewing a PR or diff, or self-reviewing before pushing.
---

# Reviewing Argo Rollouts Changes

Apply general review judgment plus these repo-specific checks.

## Reconciler correctness (highest-value area)

- **Idempotence**: would a second sync with the post-change state mutate anything again?
  Reconcile must converge; look for unconditional writes, or decisions based on values
  the same sync just wrote.
- **Informer cache staleness**: objects from listers are shared and possibly stale.
  Flag mutation of lister-returned objects without `DeepCopy()`, and logic that assumes
  a just-created object is immediately visible in the cache.
- **Status handling**: status changes must flow through the existing patch path in
  `rollout/sync.go` (`persistRolloutStatus`) — not ad-hoc `Update` calls.
  `observedGeneration` and conditions should stay consistent with the new behavior.
- **Abort/pause/promote paths**: any change to canary/bluegreen progression needs to be
  checked against abort, retry, manual promote, and rollback-within-window flows, not
  just the happy path.
- **Requeue behavior**: `enqueueRolloutAfter` durations and rate-limiter interactions —
  watch for hot loops (requeue every sync) or lost wakeups (state that changes with no
  event and no requeue).

## API / compatibility

- Changes to `pkg/apis/rollouts/v1alpha1/types.go`: additive only; correct
  json/protobuf tags; `+optional` markers; field numbers unique and never recycled;
  validation added in `pkg/apis/rollouts/validation/`; defaults in `utils/defaults`.
- If `types.go` changed, the diff **must** include regenerated code (deepcopy, proto,
  openapi, client, CRDs in `manifests/crds/`) — a types-only diff means codegen was
  skipped. Conversely, hand edits inside generated files are a red flag (see the
  `refactoring` skill for the list of generated paths).
- Behavior changes affecting existing rollouts mid-upgrade (hash computation,
  annotations, scaling defaults) need explicit justification and upgrade notes.

## Tests and docs

- Controller changes need fixture tests asserting the exact action/patch sequence
  (see `testing` skill); bug fixes need a test that fails without the fix.
- New user-facing behavior needs docs under `docs/` (mkdocs site) and, if it adds
  spec fields, examples in `docs/features/` and possibly `examples/`.
- E2E coverage expected for new strategies/steps/traffic behavior (`test/e2e/`).

## Provider parity and plugins

- A fix in one traffic router or metric provider often applies to its siblings —
  check whether the same bug exists in the other `rollout/trafficrouting/*` or
  `metricproviders/*` implementations and say so in the review.
- Plugin interface changes (`rollout/steps/plugin`, `metricproviders/plugin`,
  `rollout/trafficrouting/plugin`) are cross-process RPC contracts: breaking them
  breaks third-party plugins; mocks must be regenerated.

## Hygiene

- Errors wrapped with context; no swallowed errors in reconcile paths (returning nil
  on error usually means the rollout silently stalls).
- Logging via the structured logger with rollout/namespace fields, at an appropriate
  level (debug for per-sync noise).
- No `time.Sleep` in tests; use existing time injection (`utils/time`).
- DCO sign-off (`Signed-off-by`) is required on commits in this project.
