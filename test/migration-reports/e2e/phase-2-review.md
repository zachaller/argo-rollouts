# Phase 2 review: did any e2e test check less, or something different, after the conversion?

Baseline `b8d4cc3e1d79f1545702bf79d366d36d54214183`, head `73ae1d7a`. Static review only: no cluster, no e2e run.
File and line references are at head unless they say "baseline".

Claim tags: **[R]** read in the code at the cited lines. **[C]** result of a command I ran. **[I]** my inference about runtime behaviour, not executed.

## Summary

- 57 items reviewed (12 differing bodies, 22 fixture behaviours, 16 constructors, 1 manifest, 6 parallel/namespace effects): 48 equivalent, 4 weaker, 2 different, 2 stricter, 1 unclear.
- Most important: a fixture change weakens four tests whose bodies are byte-identical, so `bodydiff` and `body-exceptions.yaml` cannot show it. The four `WaitFor...AnalysisRunPhase` helpers now wait up to 90s for the AnalysisRun to exist (`test/fixtures/when.go:728`); before, a missing run failed the test at once. For `TestCanaryInconclusiveBackgroundAnalysis` that removes a deterministic ordering check (run exists by the time the rollout is Paused).
- `TestBlueGreenDuration_SupersededRollback`: the added `Sleep(1s)` means the test can no longer hit the known "rollback not recognised within one second" defect. Documented, but it is a real loss of (intermittent) coverage.
- `TestCanaryDynamicStableScaleAbortScaleDownDelay`: only the 2s bound on traffic returning to stable was relaxed to 90s (relied on the earlier Canary report).
- `guard` does not cover everything testify's per-test recover did: `TestControllerMetrics` dereferences a nil response outside any callback, so an unreachable metrics endpoint now panics the whole test binary instead of failing one test. Louder, not weaker; the comment at `common.go:113-122` is wrong about it.
- `GetControllerConfig` now reads the controller's namespace, so the 10 step-plugin tests can run in CI for the first time. They are still on the expected-skip list, so a silent skip is still accepted.
- `TestAppMeshCanaryRollout` probably cannot pass any more against a real App Mesh controller (its Mesh selects namespace `default`). Skipped in CI; could not be determined.
- Parallel effects only apply when `E2E_PARALLEL` > 1; the default is still 1 (`Makefile:28`, `testing.yaml:212`). Above 1, the fixed-sleep "nothing happened" assertions get weaker as the controller slows down.
- No controller code changed between baseline and head [C]; all 16 `SetupSuite`s map to constructors that do the same work; the one manifest change is neutral.

## Commands run

The main ones. "[C: grep]" in the tables means a search of `test/` (or the controller packages, where stated) for the thing named; "[C: diff]" a text comparison with the baseline file.

| Command | Result |
|---|---|
| `go run ./hack/testmigration bodydiff -base b8d4cc3e... -v` | 147 compared, 135 identical, 12 accepted, 0 unexplained |
| `go run ./hack/testmigration inventory-diff -base b8d4cc3e...` | 146 converted, 1 renamed, 1 added; every baseline test maps to one test |
| `go vet -tags e2e ./test/e2e/ ./test/fixtures/` | clean |
| `go test ./test/fixtures/` | ok |
| `git diff --name-only b8d4cc3e... HEAD` outside `test/`, `hack/`, `.github/` | only `Makefile` |
| `git diff b8d4cc3e... HEAD -- 'test/e2e/*.go'` with the mechanical declaration lines filtered out | the only other changed lines are the constructors, 12 helper signatures (receiver or parameter type only), `main_test.go`, the new test, and the 12 accepted bodies |
| baseline `deleteResources` (e2e_suite.go:218-301) against `cleanup.go:48-131` | identical except receiver and `log.Fatalf` -> `t.Fatalf` |
| each test's constructor against its baseline suite | all 147 use the constructor of the suite they came from |

## Table

### A. Test bodies that differ

