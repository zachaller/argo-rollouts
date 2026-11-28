# Fix plan: a rollout can sit idle for up to ten seconds after a streak of sync errors

**Status:** not fixed. Nothing in the controller was changed for this.

## The defect

After the sync of a Rollout has failed several times in a row and then
succeeds, the controller can leave the Rollout alone for up to ten seconds
although its next step is ready to start. Nothing is wrong in the end: the
rollout continues when the delay is over. It is slower than it should be, and
anything that expects the next step within a few seconds sees it missing.

## Evidence

Fork workflow run 46107323320 (commit `9f52246a`, Kubernetes 1.37, 30 runs of
the e2e tests at parallelism 8 with reruns off).

**The symptom.** `TestIstioSubsetSplitExperimentStep` failed in 2 of the 30
runs. It waits for the rollout to reach its experiment step, sleeps 10 seconds
and expects the experiment's weight on the VirtualService
(`istio_test.go:464`: expected 70, actual 90).

**What the controller did**, from its log for one of the two failures
(job 5, namespace `e2e-istiosubsetsplitexperimentstep-jrp6v`):

| Time | Log |
|---|---|
| 04:32:01 to 04:32:02 | ten syncs end with `rollout syncHandler error: delaying destination rule switch: ReplicaSet … not fully available`, each followed by `rollout syncHandler queue retries: N`, N going from 2 to 13 |
| 04:32:06 | `Started syncing rollout`; `Rollout step 1/2 completed (setWeight: 10)`; `rollout enqueue due to update event`; `Patched: …`; `Reconciliation completed` |
| 04:32:06 to 04:32:16 | nothing for this rollout |
| 04:32:16 | `Started syncing rollout`; `Created Experiment 'istio-subset-split-7bbf694886-2-1'` |
| 04:32:17 | the experiment's pod is available; `Traffic weight updated additional: [{20 …}]` |

The test read the VirtualService at 04:32:16, a second too early. The second
failure (job 5, namespace `…-vlbjp`) has the same shape: queue retries up to
12, then 10 seconds without a sync.

**How often.** Counted over the six controller logs of that run (30 runs of
the whole suite):

- 167 rollouts reached `queue retries` of 10 or more.
- 28 times a rollout was next synced 8 seconds or more after such a streak:
  18 times in the 30 runs of `TestIstioSubsetSplitExperimentStep`, 4 in
  `TestIstioSubsetSplitSingleRoute`, 4 in `TestAnalysisWithArgs`, one each in
  two more Istio tests.

Most of those passed anyway, because the test happened not to look during the
gap. The most frequent sync errors in those logs are `delaying destination
rule switch` (about 240 in the five runs of a job) and validation errors about
analysis templates (about 170).

None of this is new with the converted tests: the code involved is from 2020,
2021 and March 2026 (see below), and neither parallelism nor namespaces play a
part in it.

## Root cause

Three pieces, all in upstream code.

1. Every change event for a Rollout, and for the ReplicaSets and other objects
   it owns, enqueues the Rollout **rate-limited** (`rollout/controller.go`,
   since 2020):

   ```go
   controller.enqueueRollout = func(obj any) {
       controllerutil.EnqueueRateLimited(obj, cfg.RolloutWorkQueue)
   }
   ```

2. The rate limiter is a per-key exponential back-off, 1 ms doubling up to
   10 seconds (`utils/queue/queue.go`, since 2021):

   ```go
   workqueue.NewItemExponentialFailureRateLimiter(time.Millisecond, 10*time.Second)
   ```

   It counts every `AddRateLimited` for a key and starts again from zero when
   the key is forgotten.

3. The worker loop (`utils/controller/controller.go`) uses the same queue,
   and so the same counter, for retries: a failed sync calls
   `AddRateLimited`, a successful one calls `Forget` **after** the sync
   handler has returned.

