package analysis

import (
	"context"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	patchtypes "k8s.io/apimachinery/pkg/types"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	controllerutil "github.com/argoproj/argo-rollouts/utils/controller"
	"github.com/argoproj/argo-rollouts/utils/diff"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

func (c *Controller) persistAnalysisRunStatus(orig *v1alpha1.AnalysisRun, newStatus v1alpha1.AnalysisRunStatus) error {
	ctx := context.TODO()
	logCtx := logutil.WithAnalysisRun(orig)
	patch, modified, err := diff.CreateTwoWayMergePatchWithResourceVersion(
		&v1alpha1.AnalysisRun{
			Status: orig.Status,
		},
		&v1alpha1.AnalysisRun{
			Status: newStatus,
		}, v1alpha1.AnalysisRun{}, orig.ResourceVersion)
	if err != nil {
		logCtx.Errorf("Error constructing AnalysisRun status patch: %v", err)
		return err
	}
	if !modified {
		logCtx.Info("No status changes. Skipping patch")
		return nil
	}
	logCtx.Debugf("AnalysisRun Patch: %s", patch)
	patched, err := c.argoProjClientset.ArgoprojV1alpha1().AnalysisRuns(orig.Namespace).Patch(ctx, orig.Name, patchtypes.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		if k8serrors.IsConflict(err) {
			// A concurrent write landed since we read from cache; requeue and retry against fresh
			// state rather than clobbering it.
			logCtx.Infof("Conflict while patching analysisRun, requeuing: %v", err)
			return controllerutil.StaleCacheError
		}
		logCtx.Warningf("Error updating analysisRun: %v", err)
		return err
	}
	c.analysisRunVersionTracker.Record(orig.Namespace+"/"+orig.Name, patched.ResourceVersion)
	logCtx.Info("Patch status successfully")
	return nil
}