| Item | Before | After | Verdict | Why |
|---|---|---|---|---|
| `TestBlueGreenDuration_SupersededRollback` (duration_test.go:710) | rev 1 Healthy, update to rev 2 at once | `Sleep(1s)` between them (:725) | **weaker** | Revisions 1 and 2 can no longer share a creation second, the only case in which `isRollback` (rollout/sync.go:1195, strict `Before`) misses the rollback. The old test failed in that case; the new one never reaches it. See Findings. |
| `TestCanaryDynamicStableScaleAbortScaleDownDelay` (canary_test.go:688) | `Sleep(2s)` then assert | `WaitForRolloutCanaryWeight(0)` (<=90s), `Sleep(2s)`, same asserts (:724-725) | **weaker** | Bound on weight reaching 0 went from 2s to 90s. Hold check kept. Relied on `test/migration-reports/e2e/canary_test.md` (gap F, accepted there); I only confirmed the current body is wait + sleep [R]. |
| `TestCanarySetCanaryScale` (canary_test.go:29) | `s.T().Parallel()` | removed; `fixtures.New` calls `t.Parallel()` (env.go:72) | equivalent | Relied on the Canary report. |
| `TestRolloutWithMaxSurgeScalingDuringUpdate` (canary_test.go:115) | update changes CPU request | update sets `command: [/bad-command]` | equivalent | Relied on the Canary report (same controller branch, precondition pinned instead of raced). Not re-derived. |
| `TestRolloutWithExperimentAndAnalysis` (experiment_test.go:29) | `s.T().Parallel()` | removed | equivalent | `fixtures.New` marks it parallel; a second `Parallel()` would panic. Assertions untouched [R]. |
| `TestKubectlWaitForPaused` (functional_test.go:1040) | `kubectl wait --for=...` | adds `-n r.Namespace` (:1085, :1095) | equivalent | `r` comes from `Rollouts(t.namespace).Get` (then.go:37), so `-n` is the test namespace; before, the kubeconfig namespace was the test namespace. Conditions and output match unchanged [R]. |
| `TestKubectlWaitForCompleted` (functional_test.go:1141) | same | adds `-n r.Namespace` (:1193, :1200) | equivalent | Same reason [R]. |
| `TestIstioHostSplit` (istio_test.go:25) | `s.TearDownSuite()` after each case | `s.DeleteResources()` (:104) | equivalent | Same `deleteResources` code and same Background policy. Selector is "label equals this test's name" (env.go:213) instead of "label exists" (baseline e2e_suite.go:182); inside the test's own namespace every labelled object has that value (common.go:546-553). IstioSuite's SetupSuite applied nothing [R]. |
| `TestIstioHostSplitOnlyTls` (istio_test.go:108) | same | same (:168) | equivalent | Same reason. |
| `TestIstioHostSplitExperimentStep` (istio_test.go:370) | `s.TearDownSuite()` as last statement | `s.DeleteResources()` (:434) | equivalent | After the last assertion; cleanup only. |
| `TestIstioSubsetSplitExperimentStep` (istio_test.go:437) | same | same (:496) | equivalent | Same. |
| `TestIstioSubsetSplitInStableDownscaleAfterCanaryAbort` (istio_test.go:551) | same | same (:575) | equivalent | Same. |

### B. Fixture behaviour

