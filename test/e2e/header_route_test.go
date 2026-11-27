//go:build e2e
// +build e2e

package e2e

import (
	"testing"
	"time"

	"github.com/tj/assert"

	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/istio"

	"github.com/argoproj/argo-rollouts/test/fixtures"
)

// newHeaderRouteTest sets up one test. It does what HeaderRouteSuite.SetupSuite did once for the whole suite.
func newHeaderRouteTest(t *testing.T) *fixtures.Test {
	s := fixtures.New(t)
	if !s.IstioEnabled {
		s.T().SkipNow()
	}
	return s
}

func TestIstioHostHeaderRoute(t *testing.T) {
	s := newHeaderRouteTest(t)
	s.Given().
		RolloutObjects("@header-routing/istio-hr-host.yaml").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		Assert(func(t *fixtures.Then) {
			vsvc := t.GetVirtualService()
			assert.Equal(s.T(), "primary", vsvc.Spec.HTTP[0].Name)
		}).
		When().
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		Sleep(1 * time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			vsvc := t.GetVirtualService()
			assert.Equal(s.T(), "set-header-1", vsvc.Spec.HTTP[0].Name)
			assertDestination(s, vsvc.Spec.HTTP[0], "canary-service", int64(100))
			assertDestination(s, vsvc.Spec.HTTP[1], "stable-service", int64(80))
			assertDestination(s, vsvc.Spec.HTTP[1], "canary-service", int64(20))
		}).
		When().
		PromoteRollout().
		WaitForRolloutStatus("Paused").
		Sleep(1 * time.Second).
		Then().
		When().
		PromoteRollout().
		WaitForRolloutStatus("Paused").
		Sleep(1 * time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			vsvc := t.GetVirtualService()
			assertDestination(s, vsvc.Spec.HTTP[0], "stable-service", int64(60))
			assertDestination(s, vsvc.Spec.HTTP[0], "canary-service", int64(40))
		}).
		When().
		PromoteRolloutFull().
		WaitForRolloutStatus("Healthy").
		Sleep(1 * time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			vsvc := t.GetVirtualService()
			assertDestination(s, vsvc.Spec.HTTP[0], "stable-service", int64(100))
			assertDestination(s, vsvc.Spec.HTTP[0], "canary-service", int64(0))
		})
}

func assertDestination(s *fixtures.Test, route istio.VirtualServiceHTTPRoute, service string, weight int64) {
	for _, destination := range route.Route {
		if destination.Destination.Host == service {
			assert.Equal(s.T(), weight, destination.Weight)
			return
		}
	}
	assert.Fail(s.T(), "Could not find the destination for service: %s", service)
}
