---
name: extend-providers
description: How to add a new metric provider, traffic router, or plugin support to Argo Rollouts — the interfaces to implement, registration points, API types, and required tests/docs. Use when adding integration with a new metrics backend (e.g. a new APM) or a new service mesh / ingress controller.
---

# Extending Argo Rollouts (Providers, Routers, Plugins)

First question: **built-in or plugin?** New integrations are increasingly preferred as
plugins (separate repo, hashicorp/go-plugin RPC, loaded via the `argo-rollouts-config`
ConfigMap) rather than built-ins compiled into the controller. Check with maintainers
before adding a new built-in. Sample plugins: `test/cmd/metrics-plugin-sample/`,
`test/cmd/trafficrouter-plugin-sample/`, `test/cmd/step-plugin-sample/`.

## Adding a built-in metric provider

1. Implement `metric.Provider` (defined in `metric/` at the repo root):
   `Run`, `Resume`, `Terminate`, `GarbageCollect`, `Type`, `GetMetadata`.
   Put it in `metricproviders/<name>/`.
2. Add the config struct to `MetricProvider` in
   `pkg/apis/rollouts/v1alpha1/analysis_types.go` (one field per provider) —
   then run full codegen (see `codegen` skill).
3. Register it in `metricproviders/metricproviders.go` (`NewProvider` switch and
   `Type()` dispatch).
4. Validation in `pkg/apis/rollouts/validation/` if the config has constraints;
   secret resolution follows the existing pattern (namespaced Secret lookups —
   copy from a sibling like `datadog` or `newrelic`).
5. Tests: unit tests against an `httptest.Server` or mocked SDK (copy a sibling's
   pattern). Measurement results must set `Phase`, `Value`, and `Measurement` timestamps
   consistently with other providers.
6. Docs: add `docs/analysis/<name>.md` and wire it into `mkdocs.yml` nav.

## Adding a built-in traffic router

1. Implement `TrafficRoutingReconciler` (defined in `rollout/trafficrouting/`):
   `SetWeight`, `SetHeaderRoute`, `SetMirrorRoute`, `VerifyWeight`, `RemoveManagedRoutes`,
   `UpdateHash`, `Type`. Put it in `rollout/trafficrouting/<name>/`.
   Routers that can't verify weight return `nil` from `VerifyWeight` per the existing
   convention — check siblings (`nginx` simple, `istio`/`alb` complex references).
2. Add the config struct to `RolloutTrafficRouting` in
   `pkg/apis/rollouts/v1alpha1/types.go` → full codegen.
3. Register in `rollout/trafficrouting.go` (`NewTrafficRoutingReconciler`).
4. RBAC: the controller needs rules for any new resource types it manages — update
   role manifests under `manifests/` (then `make manifests`).
5. Tests: unit tests with fake dynamic/typed clients like siblings; an e2e suite
   (`test/e2e/<name>_test.go` + fixture manifests) gated on that provider being
   installed.
6. Docs: `docs/features/traffic-management/<name>.md` + `mkdocs.yml`.

## Plugin interface changes

The RPC interfaces live in `metricproviders/plugin/`, `rollout/trafficrouting/plugin/`,
and `rollout/steps/plugin/` (types shared with plugin authors in
`utils/plugin/`/`rpc` subpackages). These are cross-process contracts consumed by
third-party plugins — changes must be backward compatible, and mocks regenerated
(`make gen-mocks`). Plugin resolution/download logic is in `utils/plugin/`.

## Definition of done (any provider)

- Codegen artifacts committed (types, deepcopy, proto, openapi, CRDs, clients).
- Sibling parity: supports the same secret handling, address overrides, and dry-run
  semantics as comparable providers where applicable.
- Docs page + example manifest; mention in feature matrix if one exists for that area.
