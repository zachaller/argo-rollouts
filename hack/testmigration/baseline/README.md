# Baseline

Results of the unconverted, serial e2e suite, used as the "before" side of the
migration checks.

| File | What it is |
|---|---|
| `junit-e2e-k8s1.37.xml` | JUnit results of the e2e job on Kubernetes 1.37 |
| `e2e-coverage-func-k8s1.37.txt` | Per-function coverage of the controller during that job (`go tool covdata func`, paths made repository-relative) |

Source: `argoproj/argo-rollouts`, workflow run 45973551441, commit `b8d4cc3e1d79`
(upstream master, the commit this migration branch started from).

In that run 131 tests executed, 16 were skipped (see
`test/e2e/expected-skips.txt`) and one test needed a rerun
(`TestAWSSuite/TestALBExperimentStepNoSetWeight`).
