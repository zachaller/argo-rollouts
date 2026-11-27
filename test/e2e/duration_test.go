//go:build e2e
// +build e2e

package e2e

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/test/fixtures"
)

// newDurationTest sets up one test.
func newDurationTest(t *testing.T) *fixtures.Test {
	s := fixtures.New(t)
	return s
}

// assertDurationFieldsConsistency validates that required duration fields are non-nil and consistent
func assertDurationFieldsConsistency(t *testing.T, ro *v1alpha1.Rollout) {
	t.Helper()

	require.NotNil(t, ro.Status.Duration, "Duration should be initialized")
	assert.NotNil(t, ro.Status.Duration.RolloutStartedAt, "RolloutStartedAt should be set")

	if ro.Status.Duration.FinishedAt != nil {
		assert.NotNil(t, ro.Status.Duration.CompletionStatus, "CompletionStatus should be set when completed")
		assert.Nil(t, ro.Status.Duration.ManualPauseStartedAt, "ManualPauseStartedAt should be nil when completed")
		// Some tests might be really fast and finishedAt will be equal to startedAt, so we allow it
		// Tests introducing sleeps
		if !ro.Status.Duration.FinishedAt.Equal(ro.Status.Duration.RolloutStartedAt) {
			assert.True(t, ro.Status.Duration.FinishedAt.After(ro.Status.Duration.RolloutStartedAt.Time), "FinishedAt should be after RolloutStartedAt")
		}
	}
}

// Canary Lifecycle Tests

func TestCanaryDuration_InitialRollout(t *testing.T) {
	s := newDurationTest(t)
	canarySteps := `
- setWeight: 50
- pause: {duration: 1s}`

	s.Given().
		RolloutTemplate("@functional/canary-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "canary-duration-start"}).
		SetSteps(canarySteps).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Initial rollout should have started and completed
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusPromoted, *ro.Status.Duration.CompletionStatus)
			assert.Nil(s.T(), ro.Status.Duration.TotalManualPauseDurationSeconds)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
		})
}

func TestCanaryDuration_SpecPaused(t *testing.T) {
	s := newDurationTest(t)
	canarySteps := `
- setWeight: 50
- pause: {duration: 1s}`

	s.Given().
		RolloutTemplate("@functional/canary-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "canary-duration-spec-paused"}).
		SetSteps(canarySteps).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		// Set spec.paused = true
		UpdateSpec(`{"spec":{"paused":true}}`).
		UpdateSpec(`{"spec":{"template":{"metadata":{"annotations":{"update":"1"}}}}}`).
		WaitForRolloutStatus("Paused").
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should be paused via spec.paused, tracking manual pause
			assertDurationFieldsConsistency(s.T(), ro)
			assert.NotNil(s.T(), ro.Status.Duration.ManualPauseStartedAt, "ManualPauseStartedAt should be set")
		}).
		When().
		// Resume by setting spec.paused = false
		Sleep(2*time.Second).
		UpdateSpec(`{"spec":{"paused":false}}`).
		WaitForRolloutStatus("Healthy", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should have completed after resume
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusPromoted, *ro.Status.Duration.CompletionStatus)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
			// TotalManualPauseDurationSeconds should be non-zero
			assert.Greater(s.T(), *ro.Status.Duration.TotalManualPauseDurationSeconds, int64(0), "TotalManualPauseDurationSeconds should be greater than 0")
		})
}

func TestCanaryDuration_PauseStep(t *testing.T) {
	s := newDurationTest(t)
	canarySteps := `
- setWeight: 50
- pause: {duration: 1s}`

	s.Given().
		RolloutTemplate("@functional/canary-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "canary-duration-pause-step"}).
		SetSteps(canarySteps).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateSpec().
		WaitForRolloutStatus("Progressing").
		WaitForRolloutStatus("Healthy", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusPromoted, *ro.Status.Duration.CompletionStatus)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
			assert.Nil(s.T(), ro.Status.Duration.TotalManualPauseDurationSeconds)
		})
}

