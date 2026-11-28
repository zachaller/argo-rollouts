# Baseline

Results of the unconverted, serial e2e suite, used as the "before" side of the
migration checks.

| File | What it is |
|---|---|
| `e2e-results-k8s1.37.xml` | JUnit results of the e2e job on Kubernetes 1.37 |
| `e2e-coverage-func-k8s1.37.txt` | Per-function coverage of the controller during that job (`go tool covdata func`, paths made repository-relative) |
| `seeded-bugs/<bug>.xml` | Results of the unconverted suites with one seeded bug applied (see below) |

Source: `argoproj/argo-rollouts`, workflow run 45973551441, commit `b8d4cc3e1d79`
(upstream master, the commit this migration branch started from).

In that run 131 tests executed, 16 were skipped (see
`test/e2e/expected-skips.txt`) and one test needed a rerun
(`TestAWSSuite/TestALBExperimentStepNoSetWeight`).

## Seeded bugs

`seeded-bugs/` holds one file per patch in `test/e2e/seeded-bugs/`. Each file
has the results of the unconverted, serial suites run against a controller
with that patch applied, on Kubernetes 1.37. A file keeps each test's name,
time and outcome; the output text is dropped. The `testsuite` elements name the
run each set of results came from:

| Tests | Patches | Fork workflow run | Reruns of a failed test |
|---|---|---|---|
| old Canary suite (tests of `c7439574`) | 01-20 | 46035139344 | 1 |
| old Canary suite | 21-35 | 46066775947 | 0 |
| old Canary suite | 32, 36-47 | 46076673397 | 2 |
| the other 15 suites (tests of `5a514fbb`) | 01-20 | 46061128211 | 0 |
| the other 15 suites | 21-35 | 46066726568 | 0 |
| the other 15 suites | 01-35 | 46067372629 | 2 |
| the other 15 suites | 36-47 | 46076667978 | 2 |

A test counts as catching a bug when it fails on every attempt in every run
it has for that bug. That matters: three tests fail about one run in three
whatever the patch (`TestALBExperimentStepNoSetWeight`,
`TestALBExperimentStepNoSetWeightMultiIngress` and, before it was changed,
`TestBlueGreenDuration_SupersededRollback`; see
`test/migration-reports/fix-plans/`), and one run without reruns would list
them under many unrelated bugs. One such coincidence is left in the table:
`TestALBExperimentStepNoSetWeightMultiIngress` under patch 03.

Patch 32 has no result for the 15 suites yet. Its first version stalled the
rollout informer and was replaced; the run of the replacement was still going
when this was written.

