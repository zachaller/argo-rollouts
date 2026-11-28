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
| `21-pingpong-stable-not-flipped-on-promotion` | `rollout/sync.go` | When a ping-pong canary is promoted while ping is stable, the stable role stays on the ping Service instead of moving to pong, so the traffic router sends 100% back to the old Service. |
| `22-experiment-template-weights-not-routed` | `rollout/trafficrouting.go` | Experiment templates that set a `weight` get no traffic during an experiment step, because their weight is looked up by Service name instead of template name. |
| `23-header-mirror-route-steps-skipped-on-first-update` | `rollout/trafficrouting.go` | `setHeaderRoute` and `setMirrorRoute` steps are not sent to the traffic router during the first update of a rollout (revision 2), so the managed route is never created. |
| `24-apisix-stable-weight-not-reduced` | `rollout/trafficrouting/apisix/apisix.go` | The stable backend of the ApisixRoute stays at weight 100 instead of 100 minus the canary weight. |
| `25-duration-never-finished` | `rollout/sync.go` | `status.duration.finishedAt` is never set when a rollout is promoted, because completion needs more available replicas than the rollout asks for. |
| `26-duration-abort-not-completed` | `rollout/sync.go` | Aborting a rollout does not complete `status.duration`, so it has no `Aborted` completion status and a retry does not start a new duration. |
| `27-duration-restarted-by-scaling` | `rollout/sync.go` | A completed rollout that falls short of its desired replicas, for example after `spec.replicas` is raised, starts a new `status.duration` even though the pod template did not change. |
| `28-workloadref-deployment-never-scaled-down` | `rollout/scale_utils.go` | The Deployment referenced by `workloadRef` is never scaled down, so `scaleDown: onsuccess` and `scaleDown: progressively` leave it at its original size. |
| `29-invalid-spec-not-reported-degraded` | `rollout/sync.go` | A rollout with an invalid spec gets the InvalidSpec condition but its phase is calculated from the old status, so it is never reported as Degraded. |
| `30-condition-status-change-with-same-reason-ignored` | `utils/conditions/conditions.go` | A rollout condition is not updated when only its status changes, so Healthy, Paused, Completed and Available keep their first value and the RolloutNotCompleted event is not emitted for later updates. |
| `31-promote-full-not-carried-across-reconciles` | `rollout/sync.go` | `status.promoteFull` is cleared after the first reconciliation, so a full promotion that needs the new ReplicaSet to scale up first falls back to normal steps, pauses and analysis. |
| `32-malformed-object-fallback-result-discarded` | `utils/tolerantinformer/tollerantinformer.go` | For an object that fails strict decoding (for example a Rollout with an invalid resource quantity), the result of the lenient JSON fallback is thrown away and the half-converted object is cached, so every field declared after the bad one (for a Rollout, `strategy`) is missing and the rollout is reported InvalidSpec instead of being reconciled. |
| `33-progress-deadline-check-requeued-in-minutes` | `rollout/sync.go` | A stuck rollout is requeued for its progress-deadline check after `progressDeadlineSeconds` minutes instead of seconds, so it is marked Degraded far too late. |
| `34-restart-never-marked-complete` | `rollout/context.go` | `status.restartedAt` is never updated after a restart, so a restarted rollout stays Progressing ("rollout is restarting") after all its pods have been replaced. |
| `35-service-not-cleaned-up-while-rollout-exists` | `service/service.go` | A Service the rollout no longer references keeps its `rollouts-pod-template-hash` selector and managed-by annotation for as long as the rollout exists. |
| `36-analysis-secret-arg-read-by-arg-name` | `analysis/analysis.go` | An analysis argument with `valueFrom.secretKeyRef` is read from the Secret under the argument's name instead of the referenced key, so the AnalysisRun ends in Error unless the two happen to match. |
| `37-inconclusive-measurement-counted-as-failed` | `analysis/analysis.go` | An Inconclusive measurement is added to the metric's failed count instead of its inconclusive count, so the AnalysisRun ends Failed and aborts the rollout where it should end Inconclusive and pause it. |
| `38-failed-job-measurement-recorded-successful` | `metricproviders/job/job.go` | A job metric whose Job has failed, including one killed by `activeDeadlineSeconds`, is recorded as a Successful measurement. |
| `39-running-analysisruns-never-cancelled` | `rollout/analysis.go` | The rollout controller no longer terminates AnalysisRuns that are still running, so a background run keeps running after the rollout is promoted or aborted. |
| `40-successful-analysisrun-history-not-kept` | `utils/analysis/filter.go` | Successful AnalysisRuns are deleted as soon as they are no longer the rollout's current run, instead of being kept up to `successfulRunHistoryLimit`. |
| `41-istio-tls-route-weights-not-written` | `rollout/trafficrouting/istio/istio.go` | New weights for the TLS routes of an Istio VirtualService are not written back unless the VirtualService also has TCP routes. |
| `42-abort-not-cleared-on-fully-promoted-rollout` | `rollout/controller.go` | Aborting a rollout that is already fully promoted is no longer undone, so a healthy rollout becomes Degraded and its only ReplicaSet gets an abort scale-down deadline. |
| `43-dynamic-stable-scale-abort-weight-never-lowered` | `rollout/trafficrouting.go` | After an abort with `dynamicStableScale`, the canary weight is never lowered (the larger of the expected and current weight is used instead of the smaller), so the canary keeps its traffic and its pods. |
| `44-bluegreen-preview-never-scaled-up-after-resume` | `rollout/bluegreen.go` | A blue-green rollout with `previewReplicaCount` never passes the scale-up checkpoint after it is resumed, because the preview's available count is compared with `spec.replicas`, so the preview ReplicaSet stays at the preview size and the rollout is never promoted. |
| `45-bluegreen-promote-full-waits-for-post-promotion-analysis` | `rollout/sync.go` | A full promotion of a blue-green rollout with post-promotion analysis waits for that analysis, which a full promotion never starts, so the rollout stays Progressing. |
| `46-dry-run-failure-counted-in-run-summary` | `analysis/analysis.go` | A failed dry-run metric is counted in `status.runSummary.failed` instead of `status.dryRunSummary.failed`. |
| `47-measurement-retention-limit-ignored` | `analysis/analysis.go` | `measurementRetention` limits are looked up by AnalysisRun name instead of metric name, so they never apply and the default of 10 measurements is kept. |
| `48-worker-loop-requires-metrics-server` | `utils/controller/controller.go` | The shared worker loop reports a sync error to the metrics server without checking that there is one, so the first failed sync of the Istio DestinationRule worker, which has none, ends the controller with a nil-pointer panic. This is the defect fixed in `064b3513`, put back. It breaks the rule above about crashes on purpose: only a failed DestinationRule sync triggers it, which the ordinary tests do not cause (none in 30 runs). `TestIstioDestinationRuleCleanupConflict` causes one, and needs the controller inside the test binary. It has no result in the baseline. |
