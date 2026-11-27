# Behaviour review: test/e2e/canary_test.go

This file has two parts.

1. **The review**, unedited. It was written by a reviewer that did not make the
   changes and was not shown the author's reasons for them. It compared the
   baseline (`upstream/master`) with commit `ceefbd8b` by reading code; it ran
   no cluster. Line numbers refer to that commit.
2. **The resolution**, written afterwards by the author of the changes: what
   was done about each gap the review lists.

---

# Review: non-mechanical body changes in `test/e2e/canary_test.go` (+ one in `experiment_test.go`)

## Summary

| # | Test | Change | Verdict |
|---|------|--------|---------|
| 1 | `TestCanarySetCanaryScale` | `s.T().Parallel()` line removed; `fixtures.New` calls `t.Parallel()` instead | **equivalent** (more timing exposure) |
| 2 | `TestRolloutWithMaxSurgeScalingDuringUpdate` | Update patch changed from `cpu: 2m` (pods become ready) to `command: [/bad-command]` (pods never ready) | **equivalent** (a race removed) |
| 3 | `TestCanaryScaleDownOnAbort` | Added `WaitForServiceSelector` (up to 90s) before the unchanged immediate `ExpectServiceSelector` | **weaker** |
| 4 | `TestCanaryDynamicStableScaleAbortScaleDownDelay` | `Sleep(2s)` replaced by `WaitForRolloutCanaryWeight(0)` (up to 90s) | **different** (weaker on canary hold, stricter on ordering) |
| 5 | `ExperimentSuite.TestRolloutWithExperimentAndAnalysis` | `s.T().Parallel()` line removed; nothing replaces it | **equivalent** |

Baseline: `upstream/master`. New: working tree. Static review only: nothing was run against a cluster; `go vet -tags e2e ./test/e2e/ ./test/fixtures/` passes. `rollout/` and `utils/` are unchanged against the baseline, and no manifest under `test/e2e/functional/` changed.

Claim tags: **[T]** traced in code at the cited lines, **[I]** my inference about runtime behaviour (not executed).

Disclosure: a repo-wide `git grep "Parallel()"` printed two partial lines of `test/e2e/body-exceptions.yaml` (about the removed `s.T().Parallel()` calls). I did not open the file. My reading of tests 1 and 5 was already done from testify's `suite.go` before that; the other three are untouched by it.

---

## 1. TestCanarySetCanaryScale

**Diff.** Only old line 36 `s.T().Parallel()` is gone. Steps, manifest and all six assertions are identical.

