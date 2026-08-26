---
name: refactoring
description: Guardrails for refactoring the argo-rollouts codebase safely — what is generated vs hand-written, API compatibility constraints, package layering, and the verification loop. Use when renaming, moving, extracting, or restructuring code, or cleaning up an area you're already touching.
---

# Refactoring in Argo Rollouts

## Never hand-edit generated code

A large fraction of this repo is generated. If a refactor touches these, change the
source and regenerate (see the `codegen` skill) instead of editing outputs:

- `pkg/apis/rollouts/v1alpha1/`: `zz_generated.deepcopy.go`, `generated.pb.go`,
  `generated.proto`, `openapi_generated.go` — generated from `types.go`.
- `pkg/client/` — entire tree (clientset/informers/listers) generated from `types.go`.
- `pkg/apiclient/rollout/*.pb.go`, `*.pb.gw.go`, `*.swagger.json` — from `rollout.proto`.
- `*/mocks/*` — mockery output (`make gen-mocks`).
- `manifests/crds/*` and `manifests/install.yaml`/`namespace-install.yaml` —
  `make gen-crd` / `make manifests`.
- `docs/generated/` and CLI docs — `make docs`.
- `ui/src/models/` — generated from swagger.

## Compatibility constraints

- `pkg/apis/rollouts/v1alpha1/types.go` is a public API. Renaming fields, changing JSON
  tags, or changing types breaks every user's manifests — don't do it as part of a
  refactor. Additive changes only, and they require full codegen + validation updates.
- Protobuf field numbers in `rollout.proto` must never be reused or renumbered.
- The kubectl plugin (`pkg/kubectl-argo-rollouts/`) is used as a library by other
  projects; treat its exported surface conservatively.
- Rollout status fields and annotations (e.g. `rollout.argoproj.io/revision`,
  pod-template-hash behavior in `utils/hash`) are load-bearing across upgrades —
  changing their semantics can strand existing rollouts mid-update.

## Layering rules

- `utils/*` packages are leaf helpers: they may depend on `pkg/apis` and client-go but
  must not import controller packages (`rollout`, `analysis`, `experiments`, ...).
- Controller packages may depend on `utils/*` and `pkg/*`; keep provider-specific logic
  inside its provider package (`metricproviders/<p>`, `rollout/trafficrouting/<p>`),
  behind the shared interfaces (`metric.Provider`, `TrafficRoutingReconciler`).
- Prefer extracting into an existing focused `utils/<topic>` package over creating
  grab-bag helpers in controller packages.

## Refactoring loop

1. Confirm a green baseline first: `go build ./... && go test ./<affected>/...`.
2. Make the mechanical change (gopls-style rename over sed where possible; if using
   grep, remember string literals in tests, YAML fixtures under `test/`, and docs may
   reference identifiers/annotations).
3. `make lint` (golangci-lint with `--fix`) and `go build ./...`.
4. Run unit tests for every package you touched, then the full `make test-unit` before
   pushing. If behavior around reconciliation changed at all, it's not a refactor —
   add/adjust fixture tests (see `testing` skill).
5. Keep refactor commits separate from behavior changes so reviewers can diff them
   mechanically.
