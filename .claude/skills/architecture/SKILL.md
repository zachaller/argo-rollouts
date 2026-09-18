---
name: architecture
description: Map of the Argo Rollouts codebase and how its pieces fit together. Use when planning a feature, deciding where new code belongs, tracing how a Rollout is reconciled, or answering "where does X happen" questions about the controller, traffic routing, analysis, experiments, CLI/plugin, or UI.
---

# Argo Rollouts Architecture

Argo Rollouts is a Kubernetes controller (plus a kubectl plugin and web UI) that provides
progressive delivery: blue-green and canary strategies for a `Rollout` resource that
replaces a Deployment, with optional analysis-driven promotion and traffic shaping.

## High-level components

| Component | Entry point | Purpose |
|---|---|---|
| Controller manager | `cmd/rollouts-controller/main.go` → `controller/controller.go` | Wires informers, clients, and starts all sub-controllers |
| Rollout controller | `rollout/controller.go` | Core reconciler for `Rollout` resources |
| Analysis controller | `analysis/` | Reconciles `AnalysisRun`s, executes metric measurements |
| Experiment controller | `experiments/` | Reconciles `Experiment` resources (ephemeral ReplicaSets + analysis) |
| Service/Ingress controllers | `service/`, `ingress/` | Manage rollout-referenced Services and Ingresses |
| Metric providers | `metricproviders/` | Prometheus, Datadog, CloudWatch, Kayenta, Job, web, plugin, etc. |
| Traffic routers | `rollout/trafficrouting/` | Istio, ALB, NGINX, SMI, Ambassador, AppMesh, Traefik, Apisix, plugin |
| kubectl plugin / CLI | `pkg/kubectl-argo-rollouts/` | `kubectl argo rollouts` commands (get, promote, abort, dashboard…) |
| API server + UI | `server/`, `ui/` | gRPC/REST API (`pkg/apiclient/rollout/rollout.proto`) and React dashboard |
| API types | `pkg/apis/rollouts/v1alpha1/types.go` | CRD Go types — source of truth for generated code |
| Generated clients | `pkg/client/` | clientset/informers/listers generated from types.go |
| Shared helpers | `utils/` | One package per concern (replicaset, analysis, conditions, defaults, istio, aws…) |

## Reconciliation flow (rollout controller)

1. Informer event handlers in `rollout/controller.go` enqueue rollout keys onto a rate-limited
   workqueue (`utils/controller` has the generic worker plumbing).
2. `syncHandler` builds a `rolloutContext` (`rollout/context.go`) holding the rollout, its
   stable/new/old ReplicaSets, current/pending AnalysisRuns, and the resolved traffic router.
3. Strategy logic lives in `rollout/canary.go` and `rollout/bluegreen.go`; shared
   ReplicaSet scaling/creation in `rollout/replicaset.go` and `rollout/sync.go`;
   pausing in `rollout/pause.go`; analysis orchestration in `rollout/analysis.go`;
   traffic weight changes in `rollout/trafficrouting.go`; canary step plugins in
   `rollout/stepplugin.go`.
4. Status is written back via a computed patch (`calculateRolloutConditions` /
   `persistRolloutStatus` in `rollout/sync.go`) — the controller patches status, it does not
   update the whole object.

Key invariant: reconciliation must be idempotent and converge. Everything derives from
observed cluster state + the Rollout spec; avoid state that only lives in memory.

## Where new code goes

- New canary/bluegreen behavior → `rollout/` (+ unit tests in the same package).
- New metric provider → `metricproviders/<name>/` (see `extend-providers` skill).
- New traffic router → `rollout/trafficrouting/<name>/` (see `extend-providers` skill).
- New API field → `pkg/apis/rollouts/v1alpha1/types.go`, then **must** run codegen
  (see `codegen` skill) and update validation in `pkg/apis/rollouts/validation/`.
- New CLI command → `pkg/kubectl-argo-rollouts/cmd/`.
- Cross-cutting helpers → a focused package under `utils/`.
- Install manifests → `manifests/` (regenerated via `make manifests`; CRDs via `make gen-crd`).

## Extensibility

Three plugin systems (all hashicorp/go-plugin RPC based, configured via the
`argo-rollouts-config` ConfigMap): metric plugins (`metricproviders/plugin/`), traffic router
plugins (`rollout/trafficrouting/plugin/`), and canary step plugins (`rollout/steps/plugin/`).
Sample plugins live under `test/cmd/`.
