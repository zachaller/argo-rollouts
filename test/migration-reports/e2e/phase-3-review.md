# Phase 3 review: `git diff 9f52246a..bf5065a1`

Reviewed read-only at bf5065a1 (c906827c has the same tree). Nothing was run against a cluster.
Tags: **[read]** I read the code, **[ran]** I executed something, **[inferred]** reasoning I could not execute.

Two findings matter most: the recovered-panic check does not survive gotestsum's reruns (1), and the
disabled scale-down test did not fail at bf5065a1 (2, already reworked in 580ea763).

## Findings

### 1. high — a recovered panic (or a failed `Stop`) does not fail the run when reruns are on, which is the default
`test/e2e/main_test.go:59-66`, `test/fixtures/controller.go:213-233`, `Makefile:33,55,311`
- What happens: the panic check lives in one test process and is reported only through that process's exit
  code. With `E2E_RERUN_FAILS=2` (Makefile and CI default) gotestsum takes its own exit code from the last
  round of reruns. Process 1: a test fails and the controller recovered a panic, so it exits 1. Process 2
  reruns only that test with a fresh controller and a fresh hook, sees no panic and exits 0. gotestsum exits
  0, `make test-e2e` passes, CI is green. All that is left is the line `FAIL: the controller recovered from
  N panic(s)` in process 1's output.
- How I know: [read] `run()` only turns `ctrl.Stop()`'s error into the exit code; nothing persists across
  processes, and neither the Makefile nor `hack/run-e2e.sh` looks at the log. [inferred] gotestsum's side:
  the first `go test` always exits 1 when a test fails, so `--rerun-fails` cannot carry that exit code over.
- Not verified: the case the feature was written for, a panic with no failed test. From memory of gotestsum
  v1.13 it treats "package failed, no failed test" as a failed test case with an empty name and reruns the
  package with `-test.run=^$`. That rerun starts a controller, runs nothing and exits 0. gotestsum's source
  is not in the module cache and I did not run it. To check: a package whose `TestMain` returns 1 after
  `m.Run()` returned 0, then `dist/gotestsum --rerun-fails=2 --packages=./pkg -- -count 1 ./pkg; echo $?`.
- A fix has to live outside the process, for example `run-e2e.sh` grepping `E2E_CONTROLLER_LOG` for
  `Recovered from panic` after gotestsum, or `Stop` writing a marker file that the make target checks.

### 2. medium — at bf5065a1 `TestScaleDownOnSuccessUpdateConflict` is not a failing test, so "disabled as a known failure" is not borne out
`test/e2e/controller_faults_test.go:87-154`, `rollout/bluegreen.go:257-258`, `rollout/sync.go:1362-1368`
- What happens: the test fails one `update deployments`. For a healthy blue-green Rollout,
  `shouldFullPromote` returns "Completed blue-green update" on every sync, and `promoteStable` calls
  `scaleDeployment(0)` again whenever `revision == 1 && Status.Phase == Healthy`. Any later sync therefore
  retries, and one is normally still queued right after the Healthy patch. The Deployment reaches 0, the
  1-minute poll in `ExpectDeploymentReplicasCount` succeeds, `Hits() == 1`, and the test passes.
- How I know: [read] the code above; the message of 580ea763 reports the same from fork run 46113135454.
- Also [read]: on a timeout the `assert.Equal(t, 1, conflict.Hits())` at line 153 is never reached, so a
  failure could not be told from "the fault was never hit".
- 580ea763, looked at as asked (`git diff bf5065a1..580ea763 -- test/`): the new shape is sound by reading.
  It fails every update, waits for `Hits() > 0`, waits for 3 s without writes, removes the fault and expects
  0 replicas. With the error returned, the queue back-off (1 ms doubling, 10 s cap,
  `utils/queue/queue.go:12`) reaches a 4 s gap, the quiet wait returns inside it, and the pending retry
  succeeds. Residual [inferred]: with the defect present, any Rollout sync during the 60 s poll after
  `Remove()` scales the Deployment down. The 15-minute informer resync is one, so in a long full run with
  `E2E_RUN_KNOWN_FAILURES=true` the known failure can pass (about 60/900). Run by name in a fresh process it
  cannot. `waitForControllerQuiet` keeps the `len(writes) == 0` early return of finding 3a; here the
  preceding `Hits() > 0` covers it.

