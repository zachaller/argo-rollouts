# Fix plan: the canary Service flaps after an abort with traffic routing

**Status:** plan only. No controller code has been changed, and the proposed
change below has not been built or run.

## The defect

When a canary rollout that uses traffic routing (without `dynamicStableScale`)
is aborted, the controller should point the canary Service at the stable
ReplicaSet once. Instead, for as long as the canary ReplicaSet still has an
available pod, every reconcile points the canary Service at the canary
ReplicaSet and then back at the stable one.

Each half of that is a Service patch and a `SwitchService` event.

How long it lasts depends on `abortScaleDownDelaySeconds`:

- default (30 seconds): until the canary is scaled down;
- `0` (never scale the canary down): for as long as the rollout stays aborted.

## Evidence

From the controller log of fork workflow run 46035190342 (commit `c6764a29`,
Kubernetes 1.37), counting `Switched selector for service
'canary-scaledowndelay-canary'`:

| Test | `abortScaleDownDelaySeconds` | Selector switches | Over |
|---|---|---|---|
| `TestCanaryScaleDownOnAbort` | 1 | 37 | 3 seconds |
| `TestCanaryUnScaleDownOnAbort` | 0 | 130 | 6 seconds, until the test ended |

The peak was 46 switches in one second for one rollout.

The same abort without traffic routing
(`TestCanaryScaleDownOnAbortNoTrafficRouting`) switched the canary Service
three times in total: once at creation, once on the update, once on the abort.
That is the expected count.

`TestCanaryScaleDownOnAbort` reads the selector once, right after the rollout
reports Degraded, and fails when the read lands on the canary half of a flap.
It failed 6 times in 60 runs of the converted tests at the current head, and it
is the only Canary test that failed in those runs.

## Root cause

Two reconcile stages both set the canary Service's selector, in this order
(`rollout/stages.go`, `canaryStages`):

1. **`stableCanaryService`**, `reconcileStableAndCanaryService` in
   `rollout/service.go`. Its abort branch only covers rollouts *without*
   traffic routing:

   ```go
   if c.pauseContext != nil && c.pauseContext.IsAborted() && c.rollout.Spec.Strategy.Canary.TrafficRouting == nil {
       err = c.ensureSVCTargets(c.rollout.Spec.Strategy.Canary.CanaryService, c.stableRS, true)
       ...
       return nil
   }
   ```

   An aborted rollout *with* traffic routing falls through to the end of the
   function, which points the canary Service at `c.newRS`, the canary
   ReplicaSet, whenever that ReplicaSet has at least one available pod:

   ```go
   err = c.ensureSVCTargets(c.rollout.Spec.Strategy.Canary.CanaryService, c.newRS, true)
   ```

2. **`trafficRouting`**, the abort branch of `reconcileTrafficRouting` in
   `rollout/trafficrouting.go`, which points it back at stable:

   ```go
   err = c.ensureSVCTargets(c.rollout.Spec.Strategy.Canary.CanaryService, c.stableRS, false)
   ```

`switchServiceSelector` updates the lister's copy of the Service in place, so
stage 2 sees what stage 1 just did and patches again.

The rate in the log (dozens of switches per second) suggests each patch also
triggers the next reconcile through the Service informer. That part is
inferred from the rate, not traced.

### How it got this way

| Change | What it did |
|---|---|
| #2540 (February 2023) | Added the reset to stable in the traffic-routing abort branch. |
| #2818 (July 2023) | Added the abort branch in `reconcileStableAndCanaryService`, restricted to rollouts without traffic routing, to fix #2781. |
| #3338 (February 2024) | Dropped the availability check from the traffic-routing reset, because of a deadlock with ALB readiness gates. |

None of them stopped `reconcileStableAndCanaryService` from pointing the canary
Service at the canary ReplicaSet for an aborted, traffic-routed rollout.

## Proposed change

In `reconcileStableAndCanaryService`, return early for every aborted rollout.
Keep the existing reset for rollouts without traffic routing, and leave the
canary Service alone for those with it, because `reconcileTrafficRouting`
already owns that decision.

