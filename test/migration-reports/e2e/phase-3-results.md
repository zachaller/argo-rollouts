# Phase 3: the controller inside the e2e test binary

What was changed, what it was compared with, and what the comparison shows.
All runs are manual dispatches of the `Testing` workflow on the fork, on
Kubernetes 1.37 unless a version is named.

## What changed

- `controller.Options` and `NewManagerFromOptions` hold the wiring that was
  inside the `rollouts-controller` command. `Manager.Run` returns its errors
  instead of ending the process (`95a9155d`).
- The e2e test binary runs the controller itself, built from that wiring
  (`test/fixtures/controller.go`, `test/e2e/main_test.go`). This is the default
  since `c906827c`. `E2E_CONTROLLER=external` uses a controller started with
  `make start-e2e`, as before.
- CI runs three Kubernetes versions that way and the oldest one with the
  controller binary as a process of its own.
- The in-process controller's API calls go through a transport of the fixtures
  (`test/fixtures/apicalls.go`). A test can make calls fail
  (`FailControllerCalls`), read what the controller wrote (`ControllerWrites`)
  and require that it writes nothing (`ExpectControllerQuiet`).

## What a test run gains

- A failed test prints the controller's last log lines about its namespace and
  a count of the controller's writes there, next to the rollout it already
  printed.
- `make test-e2e` fails when the controller recovered from a panic in a
  reconcile. The controller logs such a panic and retries; no test noticed
  before. None occurred in any run below. (As first written this did not
  work with reruns on, which is the default; see finding 1 of
  `phase-3-review.md`.)
- No separate process to build, start, wait for and watch.

## The comparison with the separate controller

### Same tests, same results

One full run each on the same controller code, reruns off: runs 46110373062
(separate, `95a9155d`) and 46112000763 (in-process, `0cdbaaaa`). The same 148
tests; 141 passed and 7 skipped in both.

The first run of the new default (run 46113727872, `c906827c`) passed on all
four versions: 1.34 with the separate controller, 1.35 to 1.37 in-process. Two
tests needed one rerun each: `TestRolloutAbortWhenStepFails` on 1.34 and
`TestBlueGreenAnalysis` on 1.37.

### Thirty runs each, reruns off

Runs 46107323320 (separate, `9f52246a`) and 46113140366 (in-process,
`1e198b70`), six jobs of five runs.

| | Separate | In-process |
|---|---|---|
| Runs that passed | 20 of 30 | 21 of 30 |
| Test failures | 10, in 6 tests | 11, in 7 tests |
| Recovered panics, controller exits | 0 | 0 |
| Run time, median (min to max) | 326 s (279 to 427) | 379 s (288 to 471) |

What failed:

| Test | Separate | In-process | Cause |
|---|---|---|---|
| `TestCanaryScaleDownOnAbort` | 2 | 4 | canary Service flap (`fix-plans/canary-service-flap-after-abort.md`) |
| `TestScaleDownOnSuccess` | 2 | 1 | dropped scale-down error (`fix-plans/workloadref-scaledown-conflict-ignored.md`) |
| `TestBlueGreenAnalysis` | 2 | 0 | same-second rollback (`fix-plans/rollback-not-detected-within-one-second.md`) |
| `TestIstioSubsetSplitExperimentStep` | 2 | 0 | idle after error back-off (`fix-plans/rollout-idles-after-error-backoff.md`) |
| `TestBlueGreenUpdate` | 1 | 1 | the compared event list lacked its last entries; not looked into |
| `TestRolloutAbortRetryPromote` | 1 | 0 | the same |
| `TestCanaryProgressDeadlineExceededWithPause` | 0 | 2 | see below |
| `TestRolloutAbortWhenStepFails` | 0 | 1 | `expected: 3 actual: 2` at `step_plugin_test.go:93`; not looked into |
| `TestCanaryBackgroundAnalysisLongRunningJob` | 0 | 1 | see below |
| `TestBlueGreenKitchenSink` | 0 | 1 | two pods of revision 2 where one was expected; not looked into |

