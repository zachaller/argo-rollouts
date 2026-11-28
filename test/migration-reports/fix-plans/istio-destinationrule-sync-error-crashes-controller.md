# Fix plan: an error while syncing a DestinationRule crashes the controller

**Status:** fixed in `064b3513` with the change proposed below. The unit test named
under "Tests" is no longer skipped. The rest of this file is the plan as it was
written before the fix.

## The defect

When the Istio DestinationRule worker's sync returns any error, the whole
rollouts controller process exits with a nil-pointer panic. The error can be as
ordinary as an update conflict.

## Evidence

Fork workflow run 46067850299, job `parallel 8, job 2` (commit `894598e8`,
Kubernetes 1.37). The controller log ends with:

```
level=error msg="destinationrule syncHandler error: Operation cannot be fulfilled on
  destinationrules.networking.istio.io \"istio-subset-split-destrule\": the object has been
  modified; please apply your changes to the latest version and try again"
panic: runtime error: invalid memory address or nil pointer dereference [recovered, repanicked]
...
github.com/argoproj/argo-rollouts/utils/controller.processNextWorkItem.func1(...)
	utils/controller/controller.go:174
github.com/argoproj/argo-rollouts/utils/controller.RunWorker(...)
	utils/controller/controller.go:108
github.com/argoproj/argo-rollouts/rollout/trafficrouting/istio.(*IstioController).Run.func1()
	rollout/trafficrouting/istio/controller.go:147
exit status 2
```

The e2e run in progress was stopped at that point.

It happened again in the same test in fork workflow run 46080392030, job
`parallel 16, job 4` (commit `5acb458b`), this time with nothing else wrong.
The log shows the worker's own two steps:

```
msg="cleaning destinationrule: rollout does not exist" destinationrule=istio-subset-split-destrule
msg="cleaned destination rule" destinationrule=istio-subset-split-destrule
level=error msg="destinationrule syncHandler error: Operation cannot be fulfilled on
  destinationrules.networking.istio.io \"istio-subset-split-destrule\": the object has been modified; ..."
panic: runtime error: invalid memory address or nil pointer dereference [recovered, repanicked]
```

The first sync cleaned the DestinationRule. The update it made queued the rule
again, the second sync read a copy from before that update, tried the same
clean-up, and got the conflict.

It did not happen in 68 runs of the unconverted, serial suites that were
collected for the migration. It happened in 2 of 17 runs of the converted
tests at parallelism 8 and 16, where the controller is busier and its caches
lag further behind. Until the fix, the four e2e tests that use a
DestinationRule ran one at a time; they run in parallel again.

## Root cause

`processNextWorkItem` in `utils/controller/controller.go` reports a sync error
to the metrics server without checking that there is one:

```go
logCtx.Errorf("%s syncHandler error: %v", objType, err)
metricsServer.IncError(namespace, name, objType)
```

The Istio controller is the only caller that passes none
(`rollout/trafficrouting/istio/controller.go`):

```go
controllerutil.RunWorker(ctx, c.destinationRuleWorkqueue, "destinationrule", c.syncDestinationRule, nil)
```

The `recover` in `processNextWorkItem` only wraps the sync handler itself, so
the nil call is outside it. `wait.Until` logs the panic and panics again, and
the process exits.

`git blame` dates the unguarded call to 2020 and the `nil` argument to 2022 or
earlier.

`syncDestinationRule` returns an error for a failed Rollout lookup other than
NotFound, a DestinationRule that does not decode, and a failed update of the
DestinationRule. Any of those is enough.

## How it was reached the first time

Three things lined up in the first run:

1. The Experiment of `TestIstioSubsetSplitExperimentStep` lost its Service name
   and pod-template hash from its status (see
   `experiment-status-loses-service-name.md`). The rollout event shows it:
   `Traffic weight updated additional: [{20  }]`.
2. With an empty hash, the rollout controller added one more subset with an
   empty name to the DestinationRule on every reconcile, and each update
   triggered the next reconcile: 18 to 78 updates per second for eight seconds,
   the list growing by one empty subset each time.
3. The Rollout was deleted while that loop was running. The Istio worker then
   tried to clean the DestinationRule from a copy that was already out of
   date, got the conflict above, and crashed the controller. The test's own
   output was not kept, so why it ended there is not known; the likely reason
   is that it read the experiment's route from the VirtualService, which was
   missing, and panicked.

Step 2 is a consequence of the Experiment defect and is not planned separately:
with the names in place the subset has a name and is matched on the next
reconcile. Whether the router should refuse to write a subset without a name
is worth a look when that defect is fixed.

Step 3 does not need steps 1 and 2, as the second run showed. Any conflict on a
DestinationRule update is enough, including the one the worker causes itself
when it syncs a rule twice in quick succession.

## Proposed change

Guard the call in `processNextWorkItem`:

```diff
 			logCtx.Errorf("%s syncHandler error: %v", objType, err)
-			metricsServer.IncError(namespace, name, objType)
+			if metricsServer != nil {
+				metricsServer.IncError(namespace, name, objType)
+			}
```

With that, a DestinationRule sync error is logged and the key is requeued with
back-off, as for every other worker.

## Alternatives considered

- **Give the Istio controller the metrics server.** Its errors would then be
  counted like the others. It needs the server threaded through
  `NewIstioController` and its config, and the nil guard is still worth having:
  a helper that takes an optional argument should not crash without it.
- **Widen the `recover`** to cover the whole work item. It would turn this
  crash into a logged error, but it would also hide the next bug of this kind.

## Risks and what to check

- `metricsServer` is an interface. A typed nil pointer stored in it would pass
  the `!= nil` check and still panic. No caller does that today; the Istio
  controller passes a plain `nil`.
- Nothing else changes for callers that pass a server.

## Tests

- **Already written, disabled:**
  `TestProcessNextWorkItemSyncErrorWithoutMetricsServer` in
  `utils/controller/controller_test.go`. It runs the worker loop with a sync
  handler that returns a conflict error and no metrics server, and expects no
  panic and the key requeued. It is skipped by default. Enabled, it fails today
  with the same nil-pointer panic as the run above:

  ```
  RUN_KNOWN_FAILURES=true go test ./utils/controller/ -run TestProcessNextWorkItemSyncErrorWithoutMetricsServer
  ```

- An e2e test cannot hit this on purpose; it needs an update conflict.

## How to validate the fix

1. Apply the change and remove the skip from the unit test.
2. Run the converted e2e tests repeatedly at parallelism 8 and 16 with reruns
   off (manual dispatch of the `Testing` workflow: `e2e_parallel: 8,16`,
   `e2e_jobs: 4`, `e2e_repeat: 5`, `e2e_rerun_fails: 0`). Expect no
   `controller exited` line in any job's `e2e-timings.txt`.
3. In a controller log from a run where an Istio experiment test failed, expect
   `destinationrule syncHandler error` lines followed by a requeue, not a panic.
