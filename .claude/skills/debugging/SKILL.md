---
name: debugging
description: Techniques for debugging the Argo Rollouts controller and tests — running the controller locally against a cluster, log levels, tracing a stuck or misbehaving Rollout through the reconcile loop, and debugging flaky unit/e2e tests. Use when investigating bugs, unexpected rollout behavior, or test failures.
---

# Debugging Argo Rollouts

## Run the controller locally

The controller runs fine outside the cluster against your kubeconfig context:

```bash
kubectl create ns argo-rollouts                      # once
kubectl apply --server-side -k manifests/crds        # install CRDs
go run ./cmd/rollouts-controller/main.go --loglevel debug --kloglevel 6
```

- `--loglevel debug` for controller logs (logrus), `--kloglevel 6` for client-go/informer
  internals (watch events, queue activity).
- `--instance-id <id>` restricts reconciliation to Rollouts labeled
  `argo-rollouts.argoproj.io/controller-instance-id=<id>` — useful to avoid fighting an
  in-cluster controller.
- Scale down any in-cluster controller first, or two controllers will fight over status.
- Delve: `dlv debug ./cmd/rollouts-controller -- --loglevel debug`. Sample plugins have
  debug builds: `make build-sample-metric-plugin-debug` etc.

## Tracing a misbehaving Rollout

Logs are structured with `namespace=... rollout=...` fields — grep on the rollout name to
follow one object's reconciles. The reconcile entry point is `syncHandler` in
`rollout/controller.go`; each sync builds a `rolloutContext` (`rollout/context.go`).

Checklist for "rollout is stuck/wrong":

1. `kubectl argo rollouts get rollout <name>` (or `kubectl get rollout -o yaml`) — read
   `status`: `phase`, `message`, `pauseConditions`, `abort`, `currentStepIndex`,
   `canary.weights`, and `conditions`.
2. Check events: the controller emits reasons like `RolloutUpdated`, `NewReplicaSetCreated`,
   `RolloutStepCompleted`, `RolloutAborted` (`utils/record`).
3. Find which code path decided the state: step progression in `rollout/canary.go`
   (`reconcileCanaryReplicaSets`, step handling), pauses in `rollout/pause.go`, abort/analysis
   verdicts in `rollout/analysis.go`, traffic weight in `rollout/trafficrouting.go`, and the
   final status computation in `rollout/sync.go` (`calculateRolloutConditions`,
   `persistRolloutStatus`).
4. For traffic routing issues, inspect the generated mesh/ingress resources (VirtualService,
   ALB annotations, canary Ingress) — routers live in `rollout/trafficrouting/<provider>/`.
5. For analysis issues, inspect the `AnalysisRun` object status; measurement logic is in
   `metricproviders/<provider>/` and run bookkeeping in `analysis/controller.go`.

Common root causes: status patch conflicts (another controller/human writing status),
`observedGeneration` lag, informer cache staleness right after a write, missing
`instance-id` label, and ReplicaSet hash collisions (see `utils/hash`, pod-template-hash).

## Debugging tests

- Rerun one test verbosely: `go test ./rollout/ -run TestFoo -v -count=1`.
- Fixture tests fail with "unexpected action" / "expected action ... didn't happen": dump
  `f.kubeclient.Actions()` in the failure to see the actual sequence; ordering matters.
- Enqueue-count mismatches usually mean your change added/removed a requeue — adjust
  `f.runWithSyncs`/expectations deliberately, don't just bump numbers until green.
- Flaky time-dependent tests: this codebase injects time via `timeutil` (`utils/time`) —
  use/extend the existing override hooks rather than `time.Sleep`.
- E2E failures: see the `e2e-testing` skill; check controller output in the `start-e2e`
  terminal and the test's dumped state before assuming the test is wrong.