| Item | Before | After | Verdict | Why |
|---|---|---|---|---|
| Clients, QPS/burst, klog level: `New` (env.go:65), `newSharedEnv` (shared.go:57) | built per suite in `SetupSuite` | built once per process, shared | equivalent | Same config (shared.go:60-67). One client-side rate limiter is now shared by all tests; affects timing only [I]. |
| Capability probes (shared.go:86-89) | once per suite, in the kubeconfig namespace | once per process, in the kubeconfig namespace | equivalent | Same functions and namespace. In both, any list error means "not installed" and the tests skip (utils/istio/istio.go:16-22). A transient error now skips all 15 Istio/APISIX tests instead of one suite; CI's skip check would flag it. |
| Test namespace: `createTestNamespace` (env.go:132) | kubeconfig namespace (`default` in CI) | new `e2e-<test>-xxxxx` namespace, waits for the default ServiceAccount | equivalent | All lookups in common.go, when.go, then.go use `c.namespace` [R]. Anything a developer pre-installed in their namespace (pull secrets, mesh membership, a `--namespaced` controller) is not there; in CI nothing was (workflow only creates `argo-rollouts` and CRDs). |
| `Common.Context` (env.go:87) | `context.TODO()` | `t.Context()` | equivalent | Cancelled only when cleanups start; `dumpOnFailure` and `deleteNamespace` make their own (env.go:171, :225). No test defers a fixture call [C: grep]. |
| `Test.Given` (env.go:125) | copy of `Common`, `c.t = s.T()` | copy of `Common`, `t` already the test's | equivalent | Same value for a top-level test. Direct calls on `s` (`ApplyManifests`, `GetReplicaSetFromExperiment`) now fail the test's own T instead of the suite's. |
| `E2ESuite.Run` | subtest wrapper | removed | equivalent | No test called `s.Run` at baseline or head [C: grep]. |
| `BeforeTest` | foreground-delete objects labelled with the test name, wait | removed; new namespace per test, `SweepTestNamespaces` in `TestMain` (env.go:246, main_test.go:17) | equivalent | A rerun starts clean in both. Cluster-scoped leftovers are no longer deleted before a test, but no test creates a ClusterAnalysisTemplate [C: grep]. |
| `AfterTest` -> `dumpOnFailure` (env.go:165), `deleteNamespace` (env.go:220) | dump labelled rollouts/experiments; delete labelled objects and wait | dump all in the namespace; delete the namespace, no wait | equivalent | Not a check. The old wait could only catch a stuck deletion; the controller sets no finalizers [C: grep], so no controller behaviour was covered by it. |
| `TearDownSuite` -> `Test.DeleteResources` (env.go:205) | delete everything with the label | delete everything with this test's label value | equivalent | See A, Istio rows. `E2E_DEBUG=true` skips in both. |
| `deleteResources` (cleanup.go:48) | on `E2ESuite` | on `Common` | equivalent | Verbatim move [C: diff]. |
| `applyObject` (common.go:538) | `kubectl apply -f -`; label = part of the test name after the first `/` | adds `-n <test ns>` unless the object names a namespace; same label code | equivalent | Label value is the same string as before for every test except the renamed `TestCanaryEphemeralMetadata`, whose assertions do not read it (canary_test.go:299-338). Objects applied by a constructor now carry the test name instead of the suite name; only `DeleteResources` selects on it and no suite with constructor objects calls it. |
| `deleteObject` (common.go:580) | `kubectl delete kind name` | adds `-n` | equivalent | One caller (functional_test.go:349). |
| `guard`, `guardBool` (common.go:123, :134) and the 18 callback call sites | testify recovered a panic anywhere in a test method and failed that test | a panic is recovered only inside a callback handed to a fixture | **different** | Every fixture callback invocation is wrapped [C: grep], message and `FailNow` match testify's `failOnPanic`. But `TestControllerMetrics` can panic outside a callback. See Findings. |
| `StartEventWatch`, `GetRolloutEventReasons` (common.go:730, :766) | goroutine appended to `c.events` unsynchronised and called `c.t.Fatalf` on a non-event | mutex-guarded log; a broken watch is recorded and raised by the next `GetRolloutEventReasons` | equivalent | All tests that start a watch end with an events assertion, so a broken watch still fails them. A watch closed by the test's own `cancel()` is no longer treated as an error. |
| `GetControllerConfig` (common.go:786) | ConfigMap `argo-rollouts-config` in the test namespace | in `defaults.Namespace()` (`argo-rollouts` unless `POD_NAMESPACE` or an in-cluster token says otherwise) | **different** | Reads a different object. Only caller is the step-plugin skip gate. See Findings. |
| `GetApisixSetHeaderRoute` (common.go:687) | namespace from the manifest (`default`) | `c.namespace` | equivalent | Goes with the manifest change (D). |
| `waitForAnalysisRunOfType` and the four `WaitFor...AnalysisRunPhase` helpers (when.go:728-765) | look the run up once; missing run fails at once | poll up to `E2EWaitTimeout` for a run of that type, then look up | **weaker** | See Findings. |
| `WaitForRolloutCanaryWeight` (when.go:516) | did not exist | new | equivalent | Used by one test (A, row 2); changes nothing else. |
| `PrintRollout`, `PrintRolloutYAML`, `PrintExperiment*`, `PrintObjectEvents` (common.go:161-207, :812) | process stdout / logrus output | the test's own output | equivalent | Diagnostics only. |
| `StopLoad` (when.go:775) | `kubectl exec job/load-test` in the kubeconfig namespace | unchanged, while `StartLoad` now applies into the test namespace | equivalent | No test calls either [C: grep]. Latent: the pair no longer works together. |
| `settings.go`, `known_failure.go`, `Serial` (env.go:53) | constants and `init` in e2e_suite.go | moved; `KnownFailure` and `Serial` new | equivalent | Settings identical [C: diff]. No converted test uses `KnownFailure` or `Serial` [C: grep]. |
| Per-test logger (env.go:80-83, :102) | logrus standard logger | logger writing to `t.Output()` | equivalent | Failures go through `t.FailNow`/`t.Fatal`, not through the logger. |