**Old.** Verified that canary scale follows `setCanaryScale` and not `setWeight`, sampling pod counts once at each pause. The old suite had exactly one parallel method, so it paused at `Parallel()`, the other 16 methods ran, `TearDownSuite` ran, and only then did the body run, alone in the kubeconfig namespace **[T]** (testify `suite.Run`: the method is called inside `t.Run`; the deferred `TearDownSuite` runs before paused subtests resume; `E2ESuite.Given()` at `test/fixtures/e2e_suite.go:331-338` re-reads `s.T()`, which was back on this test's `T`).

**New.** Same assertions, `test/e2e/canary_test.go`:
- `:54` (0,4) at step 0
- `:60` (1,4) at step 2: `setCanaryScale{weight:25}` gives ceil(4×25/100)=1
- `:66` (1,4) at step 4: `setWeight: 50` does not change scale
- `:72` (3,4) at step 6: `replicas: 3`
- `:78` (2,4) at step 8: `matchTrafficWeight` gives ceil(4×50/100)=2
- `:83` (4,4) when Healthy

Arithmetic **[T]**: `UseSetCanaryScale` (`utils/replicaset/canary.go:584-612`), `GetCanaryReplicasOrWeight` (`:503-516`), `CalculateReplicaCountsForTrafficRoutedCanary` (`:378-394`, stable is always `spec.replicas` without dynamicStableScale).

**Verdict: equivalent.** The test is still parallel (`test/fixtures/env.go:70`). What changed is its company: it now runs beside up to `-parallel N` other tests instead of alone.

**Break attempts.** No assertion or input differs, so no bug can separate the versions by logic. Tried: wrong replica count for `setCanaryScale.replicas` (e.g. `canary.go:509` returning the weight pointer) fails `:72` in both; scale following `setWeight` fails `:66` in both. Reverse: under concurrency the new run could expose cross-rollout interference in the controller that a solitary run cannot, but only nondeterministically **[I]**.

**Timing.** Two assertions must land inside timed pauses, and the same windows are now shared with other tests **[T for the windows, I for the load effect]**:
- `:66` within the 10s pause of step 4 (`:36`), otherwise step 5 scales the canary to 3.
- `:78` within the 5s pause of step 8 (`:42`), otherwise the rollout completes and the canary hash has 4 pods.

`PromoteRollout`'s fallback (`test/fixtures/when.go:821-891`) patches `spec.replicas` +1 and back if the controller has not processed a promote after the backoff (about 15s per `when.go:48-50`); under heavy parallel load that would disturb the counts **[I]**.

---

## 2. TestRolloutWithMaxSurgeScalingDuringUpdate

**Diff.** The update patch (old lines 150-157: `resources.requests.cpu: 2m`) became `command: [/bad-command]` (new `:146-152`). Waits and assertions are unchanged.

**Old.** Rollout of 4, `maxSurge: 100%`, default maxUnavailable, no steps. After a *healthy* template change it asserted (canary, stable) = (4,3) at status.replicas 7, (8,3) after scaling to 8, (4,3) after scaling back to 4. Each check ran once, right after the watch saw the replica total.

The old checks could only pass while no new pod was available **[T]**: at replicas 4, as soon as k new pods are available, `replicasToScaleDown = k + 3 > minAvailable 3` (`utils/replicaset/canary.go:194-200`) and stable drops to 3−k. So the old test raced nginx start-up (about 1–3s) through the first check and the scale-up **[I]**; had it lost, stable would be 2 and `WaitForRolloutReplicas(11)` would never be met.

**New.** Same assertions at `test/e2e/canary_test.go:155`, `:160`, `:165`, after the waits at `:153`, `:158`, `:163`. New pods crash on start, so canary availability is pinned at 0, the regime the old test depended on by timing.

**Same code path? Yes [T].** In both versions the numbers come from `CalculateReplicaCountsForBasicCanary` (`utils/replicaset/canary.go:124-211`) with `newRS.Status.AvailableReplicas == 0`; scale changes go through `isScalingEvent` → `syncReplicasOnly` → `reconcileCanaryReplicaSets` (`rollout/sync.go:273-341`). The controller does not distinguish "starting" from "crash-looping" pods here; it reads only RS spec and status counts.

**Arithmetic in the new scenario [T].** Defaults: maxUnavailable "25" is parsed as 25%, floored (`utils/defaults/defaults.go:26`, `utils/replicaset/replicaset.go:407-426`); maxSurge 100%, ceiled.
- *Replicas 4* (surge 4, unavailable 1, minAvailable 3, max 8). Desired (4,0). First pass: new 0→4 allowed (scaleUpCount 4); `isIncreasing`; replicasToScaleDown = new available 0 + stable spec 4 = 4 > 3, scaleDownCount 1, stable = max(0, 4−1) = 3; `adjustReplicaWithinLimits` leaves (4,3). Total 7. Steady state: 0 + 3 = 3 ≤ 3, returns (4,3) at `:195-198`. It holds indefinitely.
- *Replicas 8* (surge 8, unavailable 2, minAvailable 6, max 16). Desired (8,0). scaleUpCount = 16−7 = 9, new → 8. replicasToScaleDown = 0 + 3 = 3 ≤ 6, returns (8,3). Total 11.
- *Back to 4*. new spec 8 > desired 4, so not increasing. replicasToScaleDown = new spec 8 (availability ignored, `:469-485`) + stable available 3 = 11; scaleDownCount 8; new = max(4, 8−8) = 4; adjust: 4+3 ≤ 8 and available 0+3 ≥ 3, so (4,3). Total 7.

All three asserted pairs follow.

**Verdict: equivalent.** Same assertions, same branch of the controller, with the precondition now held by the input instead of by timing. It mirrors the sibling `TestRolloutScalingDuringUpdate` (`:199-205`).

**Break attempts.** Old catches, new misses: none found. Tried (a) wrong `minAvailable` (`canary.go:188`): both fail at the first wait or count; (b) a bug that only bites once canary pods become available (stable scaled down too far): the old test never asserted in that regime, and landing there made the *correct* controller fail it too; (c) special handling of unhealthy new pods (e.g. `cleanupUnhealthyReplicas` wrongly applied to the new RS): the new test catches it, the old one had zero-available pods as well. Reverse: none beyond determinism.

**Flake.** Lower than before. No new flake path found.

---

## 3. TestCanaryScaleDownOnAbort

**Diff.** One added line, `:575` `WaitForServiceSelector("canary-scaledowndelay-canary", {app, hash 674d8cf959})`, between `WaitForRolloutStatus("Degraded")` and the unchanged `ExpectServiceSelector`.

**Old.** After abort (ALB traffic routing, `abortScaleDownDelaySeconds: 1`, one canary pod): (a) *at the moment Degraded is first observed*, one GET must show the canary Service selecting the stable hash; (b) 3s later revision 2 has 0 pods.

What the controller does **[T]**:
- Degraded comes only from `status.phase`, written by `persistRolloutStatus` when the `RolloutAborted` condition is set (`rollout/sync.go:879-889`, `:1023`; `utils/rollout/rolloututil.go:82-85`, `:142-145`).
- In that same reconcile, the traffic-routing stage has already patched the canary Service to stable (`rollout/trafficrouting.go:204-216`, call at `:212`; stage order `rollout/stages.go:40-45`; status sync comes after the stages). So at the first Degraded the selector is already stable.
- In every *later* reconcile while the canary RS still has an available pod, the earlier stage `reconcileStableAndCanaryService` points the canary Service back at the canary RS (`rollout/service.go:264` excludes traffic-routed aborts, so `:283` runs; the gate at `:325` passes), and the traffic-routing stage then patches it to stable again. `switchServiceSelector` also edits the lister's object in place (`service.go:73`), so the second stage sees the first stage's change.

So the selector flaps canary→stable once per reconcile for roughly 1–2s, until the canary is scaled down **[T for the code path, I for durations]**. The old single GET raced that flap.

Note: the new comment at `:574` says the selector "can still be on the canary when the rollout first reports Degraded". By the trace above it is not *still* there; it is put back and removed again by later reconciles. This matters for the residual race below.

**New.**
- `:575`: polls every 250ms for up to 90s until the selector equals stable (`test/fixtures/when.go:527-549`).
- `:578`: one more immediate GET must equal stable.
- `:580`, `:582`: sleep 3s, then revision 2 has 0 pods.

**Verdict: weaker.** "Reset by the time Degraded is reported" became "reset at some sample within 90s". The 3s bound on canary scale-down is also now counted from the end of the wait.

**Old catches, new misses.** A controller that leaves the canary Service on the canary until the canary pods are gone. Concretely: swap `canaryStageStableCanaryService` and `canaryStageTrafficRouting` in `rollout/stages.go:40-41`, or make `rollout/trafficrouting.go:206` additionally require `c.newRS.Status.AvailableReplicas == 0`. The selector then rests on canary through the whole abort delay (30s by default, about 1–2s with this manifest). Old: the GET at Degraded sees canary and fails every time. New: `:575` succeeds once the canary is scaled down, `:578` and `:582` pass **[T for the code, I for the timing]**.

**Reverse.** Nothing found. Tried: selector never reset (both fail; new by timeout); selector stable at Degraded then permanently reverted (both pass on their first sample); two samples instead of one adds almost nothing.

**Can the wait pass on a transient?** Yes, in principle: with the stage-swap bug the selector is stable for a few ms per reconcile. A 250ms poll must hit that window and `:578` must hit it again, so it is unlikely per run **[I]**.

**Can it still flake?** Yes **[I]**. `:578` is still a single GET taken a few ms after the wait returns. In the common case the wait's first poll succeeds, so `:578` runs right after Degraded, exactly when the next reconcile is flapping. The wait only helps when its own first sample lands inside a flap. No new flake mode was introduced. A wrong hash (k8s upgrade, `E2E_POD_DELAY`, `E2E_IMAGE_PREFIX`) now costs 90s instead of failing at once.

---

## 4. TestCanaryDynamicStableScaleAbortScaleDownDelay

**Diff.** Old line 716 `Sleep(2*time.Second)` became `:725` `WaitForRolloutCanaryWeight(0)`. Everything else is identical.

**Old.** After the last stable pod's readiness gate was set, wait a fixed 2s, then assert once: revision 2 still has 3 pods; canary weight 0 and stable 100; RS 2 carries the scale-down-deadline annotation. So it verified (i) weight reaches 0 and the annotation appears within 2s, and (ii) the canary still has its pods *some time after* traffic returned, the only evidence in the test that `abortScaleDownDelaySeconds: 10` is honoured. The fourth stable pod had only just been created, so its container start ate into the 2s, a real flake source **[I]**.

**New.**
- `:725`: waits up to 90s for `status.canary.weights.canary.weight == 0` (`test/fixtures/when.go:517-522`).
- `:729`: revision 2 has 3 pods.
- `:732`, `:733`: weights 0 and 100.
- `:735`: annotation present.
- `:738`-`:745`: unchanged tail (canary drains, stable has 4, both Services select the same hash).

All of `:729`-`:735` now run at the first instant weight 0 is visible.

**Is the wait sound? [T]**
- Weight 0 requires stable fully available: `expectedCanaryWeight = max(0, 100 − 100×avail/4)` (`rollout/trafficrouting.go:373`, `:379-383`), plus the guard at `:273`. With 3 of 4 available it is 25, so the wait cannot be met early by a correct controller.
- In the reconcile that sets weight 0, the annotation is written first: stage `replicaSetScaling` reaches `rollout/replicaset.go:201-202` under the same condition (`AvailableReplicas >= spec.replicas`), stable is already at 4 so `rollout/canary.go:348-355` does not stop early, and the status carrying weight 0 is persisted afterwards. So `:735` is not racy today.
- The helper cannot miss an already-true state: the watch function ignores the resource version it is handed (`when.go:578-583`), so every watch starts with the current object.

**Verdict: different.**

*Weaker: canary hold time.* **Old catches, new misses:** the canary is scaled down right after the deadline is written, ignoring the delay. Concretely: invert `rollout/replicaset.go:173` (`scaleDownAt.After(now)`), or compute the deadline at `replicaset.go:64` without adding the delay. The next reconcile sets `newReplicasCount = 0` (`:187`) and the canary pods go within a few hundred ms. Old: at +2s, `ExpectRevisionPodCount("2", 3)` sees 0 and fails. New: `:729` runs within ms of the status patch that ends the annotating reconcile, before the next reconcile and the ReplicaSet controller have removed anything; it sees 3. `:735` passes (the annotation is kept, `rollout/sync.go:375-378`), `:738` passes at once, the tail passes **[T for the code, I for who wins the race]**. Nothing else in the test puts a lower bound on how long the canary survives.

*Weaker: latency bound.* Traffic returning to stable in 2–90s now passes. This is the intended de-flake.

*Stricter: ordering.* **New catches, old misses:** the annotation written one or more reconciles after the status that reports weight 0 (for instance if `replicaset.go:201` were gated on the persisted `status.canary.weights` instead of stable availability). Old had 2s of slack; new asserts at once and would fail intermittently **[I]**. That is a benign reordering, so this is brittleness to refactors more than added protection. I looked for a single-line bug that reports weight 0 before stable is fully available and found none; `trafficrouting.go:273` blocks it.

**Transient satisfying the wait?** A weight that dips to 0 and returns would satisfy `:725`, but `:732` re-reads it. Not a practical concern **[I]**.

**New flake?** None with the current controller. The removed sleep was itself the flake.

---

## 5. ExperimentSuite.TestRolloutWithExperimentAndAnalysis

**Diff.** Old line 29 `s.T().Parallel()` removed. Nothing else.

**Old.** Verified that an experiment step launches one experiment that succeeds, that `specRef: stable/canary` copy the pod specs, that `podTemplateHashValue: Stable/Latest` and `templates.*.podTemplateHash` resolve correctly into the analysis Job's env, and that Job metadata is propagated. It was the only parallel method and alphabetically the last of the six, so it paused, `TearDownSuite` ran, and then it ran alone **[T]**.

**New.** Same body, `test/e2e/experiment_test.go:28-87`: `:35`, `:40`, `:41`, the `Assert` block `:42-83` (hash checks `:47-49`, spec equality `:56-57`, env values `:63-64`, Job metadata `:68-69`, template hashes `:74-76`, `:81`), `:86`. It now runs as the last serial method, before `TearDownSuite`. Converted top-level tests are held at `t.Parallel()` until every serial top-level test has finished, so it is still alone **[T]** (`test/fixtures/env.go:70` is the first thing a converted test does).

**Verdict: equivalent.** Same assertions, same position (last in its suite), same isolation; only its order relative to `TearDownSuite` changed, and it depends on nothing that teardown removes.

**Break attempts.** Swapped stable/new arguments when building the experiment's analysis args (`rollout/experiment.go`, the `BuildArgumentsForRolloutAnalysisRun` call): `:63-64` fail in both. Wrong `specRef` source: `:56-57` fail in both. No difference in timing or input to exploit.

Unchanged and outside this change: `:80` compares a hash string with a `*ReplicaSet`, so it can never fail.

---

## Gaps and risks

| # | Where | Kind | What | What would close it |
|---|-------|------|------|---------------------|
| A | Test 4, `:725`-`:729` | weaker | Nothing now shows the canary keeps its pods after the deadline is set. A controller that drains the canary immediately after annotating passes. | Keep the wait and add the hold back after it: `WaitForRolloutCanaryWeight(0).Sleep(2*time.Second)` (8s of margin on a 10s delay). Stronger: assert 3 pods several seconds in, and that pods reach 0 no earlier than the annotated deadline. |
| B | Test 3, `:575` | weaker | "Selector reset when Degraded is reported" became "within 90s". A reset deferred until the canary is scaled down passes. | Assert ordering instead of immediacy: raise `abortScaleDownDelaySeconds` in `canary-scaledownonabort.yaml` to 5–10s, and after the selector wait assert `ExpectRevisionPodCount("2", 1)` (reset happened while the canary was still up), then wait for 0. Give the wait a short explicit timeout. |
| C | Test 3, controller | masked defect | The wait hides a controller flap: after an abort with traffic routing, each reconcile re-points the canary Service at the canary RS (`rollout/service.go:283`) and back (`rollout/trafficrouting.go:212`) while the canary has available pods. Traced in code, not run. The same flap applies to test 4 during its 10s hold. | Confirm on a cluster (two `SwitchService` events per reconcile after abort). If confirmed, skip the canary-Service-to-newRS step in `reconcileStableAndCanaryService` when aborted with traffic routing; the original immediate check then becomes deterministic. |
| D | Test 3, `:578` | flake | The retained single GET can still land in a flap right after the wait returns. | Drop it (redundant with `:575`), or move it after the 3s sleep, when the canary has no available pods and the selector is at rest. |
| E | Test 4, `:735` | flake (latent) | Asserts the annotation the instant weight 0 is visible. Correct today only because the annotation patch precedes the status patch in one reconcile. A refactor that reorders them makes this intermittent. | The 2s hold from A also covers this. |
| F | Test 4, `:725`; test 3, `:575` | weaker bound | Latency bound went from 2s / 0s to 90s. | Pass an explicit shorter timeout to both waits (e.g. 30s) if latency should stay bounded. |
| G | Test 1, `:66`, `:78` | flake | Assertions must land inside 10s and 5s pauses, now under parallel load. | Lengthen the pauses at `:36` and `:42` (the test promotes through every pause, so this costs no time), or make them indefinite. |
| H | Test 1, `PromoteRollout` fallback | flake (inferred) | Under load the helper may bump `spec.replicas` +1 and back, disturbing pod counts. | Out of scope for this file; watch for "Forcing reconciliation" in failing logs. |
| I | All | process | `E2E_RERUN_FAILS` defaults to 5 (`Makefile`), so any catch that depends on winning a race is retried away. Claims above that say "likely misses" assume this. | Judge these tests with `E2E_RERUN_FAILS=0`. |

Tests 2 and 5: no gap found.

---

# Resolution

Written by the author of the changes, after the review. Letters refer to the
rows of "Gaps and risks" above.

| Gap | What was done |
|---|---|
| A | Fixed in `d7dcacc0`: the wait stays and `Sleep(2*time.Second)` is back after it, so the test again shows the canary holding its pods after traffic returns to stable. |
| B, D, F (test 3) | The added wait was taken out again in `4c8f2b2b`. `TestCanaryScaleDownOnAbort` is byte-identical to the baseline. |
| C | Confirmed on a cluster, see below. Not fixed here: it is a controller change, and whether and how to make it is the maintainers' decision. |
| E | Covered by the fix for A. |
| F (test 4) | Accepted. The wait uses the suite's normal timeout. These tests assert state and order, not how fast the controller gets there. |
| G | No change. `TestCanarySetCanaryScale` did not fail in any measured run (60 runs at parallelism 4, 8 and 16). To revisit if it does. |
| H | No change. Watch for "Forcing reconciliation" in the output of failing runs. |
| I | All measurements of these tests are taken with reruns off. |

## Gap C: the canary Service flaps after an abort

The review traced this in code. A controller log from a real run confirms it.

In fork workflow run 46035190342 (commit `c6764a29`, Kubernetes 1.37), the
controller log has these counts of `Switched selector for service
'canary-scaledowndelay-canary'`, alternating between the stable and the canary
pod-template hash:

| Test | `abortScaleDownDelaySeconds` | Selector switches | Over |
|---|---|---|---|
| `TestCanaryScaleDownOnAbort` | 1 | 37 | 3 seconds |
| `TestCanaryUnScaleDownOnAbort` | 0 (canary is never scaled down) | 130 | 6 seconds, until the test ended |

Cause, as the review describes: for an aborted canary rollout that uses
traffic routing without `dynamicStableScale`, and whose canary ReplicaSet
still has an available pod,

- `reconcileStableAndCanaryService` (`rollout/service.go`) points the canary
  Service at the canary ReplicaSet, because its abort branch only covers
  rollouts without traffic routing;
- the abort branch of the traffic-routing reconcile
  (`rollout/trafficrouting.go`) then points it at the stable ReplicaSet.

Both happen in every reconcile, and each is a Service patch and an event.

`TestCanaryScaleDownOnAbort` reads the selector once, right after the rollout
reports Degraded. It fails when that read lands on the canary half of a flap:
4 times in 40 runs at parallelism 8 and 16, never in 20 runs at parallelism 4.
So the test catches the defect, but only by chance.

Open, for the maintainers:

- whether to fix the controller, for example by not pointing the canary
  Service at the canary ReplicaSet in `reconcileStableAndCanaryService` when
  the rollout is aborted;
- whether to add a test that catches the flap every time, for example by
  counting `SwitchService` events after an abort.
