# Fix plan: a blue-green Rollout can leave its workloadRef Deployment scaled up

**Status:** not fixed. Nothing in the controller was changed for this.

## The defect

A Rollout that takes its pod template from a Deployment (`spec.workloadRef`)
with `scaleDown: onsuccess` scales that Deployment to zero once the Rollout is
healthy. With the blue-green strategy, if that one update of the Deployment
fails, the controller drops the error and does not try again. The Deployment
keeps its replicas next to the Rollout's own until something else makes the
controller sync the Rollout, which for a healthy Rollout can be the next
resync, fifteen minutes later by default.

The update fails with an ordinary conflict when the Deployment changed between
the controller's read and its write. Right after a migration the Deployment's
own pods are still becoming ready, so its status is being written at that
moment.

## Evidence

Fork workflow run 46107323320 (commit `9f52246a`, Kubernetes 1.37, 30 runs of
the e2e tests at parallelism 8 with reruns off).

`TestScaleDownOnSuccess` failed in 2 of the 30 runs: `Deployment replicas count
expectation 'The deployment has been scaled to 0 replicas' failed. Expected: 0,
Actual: 2`, checked every five seconds for a minute.

Controller log of one of them (job 2, namespace
`e2e-scaledownonsuccess-ckpbf`, Rollout `rollout-ref-deployment`):

| Time | Log |
|---|---|
| 04:43:33 | `Patched: {"status":{… "phase":"Healthy" …}}` |
| 04:43:33 | `Started syncing rollout` (the next sync) |
| 04:43:33 | `Scaling deployment rollout-ref-deployment to 0 replicas` |
| 04:43:33 | `level=warning msg="Failed to update deployment rollout-ref-deployment: Operation cannot be fulfilled on deployments.apps "rollout-ref-deployment": the object has been modified; please apply your changes to the latest version and try again"` |
| 04:43:33 | `No status changes. Skipping patch`, `Reconciliation completed` |
| 04:43:33 to 04:44:34 | nothing for this Rollout, until the test deleted it |

The other failure (job 3, namespace `e2e-scaledownonsuccess-2kl9n`) has the
same two lines and the same silence. The 28 runs that passed have the `Scaling
deployment` line without the warning.

The same warning appears 82 more times in the six controller logs of that run,
in other tests. Those are from the progressive scale-down in
`rollout/replicaset.go`, which runs again on every later sync while the
Rollout is still progressing.

## Root cause

`promoteStable` in `rollout/sync.go` scales the Deployment and returns the
error (added in 2023, PR #3111):

```go
if revision == 1 && c.rollout.Status.Phase == v1alpha1.RolloutPhaseHealthy && c.rollout.Spec.WorkloadRef != nil && c.rollout.Spec.WorkloadRef.ScaleDown == v1alpha1.ScaleDownOnSuccess {
    var targetScale int32 = 0
    err := c.scaleDeployment(&targetScale)
    if err != nil {
        return err
    }
}
```

The canary strategy passes it on (`rollout/canary.go`):

```go
if reason := c.shouldFullPromote(newStatus); reason != "" {
    err := c.promoteStable(&newStatus, reason)
    if err != nil {
        return err
    }
```

The blue-green strategy does not look at it (`rollout/bluegreen.go`, the call
is from 2021, before `promoteStable` could fail this way):

```go
if reason := c.shouldFullPromote(newStatus); reason != "" {
    c.promoteStable(&newStatus, reason)
}
```

So the sync ends without an error, the work queue forgets the Rollout, and
nothing brings it back:

- The sync wrote nothing to the Rollout, so there is no change event for it.
- The Deployment's `spec` did not change, so the workload watcher has nothing
  to tell the Rollout either.
- A healthy Rollout is not requeued for a progress check.

`scaleDeployment` (`rollout/scale_utils.go`) reads the Deployment, sets
`spec.replicas` and sends the whole object back with `Update`, which is what
makes it sensitive to any change in between, a status update included.

## Proposed change

Return the error in the blue-green path, as the canary path does:

```diff
 	if reason := c.shouldFullPromote(newStatus); reason != "" {
-		c.promoteStable(&newStatus, reason)
+		if err := c.promoteStable(&newStatus, reason); err != nil {
+			return err
+		}
 	} else {
```

The sync then fails, is retried with back-off, and the next attempt reads the
Deployment again.

## Alternatives considered

- **Make the write not depend on the Deployment's version.** Scale through the
  `scale` subresource or with a patch of `spec.replicas`. A conflict can then
  not happen. Worth doing as well; on its own it leaves the dropped error in
  place for every other failure of that call.
- **Retry on conflict inside `scaleDeployment`.** Fixes the conflict only.

## Risks and what to check

- With the error returned, the status computed in that sync is not written in
  that sync. The canary path already behaves this way. For blue-green, check
  that nothing else in `syncRolloutStatusBlueGreen` relies on the status being
  written in the same sync as the promotion.
- A failing scale-down is then retried through the rate limiter and counts as
  a sync error in the metrics. That is the intended effect.
- The progressive scale-down (`scaleDown: progressively`, in
  `rollout/replicaset.go`) also only logs a failed scale-down, including the
  final one to zero when the Rollout is healthy. Whether that can leave
  replicas behind in the same way was not checked.

## Tests

- **Already there:** `TestScaleDownOnSuccess` in `test/e2e/functional_test.go`
  fails when the conflict happens, 2 of 30 runs. It is not disabled; it passes
  when it is rerun.
- **Written, disabled:** `TestScaleDownOnSuccessUpdateConflict` in
  `test/e2e/controller_faults_test.go`. It makes every `update` of a
  Deployment by the controller fail with a conflict until the controller has
  nothing left to do on its own, then lets the updates through and expects the
  Deployment at zero replicas within a minute. It needs the controller inside
  the test binary, which is the default, and is skipped by default as a known
  failure. Run it by name:

  ```
  make test-e2e E2E_TEST_OPTIONS="-run TestScaleDownOnSuccessUpdateConflict"
  ```

  A first version failed only the controller's first update. That passed with
  the defect in place (fork run 46113135454): a change event for the Rollout
  was still waiting when the update failed, so the controller synced once
  more, and that sync scaled the Deployment down. The defect shows when the
  failed update is in the last sync, as in the two failures above.

## How to validate the fix

1. Apply the change.
2. Remove the `KnownFailure` line from `TestScaleDownOnSuccessUpdateConflict`
   and its entry in `test/e2e/expected-skips.txt`; the test has to pass.
3. Run the e2e tests 30 times with reruns off and expect no failure of
   `TestScaleDownOnSuccess`.