### C. Constructors against the `SetupSuite` they replace

| Item | Before | After | Verdict | Why |
|---|---|---|---|---|
| `newAnalysisTest` (analysis_test.go:21) | 8 AnalysisTemplates once per suite | the same 8 per test, in the test namespace | equivalent | Same files in the same order. No test changes or counts the shared templates; no template name is redefined by a test of this suite [C: grep]. |
| `newAPISIXTest` (apisix_test.go:28) | skip suite unless `ApisixEnabled` | skip each test | equivalent | Same flag. Namespace is created before the skip; harmless. |
| `newAppMeshTest` (appmesh_test.go:23) | skip suite unless `AppMeshEnabled` | skip the test | equivalent | Constructor only; for the test itself see E. |
| `newAWSTest` (aws_test.go:21) | nothing | nothing | equivalent | The three ALB tests keep their own env-var skip (aws_test.go:37, :49, :61). |
| `newBlueGreenTest` (bluegreen_test.go:16) | nothing | nothing | equivalent | The one FunctionalSuite test in this file uses `newFunctionalTest` [C]. |
| `newCanaryTest` (canary_test.go:23) | `sleep-job` template once | per test | equivalent | |
| `newDurationTest` (duration_test.go:19) | nothing | nothing | equivalent | |
| `newExperimentTest` (experiment_test.go:17) | nothing | nothing | equivalent | Templates these tests reference are in their own manifests [C: grep]. |
| `newFunctionalTest` (functional_test.go:27) | `sleep-job` template once | per test | equivalent | |
| `newHeaderRouteTest` (header_route_test.go:18) | skip unless `IstioEnabled` | same, per test | equivalent | |
| `newIstioTest` (istio_test.go:17) | skip unless `IstioEnabled` | same, per test | equivalent | |
| `newMirrorRouteTest` (mirror_route_test.go:17) | skip unless `IstioEnabled` | same, per test | equivalent | |
| `newRollbackTest` (rollback_window_test.go:13) | `sleep-job` and the `window` rollout applied once; the first test re-applied the rollout over it, the second created it fresh | both applied per test, then re-applied by `HealthyRollout` | equivalent | The second apply is a no-op; both tests start from revision 1 with no AnalysisRun, as before. Counts are per namespace now, so a previous test's not-yet-collected runs of a rollout with the same name cannot inflate them. |
| `newSMIIngressTest` (smi_ingress_test.go:17) | skip unless `SMIEnabled` | same, per test | equivalent | Still skipped in CI, as at baseline (expected-skips.txt). |
| `newSMITest` (smi_test.go:17) | skip unless `SMIEnabled` | same, per test | equivalent | Same. |
| `newStepPluginTest` (step_plugin_test.go:24) | skip suite unless the ConfigMap in the test namespace lists both plugins | skip each test unless the ConfigMap in the controller namespace does | **stricter** | With `make setup-e2e` now in CI (testing.yaml:283-287) the gate can open. See Findings (with `GetControllerConfig`). |

### D. Manifests

| Item | Before | After | Verdict | Why |
|---|---|---|---|---|
| `test/e2e/apisix/rollout-apisix-canary-set-header.yaml` | Rollout had `namespace: default` | removed | equivalent | The Rollout must be in the namespace of its Services and ApisixRoute. It is the only manifest change [C]. The one remaining hard-coded namespace is the shared Secret (E). |

### E. Parallel and separate namespaces