### 3. medium — `TestHealthyRolloutControllerQuiet`: quiet holds by construction, but the test can pass vacuously and has one flake vector
`test/fixtures/then.go:570-602`, `test/e2e/controller_faults_test.go:72-81`
- Does the controller guarantee it for `functional/rollout-basic.yaml`? By reading, yes in steady state;
  it is a property of the code, not a stated contract. [read]
  - Resync is 15 min (`controller/controller.go:64`, `options.go:137`) and only re-enqueues.
  - The status patch is sent only when the diff is non-empty (`rollout/sync.go:1041-1046`).
  - `promoteStable` runs every sync for a finished canary but emits its event only when the stable hash
    changes (`sync.go:1336`).
  - `calculateStatusDuration` is state-driven, not clock-driven (`sync.go:450-530`).
  - ReplicaSet annotation and spec updates are conditional; the manifest has no Service, traffic routing,
    analysis or ephemeral metadata.
  - notifications-engine patches the Rollout only when annotations differ
    (`pkg/controller/controller.go`, `processResource`).
  - Leader election is off in-process (`test/fixtures/controller.go:125`), and its Lease would be in
    `argo-rollouts`, which no test watches.
- 3a, vacuous pass: the settle loop exits at once on `len(writes) == 0` (`then.go:578`), and nothing
  asserts that any write was ever recorded for the namespace. If recording is broken (finding 5, or a
  namespace that was never `watch`ed) the test passes. One line fixes it: require at least one recorded
  write before the window, since the controller must have created a ReplicaSet. [read]
- 3b, flake: Events count as writes (`create events`) and are written asynchronously by the broadcaster
  goroutine (`utils/record/record.go:94-96`) through the kube clientset's single 40 QPS / 80 burst limiter,
  shared with every other write of the controller. With 8 tests in parallel a `RolloutCompleted` event can
  land more than 2 s after the previous write in the namespace, inside the 10 s window. [inferred, not
  measured]. Excluding `events` from the quiet check, or settling longer, would remove it.
- Not verified: I did not run a reconcile of a healthy canary twice against fake clients to confirm zero
  actions; the claim rests on reading the write sites.

### 4. medium — `e2e_tests_ref` with `e2e_controller: default` runs three of four jobs with no controller
`.github/workflows/testing.yaml:209-218, 292-293, 305`; `hack/e2e-matrix.sh:75`
- What happens: the matrix gives 1.35 to 1.37 `controller: inprocess`, so "Start controller" is skipped.
  Tests from before acb3502a do not start one (their `TestMain` at 9f52246a only sweeps namespaces), so
  every test waits out its timeout. It fails loudly, not silently, but a "before" measurement from such a
  run is garbage. Only the input description warns about it. [read]
- Suggested: in the plan step, force `external` (or fail) when `inputs.e2e_tests_ref != ''`.

### 5. low — an API server URL with a path prefix disables recording and faults without any sign
`test/fixtures/apicalls.go:72-85, 291-295`
- `parseAPICall` requires the path to start with `api` or `apis`. A kubeconfig server such as
  `https://rancher/k8s/clusters/c-1` (also `kubectl proxy --api-prefix`) makes every call pass through
  unparsed: no writes recorded, no fault ever hit, and `ExpectControllerQuiet` passes (3a).
- [ran] probe: `/k8s/clusters/c-1/apis/apps/v1/namespaces/ns/deployments/d` gives `ok=false`. Not a CI
  case (k3s, no prefix).

### 6. low — an injected 429 or 5xx is not what the API server sends, and client-go treats it differently
`test/fixtures/apicalls.go:323-346`
- The response has no `Retry-After` header. A real 429 from the API server carries one and client-go
  retries it transparently; the injected one reaches the controller at once.
- [ran] against an httptest server through the wrapper: real 429 with `Retry-After: 1` gave `err=nil`,
  2 requests served and 2 writes recorded; injected 429 and 503 gave an immediate error with `hits=1`.
- The three tests only use 409, where the injected Status decodes to `IsConflict` in the typed and the
  dynamic client ([ran], existing `TestAPICallTransport`). The doc comment and the message of e153657a
  ("the error the API server would have sent") overstate this for 429 and 5xx.

### 7. low — `list` and `watch` faults cannot reach the informers of the in-process controller
`test/fixtures/apicalls.go:152-156, 293`
- The controller runs cluster-wide (`Namespace: NamespaceAll`), so informer list and watch requests carry
  no namespace (`/apis/argoproj.io/v1alpha1/rollouts`), and `RoundTrip` passes anything without a
  namespace straight through. `Fault.validate` accepts both verbs and the docs list them; such a fault
  only matches the few direct namespaced lists. [read], parse confirmed by the probe in 5.