func TestCanaryDuration_IndefinitePauseStep(t *testing.T) {
	s := newDurationTest(t)
	canarySteps := `
- setWeight: 50
- pause: {}`

	s.Given().
		RolloutTemplate("@functional/canary-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "canary-duration-indefinite-pause-step"}).
		SetSteps(canarySteps).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			assertDurationFieldsConsistency(s.T(), ro)
			assert.NotNil(s.T(), ro.Status.Duration.ManualPauseStartedAt, "ManualPauseStartedAt should be set for an indefinite pause step")
		}).
		When().
		Sleep(2*time.Second).
		PromoteRollout().
		WaitForRolloutStatus("Healthy", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusPromoted, *ro.Status.Duration.CompletionStatus)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
			assert.Greater(s.T(), *ro.Status.Duration.TotalManualPauseDurationSeconds, int64(0), "TotalManualPauseDurationSeconds should be greater than 0")
		})
}

func TestCanaryDuration_FullyPromoted(t *testing.T) {
	s := newDurationTest(t)
	canarySteps := `
- setWeight: 25
- pause: {}
- setWeight: 50
- pause: {}
- setWeight: 75
- pause: {}`

	s.Given().
		RolloutTemplate("@functional/canary-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "canary-duration-fully-promoted"}).
		SetSteps(canarySteps).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		// Full promote (skip all remaining steps)
		PromoteRolloutFull().
		WaitForRolloutStatus("Healthy", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should be completed with manually-promoted status
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusFastPromoted, *ro.Status.Duration.CompletionStatus)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
			assert.GreaterOrEqual(s.T(), *ro.Status.Duration.TotalManualPauseDurationSeconds, int64(0))
		})
}

func TestCanaryDuration_Abort(t *testing.T) {
	s := newDurationTest(t)
	canarySteps := `
- setWeight: 50
- pause: {}`

	s.Given().
		RolloutTemplate("@functional/canary-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "canary-duration-abort"}).
		SetSteps(canarySteps).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		AbortRollout().
		WaitForRolloutStatus("Degraded", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should be completed with abort status
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusAborted, *ro.Status.Duration.CompletionStatus)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
			assert.GreaterOrEqual(s.T(), *ro.Status.Duration.TotalManualPauseDurationSeconds, int64(0))
		})
}

func TestCanaryDuration_Retry(t *testing.T) {
	s := newDurationTest(t)
	initialStartedAt := metav1.Time{}
	canarySteps := `
- setWeight: 50
- pause: {}`

	s.Given().
		RolloutTemplate("@functional/canary-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "canary-duration-retry"}).
		SetSteps(canarySteps).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		AbortRollout().
		WaitForRolloutStatus("Degraded").
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			initialStartedAt = *ro.Status.Duration.RolloutStartedAt
		}).
		When().
		// Retry after abort
		Sleep(2*time.Second).
		RetryRollout().
		WaitForRolloutStatus("Paused", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should have restarted, duration reset
			assertDurationFieldsConsistency(s.T(), ro)
			assert.NotEqual(s.T(), initialStartedAt, *ro.Status.Duration.RolloutStartedAt)
			assert.Nil(s.T(), ro.Status.Duration.FinishedAt, "FinishedAt should be nil after retry")
			assert.Nil(s.T(), ro.Status.Duration.CompletionStatus, "CompletionStatus should be nil after retry")
		})
}

func TestCanaryDuration_SupersededRollout(t *testing.T) {
	s := newDurationTest(t)
	initialStartedAt := metav1.Time{}
	canarySteps := `
- setWeight: 50
- pause: {}`

	s.Given().
		RolloutTemplate("@functional/canary-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "canary-duration-superseded-forward"}).
		SetSteps(canarySteps).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateVersion("2").
		WaitForRolloutStatus("Paused").
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			initialStartedAt = *ro.Status.Duration.RolloutStartedAt
		}).
		When().
		// metav1.Time truncates to whole seconds, so give the next RolloutStartedAt
		// a full second of separation to avoid a spurious NotEqual failure.
		Sleep(1 * time.Second).
		UpdateVersion("3").
		WaitForRolloutStatus("Paused").
		PromoteRollout().
		WaitForRolloutStatus("Healthy", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			assertDurationFieldsConsistency(s.T(), ro)
			assert.NotEqual(s.T(), initialStartedAt, *ro.Status.Duration.RolloutStartedAt)
			assert.Equal(s.T(), v1alpha1.CompletionStatusPromoted, *ro.Status.Duration.CompletionStatus)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
		})
}