| Item | Before | After | Verdict | Why |
|---|---|---|---|---|
| Cluster-scoped `Mesh` in `appmesh/appmesh-canary-rollout.yaml:6-8` (`TestAppMeshCanaryRollout`, appmesh_test.go:31) | selects namespace `default`; test ran in the kubeconfig namespace | still selects `default`; test runs in `e2e-...` | **unclear** | See Findings. |
| Secret `shared-example-secret` in `argo-rollouts` (`functional/rollout-shared-secret.yaml:38`, `TestAnalysisWithSharedSecret`, analysis_test.go:731) | applied there, never deleted (Secrets were not in the delete list) | same | equivalent | One test uses it; `applyObject` leaves an object's own namespace alone (common.go:562-565). |
| Metrics endpoint (`TestControllerMetrics`, functional_test.go:1422) | status 200 | status 200 | equivalent | Reads no counter, so other tests cannot change the result. (Its panic is under B, `guard`.) |
| Count and list helpers: `GetRolloutAnalysisRuns` (common.go:299), `GetRolloutExperiments`, `GetReplicaSetByRevision`, `ExpectExperimentServices`, `GetJobFromAnalysisRun` | list the shared namespace, filter by owner name/UID or selector | list the test's namespace, same filters | equivalent | A test sees only its own objects. Before, leftovers of an earlier rollout with the same name could only add objects (a failure), never hide one. |
| Fixed-sleep negative assertions when `E2E_PARALLEL` > 1 | controller serves one test | controller serves N tests | **weaker** | Conditional and inferred. See Findings. |
| Malformed Rollouts of `TestMalformedRollout` / `TestMalformedRolloutEphemeralCtr` (functional_test.go:422, :428) | existed only while that test ran | exist while other tests run, when parallel | **stricter** | Inferred. See Findings. |

## Findings

### `waitForAnalysisRunOfType` (test/fixtures/when.go:728-765): weaker

Before [R, baseline when.go:714-732]: each helper was

```go
arun := w.GetBackgroundAnalysisRun()   // fails "Background AnalysisRun not found" if absent
return w.WaitForAnalysisRunCondition(arun.Name, ...)
```

After [R]: `w.waitForAnalysisRunOfType(...)` first polls every 500ms for up to `E2EWaitTimeout` (90s) until the rollout owns a run with that type label. A run that never appears still fails.

Of the 15 call sites, 11 follow an assertion that the run already exists (`ExpectAnalysisRunCount(1)`, `Expect...AnalysisRunPhase`, or `Items[0]`), so nothing changes for them [R]. For four tests the helper is the first check that the run exists:

| Test | Call | Old bound on the run existing | New bound |
|---|---|---|---|
| `TestCanaryInconclusiveBackgroundAnalysis` | analysis_test.go:84, straight after `WaitForRolloutStatus("Paused")` | at the moment Paused is seen | 90s |
| `TestCanaryFailInlineMultipleAnalysis` | analysis_test.go:158 | 1s after promote | 90s |
| `TestCanaryPromoteFull` | functional_test.go:165 | 3s after promote | 3s + 90s |
| `TestBlueGreenPromoteFull` | functional_test.go:232 | 5s after update | 5s + 90s |

What can now go undetected:

- `TestCanaryInconclusiveBackgroundAnalysis`. The old check was not a race. In the canary stage list the `analysis` stage runs before `canaryPause` (rollout/stages.go:33-47), a failed run creation stops the reconcile without a status sync (stages.go:183-193), and the phase comes from the status written at the end (utils/rollout/rolloututil.go:82-85). So with the current controller the background run always exists before `Paused` is visible [R for the code, I for runtime]. A regression that creates the background run in a later reconcile than the pause (stages reordered, creation deferred to a requeue) failed the old test on every attempt. It passes now as long as the run shows up and turns Inconclusive inside the 30s pause step.
- The other three: a controller that starts step or pre-promotion analysis tens of seconds late passes. The old bound was a fixed sleep, which is the flake the commit message (`894598e8`) describes, so this part is a latency bound traded for stability.

None of this is in `body-exceptions.yaml`, and cannot be: the bodies did not change.

To close: keep the old immediate lookup in the helpers and make the pre-wait explicit where it is needed (a separate step in the three sleep-guarded tests, each a listed body exception). Or give the pre-wait its own short timeout (10-15s) instead of `E2EWaitTimeout`. Either way, list fixture changes that alter what unchanged bodies check next to the body exceptions.

### `TestBlueGreenDuration_SupersededRollback` (test/e2e/duration_test.go:710): weaker

