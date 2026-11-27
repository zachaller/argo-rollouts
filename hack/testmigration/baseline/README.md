# Baseline

Results of the unconverted, serial e2e suite, used as the "before" side of the
migration checks.

| File | What it is |
|---|---|
| `junit-e2e-k8s1.37.xml` | JUnit results of the e2e job on Kubernetes 1.37 |
| `e2e-coverage-func-k8s1.37.txt` | Per-function coverage of the controller during that job (`go tool covdata func`, paths made repository-relative) |
| `seeded-bugs-canary/<bug>.xml` | Results of the unconverted Canary suite with one seeded bug applied (see below) |

Source: `argoproj/argo-rollouts`, workflow run 45973551441, commit `b8d4cc3e1d79`
(upstream master, the commit this migration branch started from).

In that run 131 tests executed, 16 were skipped (see
`test/e2e/expected-skips.txt`) and one test needed a rerun
(`TestAWSSuite/TestALBExperimentStepNoSetWeight`).

## Seeded bugs against the Canary suite

`seeded-bugs-canary/` holds one JUnit file per patch in
`test/e2e/seeded-bugs/`, from running the old, serial `TestCanarySuite` against
a controller with that patch applied (fork workflow run 46035139344, commit
`c7439574`, Kubernetes 1.37, one rerun of failed tests). The files keep each
test's name, time and outcome; the output text is dropped.

A test counts as catching a bug when it fails on every attempt.

| Seeded bug | Canary tests that catch it |
|---|---|
| `01-setweight-step-ignores-progress-deadline` | `TestCanaryProgressDeadlineExceededWithPause` |
| `02-setcanaryscale-replicas-ignored` | `TestCanarySetCanaryScale`, `TestCanaryUnScaleDownOnAbort` |
| `03-spec-paused-progressing-condition-flap` | `TestCanaryWithSpecPause` |
| `04-canary-scaling-ignored-while-paused` | `TestRolloutScalingWhenPaused` |
| `05-canary-min-available-uses-maxsurge` | `TestEphemeralMetadata`, `TestReduceWeightAndHonorMaxUnavailable`, `TestRolloutScalingDuringUpdate`, `TestRolloutWithMaxSurgeScalingDuringUpdate` |
| `06-ephemeral-metadata-not-switched-to-stable` | `TestEphemeralMetadata` |
| `07-progress-deadline-ignores-pending-scaledown` | `TestCanaryScaleDownDelayWithProgressDeadline` |
| `08-scaledown-delay-revision-limit-off-by-one` | `TestCanaryScaleDownDelay` |
| `09-abort-canary-service-reset-inverted` | `TestCanaryDynamicStableScale`, `TestCanaryScaleDownOnAbortNoTrafficRouting` |
| `10-abort-scaledown-deadline-guard-inverted` | `TestCanaryDynamicStableScaleAbortScaleDownDelay`, `TestCanaryScaleDownOnAbort` |
| `14-failed-step-analysis-does-not-abort` | `TestReduceWeightAndHonorMaxUnavailable` |

The other nine patches (11-13 and 15-20) break behaviour the Canary suite does
not cover, and no Canary test fails under them. Two Canary tests are not caught
out by any patch: `TestCanaryWithPausedRollout` and
`TestCanaryDynamicStableScaleRollbackToStable`.

Compare a later run against this with:

```
go run ./hack/testmigration killmatrix -base upstream/master \
    -base-dir hack/testmigration/baseline/seeded-bugs-canary -new-dir <dir of new <bug>.xml files>
```