```diff
-	if c.pauseContext != nil && c.pauseContext.IsAborted() && c.rollout.Spec.Strategy.Canary.TrafficRouting == nil {
-		err = c.ensureSVCTargets(c.rollout.Spec.Strategy.Canary.CanaryService, c.stableRS, true)
-		if err != nil {
-			return err
-		}
-		return nil
-	}
+	if c.pauseContext != nil && c.pauseContext.IsAborted() {
+		if c.rollout.Spec.Strategy.Canary.TrafficRouting == nil {
+			// Without traffic routing this is the only place that moves the canary
+			// Service back to stable.
+			return c.ensureSVCTargets(c.rollout.Spec.Strategy.Canary.CanaryService, c.stableRS, true)
+		}
+		// With traffic routing, reconcileTrafficRouting moves the canary Service back
+		// to stable: at once, or for dynamicStableScale when the abort weight reaches
+		// zero. Pointing it at the canary ReplicaSet here would undo that on every
+		// reconcile.
+		return nil
+	}
```

Expected effect, by case:

| Aborted rollout | Today | After the change |
|---|---|---|
| No traffic routing | Service stage sets stable. | Unchanged. |
| Traffic routing, no `dynamicStableScale` | Service stage sets canary, traffic stage sets stable, every reconcile. | Service stage does nothing; traffic stage sets stable once. |
| Traffic routing with `dynamicStableScale`, abort weight above zero | Service stays on canary. | Service stage does nothing, so it stays on canary. |
| Traffic routing with `dynamicStableScale`, abort weight zero | Service stage sets canary, traffic stage sets stable, every reconcile while the canary has pods. | Traffic stage sets stable once. |

The last row is read from the code. I have not looked for the flap in a log of
a `dynamicStableScale` abort.

## Alternatives considered

- **Make one stage the only owner.** Move the whole abort reset into
  `reconcileStableAndCanaryService` and delete it from the traffic-routing
  branch. Cleaner, but it has to carry over the `dynamicStableScale` weight
  condition and the "no availability check" rule from #3338, and the Service
  stage runs before the traffic-routing stage computes the abort weight.
- **Guard `ensureSVCTargets`.** Refuse to point the canary Service at the
  canary ReplicaSet when aborted. It hides the problem inside a helper that
  other callers use for unrelated switches.

## Risks and what to check

- **ALB readiness gates (#3338).** The proposed change does not touch the
  traffic-routing reset or its availability rule. Still worth a run on a real
  ALB if one is available; CI has none.
- **Abort, then retry or a new update.** Once the abort is cleared the rollout
  is no longer aborted and the function behaves as today. Check that the
  canary Service returns to the canary ReplicaSet after `retry`.
- **Abort before the canary Service was ever pointed at the canary.** It then
  stays on stable, which is the end state anyway.
- **Ping-pong services.** They are reconciled in a different stage and are not
  part of this change. Whether they have the same problem was not checked.
- **Unit tests that pin the current calls.** `rollout/service_test.go` and
  `rollout/canary_test.go` assert exact API actions and may expect the extra
  Service patch.

## Tests

- **Already written, disabled:** `TestCanaryAbortSwitchesCanaryServiceOnce` in
  `test/e2e/canary_test.go`. It aborts a traffic-routed canary whose canary is
  never scaled down and asserts exactly one `SwitchService` event after the
  abort. It is skipped by `fixtures.KnownFailure` so it does not block CI. Run
  it on purpose with:

  ```
  make test-e2e E2E_TEST_OPTIONS="-run '^TestCanaryAbortSwitchesCanaryServiceOnce$'"
  ```

- **Already there:** `TestCanaryScaleDownOnAbort` should stop failing
  intermittently under parallel load.
- **Worth adding with the fix:** the same assertion for an abort with
  `dynamicStableScale`.

## How to validate the fix

1. Apply the change and delete the `fixtures.KnownFailure` line from
   `TestCanaryAbortSwitchesCanaryServiceOnce`. Remove its entry from
   `test/e2e/expected-skips.txt`.
2. Run the Canary tests 20 times at parallelism 16 with reruns off (manual
   dispatch of the `Testing` workflow: `e2e_test_options: -skip Suite`,
   `e2e_parallel: 16`, `e2e_jobs: 4`, `e2e_repeat: 5`, `e2e_rerun_fails: 0`).
   Expect no failures; today `TestCanaryScaleDownOnAbort` failed in 7 of 40
   runs at that level.
3. Run the seeded bugs (`e2e_seeded_bugs`) and compare with
   `go run ./hack/testmigration killmatrix`. Bugs 09 and 10 are the abort ones
   and must still be caught.
4. Run the full suite on all four Kubernetes versions.
5. Check a controller log from step 2: after an abort there should be one
   `Switched selector` line for the canary Service, not dozens.