- Same root: writes to cluster-scoped objects are never recorded, so "records every write the controller
  makes" (e153657a) holds for namespaced objects only.

### 8. low — `TestIstioDestinationRuleCleanupConflict` cannot pass vacuously, but its first assertion races the controller
`test/e2e/controller_faults_test.go:39-49`, `rollout/trafficrouting.go:184-187`, `utils/replicaset/canary.go:632-635`
- On an initial deploy `c.stableRS` is nil until `status.stableRS` is persisted, which is the same patch
  that makes the Rollout Healthy. The stable subset's hash label is therefore written by the next
  reconcile, after Healthy is visible. Line 42 reads the DestinationRule immediately. If the test wins the
  race the assertion fails, and the rollout controller's own DestinationRule update then consumes the
  injected conflicts. `TestIstioSubsetSplit` (`istio_test.go:187`) has the same exposure, and line 208 of
  that file sleeps 1 s for this ordering after a promote. [read]/[inferred]
- Why it is not vacuous [read]: once line 42 holds, no further DestinationRule update by the rollout
  controller is pending. After the delete, the only updater is `syncDestinationRule`
  (`istio/controller.go:268-286`), which returns the error and is retried by the worker; the clean-up
  needs a third, real update, and `Hits() == 2` is asserted after it. If the controller used `patch`
  instead, the test would fail, not pass.

### 9. low — the per-test "Controller log" is not complete, and the log file is not only the controller's
`test/fixtures/controller.go:143-157, 273-302`, `utils/log/log.go:111-121`, `analysis/analysis.go:376`
- Lines logged through `logutil.WithRedactor` (the per-metric logger of the analysis controller) use a new
  logrus logger without hooks. They reach the file but not the failure dump, and a `Recovered from panic`
  logged that way would not be counted. [ran] probe: 1 of 3 lines captured, 0 panics seen. Today the only
  `Recovered from panic` sites (`utils/controller/controller.go:159`, notifications-engine) log through
  the standard logger, so the panic count itself is not affected.
- klog is process-wide, so client-go lines from the tests' own clients (the reflectors of the view
  controllers behind `WaitForRolloutStatus`) land in the controller's log file. [inferred]
- Latent trap [read]: `options.PersistentPreRunE` (`pkg/kubectl-argo-rollouts/options/options.go:84-90`)
  sets the global klog `-v` to 0 once that flag exists, which it now does in-process. The fixtures only
  construct options (`common.go:166, 191`) and never execute a plugin command, so it is not triggered.

### 10. low — CI and tooling details
- `Makefile:47`: `$(shell go list ...)` does not fail make. If it prints nothing, `-coverpkg=` is empty and
  go test silently covers only the package under test. [ran] `go test -cover -coverpkg= ./utils/queue/` is
  accepted. The e2e coverage would be near empty with a green job.
- `Makefile:50`: `-test.gocoverdir=$(CURDIR)/...` is unquoted and breaks in a checkout path with a space
  (line 316 has the same pattern from before).
- `Makefile:47` and the fixtures' imports: coverage with flag `e2e` now also counts statements that only
  the test code executes in packages on the `-coverpkg` list (`pkg/client/clientset/versioned`, `utils/*`,
  `rollout/trafficrouting/istio`, `experiments`). The separate binary never counted those. [read]
- `test/fixtures/controller.go:197-207`: without `E2E_CONTROLLER_LOG` every test process, reruns included,
  leaves a debug-level `e2e-controller-*.log` in the temp directory, and nothing removes them. [read]

## By category
- **A (binary behaviour off the failure paths):** no finding. The one difference I found is on a failure
  path: `lead` now waits for `startLeading`, so a SIGTERM while start-up is inside a call without a context
  (`DoesIstioExist`, `utils/istio/istio.go:17`, called at `controller/controller.go:832`) delays shutdown
  until that call returns. The old code went on at once. [read]
