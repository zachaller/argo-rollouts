package fixtures

import (
	"context"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"

	rov1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	istioutil "github.com/argoproj/argo-rollouts/utils/istio"
)

var (
	deploymentGVR = schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "deployments",
	}

	serviceGVR = schema.GroupVersionResource{
		Version:  "v1",
		Resource: "services",
	}
	ingressGVR = schema.GroupVersionResource{
		Group:    "networking.k8s.io",
		Version:  "v1",
		Resource: "ingresses",
	}
	pdbGVR = schema.GroupVersionResource{
		Group:    "policy",
		Version:  "v1beta1",
		Resource: "poddisruptionbudgets",
	}
	jobGVR = schema.GroupVersionResource{
		Group:    "batch",
		Version:  "v1",
		Resource: "jobs",
	}
)

// deleteResources deletes the objects that match req, in the namespace of c
// and, for cluster-scoped kinds, in the whole cluster. It waits until they
// are gone.
func (c *Common) deleteResources(req *labels.Requirement, propagationPolicy metav1.DeletionPropagation) {
	c.log.Infof("Deleting %s", req.String())
	ctx := context.TODO()
	resources := []schema.GroupVersionResource{
		rov1.RolloutGVR,
		rov1.AnalysisRunGVR,
		rov1.AnalysisTemplateGVR,
		rov1.ClusterAnalysisTemplateGVR,
		rov1.ExperimentGVR,
		deploymentGVR,
		serviceGVR,
		ingressGVR,
		pdbGVR,
		istioutil.GetIstioVirtualServiceGVR(),
		istioutil.GetIstioDestinationRuleGVR(),
		jobGVR,
	}
	deleteOpts := metav1.DeleteOptions{PropagationPolicy: &propagationPolicy}
	listOpts := metav1.ListOptions{LabelSelector: req.String()}

	listResources := func(gvr schema.GroupVersionResource) []unstructured.Unstructured {
		var err error
		var lst *unstructured.UnstructuredList
		if gvr == rov1.ClusterAnalysisTemplateGVR {
			lst, err = c.dynamicClient.Resource(gvr).List(ctx, listOpts)
		} else {
			lst, err = c.dynamicClient.Resource(gvr).Namespace(c.namespace).List(ctx, listOpts)
		}
		if err != nil && !k8serrors.IsNotFound(err) {
			c.CheckError(err)
		}
		if lst == nil {
			return nil
		}
		return lst.Items
	}

	// Delete all resources with test label
	var err error
	resourcesRemaining := resources[:0]
	for _, gvr := range resources {
		switch gvr {
		case rov1.ClusterAnalysisTemplateGVR:
			err = c.dynamicClient.Resource(gvr).DeleteCollection(ctx, deleteOpts, listOpts)
		case serviceGVR:
			// Services do not support deletecollection
			for _, obj := range listResources(gvr) {
				if obj.GetDeletionTimestamp() != nil {
					continue
				}
				err = c.dynamicClient.Resource(gvr).Namespace(c.namespace).Delete(ctx, obj.GetName(), deleteOpts)
				c.CheckError(err)
			}
		default:
			// NOTE: deletecollection does not appear to work without supplying a namespace.
			// It errors with: the server could not find the requested resource
			err = c.dynamicClient.Resource(gvr).Namespace(c.namespace).DeleteCollection(ctx, deleteOpts, listOpts)
		}
		if err != nil && !k8serrors.IsNotFound(err) {
			c.t.Fatalf("could not delete %v: %v", gvr, err)
		}
		count := len(listResources(gvr))
		if count > 0 {
			c.log.Infof("Waiting for %d %s to delete", count, gvr.Resource)
			resourcesRemaining = append(resourcesRemaining, gvr)
		}
	}
	resources = resourcesRemaining

	// Wait for all instances to become deleted
	for {
		resourcesRemaining := resources[:0]
		for _, gvr := range resources {
			if len(listResources(gvr)) > 0 {
				resourcesRemaining = append(resourcesRemaining, gvr)
			}
		}
		resources = resourcesRemaining
		if len(resources) == 0 {
			break
		}
		time.Sleep(2 * time.Second)
	}
}
