//go:build e2e
// +build e2e

package e2e

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/test/fixtures"
)

// These tests reach into the controller: they make some of its calls to the
// API server fail, or look at what it writes. They need the controller inside
// the test binary (E2E_CONTROLLER=inprocess) and are skipped without it.

// TestIstioDestinationRuleCleanupConflict deletes a Rollout whose
// DestinationRule the controller then has to clean, and makes the first two
// attempts fail with an update conflict. The controller has to try again
// until the clean-up is done.
//
// A conflict here used to end the controller process with a nil-pointer panic
// (test/migration-reports/fix-plans/istio-destinationrule-sync-error-crashes-controller.md).
func TestIstioDestinationRuleCleanupConflict(t *testing.T) {
	fixtures.RequireInProcessController(t)
	s := newIstioTest(t)

	var conflicts *fixtures.InjectedFault
	s.Given().
		RolloutObjects("@istio/istio-subset-split.yaml").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		Assert(func(t *fixtures.Then) {
			destrule := t.GetDestinationRule()
			assert.Contains(s.T(), destrule.Annotations, v1alpha1.ManagedByRolloutsKey)
			assert.Contains(s.T(), destrule.Spec.Subsets[0].Labels, v1alpha1.DefaultRolloutUniqueLabelKey)

			conflicts = s.FailControllerCalls(fixtures.Fault{
				Verb:     "update",
				Resource: "destinationrules",
				Status:   http.StatusConflict,
				Times:    2,
			})
		}).
		When().
		DeleteRollout().
		WaitFor("the DestinationRule to be cleaned", func(w *fixtures.When) bool {
			_, managed := w.GetDestinationRule().Annotations[v1alpha1.ManagedByRolloutsKey]
			return !managed
		}).
		Then().
		Assert(func(t *fixtures.Then) {
			// Without the conflicts the test would have shown nothing.
			assert.Equal(s.T(), 2, conflicts.Hits())

			destrule := t.GetDestinationRule()
			for _, subset := range destrule.Spec.Subsets {
				assert.NotContains(s.T(), subset.Labels, v1alpha1.DefaultRolloutUniqueLabelKey)
			}
		})
}

// TestHealthyRolloutControllerQuiet checks that the controller leaves a
// Rollout alone once it is healthy: nothing changes, so there is nothing to
// write. A controller that keeps writing is reacting to its own changes.
func TestHealthyRolloutControllerQuiet(t *testing.T) {
	fixtures.RequireInProcessController(t)
	s := fixtures.New(t)

	s.Given().
		HealthyRollout("@functional/rollout-basic.yaml").
		When().
		Then().
		ExpectControllerQuiet(10 * time.Second)
}

// TestScaleDownOnSuccessUpdateConflict is TestScaleDownOnSuccess with one
// difference: the controller's first update of the Deployment fails with a
// conflict, as it does when the Deployment's status is written at the same
// moment. The controller has to try again.
func TestScaleDownOnSuccessUpdateConflict(t *testing.T) {
	fixtures.KnownFailure(t, "a blue-green Rollout does not retry a failed scale-down of its workloadRef Deployment: "+
		"test/migration-reports/fix-plans/workloadref-scaledown-conflict-ignored.md")
	fixtures.RequireInProcessController(t)
	s := newFunctionalTest(t)

	conflict := s.FailControllerCalls(fixtures.Fault{
		Verb:     "update",
		Resource: "deployments",
		Status:   http.StatusConflict,
		Times:    1,
	})
	s.Given().
		RolloutObjects(`
kind: Service
apiVersion: v1
metadata:
  name: rollout-bluegreen-active
spec:
  selector:
    app: rollout-ref-deployment
  ports:
  - protocol: TCP
    port: 80
    targetPort: 8080
---
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: rollout-ref-deployment
spec:
  replicas: 2
  workloadRef:
    apiVersion: apps/v1
    kind: Deployment
    name: rollout-ref-deployment
    scaleDown: onsuccess
  strategy:
    blueGreen:
      activeService: rollout-bluegreen-active
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: rollout-ref-deployment
spec:
  replicas: 2
  selector:
    matchLabels:
      app: rollout-ref-deployment
  template:
    metadata:
      labels:
        app: rollout-ref-deployment
    spec:
      containers:
        - name: rollouts-demo
          image: argoproj/rollouts-demo:green
`).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectDeploymentReplicasCount("The deployment has been scaled to 0 replicas", "rollout-ref-deployment", 0)

	// Without the conflict the test would have shown nothing.
	assert.Equal(t, 1, conflict.Hits())
}