- **B (harness):** findings 1, 9, 10. No race found; see the checked list.
- **C (faults and the three tests):** findings 2, 3, 5, 6, 7, 8.
- **D (CI and tooling):** findings 4, 10. The workflow conditions and the skip check are correct.
- **E (claims the code does not bear out):**
  - 1e198b70 and the test comment: "disabled as a known failure" (finding 2).
  - acb3502a: "the run fails when the controller recovered from a panic" (finding 1).
  - e153657a: "records every write" (finding 7) and "the error the API server would have sent" (finding 6).
  - 95a9155d: "a SIGTERM during the initial cache sync always ends with exit code 1". With leader election,
    a signal between acquiring the lease and the `OnStartedLeading` goroutine starting lets
    `leadOnce.Do(func() {})` win, and `lead` returns nil (`controller/controller.go:779-784`). [read]
  - `StartController` doc: "configured as `make start-e2e` configures the binary". `E2E_CONTROLLER_FLAGS`
    is not applied in-process. [read]

## Checked and found correct
- [read] Every flag of the old `RunE` still reaches the manager: same defaults and help text, same argument
  order into `newManager`, `newAnalysisManager` and `Run`; `defaults.Set*` still precede client creation;
  QPS and Burst are set on the config; `rest.CopyConfig` (client-go v0.34.5) copies every field including
  `WrapTransport`; `*electOpts` is copied after flag parsing.
- [read] Leader election: `NewLeaderElector` + `Run` is what `RunOrDie` did; `ReleaseOnCancel` is still
  false; a lost lease still returns nil (exit 0); `leadCtx` is cancelled only when `lead` returns; the
  shutdown order (plugins, queues, HTTP servers, `wg.Wait`) is unchanged.
- [ran] `go test ./controller/ ./cmd/... ./utils/controller/ ./test/fixtures/ ./hack/testmigration/`: ok.
  `go test -race ./test/fixtures/`: ok. `go vet -tags e2e ./test/...`: exit 0.
- [read] `DefaultOptions` equals the flag defaults; the `utils/defaults` initial values equal the flag
  defaults; neither the harness nor any e2e test calls `defaults.Set*`.
- [read] Shared state: tests log through their own loggers (`env.go:81-84`); `inProcess` and
  `manifestRoot` are written only before `m.Run()` and after it; the klog flags are registered before
  testing parses flags; the config singleton and plugin download happen once, from the repository root;
  no other relative path is used in `test/e2e` or `test/fixtures` (grep).
- [read] The tests' clients are built without the fault wrapper and before the klog level is raised; the
  wrapper sits under the metrics wrapper on all five controller clients; hook, recorder and faults are
  mutex-guarded; `Remove` does not alias the slice; faults and captures end in `dumpOnFailure` before the
  namespace is deleted.
- [read] Path parsing for core and group APIs, subresources, `namespaces/<n>/status|finalize`,
  cluster-scoped objects, collection delete and watch.
- [ran] before the restart, gotestsum v1.13.0 on a toy module with the Makefile's argument order: it
  executed `go test -json ... ./pkg -test.gocoverdir=<dir> ./pkg`; the test binary got go's own
  `-test.gocoverdir` first and the Makefile's last, and the directory received `covmeta` and
  `covcounters`. Coverage in the in-process jobs is recorded. [read] `E2E_TEST_OPTIONS` such as
  `-run X` still parse after the unknown flag.
- [read] Workflow expressions: `CONTROLLER` is empty on push, PR and `default`; "Start controller" and
  "Stop" run only for `external`; `E2E_COVERAGE` is true only for `inprocess`; the watchdog runs only for
  `external`; `off` starts nothing.
- [read] Skip check: the external list is added only when the job is not `inprocess`; a missing list file
  makes the check stricter; patterns that match nothing are reported and do not pass anything;
  `KnownFailure` runs before `RequireInProcessController`, so the known failure is on the main list in
  every mode. killmatrix: an exception is accepted only for a test in `Lost`, never for `NotRun`.
- [read] d03c3898's arithmetic: 1 - 0.5^6 = 98.4 %, 1 - 0.5^3 = 87.5 %.

## Not verified
- Anything that needs a cluster: the three new tests, the in-process start and stop, whether shutdown
  finishes within `controllerStopTimeout` (1 min) on every run, and whether plugin child processes outlive
  a test binary that dies without `Stop`.
- gotestsum's handling of a package failure with no failed test (finding 1).
- `hack/e2e-matrix.sh` was read, not executed.
- Housekeeping: after the restart I ran only `go test` and `go vet` in the scratch worktree and read-only
  git commands. I did not run `git worktree remove`, so the worktree at
  `/tmp/claude-0/-home-user/5e4760d8-7f6e-547b-8843-758fceae91fb/scratchpad/review-wt` is still there. It
  contains one untracked probe file of mine, `test/fixtures/zz_review_test.go`. The toy module from the
  gotestsum experiment is at `/tmp/claude-0/-home-user/5e4760d8-7f6e-547b-8843-758fceae91fb/scratchpad/gts`.