So events and errors add to one counter, and only a successful sync resets it.
After ten failed syncs and a few events the counter is at 13 or 14. The sync
that finally succeeds patches the Rollout's status. The change event for that
patch arrives while the sync handler is still returning, is enqueued with the
back-off of the counter as it is then (8 or 10 seconds), and only afterwards
does `Forget` reset the counter. The queue keeps the delay it was given. If
nothing else changes for that Rollout, and nothing does when the next thing to
happen is the controller's own next step, the Rollout waits the delay out.

What makes the streak in the Istio tests is that the Istio router reports "not
yet" as an error (`rollout/trafficrouting/istio/istio.go`, since March 2026,
PR #4612):

```go
if shouldDelay, rsName := r.shouldDelayDestinationRuleUpdate(canaryHash, stableHash); shouldDelay {
    return fmt.Errorf("delaying destination rule switch: ReplicaSet %s not fully available", rsName)
}
```

Each of those errors is retried through the rate limiter, about ten times in
the second or two the ReplicaSet needs. Every subset-level Istio rollout goes
through this on every update.

## Options

**A. Count events and errors apart.** Keep the exponential back-off for
retries after an error. Give change events their own limiter, also per key and
also reset by a successful sync, so that an event storm is still damped but an
error streak no longer delays the event that follows the recovery. This keeps
what the 2021 change was for (issue #1193, objects re-added constantly) and
removes the stall. It needs a queue wrapper or a second limiter in the three
places that enqueue Rollouts this way: the rollout, service and ingress
controllers.

**B. Look again shortly after a recovery.** In `processNextWorkItem`, when a
sync succeeds for a key that had retries, add the key again after a short
fixed delay (the stale-cache path already uses 100 ms). The queue keeps the
earlier of two delays, so this overrides the long one. Smallest change, one
place, all controllers. It costs one extra sync after each recovery, and
because events count as retries today, after many ordinary syncs too.

**C. Stop reporting "not yet" as an error in the Istio router.** Return a
dedicated error that the worker loop requeues after a fixed delay without
counting it, as it does for a stale cache. Removes the largest source of
streaks and about 50 error log lines per e2e run, but leaves the stall in
place for every other error.

**D. Enqueue events without the rate limiter.** The experiment and analysis
controllers already enqueue their own objects that way
(`controllerutil.Enqueue`). Simple, but for Rollouts it removes the only
damping there is for loops in which the controller's own writes trigger its
next sync. Two such loops were seen in this migration (the canary Service
flap, and the DestinationRule loop described in
`istio-destinationrule-sync-error-crashes-controller.md`); without the limiter
they would run as fast as the API client allows.

A and C fit together. B is the one to pick if a small change is wanted first.

## Risks and what to check

- Three controllers feed the Rollout queue. Whatever is changed, check the
  queue depth and sync rate metrics on a busy cluster, not only the e2e tests.
- Option B and option D raise the number of syncs. The e2e controller log
  gives a count (`Started syncing rollout` lines) to compare before and after.
- The stall also hides behind other symptoms. `TestAnalysisWithArgs` showed
  it 4 times in the 30 runs without failing.

## Tests

- **Already there:** `TestIstioSubsetSplitExperimentStep` fails when the stall
  is 10 seconds and the experiment's pod takes about a second, which was 2 of
  30 runs. It is not disabled; it passes when it is rerun.
- **Worth adding with the fix:** a unit test of the worker loop with a fake
  clock: a key fails thirteen times, the fourteenth sync succeeds and the key
  is enqueued by an event during it; the key has to be ready again within a
  second. Which function the test calls for the event depends on the option
  chosen, so it is not written yet.

## How to validate the fix

1. Run the e2e tests 30 times at parallelism 8 with reruns off (manual
   dispatch of the `Testing` workflow: `e2e_jobs: 6`, `e2e_repeat: 5`,
   `e2e_rerun_fails: 0`).
2. Count the stalls in the controller logs. Today: 28 in 30 runs. Expected:
   none. The count is: for each rollout, a `Started syncing rollout` 8 seconds
   or more after the previous `Reconciliation completed`, within 12 seconds of
   a `queue retries` line of 10 or more.
3. `TestIstioSubsetSplitExperimentStep` passes in all 30.
