package rollout

import (
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	metricsmocks "github.com/argoproj/argo-rollouts/controller/metrics/mocks"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
)

// durationTestContext builds a minimal rolloutContext for exercising
// calculateStatusDuration directly, recording every EmitRolloutDuration call.
func durationTestContext(t *testing.T, ro *v1alpha1.Rollout, newRS, stableRS *appsv1.ReplicaSet, emitted *[]*v1alpha1.RolloutDurationStatus) *rolloutContext {
	recorder := metricsmocks.NewMetricsRecorder(t)
	recorder.On("EmitRolloutDuration", mock.Anything).Run(func(args mock.Arguments) {
		ds := args.Get(0).(*v1alpha1.RolloutDurationStatus)
		*emitted = append(*emitted, ds.DeepCopy())
	}).Return().Maybe()

	logCtx := log.WithField("rollout", ro.Name)
	return &rolloutContext{
		rollout:  ro,
		newRS:    newRS,
		stableRS: stableRS,
		log:      logCtx,
		pauseContext: &pauseContext{
			rollout: ro,
			log:     logCtx,
		},
		reconcilerBase: reconcilerBase{metricsServer: recorder},
	}
}

func inProgressDurationRollout(currentPodHash string) *v1alpha1.Rollout {
	startedAt := metav1.Time{Time: timeutil.MetaNow().Add(-5 * time.Minute)}
	return &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{Name: "guestbook", Namespace: metav1.NamespaceDefault, Generation: 3},
		Spec: v1alpha1.RolloutSpec{
			Replicas: ptr.To(int32(3)),
			Strategy: v1alpha1.RolloutStrategy{Canary: &v1alpha1.CanaryStrategy{}},
		},
		Status: v1alpha1.RolloutStatus{
			CurrentPodHash: currentPodHash,
			Duration: &v1alpha1.RolloutDurationStatus{
				RolloutStartedAt: &startedAt,
			},
		},
	}
}

// TestCalculateStatusDurationSupersededWhileAborted verifies that a rollout that is
// superseded by a new pod spec while status.abort is still set only emits the
// superseded duration metrics once (the pending abort is moot once a new spec arrives).
func TestCalculateStatusDurationSupersededWhileAborted(t *testing.T) {
	ro := inProgressDurationRollout("old-hash-1234")
	ro.Status.Abort = true

	var emitted []*v1alpha1.RolloutDurationStatus
	ctx := durationTestContext(t, ro, nil, nil, &emitted)

	result := ctx.calculateStatusDuration(ro.Status.DeepCopy())

	assert.Equal(t, 1, len(emitted), "a superseded rollout must be counted exactly once")
	assert.Equal(t, v1alpha1.CompletionStatusSuperseded, emitted[0].GetCompletionStatus())
	// a new duration is started for the superseding revision
	assert.NotNil(t, result)
	assert.NotNil(t, result.RolloutStartedAt)
	assert.Nil(t, result.FinishedAt)
}

// TestCalculateStatusDurationRollbackWhileAborted verifies that when the user aborts a
// rollout and then reverts to a previous revision, the duration keeps being tracked
// until the rollback reaches the desired state instead of being completed (and its
// metrics emitted) at abort-processing time.
func TestCalculateStatusDurationRollbackWhileAborted(t *testing.T) {
	ro := inProgressDurationRollout("failed-new-hash")
	ro.Status.Abort = true
	ro.Status.StableRS = "stable-hash"

	// the rollback target RS is older than the stable RS
	newRS := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{
		Name:              "guestbook-old",
		CreationTimestamp: metav1.Time{Time: timeutil.MetaNow().Add(-1 * time.Hour)},
		Labels:            map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "old-rev-hash"},
	}}
	stableRS := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{
		Name:              "guestbook-stable",
		CreationTimestamp: metav1.Time{Time: timeutil.MetaNow().Add(-30 * time.Minute)},
		Labels:            map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "stable-hash"},
	}}

	var emitted []*v1alpha1.RolloutDurationStatus
	ctx := durationTestContext(t, ro, newRS, stableRS, &emitted)

	result := ctx.calculateStatusDuration(ro.Status.DeepCopy())

	assert.Equal(t, 0, len(emitted), "rollback duration must not be emitted before the rollback completes")
	assert.NotNil(t, result)
	assert.Equal(t, v1alpha1.CompletionStatusRollbacked, result.GetCompletionStatus())
	assert.Nil(t, result.FinishedAt, "rollback is still in progress")
	assert.Equal(t, ro.Status.Duration.RolloutStartedAt, result.RolloutStartedAt, "original start time is kept")
}

// TestCalculateStatusDurationRollbackToStableCompletes verifies that a rollback to the
// currently-stable ReplicaSet completes (and emits) in the same reconciliation.
func TestCalculateStatusDurationRollbackToStableCompletes(t *testing.T) {
	ro := inProgressDurationRollout("failed-new-hash")
	ro.Status.StableRS = "stable-hash"

	newRS := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "guestbook-stable",
			CreationTimestamp: metav1.Time{Time: timeutil.MetaNow().Add(-1 * time.Hour)},
			Labels:            map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "stable-hash"},
		},
		Status: appsv1.ReplicaSetStatus{AvailableReplicas: 3},
	}

	var emitted []*v1alpha1.RolloutDurationStatus
	ctx := durationTestContext(t, ro, newRS, newRS, &emitted)

	// as computed by calculateBaseStatus for this reconciliation
	newStatus := ro.Status.DeepCopy()
	newStatus.CurrentPodHash = "stable-hash"

	result := ctx.calculateStatusDuration(newStatus)

	assert.Equal(t, 1, len(emitted), "rollback to stable completes immediately")
	assert.Equal(t, v1alpha1.CompletionStatusFastRollbacked, emitted[0].GetCompletionStatus())
	assert.NotNil(t, result)
	assert.NotNil(t, result.FinishedAt)
}