## Resolution

Written by the author of the changes, after the review above. The review was
made at `bf5065a1`, whose tree is that of `c906827c`. The review text above is
as the reviewer wrote it. Where no commit is named, the change is in the commit
that adds this file.

| Finding | Severity | What was done |
|---|---|---|
| 1. A recovered panic or a failed stop does not fail the run when reruns are on | high | Fixed. Confirmed first with gotestsum 1.13.0 on a toy package: it exits 0 both when a failed test passes in its rerun and when no test failed and only `TestMain` returned 1. The controller now appends a line to the file `E2E_CONTROLLER_FAILURES` names when it recovers from a panic, as it happens, and when it does not stop cleanly. `make test-e2e` sets the file, removes it before the run and fails when it is not empty after gotestsum and its reruns. Checked with a unit test of the hook and by running the make target with a stand-in for gotestsum that passes and writes the file. Not exercised in CI with a real panic: none has occurred. A direct `go test` without the variable has only the exit code of its own process, as before. |
| 2. `TestScaleDownOnSuccessUpdateConflict` did not fail | medium | Already reworked in `580ea763` when the review arrived; in that form it fails when run by name (fork run 46114380945). The assertion on the hit count that a timeout skipped is gone: the test now waits for the first hit before it goes on. Accepted as it is: in a run that lasts beyond the informers' 15-minute resync and has `E2E_RUN_KNOWN_FAILURES=true`, the resync can scale the Deployment down inside the test's minute, and the known failure passes. |
| 3a. `TestHealthyRolloutControllerQuiet` passes when nothing is recorded | medium | Fixed. The test first requires that `ControllerWrites` is not empty. `ExpectControllerQuiet` says in its comment that it cannot tell the two apart. |
| 3b. A late event can fall into the quiet window | medium | The settling time before the window is five seconds instead of two. Events still count as writes: a controller that keeps sending events about an unchanged object is what the check is for. The test passed in 33 of 33 runs with two seconds. |
| 4. `e2e_tests_ref` with the default controller setting runs old tests without a controller | medium | Fixed. With `e2e_tests_ref` set and `e2e_controller` left at `default`, every job gets the separate controller. |
| 5. An API server address with a path disables recording and faults | low | Fixed. The transport takes the path from the kubeconfig's server address and strips it; unit test `TestParseAPICallBehindAPrefix`. |
| 6. An injected 429 or 5xx is not what the API server sends | low | Documented on `Fault`: no `Retry-After` header, so client-go does not retry by itself. The three tests use 409. |
| 7. Faults do not reach the informers; cluster-scoped writes are not recorded | low | Documented on `Fault` and on the recorder. Both follow from scoping everything to the test's namespace, which is what lets tests run side by side. |
| 8. `TestIstioDestinationRuleCleanupConflict` reads the DestinationRule too early | low | Fixed. The test waits until both subsets carry the controller's label and the controller has made no write for two seconds, and only then injects the conflicts. |
| 9. The failure dump does not have every line; klog lines of the tests are in the controller's log | low | Documented on the log hook. No change to the controller's logging. The places that recover from a panic log through the shared logger, so the panic count is not affected. The `-v` flag trap is noted here and not acted on: the fixtures do not execute plugin commands. |
| 10. Empty `-coverpkg`, unquoted path, temporary logs left behind | low | `make` stops when the package list for `-coverpkg` is empty. The coverage directory is quoted. A test process that passed removes its temporary controller log; one that failed keeps it, and a log named with `E2E_CONTROLLER_LOG` is never removed. That coverage from the test binary includes what the tests call is in `phase-3-results.md`. |
| A. Shutdown waits for a start-up call that takes no context | failure path | Accepted. It is the price of `lead` returning the error of `startLeading`. |
| E. `95a9155d`: "a SIGTERM during the initial cache sync always ends with exit code 1" | claim | Holds as worded, for a signal while the caches sync. The reviewer's case is a signal earlier, between winning the lease and the start of `startLeading`: the controller then exits 0 without having started, as it did before the change. |
| E. `StartController`: "configured as `make start-e2e` configures the binary" | claim | Corrected: it has no counterpart of `E2E_CONTROLLER_FLAGS`. |
| E. `acb3502a`, `e153657a`, `1e198b70` | claims | See findings 1, 6, 7 and 2. |
