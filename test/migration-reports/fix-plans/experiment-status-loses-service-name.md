# Fix plan: an Experiment's status can lose its Service names

**Status:** plan only. No controller code has been changed, and the proposed
change below has not been built or run.

## The defect

An Experiment that creates a Service for a template records the Service in
`status.templateStatuses[].serviceName`, together with `podTemplateHash`. A
reconcile that starts from a slightly older copy of the Experiment can wipe both
fields again, and nothing puts them back.

A rollout that gives the experiment templates a traffic weight then routes to
Services without a name. With ALB the ingress action ends up with target groups
that have a weight and a port but no `ServiceName`:

```
{"Type":"forward","ForwardConfig":{"TargetGroups":[
  {"ServiceName":"alb-rollout-canary","ServicePort":"80","Weight":0},
  {"ServicePort":"80","Weight":20},
  {"ServicePort":"80","Weight":20},
  {"ServiceName":"alb-rollout-stable","ServicePort":"80","Weight":60}]}}
```

The Experiment and both of its ReplicaSets are healthy the whole time, and the
Services exist. Only the status is wrong.

## Evidence

**How often.** `TestALBExperimentStepNoSetWeight` and
`TestALBExperimentStepNoSetWeightMultiIngress` each failed with exactly this
ingress action in 6 of 20 runs of the unconverted, serial suite with reruns off
(fork workflow run 46061128211, commit `5a514fbb`, Kubernetes 1.37). Each of
those runs had a seeded bug applied, in code unrelated to this. Both tests
carry a `TODO: this test is flaky` comment upstream, and CI hides the failures
by rerunning failed tests up to five times.

**What happens.** From the controller log of fork workflow run 46062202596
(commit `c1bc7937`, parallelism 4), Experiment `alb-rollout-b54ff46db-2-0`.
Three reconciles ran within about 40 ms, each patching the status:

| Reconcile started | Status patch it sent |
|---|---|
| 20:44:06.964 | `templateStatuses` without `serviceName` (the ReplicaSets were just created) |
| 20:44:06.972 | `templateStatuses` with `serviceName` and `podTemplateHash` for both templates (it created the Services) |
| 20:44:06.996 | `templateStatuses` with `replicas: 1` and **without** `serviceName` or `podTemplateHash` |

After the third patch the status never had the names again. Ten seconds later
the test read the Experiment and the ingress and failed. The rollout's own event
shows what the traffic router was given: `Traffic weight updated additional:
[{20  } {20  }]`.

## Root cause

`reconcileTemplate` in `experiments/experiment.go` starts from the template
status in the controller's cached copy of the Experiment:

```go
templateStatus := experimentutil.GetTemplateStatus(ec.ex.Status, template.Name)
```

`createTemplateService` sets the two fields only when it creates the Service:

```go
if (svc == nil || svc.Name != rs.Name) && len(ports) > 0 {
    ...
    newService, err := ec.CreateService(serviceName, *template, rs.Labels, ports)
    ...
        templateStatus.ServiceName = newService.Name
        templateStatus.PodTemplateHash = podTemplateHash
}
```

The status is written with a JSON merge patch (`experiments/controller.go`,
`patchtypes.MergePatchType`). A merge patch replaces a list as a whole, so any
change to one template status sends the entire `templateStatuses` list.

Put together:

1. Reconcile 2 creates the Services and patches the names into the status.
2. Reconcile 3 starts before the informer has delivered that update. Its copy
   of the status has no names. The Services exist, so it does not create them
   and does not set the names.
3. Reconcile 3 sees a replica count change and patches `templateStatuses`. The
   list it sends has no names, and it replaces the list on the server.
4. Every later reconcile finds the Services in place and leaves the fields as
   they are: empty.

The rollout controller guards against acting on a stale copy (`requeuing
rollout due to stale cache`). The experiment controller has no such guard.

## Proposed change

Record the Service whenever one exists for the template, not only when it is
created. In `createTemplateService`:

```diff
 	if (svc == nil || svc.Name != rs.Name) && len(ports) > 0 {
 		...
 			templateStatus.ServiceName = newService.Name
 			templateStatus.PodTemplateHash = podTemplateHash
 		}
+	} else if svc != nil {
+		// The Service is already there. Record it again: the status this reconcile
+		// started from can be older than the one that first recorded it, and the
+		// status patch replaces the whole templateStatuses list.
+		templateStatus.ServiceName = svc.Name
+		templateStatus.PodTemplateHash = podTemplateHash
 	}
```

This makes the two fields a function of what exists, so a stale starting point
no longer matters for them.

## Alternatives considered

- **Skip a reconcile that starts from a stale Experiment**, as the rollout
  controller does. It addresses the cause for every status field at once, but
  it needs the controller to know the resourceVersion it last wrote, which the
  experiment controller does not track today.
- **Patch template statuses one field at a time** so that a patch cannot drop
  fields it does not mention. JSON merge patch cannot address list elements, so
  this means a different patch type or a different status shape.

## Risks and what to check

- **Other fields with the same exposure.** `collisionCount`, `message` and
  `lastTransitionTime` in a template status are also carried over from the
  cached copy. Whether a stale reconcile can lose one of them in a way that
  matters was not checked.
- **`template.service.name` set.** The condition `svc.Name != rs.Name` is then
  true on every reconcile, so the create branch runs each time. What
  `CreateService` does for a Service that already exists was not traced.
- **Service deletion.** `deleteTemplateService` clears both fields. The new
  branch must not run for a template whose Service was just deleted in the same
  reconcile; `ec.templateServices[templateName]` is set to nil there, which
  should keep it out. Not verified.
- **Unit tests that pin exact patches** in `experiments/` may expect a status
  without the names in some step.

## Tests

- **Already there:** `TestALBExperimentStepNoSetWeight` and
  `TestALBExperimentStepNoSetWeightMultiIngress` in `test/e2e/aws_test.go` fail
  when this happens. They are not disabled: they pass about two runs in three,
  and they cover experiment weights without a `setWeight` step.
- **Worth adding with the fix:** a unit test that reconciles an Experiment whose
  Services exist from a status without names and expects the names in the
  result.

## How to validate the fix

1. Apply the change.
2. Run the two tests 100 times with reruns off, for example by dispatching the
   `Testing` workflow with `e2e_test_options: -run TestALBExperimentStepNoSetWeight`,
   `e2e_repeat: 25`, `e2e_jobs: 4`, `e2e_rerun_fails: 0`. Expect no failure; at
   today's rate about 30 of the 100 runs would fail.
3. Remove the `TODO: this test is flaky` comments from the two tests.
4. Run the seeded bugs and compare with `go run ./hack/testmigration killmatrix`.
   Bugs 16, 17 and 22 touch experiments and must still be caught.