func TestCanaryDuration_SupersededRollback(t *testing.T) {
	s := newDurationTest(t)
	initialStartedAt := metav1.Time{}
	canarySteps := `
- setWeight: 50
- pause: {}`

	s.Given().
		RolloutTemplate("@functional/canary-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "canary-duration-superseded-rollback"}).
		SetSteps(canarySteps).
		SetVersion("1").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		// Start a rollout (revision 2)
		UpdateVersion("2").
		WaitForRolloutStatus("Paused").
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			initialStartedAt = *ro.Status.Duration.RolloutStartedAt
		}).
		When().
		// Supersede with rollback to revision 1
		UpdateVersion("1").
		// should be a fast rollback
		WaitForRolloutStatus("Healthy", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should be completed with fast rollback status
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), initialStartedAt, *ro.Status.Duration.RolloutStartedAt)
			assert.Equal(s.T(), v1alpha1.CompletionStatusFastRolledBack, *ro.Status.Duration.CompletionStatus)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
			assert.GreaterOrEqual(s.T(), *ro.Status.Duration.TotalManualPauseDurationSeconds, int64(0))
		})
}

func TestCanaryDuration_RollbackOutsideWindow(t *testing.T) {
	s := newDurationTest(t)
	canarySteps := `
- setWeight: 50
- pause: {}`

	s.Given().
		RolloutTemplate("@functional/canary-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "canary-duration-rollback-outside"}).
		SetSteps(canarySteps).
		SetVersion("1").
		RevisionHistoryLimit(5).
		SetRollbackWindow(1).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		// metav1.Time truncates to whole seconds; isRollbackWithinWindow compares
		// ReplicaSet CreationTimestamps, so give each revision a full second of
		// separation to avoid a spurious fast-rollback decision.
		Sleep(1 * time.Second).
		// Complete a rollout to revision 2
		UpdateVersion("2").
		WaitForRolloutStatus("Paused").
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		Sleep(1 * time.Second).
		// Complete a rollout to revision 3
		UpdateVersion("3").
		WaitForRolloutStatus("Paused").
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		Sleep(1 * time.Second).
		// Complete a rollout to revision 4
		UpdateVersion("4").
		WaitForRolloutStatus("Paused").
		// Rollback to revision 1 (outside scaledown window)
		UpdateVersion("1").
		WaitForRolloutStatus("Paused").
		PromoteRollout().
		WaitForRolloutStatus("Healthy", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should be completed with rolled-back status
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusRolledBack, *ro.Status.Duration.CompletionStatus)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
		})
}

func TestCanaryDuration_RollbackInsideWindow(t *testing.T) {
	s := newDurationTest(t)
	canarySteps := `
- setWeight: 50
- pause: {}`

	s.Given().
		RolloutTemplate("@functional/canary-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "canary-duration-rollback-inside"}).
		SetSteps(canarySteps).
		SetVersion("1").
		RevisionHistoryLimit(5).
		SetRollbackWindow(1).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		// metav1.Time truncates to whole seconds; isRollbackWithinWindow compares
		// ReplicaSet CreationTimestamps, so give each revision a full second of
		// separation to avoid a spurious full-rollback decision.
		Sleep(1 * time.Second).
		// Complete a rollout to revision 2
		UpdateVersion("2").
		WaitForRolloutStatus("Paused").
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		Sleep(1 * time.Second).
		// Complete a rollout to revision 3
		UpdateVersion("3").
		WaitForRolloutStatus("Paused").
		// Rollback to revision 1 (inside scaledown window)
		UpdateVersion("1").
		WaitForRolloutStatus("Healthy", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should be completed with rolled-back status
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusFastRolledBack, *ro.Status.Duration.CompletionStatus)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
		})
}