| Seeded bug | Tests that catch it |
|---|---|
| `01-setweight-step-ignores-progress-deadline` | `TestCanaryProgressDeadlineExceededWithPause` |
| `02-setcanaryscale-replicas-ignored` | `TestAPISIXCanarySetHeaderStep`, `TestCanarySetCanaryScale`, `TestCanaryUnScaleDownOnAbort`, `TestIstioAbortUpdateDeleteAllCanaryPods`, `TestIstioUpdateInMiddleZeroCanaryReplicas` |
| `03-spec-paused-progressing-condition-flap` | `TestALBExperimentStepNoSetWeightMultiIngress`, `TestCanaryWithSpecPause` |
| `04-canary-scaling-ignored-while-paused` | `TestRolloutScalingWhenPaused`, `TestSpecAndReplicaChangeSameTime` |
| `05-canary-min-available-uses-maxsurge` | `TestEphemeralMetadata`, `TestReduceWeightAndHonorMaxUnavailable`, `TestRolloutScalingDuringUpdate`, `TestRolloutWithMaxSurgeScalingDuringUpdate`, `TestRolloutBadToBadUpdate`, `TestRolloutGoodToBadUpdate` |
| `06-ephemeral-metadata-not-switched-to-stable` | `TestEphemeralMetadata`, `TestEphemeralMetadata` |
| `07-progress-deadline-ignores-pending-scaledown` | `TestCanaryScaleDownDelayWithProgressDeadline` |
| `08-scaledown-delay-revision-limit-off-by-one` | `TestCanaryScaleDownDelay`, `TestBlueGreenScaleDownDelay` |
| `09-abort-canary-service-reset-inverted` | `TestCanaryDynamicStableScale`, `TestCanaryScaleDownOnAbortNoTrafficRouting` |
| `10-abort-scaledown-deadline-guard-inverted` | `TestCanaryDynamicStableScaleAbortScaleDownDelay`, `TestCanaryScaleDownOnAbort`, `TestBlueGreenExceedProgressDeadlineAbort`, `TestBlueGreenScaleDownOnAbort`, `TestIstioAbortUpdateDeleteAllCanaryPods` |
| `11-bluegreen-autopromotion-seconds-not-awaited` | `TestBlueGreenKitchenSink` |
| `12-bluegreen-abort-keeps-active-on-new-rs` | `TestBlueGreenAbortAndUpdate`, `TestBlueGreenKitchenSink`, `TestBlueGreenPostPromotionFail`, `TestBlueGreenDuration_Retry` |
| `13-bluegreen-promoted-before-postpromotion-analysis` | `TestBlueGreenAbortAndUpdate`, `TestBlueGreenAnalysis`, `TestBlueGreenKitchenSink`, `TestBlueGreenPostPromotionFail` |
| `14-failed-step-analysis-does-not-abort` | `TestCanaryFailInlineMultipleAnalysis`, `TestCanaryInlineAnalysisLongRunningJobWithDeadline`, `TestReduceWeightAndHonorMaxUnavailable` |
| `15-inconclusive-background-analysis-does-not-pause` | `TestCanaryBackgroundAnalysisInvalidImageJob`, `TestCanaryInconclusiveBackgroundAnalysis` |
| `16-experiment-step-analysis-hash-args-swapped` | `TestRolloutWithExperimentAndAnalysis` |
| `17-experiment-service-not-deleted-after-scaledown` | `TestExperimentWithMultiportServiceAndScaleDownDelay`, `TestExperimentWithServiceAndScaleDownDelay`, `TestExperimentWithServiceNameAndScaleDownDelay` |
| `18-restart-ignores-max-unavailable` | none |
| `19-rollback-window-off-by-one` | `TestBlueGreenDuration_RollbackOutsideWindow`, `TestCanaryDuration_RollbackOutsideWindow`, `TestRollbackAnalysisOutsideWindow` |
| `20-istio-destinationrule-canary-subset-hash` | `TestIstioSubsetSplit`, `TestIstioSubsetSplitExperimentStep`, `TestIstioSubsetSplitSingleRoute` |
| `21-pingpong-stable-not-flipped-on-promotion` | `TestALBMesh_PingPong_StableCanary_Update`, `TestALBPingPongUpdate`, `TestALBPingPongUpdateMultiIngress`, `TestIstioPingPongUpdate` |
| `22-experiment-template-weights-not-routed` | `TestALBExperimentStep`, `TestALBExperimentStepMultiIngress`, `TestALBExperimentStepNoSetWeight`, `TestALBExperimentStepNoSetWeightMultiIngress`, `TestIstioHostSplitExperimentStep`, `TestIstioSubsetSplitExperimentStep` |
| `23-header-mirror-route-steps-skipped-on-first-update` | `TestAPISIXCanarySetHeaderStep`, `TestAlbHeaderRoute`, `TestAlbHeaderRouteMultiIngress`, `TestIstioHostHeaderRoute`, `TestIstioHostMirrorRoute` |
| `24-apisix-stable-weight-not-reduced` | `TestAPISIXCanarySetHeaderStep`, `TestAPISIXCanaryStep` |
| `25-duration-never-finished` | `TestBlueGreenDuration_AutoPromotion`, `TestBlueGreenDuration_FullyPromoted`, `TestBlueGreenDuration_InitialRollout`, `TestBlueGreenDuration_ManualPromotion`, `TestBlueGreenDuration_RollbackInsideWindow`, `TestBlueGreenDuration_RollbackOutsideWindow`, `TestBlueGreenDuration_SpecPaused`, `TestBlueGreenDuration_SupersededRollback`, `TestBlueGreenDuration_SupersededRollbackToStable`, `TestBlueGreenDuration_SupersededRollout`, `TestCanaryDuration_FullyPromoted`, `TestCanaryDuration_IndefinitePauseStep`, `TestCanaryDuration_InitialRollout`, `TestCanaryDuration_PauseStep`, `TestCanaryDuration_RollbackInsideWindow`, `TestCanaryDuration_RollbackOutsideWindow`, `TestCanaryDuration_SpecPaused`, `TestCanaryDuration_SupersededRollback`, `TestCanaryDuration_SupersededRollout` |
| `26-duration-abort-not-completed` | `TestBlueGreenDuration_Abort`, `TestBlueGreenDuration_Retry`, `TestCanaryDuration_Abort`, `TestCanaryDuration_Retry` |
| `27-duration-restarted-by-scaling` | `TestCanaryDynamicStableScaleAbortScaleDownDelay`, `TestBlueGreenDuration_Abort`, `TestBlueGreenDuration_Retry`, `TestBlueGreenDuration_ScalingOnly`, `TestCanaryDuration_Abort` |
| `28-workloadref-deployment-never-scaled-down` | `TestScaleDownOnSuccess`, `TestScaleDownProgressively`, `TestScaleDownProgressivelyCompleted`, `TestScaleDownProgressivelyCompletedCanary` |
| `29-invalid-spec-not-reported-degraded` | `TestFixInvalidSpec`, `TestWorkloadRef`, `TestWorkloadRefTemplate` |
| `30-condition-status-change-with-same-reason-ignored` | `TestCanaryProgressDeadlineExceededWithPause`, `TestBlueGreenUpdate`, `TestKubectlWaitForCompleted`, `TestKubectlWaitForPaused`, `TestRolloutAbortRetryPromote` |
| `31-promote-full-not-carried-across-reconciles` | `TestBlueGreenPreviewReplicaCountPromoteFull`, `TestCanaryPromoteFull` |
| `32-malformed-object-fallback-result-discarded` | none |
| `33-progress-deadline-check-requeued-in-minutes` | `TestBlueGreenProgressDeadlineExceededWithPause`, `TestBlueGreenProgressDeadlineExceededWithoutPause`, `TestCanaryProgressDeadlineExceededWithPause`, `TestCanaryWithSpecPause`, `TestRolloutBadToBadUpdate`, `TestRolloutBadToGoodSetWeightUpdate`, `TestRolloutBadToGoodUpdate`, `TestRolloutGoodToBadUpdate`, `TestWorkloadRef` |
| `34-restart-never-marked-complete` | `TestCompleteRolloutRestart` |
| `35-service-not-cleaned-up-while-rollout-exists` | `TestBlueGreenToCanary` |
| `36-analysis-secret-arg-read-by-arg-name` | `TestAnalysisWithArgs`, `TestAnalysisWithSecret`, `TestAnalysisWithSharedSecret`, `TestBackgroundAnalysisWithArgs` |
| `37-inconclusive-measurement-counted-as-failed` | `TestCanaryBackgroundAnalysisInvalidImageJob`, `TestCanaryBackgroundAnalysisLongRunningJob`, `TestCanaryInconclusiveBackgroundAnalysis`, `TestCanaryInlineAnalysisInvalidImageJob` |
| `38-failed-job-measurement-recorded-successful` | `TestBlueGreenAbortAndUpdate`, `TestBlueGreenKitchenSink`, `TestBlueGreenPostPromotionFail`, `TestBlueGreenPrePromotionFail`, `TestCanaryBackgroundAnalysisLongRunningJobWithDeadline`, `TestCanaryFailInlineMultipleAnalysis`, `TestCanaryInlineAnalysisLongRunningJobWithDeadline`, `TestReduceWeightAndHonorMaxUnavailable`, `TestExperimentWithDryRunMetrics` |
| `39-running-analysisruns-never-cancelled` | `TestBackgroundAnalysisWithArgs`, `TestCanaryBackgroundAnalysis`, `TestCanaryBackgroundAnalysisLongRunningJob` |
| `40-successful-analysisrun-history-not-kept` | `TestALBExperimentStepNoSetWeightMultiIngress`, `TestAnalysisWithArgs`, `TestAnalysisWithSecret`, `TestAnalysisWithSharedSecret`, `TestBackgroundAnalysisWithArgs`, `TestBlueGreenAnalysis`, `TestCanaryBackgroundAnalysis`, `TestCanaryInlineAnalysis`, `TestCanaryInlineMultipleAnalysis`, `TestMultipleAnalysis`, `TestRollbackAnalysisOutsideWindow`, `TestRollbackAnalysisWithinWindow` |
| `41-istio-tls-route-weights-not-written` | `TestIstioHostSplit`, `TestIstioHostSplitOnlyTls` |
| `42-abort-not-cleared-on-fully-promoted-rollout` | none |
| `43-dynamic-stable-scale-abort-weight-never-lowered` | `TestCanaryDynamicStableScale`, `TestCanaryDynamicStableScaleAbortScaleDownDelay`, `TestIstioSubsetSplitInStableDownscaleAfterCanaryAbort` |
| `44-bluegreen-preview-never-scaled-up-after-resume` | `TestBlueGreenKitchenSink`, `TestBlueGreenPrePromotionFail`, `TestBlueGreenPreviewReplicaCount` |
| `45-bluegreen-promote-full-waits-for-post-promotion-analysis` | `TestBlueGreenPromoteFull` |
| `46-dry-run-failure-counted-in-run-summary` | `TestExperimentWithDryRunMetrics` |
| `47-measurement-retention-limit-ignored` | `TestExperimentWithMeasurementRetentionMetrics` |

Of the 131 tests that run in CI, these 13 are caught by no patch, so the
comparison says nothing about them:

- `TestCanaryDynamicStableScaleRollbackToStable`
- `TestCanaryWithPausedRollout`
- `TestCanaryDuration_ScalingOnly`
- `TestContainerResourceFormats`
- `TestControllerMetrics`
- `TestMalformedRollout`
- `TestMalformedRolloutEphemeralCtr`
- `TestNeverScaleDown`
- `TestRolloutPDBRestart`
- `TestRolloutPauseDurationGreaterThanProgressDeadlineSeconds`
- `TestRolloutPodVolumesItemsSpec`
- `TestRolloutRestart`
- `TestIstioAbortUpdate`

Compare a later run against this with:

```
go run ./hack/testmigration killmatrix -base upstream/master \
    -base-dir hack/testmigration/baseline/seeded-bugs -new-dir <dir of new <bug>.xml files>
```
