---
name: testing
description: How to write and run tests in argo-rollouts — unit tests with the fake-client fixture pattern, kustomize manifest tests, and pointers to the e2e suite. Use whenever adding or modifying tests, running the test suite, or verifying a controller change.
---

# Testing Argo Rollouts

## Running tests

```bash
make test-unit                 # all unit tests via gotestsum (installs devtools into ./dist)
go test ./rollout/...          # faster: plain go test on the package you touched
go test ./rollout/ -run TestName -v
make test-kustomize            # validates manifests/ kustomize overlays build
make test                      # kustomize + unit
make lint                      # golangci-lint run --fix (runs go mod tidy/vendor first)
```

`kustomize` must be installed for `make test`. E2E tests need a real cluster — see the
`e2e-testing` skill; never try to run `make test-e2e` without one.

## Unit test conventions

- Tests are table-driven where natural, use `stretchr/testify` (`assert`/`require`), and
  live next to the code (`foo.go` → `foo_test.go`, same package).
- Controller behavior tests use the **fixture pattern** in `rollout/controller_test.go`:
  `newFixture(t)` builds a controller backed by `k8sfake`/rollout fake clientsets.
  You seed objects into `f.rolloutLister`, `f.replicaSetLister`, `f.objects`,
  `f.kubeobjects`, declare expected client actions with `f.expectPatchRolloutAction(r)`,
  `f.expectUpdateReplicaSetAction(rs)`, `f.expectCreateReplicaSetAction(rs)` etc.,
  then call `f.run(getKey(rollout, t))`. Unexpected or missing actions fail the test.
  Helpers like `newCanaryRollout`, `newBlueGreenRollout`, `newReplicaSetWithStatus`,
  and `updateCanaryRolloutStatus` construct realistic objects — reuse them instead of
  building objects by hand.
- Assert status patches by capturing the patch index
  (`patchIndex := f.expectPatchRolloutAction(r)`) and comparing against
  `f.getPatchedRollout(patchIndex)` — many tests compare against an expected JSON patch
  string with `calculatePatch`.
- Analysis/experiment controllers have their own analogous fixtures
  (`analysis/controller_test.go`, `experiments/controller_test.go`).
- Mocks are generated with mockery via `make gen-mocks` (`hack/update-mocks.sh` lists
  each mocked interface explicitly; output goes to `*/mocks/`). Never hand-edit mock
  files; if an interface changed, regenerate — and add a new mockery stanza to the
  script when mocking a new interface.
- Metric providers are tested against `httptest` servers or mocked SDK clients — follow
  the existing pattern in the sibling provider package.

## What to test for a controller change

1. The happy-path reconcile produces exactly the expected actions/patch.
2. Idempotence: a second sync with the resulting state produces no new mutations.
3. Edge states relevant to the change: paused, aborted, degraded, scaled-to-zero,
   mid-step canary, missing stable ReplicaSet.
4. If you touched `pkg/apis/rollouts/validation/`, add cases to its table tests.

Do not assert on log output; assert on actions, status patches, and events
(`f.expectPatchRolloutAction`, recorder events via `utils/record` fake recorder).