func TestCanaryDuration_ScalingOnly(t *testing.T) {
	s := newDurationTest(t)
	initialStartedAt := metav1.Time{}
	canarySteps := `
- setWeight: 50
- pause: {duration: 1s}`

	s.Given().
		RolloutTemplate("@functional/canary-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "canary-duration-scaling-only"}).
		SetSteps(canarySteps).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			initialStartedAt = *ro.Status.Duration.RolloutStartedAt
		}).
		When().
		UpdateSpec(`{"spec":{"replicas":2}}`).
		WaitForRolloutReplicas(2, 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Scaling only should not affect duration tracking
			// Duration should still be from the last rollout (completed)
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), initialStartedAt, *ro.Status.Duration.RolloutStartedAt)
		})
}

// BlueGreen Lifecycle Tests

func TestBlueGreenDuration_InitialRollout(t *testing.T) {
	s := newDurationTest(t)
	s.Given().
		RolloutTemplate("@functional/bluegreen-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "bluegreen-duration-start"}).
		AutoPromotionSeconds(2).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Initial rollout should have started and completed
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusPromoted, *ro.Status.Duration.CompletionStatus)
			assert.Nil(s.T(), ro.Status.Duration.TotalManualPauseDurationSeconds)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
		})
}

func TestBlueGreenDuration_SpecPaused(t *testing.T) {
	s := newDurationTest(t)
	s.Given().
		RolloutTemplate("@functional/bluegreen-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "bluegreen-duration-spec-paused"}).
		AutoPromotionSeconds(2).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateSpec(`{"spec":{"paused":true}}`).
		UpdateVersion("1").
		WaitForRolloutStatus("Paused").
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should be paused via spec.paused, tracking manual pause
			assertDurationFieldsConsistency(s.T(), ro)
			assert.NotNil(s.T(), ro.Status.Duration.ManualPauseStartedAt, "ManualPauseStartedAt should be set")
		}).
		When().
		// Resume by setting spec.paused = false
		Sleep(2*time.Second).
		UpdateSpec(`{"spec":{"paused":false}}`).
		WaitForRolloutStatus("Healthy", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should have completed after resume
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusPromoted, *ro.Status.Duration.CompletionStatus)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
			// TotalManualPauseDurationSeconds should be non-zero
			assert.Greater(s.T(), *ro.Status.Duration.TotalManualPauseDurationSeconds, int64(0), "TotalManualPauseDurationSeconds should be greater than 0")
		})
}

func TestBlueGreenDuration_AutoPromotion(t *testing.T) {
	s := newDurationTest(t)
	s.Given().
		RolloutTemplate("@functional/bluegreen-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "bluegreen-duration-auto-promotion"}).
		AutoPromotionSeconds(2).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateSpec().
		WaitForRolloutStatus("Healthy", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should auto-promote after delay
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusPromoted, *ro.Status.Duration.CompletionStatus)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
			assert.Nil(s.T(), ro.Status.Duration.TotalManualPauseDurationSeconds)
		})
}

func TestBlueGreenDuration_ManualPromotion(t *testing.T) {
	s := newDurationTest(t)
	s.Given().
		RolloutTemplate("@functional/bluegreen-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "bluegreen-duration-manual-promotion"}).
		AutoPromotionEnabled(false).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		PromoteRollout().
		WaitForRolloutStatus("Healthy", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusPromoted, *ro.Status.Duration.CompletionStatus)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
			assert.GreaterOrEqual(s.T(), *ro.Status.Duration.TotalManualPauseDurationSeconds, int64(0), "TotalManualPauseDurationSeconds should be greater than 0")
		})
}

func TestBlueGreenDuration_FullyPromoted(t *testing.T) {
	s := newDurationTest(t)
	s.Given().
		RolloutTemplate("@functional/bluegreen-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "bluegreen-duration-fully-promoted"}).
		AutoPromotionEnabled(false).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		PromoteRolloutFull().
		WaitForRolloutStatus("Healthy", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusFastPromoted, *ro.Status.Duration.CompletionStatus)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
			assert.GreaterOrEqual(s.T(), *ro.Status.Duration.TotalManualPauseDurationSeconds, int64(0), "TotalManualPauseDurationSeconds should be greater than 0")
		})
}

