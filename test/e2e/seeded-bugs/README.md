# Seeded bugs

Each patch in this directory deliberately breaks one behaviour of the
controller. They are not applied in normal builds. They exist to check the
tests: with a patch applied, the e2e tests that cover that behaviour must fail.

The test migration uses them as a before-and-after check. If a test failed
under a seeded bug before it was converted and passes under the same bug
afterwards, the conversion weakened it. `go run ./hack/testmigration killmatrix`
does that comparison.

## Running them

Dispatch the `Testing` workflow manually with `e2e_seeded_bugs` set to `all`
or to a comma-separated list of patch names without `.patch`. Each bug gets its
own e2e job, which applies the patch before building the controller. To try
one locally:

```
git apply test/e2e/seeded-bugs/02-setcanaryscale-replicas-ignored.patch
make start-e2e          # in one terminal
make test-e2e           # in another
git apply -R test/e2e/seeded-bugs/02-setcanaryscale-replicas-ignored.patch
```

## Adding one

- Change one behaviour in a few lines, written like a realistic mistake: an
  inverted condition, a skipped step, a wrong field, an off-by-one.
- Do not make the controller crash or fail every rollout. A bug that fails
  every test says nothing about any one of them.
- Do not patch code the test helpers in `test/fixtures` call. That breaks the
  tests themselves, not the controller.
- The patch must apply on its own to a clean tree, and both the controller and
  `go vet -tags e2e ./test/e2e/` must still build.

## The patches

| Patch | File | Behaviour it breaks |
|---|---|---|
| `01-setweight-step-ignores-progress-deadline` | `rollout/sync.go` | A canary sitting at a `setWeight` step is never marked Degraded when `progressDeadlineSeconds` passes. |
| `02-setcanaryscale-replicas-ignored` | `utils/replicaset/canary.go` | `setCanaryScale.replicas` is ignored, so the canary is scaled to zero instead of the requested count. |
| `03-spec-paused-progressing-condition-flap` | `rollout/sync.go` | Setting `spec.paused` on a fully promoted rollout makes the Progressing condition flap between Paused/Unknown and NewReplicaSetAvailable. |
| `04-canary-scaling-ignored-while-paused` | `rollout/sync.go` | A change to `spec.replicas` is not applied while a canary is paused at a step. |
| `05-canary-min-available-uses-maxsurge` | `utils/replicaset/canary.go` | Basic canary computes minimum availability from `maxSurge` instead of `maxUnavailable`, so it scales down too many pods during an update. |
| `06-ephemeral-metadata-not-switched-to-stable` | `rollout/ephemeralmetadata.go` | A fully rolled out ReplicaSet gets the canary/preview ephemeral metadata instead of the stable/active metadata. |
| `07-progress-deadline-ignores-pending-scaledown` | `rollout/sync.go` | A promoted rollout whose old ReplicaSet is still inside `scaleDownDelaySeconds` is marked Degraded once the progress deadline passes. |
| `08-scaledown-delay-revision-limit-off-by-one` | `rollout/replicaset.go` | `scaleDownDelayRevisionLimit` keeps one old ReplicaSet fewer than configured, so the newest old ReplicaSet is scaled down immediately when the limit is 1. |
| `09-abort-canary-service-reset-inverted` | `rollout/service.go` | On abort, the canary Service is pointed back at stable only when traffic routing is used (it should be only when it is not), so basic canary never resets it and dynamicStableScale resets it too early. |
| `10-abort-scaledown-deadline-guard-inverted` | `rollout/replicaset.go` | After an abort the scale-down deadline is added only while stable is not fully available, so a canary or preview ReplicaSet is never scaled down when stable is healthy and is annotated too early with dynamicStableScale. |
| `11-bluegreen-autopromotion-seconds-not-awaited` | `rollout/pause.go` | A blue-green rollout with `autoPromotionSeconds` promotes as soon as the pause starts instead of after the delay. |
| `12-bluegreen-abort-keeps-active-on-new-rs` | `rollout/service.go` | When a blue-green rollout aborts after promotion, the active Service stays on the new ReplicaSet instead of returning to stable. |
| `13-bluegreen-promoted-before-postpromotion-analysis` | `rollout/sync.go` | A blue-green rollout is marked stable right after the active switch, before post-promotion analysis has started. |
| `14-failed-step-analysis-does-not-abort` | `rollout/analysis.go` | A Failed inline (step) AnalysisRun no longer aborts the canary, which stays Progressing. |
| `15-inconclusive-background-analysis-does-not-pause` | `rollout/analysis.go` | An Inconclusive background AnalysisRun no longer pauses the canary. |
| `16-experiment-step-analysis-hash-args-swapped` | `rollout/experiment.go` | An experiment step passes the stable and latest pod-template hashes to its analysis the wrong way round. |
| `17-experiment-service-not-deleted-after-scaledown` | `experiments/experiment.go` | The Service an Experiment creates for a template is left behind after the experiment finishes and its ReplicaSet is scaled to zero. |
| `18-restart-ignores-max-unavailable` | `rollout/restart.go` | A restart evicts every outdated pod in one pass instead of at most `maxUnavailable` at a time. |
| `19-rollback-window-off-by-one` | `rollout/sync.go` | `rollbackWindow.revisions` admits one revision too many, so a rollback just outside the window is fast-tracked and skips its steps and analysis. |
| `20-istio-destinationrule-canary-subset-hash` | `rollout/trafficrouting/istio/istio.go` | The canary subset of the Istio DestinationRule is labelled with the stable pod-template hash instead of the canary hash. |