Before: revision 2 was created as soon as revision 1 was Healthy. When both ReplicaSets got the same creation second, `isRollback` (rollout/sync.go:1195, `c.newRS.CreationTimestamp.Before(&c.stableRS.CreationTimestamp)`) returned false for the later update back to revision 1, the rollback was not fast-tracked, and `WaitForRolloutStatus("Healthy", 10*time.Second)` timed out [R for the code; the 7-of-20 rate in the exception text is the author's measurement, not verified].

After: `Sleep(1 * time.Second)` at :725 guarantees a different second. The assertions (:738-744) are unchanged and the sleep cannot affect them: the rollout is idle and Healthy while it sleeps, and 1s is far inside the 30s scale-down delay [R].

What can go undetected: exactly the defect described in `test/migration-reports/fix-plans/rollback-not-detected-within-one-second.md`, and any future change to that comparison that only matters for equal timestamps. This was the last of the four rollback tests in the file without the sleep, so no e2e test reaches the case now. In default CI the old test rarely reported it either, because failed tests are rerun five times (`Makefile:32`).

To close: a unit test of `isRollback` and `isRollbackWithinWindow` with equal creation timestamps, marked as a known failure until the defect is fixed, covers the case every time; an e2e test can only hit it by chance.

### `TestCanaryDynamicStableScaleAbortScaleDownDelay` (test/e2e/canary_test.go:688): weaker

Relied on `test/migration-reports/e2e/canary_test.md` (test 4, gaps A, E, F and its Resolution). I confirmed the body at head is `WaitForRolloutCanaryWeight(0)` followed by `Sleep(2*time.Second)` (:724-725) [R].

Remaining difference from the baseline: traffic had to be back on stable within 2s of the last stable pod being marked ready; now within 90s. The canary-hold assertions run 2s after weight 0 is seen, which is no earlier than before.

To close, if the latency should stay bounded: pass an explicit timeout, for example `WaitForRolloutCanaryWeight(0, 30*time.Second)`.

### `guard` / `guardBool` (test/fixtures/common.go:123, :134): different

Before: testify's `failOnPanic` (suite.go:90-96 of v1.12.1) recovered a panic anywhere in a test method and failed that one test.

After: only callbacks passed to fixtures are covered. All 18 places where a fixture calls test-supplied code are wrapped [C: grep of then.go and when.go], with the same message and `FailNow`.

The comment at common.go:113-122 says "All such code in the tests is in callbacks given to the fixtures". One test contradicts it [R, and C: an AST listing of top-level statements in every `Test*` body]:

```go
// test/e2e/functional_test.go:1424-1426
resp, err := http.Get(fmt.Sprintf("http://localhost:%d/metrics", controller.DefaultMetricsPort))
assert.NoError(s.T(), err)
assert.Equal(s.T(), http.StatusOK, resp.StatusCode)
```

If the endpoint is unreachable, `resp` is nil and line 1426 panics in the test goroutine. Before, that failed `TestControllerMetrics`. Now it ends the test binary: tests running at the time are lost and tests not yet started do not run [I]. That is the situation with the controller down or with the workflow's `e2e_controller: off` input. The run still fails, so nothing is hidden, but the result of that run is no longer per-test. (As far as I remember gotestsum does not rerun after a panic; not checked here.)

Fixture code outside callbacks can also panic (`ScaleRolloutWithWorkloadRef`, when.go:287, dereferences `Spec.Replicas`), but only on what the test manifest contains, not on controller behaviour [R].

To close: `require.NoError` (or an early return) in `TestControllerMetrics`, as a listed body exception, and correct the comment.

### `GetControllerConfig` (test/fixtures/common.go:786) and `newStepPluginTest` (test/e2e/step_plugin_test.go:24): different / stricter

Before: the ConfigMap was read from the test namespace. In CI that was `default` (the workflow only creates `argo-rollouts` and never switches the kubeconfig namespace), while `make setup-e2e` applies it to `argo-rollouts` (`Makefile:278`) and CI did not run `setup-e2e`. The suite always skipped; the baseline JUnit file agrees (16 skips, 10 of them these tests) [R, C].

After: read from `defaults.Namespace()`, and CI runs `make setup-e2e` before starting the controller (testing.yaml:283-287). The 10 tests can run.

Consequences:

- Newly possible failures: 10 tests that have never run in CI. Whether they pass could not be determined.
- Still possible silent skip: all 10 remain in `test/e2e/expected-skips.txt` (:18-29), whose comment ("CI never runs `make setup-e2e`") is now out of date. If the ConfigMap is missing or the gate reads the wrong namespace, the run is green and `skips` only prints "did not skip" for the others.
- `defaults.Namespace()` prefers `POD_NAMESPACE` and the in-cluster token file (utils/defaults/defaults.go:269-282). Run from inside a pod, the gate looks in the pod's namespace, which need not be the controller's.

To close: once a run shows them passing, take the 10 names off the expected-skip list so a skip fails CI.

### `TestAppMeshCanaryRollout` and its cluster-scoped `Mesh` (test/e2e/appmesh/appmesh-canary-rollout.yaml:1-8): unclear

```yaml
kind: Mesh
spec:
  namespaceSelector:
    matchLabels:
      kubernetes.io/metadata.name: default
```

Before: the VirtualService, VirtualRouter and VirtualNodes were applied in the kubeconfig namespace, so the test worked when that was `default`. After: they go to `e2e-appmeshcanaryrollout-xxxxx`, which this Mesh does not select. With the real App Mesh controller, resources in a namespace without a matching mesh are rejected [I, from how that controller's admission works; not run]. With only the CRDs installed nothing would reject them. The test is skipped in CI (CRDs absent), so no run shows which.

This is a possible new failure, not a weaker check. The Mesh is also never deleted (it is not in `deleteResources`' list), as before.

To close: select the test namespace instead, for example by the label the harness puts on every test namespace (`e2e-test-name: TestAppMeshCanaryRollout`, env.go:140).

### Fixed-sleep negative assertions when `E2E_PARALLEL` > 1: weaker (conditional, inferred)

Several tests show that the controller did not do something by sleeping a fixed time and then reading state once:

- analysis_test.go:253-256: `Sleep(2s)` "checking too early may not catch the bug where we create analysis unnecessarily", then `ExpectAnalysisRunCount(2)`.
- canary_test.go:281-284 and :289-293: `Sleep(2s)`, then "verify we don't scale down immediately".
- canary_test.go:812-815: `Sleep(2s)`, then revision 2 must still have its pod.
- canary_test.go:895-902: `WaitForRolloutConditionToNotExist(..., 5*time.Second)`.
- functional_test.go:183, :1655, :1664, :1674, :1760, :1775: "no new analysis", "Deployment should remain at 0".

These only catch a wrong action if the controller performs it inside the window. With one test at a time the controller reacts in well under a second. With N tests sharing its work queue (and one client-side rate limiter in the test process, shared.go:66-67) its reaction time grows, and a wrong action that lands after the read is missed [I]. The effect is zero at the default `E2E_PARALLEL=1` (`Makefile:28`, `testing.yaml:212`), where `t.Parallel()` tests still run one at a time, and grows with the setting. I did not measure it.

To close, if parallelism is raised: run these tests with `fixtures.Serial(...)`, or replace the sleeps with a wait for evidence that the controller has reconciled since the trigger (for example an observed-generation change) before asserting absence.

### Malformed Rollouts coexisting with other tests: stricter (inferred)

`TestMalformedRollout` and `TestMalformedRolloutEphemeralCtr` create Rollouts with an invalid resource quantity. Serially, such an object existed only during its own test. In parallel it sits in the controller's cluster-wide informers while other tests run, so a controller that handles it badly for all rollouts would now fail other tests too [I]. No action needed.

### Smaller points, no verdict change

- `body-exceptions.yaml` says of the Istio entries that `DeleteResources` is "the same cleanup". It is the same function with a narrower selector (this test's label value instead of any value). Equivalent in a per-test namespace, as the comment at env.go:210-212 explains; the exception text could say so.
- `SweepTestNamespaces` deletes every `e2e-` namespace carrying the label when a test process starts. A second `go test` process started against the same cluster while one is running would delete the first one's namespaces. Reruns by gotestsum are sequential, so CI is not affected [I].
- The comment in `applyObject` (common.go:548-552, "when test name is the SuiteName") describes the old suites; for a top-level test the `else` branch now runs and gives the test name.

## Not reviewed

- The three Canary body changes were not re-derived; I relied on `test/migration-reports/e2e/canary_test.md` and checked only that the current bodies match what its Resolution describes.
- The 135 byte-identical bodies were not read line by line for sensitivity to parallel load. I searched them for cluster-scoped kinds, hard-coded namespaces, direct `kubectl`, the metrics port, package-level state, goroutines, negative assertions and statements outside fixture callbacks, and read the tests those searches pointed to.
- Not determinable without a run:
  - the failure rates quoted in `body-exceptions.yaml` (2 of 20, 14 of 60, 7 of 20) and in the fix plans;
  - whether the 10 step-plugin tests run and pass in CI;
  - how `TestAppMeshCanaryRollout` behaves on a cluster with App Mesh;
  - how much the negative assertions lose at a given `E2E_PARALLEL`, and whether any positive timing window (for example the 10s and 5s pauses in `TestCanarySetCanaryScale`) starts to flake;
  - whether `TestControllerMetrics` actually takes the binary down with the controller off (read from the code, not executed);
  - gotestsum's behaviour after a panic (from memory).
- `.github/workflows/testing.yaml` and the `Makefile` were read only for what the e2e job does by default (parallelism 1, reruns 5, `make setup-e2e`), not reviewed as changes.
- As instructed, not reviewed: `test/e2e/seeded-bugs/`, `hack/` (beyond running the two tools and reading how `bodydiff` and `skips` decide), and `TestCanaryAbortSwitchesCanaryServiceOnce`.

---

## Resolution

Written by the author of the changes, after the review above. The review was
made at `73ae1d7a`; the commits named here come after it. The review text above
is as the reviewer wrote it.

| Finding | Verdict in the review | What was done |
|---|---|---|
| `waitForAnalysisRunOfType` in the four `WaitFor...AnalysisRunPhase` helpers | weaker | Reverted. The helpers look the run up once again, as upstream, so `TestCanaryInconclusiveBackgroundAnalysis`, `TestCanaryFailInlineMultipleAnalysis` and `TestCanaryPromoteFull` check what they checked before. The wait is a step of its own, `WaitForPrePromotionAnalysisRun`, used only by `TestBlueGreenPromoteFull`, the one test that failed, and listed in `body-exceptions.yaml`. That test now allows the run 90 seconds to appear where it allowed five. |
| `TestBlueGreenDuration_SupersededRollback` | weaker | Kept, and accepted as a loss: no e2e test reaches the same-second case any more. The defect is written up in `test/migration-reports/fix-plans/rollback-not-detected-within-one-second.md`. The suggested unit test was not written: a test that expects the rollback to be recognised has to say which information tells the two ReplicaSets apart, and that is the open design question in the plan. |
| `TestCanaryDynamicStableScaleAbortScaleDownDelay` | weaker | Unchanged. Accepted in `canary_test.md`. |
| `guard` does not cover `TestControllerMetrics` | different | `TestControllerMetrics` now stops at `require.NoError` and no longer dereferences a nil response (listed in `body-exceptions.yaml`). The comment on `guard` no longer claims that all such code is in callbacks. A listing of every statement that runs outside a callback in every test body shows no other code that can panic on cluster state. |
| `GetControllerConfig`, `newStepPluginTest` | different / stricter | Open until a run shows what the ten step-plugin tests do. They leave `expected-skips.txt` when they pass; until then a skip is still accepted. |
| `TestAppMeshCanaryRollout` and its `Mesh` | unclear | The Mesh now selects the namespace labelled `e2e-test-name: TestAppMeshCanaryRollout`, which is the label the harness gives the test's namespace. Not run: CI has no App Mesh CRDs. |
| Fixed-sleep negative assertions when tests run in parallel | weaker (conditional, inferred) | No test changed. The seeded-bug comparison is the measurement that bears on this: the same tests must fail under the same bug at the parallelism CI uses as they did one at a time. Of the tests named, `TestBlueGreenAnalysis` (bug 13), `TestReduceWeightAndHonorMaxUnavailable` (bugs 05 and 14), `TestCanaryWithSpecPause` (bug 03), `TestCanaryPromoteFull` (bug 31) and the two `TestScaleDownProgressivelyCompleted` tests (bug 28) are each caught by a seeded bug in the serial baseline. `TestCanaryDynamicStableScaleRollbackToStable` is caught by none so far, so the comparison says nothing about it. The comparison shows that a test still catches a bug; it does not show which of its assertions does. Open until it has run on the converted tests. |
| Malformed Rollouts coexisting with other tests | stricter (inferred) | No action. |
| Wording of the Istio body exceptions; stale comment in `applyObject`; two test processes on one cluster | no verdict change | The exceptions now say that `DeleteResources` selects by this test's label value. The comment is corrected. `SweepTestNamespaces` documents that two test processes must not share a cluster. |