func TestBlueGreenDuration_Abort(t *testing.T) {
	s := newDurationTest(t)
	s.Given().
		RolloutTemplate("@functional/bluegreen-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "bluegreen-duration-abort"}).
		AutoPromotionEnabled(false).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		AbortRollout().
		WaitForRolloutStatus("Degraded", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should be completed with abort status
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusAborted, *ro.Status.Duration.CompletionStatus)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
			assert.GreaterOrEqual(s.T(), *ro.Status.Duration.TotalManualPauseDurationSeconds, int64(0))
		})
}

func TestBlueGreenDuration_Retry(t *testing.T) {
	s := newDurationTest(t)
	initialStartedAt := metav1.Time{}
	s.Given().
		RolloutTemplate("@functional/bluegreen-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "bluegreen-duration-retry"}).
		AutoPromotionEnabled(false).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		AbortRollout().
		WaitForRolloutStatus("Degraded").
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			initialStartedAt = *ro.Status.Duration.RolloutStartedAt
		}).
		When().
		// Retry after abort
		Sleep(2*time.Second).
		RetryRollout().
		WaitForRolloutStatus("Paused", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should have restarted, duration reset
			assertDurationFieldsConsistency(s.T(), ro)
			assert.NotEqual(s.T(), initialStartedAt, *ro.Status.Duration.RolloutStartedAt)
			assert.Nil(s.T(), ro.Status.Duration.FinishedAt, "FinishedAt should be nil after retry")
			assert.Nil(s.T(), ro.Status.Duration.CompletionStatus, "CompletionStatus should be nil after retry")
		})
}

func TestBlueGreenDuration_SupersededRollout(t *testing.T) {
	s := newDurationTest(t)
	initialStartedAt := metav1.Time{}
	s.Given().
		RolloutTemplate("@functional/bluegreen-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "bluegreen-duration-superseded-rollout"}).
		AutoPromotionEnabled(false).
		SetVersion("1").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateVersion("2").
		WaitForRolloutStatus("Paused").
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			initialStartedAt = *ro.Status.Duration.RolloutStartedAt
		}).
		When().
		// metav1.Time truncates to whole seconds, so give the next RolloutStartedAt
		// a full second of separation to avoid a spurious NotEqual failure.
		Sleep(1 * time.Second).
		UpdateVersion("3").
		WaitForRolloutStatus("Paused").
		PromoteRollout().
		WaitForRolloutStatus("Healthy", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			assertDurationFieldsConsistency(s.T(), ro)
			assert.NotEqual(s.T(), initialStartedAt, *ro.Status.Duration.RolloutStartedAt)
			assert.Equal(s.T(), v1alpha1.CompletionStatusPromoted, *ro.Status.Duration.CompletionStatus)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
		})
}

func TestBlueGreenDuration_SupersededRollbackToStable(t *testing.T) {
	s := newDurationTest(t)
	initialStartedAt := metav1.Time{}
	s.Given().
		RolloutTemplate("@functional/bluegreen-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "bluegreen-duration-superseded-rollback-stable"}).
		AutoPromotionEnabled(false).
		SetVersion("1").
		ScaleDownDelaySeconds(0). // No scale-down delay so we know we ecaluate a rollback to stable
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		// Start a rollout (revision 2)
		UpdateVersion("2").
		WaitForRolloutStatus("Paused").
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			initialStartedAt = *ro.Status.Duration.RolloutStartedAt
		}).
		When().
		// Supersede with rollback to revision 1
		UpdateVersion("1").
		// should be a fast rollback
		WaitForRolloutStatus("Healthy", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should be completed with fast rollback status
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), initialStartedAt, *ro.Status.Duration.RolloutStartedAt)
			assert.Equal(s.T(), v1alpha1.CompletionStatusFastRolledBack, *ro.Status.Duration.CompletionStatus)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
			assert.GreaterOrEqual(s.T(), *ro.Status.Duration.TotalManualPauseDurationSeconds, int64(0))
		})
}