The four tests that failed only in-process all failed in the three slowest of
the six jobs (see "Time" below).

- `TestCanaryProgressDeadlineExceededWithPause`: its pods have a readiness
  probe every 30 seconds, and its duration comes in steps of 30 seconds in
  both modes (about 128, 158 and 188 seconds). The two failures needed more
  than the 90 seconds the test waits for its last phase. The longest step was
  reached in 2 of 30 separate runs and 8 of 30 in-process runs.
- `TestCanaryBackgroundAnalysisLongRunningJob`: the test promotes the rollout
  about a second after the background AnalysisRun started. The run was
  cancelled before its first measurement and ended `Successful`; the test
  waits for `Inconclusive`. Whether that is a defect of the analysis
  controller or an assumption of the test was not established.

### Time

Not established whether the in-process controller is slower.

The median run was 53 seconds longer in-process. The medians per job overlap:
310 to 362 seconds separate, 313 to 393 in-process. Jobs differ as much within
each set as the sets differ from each other. In the three controller logs read
from each set, the reconcile times go with the overall speed of the job in
both: for syncs of 5 ms or more, a median of 15 to 19 ms in the fast jobs of
either set and 29 to 30 ms in the slow ones. Three of the six in-process jobs
were slow by that measure, one of the six separate jobs was.

That fits slower runners for those jobs, and it fits an in-process mode that
is slower on some runners. Six jobs per side, each on a runner of its own,
cannot tell the two apart. A job that alternates the modes on one runner
would.

Per job, the in-process mode builds one instrumented test binary (about three
minutes with a cold cache) instead of an instrumented controller (one and a
half to two and a half minutes) and a plain test binary.

### Coverage

22.7% of statements in both (the two single runs above), over the same
packages: those the controller is built from.

19 functions covered with the separate controller are not covered in-process:

- the command's `main` package, and what only it calls: the `defaults`
  setters, the signal handler, `CheckError`;
- `leaseLockName`: the in-process controller runs without leader election;
- the healthz handler: CI called it to wait for the separate controller;
- one shutdown function. The test binary writes its coverage before the
  controller is stopped, so the shutdown path is not in it.

This is why one Kubernetes version keeps the separate controller in CI.

33 functions are covered in-process that were not: generated informers and
listers, and helpers such as `GetRolloutPhase` and `SplitYAML`, which the
tests themselves call. Coverage from the test binary cannot tell the
controller's calls from the tests' calls. For a question such as "does any
test still make the controller run this function", use
`E2E_CONTROLLER=external`.

### Seeded bugs

Not in yet. Run 46113147342 applies all 48 patches with the controller
in-process; its comparison with the baseline (`killmatrix`) will be added
here.

## The new tests

All in `test/e2e/controller_faults_test.go`; they skip with
`E2E_CONTROLLER=external`.

| Test | What it does | Result |
|---|---|---|
| `TestIstioDestinationRuleCleanupConflict` | deletes a Rollout and fails the controller's first two updates of its DestinationRule with a conflict; the clean-up has to be retried | passed 30 of 30 |
| `TestHealthyRolloutControllerQuiet` | expects no API write from the controller for ten seconds once a Rollout is healthy | passed 30 of 30 |
| `TestScaleDownOnSuccessUpdateConflict` | fails the controller's updates of the workloadRef Deployment until it is idle, then expects the scale-down | disabled as a known failure; fails when run by name (run 46114380945) |

The third test was wrong at first. It failed only the first update, and
passed with the defect in place (run 46113135454), because the controller
happened to sync once more. It was reworked in `580ea763`.

## Not established

- Whether the in-process controller is slower (above).
- Whether the tests that failed only in-process fail more often in-process,
  or failed there because those jobs were slow for another reason. Thirty
  runs per side, and one or two failures per test.
- What `phase-3-review.md` lists as not verified.
