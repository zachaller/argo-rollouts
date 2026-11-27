//go:build e2e
// +build e2e

package e2e

import (
	"testing"

	"github.com/argoproj/argo-rollouts/test/fixtures"
)

// newRollbackTest sets up one test. It does what RollbackSuite.SetupSuite did once for the whole suite.
func newRollbackTest(t *testing.T) *fixtures.Test {
	s := fixtures.New(t)
	// shared analysis templates for suite
	s.ApplyManifests("@functional/analysistemplate-sleep-job.yaml")
	s.ApplyManifests("@functional/rollout-rollback-window.yaml")
	return s
}

func TestRollbackAnalysisWithinWindow(t *testing.T) {
	s := newRollbackTest(t)
	s.Given().
		HealthyRollout("@functional/rollout-rollback-window.yaml").
		When().
		UpdateSpec(`
metadata:
  labels:
    rev: two
spec:
  template:
    metadata:
      labels:
        rev: two`). // update to revision 2
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(1).
		When().
		UpdateSpec(`
metadata:
  labels:
    rev: three
spec:
  template:
    metadata:
      labels:
        rev: three`). // update to revision 3
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(2).
		When().
		UpdateSpec(`
metadata:
  labels:
    rev: four
spec:
  template:
    metadata:
      labels:
        rev: four`). // update to revision 4
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(3).
		When().
		UpdateSpec(`
metadata:
  labels:
    rev: two
spec:
  template:
    metadata:
      labels:
        rev: two`). // rollback to revision 2 (update to revision 5)
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(3) // fast rollback, no analysis run
}

func TestRollbackAnalysisOutsideWindow(t *testing.T) {
	s := newRollbackTest(t)
	s.Given().
		HealthyRollout("@functional/rollout-rollback-window.yaml").
		When().
		UpdateSpec(`
metadata:
  labels:
    rev: two
spec:
  template:
    metadata:
      labels:
        rev: two`). // update to revision 2
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(1).
		When().
		UpdateSpec(`
metadata:
  labels:
    rev: three
spec:
  template:
    metadata:
      labels:
        rev: three`). // update to revision 3
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(2).
		When().
		UpdateSpec(`
metadata:
  labels:
    rev: four
spec:
  template:
    metadata:
      labels:
        rev: four`). // update to revision 4
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(3).
		When().
		UpdateSpec(`
metadata:
  labels:
    rev: five
spec:
  template:
    metadata:
      labels:
        rev: five`). // update to revision 5
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(4).
		When().
		UpdateSpec(`
metadata:
  labels:
    rev: two
spec:
  template:
    metadata:
      labels:
        rev: two`). // rollback to revision 2 (update to revision 7)
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(5) // regular rollback, no fast track (outside window)
}