func TestBlueGreenDuration_SupersededRollback(t *testing.T) {
	s := newDurationTest(t)

	s.Given().
		RolloutTemplate("@functional/bluegreen-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "bluegreen-duration-superseded-rollback"}).
		AutoPromotionEnabled(false).
		SetVersion("1").
		ScaleDownDelaySeconds(30). // Explicitly set scale down delay since we are testing it
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		// metav1.Time truncates to whole seconds; isRollback compares ReplicaSet
		// CreationTimestamps, so give revisions 1 and 2 a full second of
		// separation. Otherwise the update back to revision 1 is not seen as a
		// rollback and the rollout pauses.
		Sleep(1 * time.Second).
		// Complete a rollout to revision 2
		UpdateVersion("2").
		WaitForRolloutStatus("Paused").
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		// Complete a rollout to revision 3
		UpdateVersion("3").
		WaitForRolloutStatus("Paused").
		// Rollback to revision 1 (inside scaledown delay)
		UpdateVersion("1").
		WaitForRolloutStatus("Healthy", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should be completed with rolled-back status
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusFastRolledBack, *ro.Status.Duration.CompletionStatus)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
		})
}

func TestBlueGreenDuration_RollbackOutsideWindow(t *testing.T) {
	s := newDurationTest(t)

	s.Given().
		RolloutTemplate("@functional/bluegreen-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "bluegreen-duration-rollback-outside-window"}).
		AutoPromotionEnabled(false).
		SetVersion("1").
		RevisionHistoryLimit(5).
		SetRollbackWindow(1).
		ScaleDownDelaySeconds(0). // No scale-down delay so we know that windows are used for rollback
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		// metav1.Time truncates to whole seconds; isRollbackWithinWindow compares
		// ReplicaSet CreationTimestamps, so give each revision a full second of
		// separation to avoid a spurious fast-rollback decision.
		Sleep(1 * time.Second).
		// Complete a rollout to revision 2
		UpdateVersion("2").
		WaitForRolloutStatus("Paused").
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		Sleep(1 * time.Second).
		// Complete a rollout to revision 3
		UpdateVersion("3").
		WaitForRolloutStatus("Paused").
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		Sleep(1 * time.Second).
		// Complete a rollout to revision 4
		UpdateVersion("4").
		WaitForRolloutStatus("Paused").
		// Rollback to revision 1 (outside scaledown window)
		UpdateVersion("1").
		WaitForRolloutStatus("Paused", 10*time.Second).
		PromoteRollout().
		WaitForRolloutStatus("Healthy", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should be completed with rolled-back status
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusRolledBack, *ro.Status.Duration.CompletionStatus)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
		})
}

func TestBlueGreenDuration_RollbackInsideWindow(t *testing.T) {
	s := newDurationTest(t)

	s.Given().
		RolloutTemplate("@functional/bluegreen-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "bluegreen-duration-rollback-inside-window"}).
		AutoPromotionEnabled(false).
		SetVersion("1").
		RevisionHistoryLimit(5).
		SetRollbackWindow(1).
		ScaleDownDelaySeconds(0). // No scale-down delay so we know that windows are used for rollback
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		// metav1.Time truncates to whole seconds; isRollbackWithinWindow compares
		// ReplicaSet CreationTimestamps, so give each revision a full second of
		// separation to avoid a spurious full-rollback decision.
		Sleep(1 * time.Second).
		// Complete a rollout to revision 2
		UpdateVersion("2").
		WaitForRolloutStatus("Paused").
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		Sleep(1 * time.Second).
		// Complete a rollout to revision 3
		UpdateVersion("3").
		WaitForRolloutStatus("Paused").
		// Rollback to revision 1 (inside scaledown window)
		UpdateVersion("1").
		WaitForRolloutStatus("Healthy", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should be completed with rolled-back status
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusFastRolledBack, *ro.Status.Duration.CompletionStatus)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
		})
}

func TestBlueGreenDuration_ScalingOnly(t *testing.T) {
	s := newDurationTest(t)
	initialStartedAt := metav1.Time{}
	s.Given().
		RolloutTemplate("@functional/bluegreen-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "bluegreen-duration-scaling-only"}).
		AutoPromotionSeconds(2).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			initialStartedAt = *ro.Status.Duration.RolloutStartedAt
		}).
		When().
		UpdateSpec(`{"spec":{"replicas":2}}`).
		WaitForRolloutReplicas(2, 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Scaling only should not affect duration tracking
			// Duration should still be from the last rollout (completed)
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), initialStartedAt, *ro.Status.Duration.RolloutStartedAt)
		})
}
