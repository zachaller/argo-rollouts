---
name: e2e-testing
description: Running and writing argo-rollouts end-to-end tests — cluster setup, the Given/When/Then fixture DSL in test/fixtures, test suites, and running a single e2e test. Use when a change needs e2e coverage, when investigating e2e CI failures, or when asked to run tests in test/e2e.
---

# E2E Testing

E2E tests (`test/e2e/`, build tag `e2e`) run against a **real cluster** with a rollouts
controller running. They cannot run without one — don't attempt `make test-e2e` in an
environment with no Kubernetes cluster; rely on unit tests and CI instead.

## Environment setup

```bash
k3d cluster create                                  # or any cluster; kind works too
kubectl create ns argo-rollouts
kubectl apply --server-side -k manifests/crds
kubectl apply --server-side -f test/e2e/crds
make start-e2e          # runs the controller locally with --instance-id argo-rollouts-e2e
```

In another terminal:

```bash
make test-e2e                                            # everything (~long, retries flakes)
E2E_TEST_OPTIONS="-run 'TestCanarySuite' -testify.m 'TestCanaryScaleDownOnAbort'" make test-e2e
```

Knobs: `E2E_K8S_CONTEXT` (default `rancher-desktop`) used by `make setup-e2e`,
`E2E_PARALLEL`, `E2E_WAIT_TIMEOUT`, `E2E_INSTANCE_ID`. Traffic-router suites (Istio, ALB,
SMI, APISIX, ...) additionally need that mesh/ingress installed in the cluster; the
plain `TestFunctionalSuite`, `TestCanarySuite`, `TestBlueGreenSuite` do not.
Step-plugin tests need `make setup-e2e` (builds the sample plugin binary and applies
`test/e2e/step-plugin/argo-rollouts-config.yaml`).

## Test structure: Given / When / Then

Suites embed `fixtures.E2ESuite` (testify suite). Tests are a fluent chain defined in
`test/fixtures/{given,when,then}.go`:

```go
func (s *FunctionalSuite) TestExample() {
	s.Given().
		RolloutObjects("@functional/my-rollout.yaml").  // @path = file under test/e2e/
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateSpec().                                   // bumps pod template to trigger update
		WaitForRolloutStatus("Paused").
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectRevisionPodCount("2", 4).
		ExpectRolloutStatus("Healthy").
		ExpectAnalysisRunCount(1).
		When().                                          // chains can go back to When()
		AbortRollout().
		WaitForRolloutStatus("Degraded")
}
```

- Inline YAML is also accepted by `RolloutObjects`/`HealthyRollout` (raw string arg).
- Each test's objects are named uniquely and cleaned up by the suite between tests;
  tests must be self-contained and parallel-safe (`E2E_PARALLEL`).
- Common assertions live in `then.go` (`ExpectReplicaCounts`, `ExpectCanaryStablePodCount`,
  `ExpectServiceSelector`, `ExpectRolloutEvents`, ...) and waits in `when.go`
  (`WaitForRolloutStatus`, `WaitForRolloutCanaryStepIndex`, `Sleep` — avoid `Sleep`,
  prefer a Wait on observable state).
- Suite fixtures/manifests live under `test/e2e/<suite-dir>/`; shared analysis templates
  under `test/e2e/functional/`.

## Writing a new e2e test

1. Pick the matching suite file (`canary_test.go`, `bluegreen_test.go`,
   `functional_test.go`, provider-specific files) — only create a new suite for a new
   subsystem.
2. Keep it minimal: smallest replica counts and shortest step durations that still prove
   the behavior; e2e minutes are expensive and flaky-prone.
3. If the test is slow, guard with `s.T().Skip` on `testing.Short()` patterns only if
   existing tests in that suite do so (the Makefile passes `--short`... check siblings).
4. Reproduce a CI flake locally with `-count 1` and the exact `-testify.m` regex before
   changing waits; most flakes are missing Wait conditions, not broken product code.
