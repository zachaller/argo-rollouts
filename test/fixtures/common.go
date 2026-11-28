package fixtures

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"testing"
	"text/tabwriter"
	"time"

	k8errors "k8s.io/apimachinery/pkg/api/errors"

	a6util "github.com/argoproj/argo-rollouts/utils/apisix"

	smiv1alpha1 "github.com/servicemeshinterface/smi-sdk-go/pkg/apis/split/v1alpha1"
	smiclientset "github.com/servicemeshinterface/smi-sdk-go/pkg/gen/client/split/clientset/versioned"
	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rov1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/get"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/viewcontroller"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/appmesh"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/istio"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	istioutil "github.com/argoproj/argo-rollouts/utils/istio"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
	unstructuredutil "github.com/argoproj/argo-rollouts/utils/unstructured"
)

//nolint:structcheck
type Common struct {
	Context        context.Context
	t              *testing.T
	namespace      string
	log            *log.Entry
	kubernetesHost string
	kubeClient     kubernetes.Interface
	dynamicClient  dynamic.Interface
	rolloutClient  clientset.Interface
	smiClient      smiclientset.Interface

	rollout *unstructured.Unstructured
	objects []*unstructured.Unstructured

	// events is shared by pointer: Common is copied by value for every Given(),
	// and the watcher goroutine appends while the test goroutine reads.
	events *eventLog

	// out is where the Print* helpers write. Nil means the process-wide
	// outputs. A test that owns its output sets it, so that what is printed
	// about a failed test stays with that test when tests run in parallel.
	out io.Writer
}

// printOut returns the writer the Print* helpers should use.
func (c *Common) printOut(fallback io.Writer) io.Writer {
	if c.out != nil {
		return c.out
	}
	return fallback
}

// eventLog collects the events seen by StartEventWatch.
type eventLog struct {
	mu     sync.Mutex
	events []corev1.Event
	err    error
}

func (l *eventLog) add(event corev1.Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, event)
}

func (l *eventLog) fail(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.err = err
}

func (l *eventLog) snapshot() ([]corev1.Event, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]corev1.Event(nil), l.events...), l.err
}

// guard calls f, which is code of the test handed to a fixture, and turns a
// panic in it into a failure of that test.
//
// A plain test function has nothing around it that recovers: a panic stops the
// whole test binary, and with it every test running in parallel and every test
// that has not started. The testify suite these tests were written for
// recovered panics per test, and many assertions rely on that: they index an
// empty list or follow a nil pointer when the state they expect is not there.
// In the tests that code is in callbacks given to the fixtures, so guarding
// the callbacks covers it. Test code outside a callback has no such cover and
// must not rely on a panic to fail.
func (c *Common) guard(f func()) {
	defer func() {
		if r := recover(); r != nil {
			c.t.Errorf("test panicked: %v\n%s", r, debug.Stack())
			c.t.FailNow()
		}
	}()
	f()
}

// guardBool is guard for a callback that reports a condition.
func (c *Common) guardBool(f func() bool) (ok bool) {
	c.guard(func() { ok = f() })
	return ok
}

func (c *Common) CheckError(err error) {
	c.t.Helper()
	if err != nil {
		c.t.Fatal(err)
	}
}

// Rollout returns the original rollout manifest used in the test
func (c *Common) Rollout() *rov1.Rollout {
	var ro rov1.Rollout
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(c.rollout.Object, &ro)
	c.CheckError(err)
	return &ro
}

// GetRollout returns the live rollout object in the cluster
func (c *Common) GetRollout() *rov1.Rollout {
	ro, err := c.rolloutClient.ArgoprojV1alpha1().Rollouts(c.namespace).Get(context.TODO(), c.Rollout().GetName(), metav1.GetOptions{})
	c.CheckError(err)
	return ro
}

func (c *Common) PrintRollout(name string) {
	// ErrOut stays os.Stderr: NewArgoRolloutsOptions points klog's process-wide output at it.
	streams := genericclioptions.IOStreams{In: os.Stdin, Out: c.printOut(os.Stdout), ErrOut: os.Stderr}
	o := options.NewArgoRolloutsOptions(streams)
	getOptions := get.GetOptions{
		ArgoRolloutsOptions: *o,
	}
	controller := viewcontroller.NewRolloutViewController(c.namespace, name, c.kubeClient, c.rolloutClient)
	ctx := context.Background()
	controller.Start(ctx)
	ri, err := controller.GetRolloutInfo()
	c.CheckError(err)
	getOptions.PrintRollout(ri)
}

func (c *Common) PrintRolloutYAML(ro *rov1.Rollout) {
	ro = ro.DeepCopy()
	// declutter the output
	delete(ro.Annotations, "kubectl.kubernetes.io/last-applied-configuration")
	ro.ManagedFields = nil
	yamlBytes, err := yaml.Marshal(ro)
	c.CheckError(err)
	fmt.Fprintf(c.printOut(logrus.StandardLogger().Out), "\n---\n%s\n", string(yamlBytes))
}

func (c *Common) PrintExperiment(name string) {
	// ErrOut stays os.Stderr: NewArgoRolloutsOptions points klog's process-wide output at it.
	streams := genericclioptions.IOStreams{In: os.Stdin, Out: c.printOut(os.Stdout), ErrOut: os.Stderr}
	o := options.NewArgoRolloutsOptions(streams)
	getOptions := get.GetOptions{
		ArgoRolloutsOptions: *o,
	}
	controller := viewcontroller.NewExperimentViewController(c.namespace, name, c.kubeClient, c.rolloutClient)
	ctx := context.Background()
	controller.Start(ctx)
	ei, err := controller.GetExperimentInfo()
	c.CheckError(err)
	getOptions.PrintExperiment(ei)
}

func (c *Common) PrintExperimentYAML(ex *rov1.Experiment) {
	ex = ex.DeepCopy()
	// declutter the output
	delete(ex.Annotations, "kubectl.kubernetes.io/last-applied-configuration")
	ex.ManagedFields = nil
	yamlBytes, err := yaml.Marshal(ex)
	c.CheckError(err)
	fmt.Fprintf(c.printOut(logrus.StandardLogger().Out), "\n---\n%s\n", string(yamlBytes))
}

func (c *Common) GetReplicaSetByRevision(revision string) *appsv1.ReplicaSet {
	selector, err := metav1.LabelSelectorAsSelector(c.Rollout().Spec.Selector)
	c.CheckError(err)

	findReplicaSet := func() *appsv1.ReplicaSet {
		replicasets, err := c.kubeClient.AppsV1().ReplicaSets(c.namespace).List(c.Context, metav1.ListOptions{LabelSelector: selector.String()})
		c.CheckError(err)
		var candidate *appsv1.ReplicaSet
		for i, rs := range replicasets.Items {
			if rs.Annotations[annotations.RevisionAnnotation] == revision {
				if candidate != nil {
					c.t.Fatalf("Found multiple ReplicaSets with revision: %s", revision)
				}
				candidate = &replicasets.Items[i]
			}
		}
		return candidate
	}
	// make several attempts since sometimes we can check too early
	var found *appsv1.ReplicaSet
	for i := 0; i < 4; i++ {
		found = findReplicaSet()
		if found != nil {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if found == nil {
		c.t.Fatalf("Could not find ReplicaSet with revision: %s", revision)
	}
	return found
}

func (c *Common) GetPodsByRevision(revision string) *corev1.PodList {
	rs := c.GetReplicaSetByRevision(revision)
	pods, err := replicasetutil.GetPodsOwnedByReplicaSet(c.Context, c.kubeClient, rs)
	c.CheckError(err)
	podList := corev1.PodList{}
	for _, pod := range pods {
		podList.Items = append(podList.Items, *pod)
	}
	return &podList
}

// MarkPodsReady is a helper to mark the readiness gates of pods of a particular revision as ready.
func (c *Common) MarkPodsReady(revision string, quantity int) int {
	rs := c.GetReplicaSetByRevision(revision)
	pods, err := replicasetutil.GetPodsOwnedByReplicaSet(c.Context, c.kubeClient, rs)
	c.CheckError(err)
	podIf := c.kubeClient.CoreV1().Pods(rs.Namespace)
	marked := 0
	// c.log.Infof("Marking %d pods as ready", quantity)
	for _, pod := range pods {
		if marked < quantity {
			foundIdx := -1
			for i, cond := range pod.Status.Conditions {
				if cond.Type == "argoproj.io/e2e-readiness" {
					foundIdx = i
				}
			}
			if foundIdx >= 0 {
				continue
			}
			// retry multiple times to deal with resource conflicts
			for i := 0; i < 5; i++ {
				pod, err = podIf.Get(c.Context, pod.Name, metav1.GetOptions{})
				c.CheckError(err)
				pod.Status.Conditions = append(pod.Status.Conditions, corev1.PodCondition{
					Type:   "argoproj.io/e2e-readiness",
					Status: "True",
				})
				_, err := podIf.UpdateStatus(c.Context, pod, metav1.UpdateOptions{})
				if err == nil {
					break
				}
				if !k8serrors.IsConflict(err) {
					c.t.Fatalf("Could not set readiness on pod: %v", err)
				}
				time.Sleep(500 * time.Millisecond)
			}
			// c.log.Infof("Conditions: %v", pod.Status.Conditions)
			marked += 1
		}
	}
	c.log.Infof("Marked %d revision %s pods as ready", marked, revision)
	return marked
}

func (c *Common) GetRolloutAnalysisRuns() rov1.AnalysisRunList {
	aruns, err := c.rolloutClient.ArgoprojV1alpha1().AnalysisRuns(c.namespace).List(c.Context, metav1.ListOptions{})
	c.CheckError(err)
	// filter analysis runs by ones owned by rollout to allow test parallelism
	var newAruns rov1.AnalysisRunList
	for _, ar := range aruns.Items {
		controllerRef := metav1.GetControllerOf(&ar)
		if controllerRef != nil && controllerRef.Name == c.rollout.GetName() {
			newAruns.Items = append(newAruns.Items, ar)
		}
	}
	return newAruns
}

func (c *Common) GetBackgroundAnalysisRun() *rov1.AnalysisRun {
	aruns := c.GetRolloutAnalysisRuns()
	var found *rov1.AnalysisRun
	for i, arun := range aruns.Items {
		if arun.Labels[rov1.RolloutTypeLabel] != rov1.RolloutTypeBackgroundRunLabel {
			continue
		}
		if found != nil {
			c.log.Error("Found multiple background analysis runs")
			c.t.FailNow()
		}
		found = &aruns.Items[i]
	}
	if found == nil {
		c.log.Error("Background AnalysisRun not found")
		c.t.FailNow()
	}
	return found
}

func (c *Common) GetExperimentByName(name string) *rov1.Experiment {
	ex, err := c.rolloutClient.ArgoprojV1alpha1().Experiments(c.namespace).Get(c.Context, name, metav1.GetOptions{})
	c.CheckError(err)
	return ex
}

// GetInlineAnalysisRun returns the latest Step analysis run. This should generally be coupled with
// a count check, to ensure we are not checking the previous one. This may fail to accurately return
// the latest if the creationTimestamps are the same
func (c *Common) GetInlineAnalysisRun() *rov1.AnalysisRun {
	aruns := c.GetRolloutAnalysisRuns()
	var latest *rov1.AnalysisRun
	for i, arun := range aruns.Items {
		if arun.Labels[rov1.RolloutTypeLabel] != rov1.RolloutTypeStepLabel {
			continue
		}
		if latest == nil {
			latest = &arun
			continue
		}
		if arun.CreationTimestamp.After(latest.CreationTimestamp.Time) {
			latest = &aruns.Items[i]
		}
		if arun.CreationTimestamp.Equal(&latest.CreationTimestamp) {
			c.log.Warnf("Found multiple inline analysis runs with same creationTimestamp: %s, %s", arun.Name, latest.Name)
		}
	}
	if latest == nil {
		c.log.Error("Inline AnalysisRun not found")
		c.t.FailNow()
	}
	return latest
}

func (c *Common) GetPrePromotionAnalysisRun() *rov1.AnalysisRun {
	aruns := c.GetRolloutAnalysisRuns()
	var found *rov1.AnalysisRun
	for i, arun := range aruns.Items {
		if arun.Labels[rov1.RolloutTypeLabel] != rov1.RolloutTypePrePromotionLabel {
			continue
		}
		if found != nil {
			c.log.Error("Found multiple pre-promotion analysis runs")
			c.t.FailNow()
		}
		found = &aruns.Items[i]
	}
	if found == nil {
		c.log.Error("Pre-promotion AnalysisRun not found")
		c.t.FailNow()
	}
	return found
}

func (c *Common) GetPostPromotionAnalysisRun() *rov1.AnalysisRun {
	aruns := c.GetRolloutAnalysisRuns()
	var found *rov1.AnalysisRun
	for i, arun := range aruns.Items {
		if arun.Labels[rov1.RolloutTypeLabel] != rov1.RolloutTypePostPromotionLabel {
			continue
		}
		if found != nil {
			c.log.Error("Found multiple post-promotion analysis runs")
			c.t.FailNow()
		}
		found = &aruns.Items[i]
	}
	if found == nil {
		c.log.Error("Post-promotion AnalysisRun not found")
		c.t.FailNow()
	}
	return found
}

// GetExperimentAnalysisRun returns the AnalysisRun associated with the experiment.
// Errors if there is more than one.
func (c *Common) GetExperimentAnalysisRun(exp *rov1.Experiment) *rov1.AnalysisRun {
	aruns, err := c.rolloutClient.ArgoprojV1alpha1().AnalysisRuns(c.namespace).List(c.Context, metav1.ListOptions{})
	c.CheckError(err)
	var found *rov1.AnalysisRun
	for i, ar := range aruns.Items {
		controllerRef := metav1.GetControllerOf(&ar)
		if controllerRef != nil && controllerRef.UID == exp.UID {
			if found != nil {
				c.log.Errorf("Found multiple AnalysisRuns associated with experiment: %s", exp.Name)
				c.t.FailNow()
			}
			found = &aruns.Items[i]
		}
	}
	if found == nil {
		c.log.Error("Experiment AnalysisRun not found")
		c.t.FailNow()
	}
	return found
}

func (c *Common) GetRolloutExperiments() rov1.ExperimentList {
	exps, err := c.rolloutClient.ArgoprojV1alpha1().Experiments(c.namespace).List(c.Context, metav1.ListOptions{})
	c.CheckError(err)
	// filter experiments by ones owned by rollout to allow test parallellism
	var newExps rov1.ExperimentList
	for _, exp := range exps.Items {
		controllerRef := metav1.GetControllerOf(&exp)
		if controllerRef != nil && controllerRef.Name == c.rollout.GetName() {
			newExps.Items = append(newExps.Items, exp)
		}
	}
	return newExps
}

func (c *Common) GetExperimentByRevision(revision string) *rov1.Experiment {
	expList := c.GetRolloutExperiments()
	var found *rov1.Experiment
	for i, exp := range expList.Items {
		if exp.Annotations[annotations.RevisionAnnotation] == revision {
			if found != nil {
				c.t.Fatalf("Found multiple Experiments with revision: %s", revision)
			}
			found = &expList.Items[i]
		}
	}
	if found == nil {
		c.t.Fatalf("Could not find Experiment with revision: %s", revision)
	}
	return found
}

// GetJobFromAnalysisRun returns the Job associated with the AnalysisRun.
// Errors if there is more than one.
func (c *Common) GetJobFromAnalysisRun(ar *rov1.AnalysisRun) *batchv1.Job {
	jobs, err := c.kubeClient.BatchV1().Jobs(c.namespace).List(c.Context, metav1.ListOptions{})
	c.CheckError(err)

	var found *batchv1.Job
	for i, job := range jobs.Items {
		controllerRef := metav1.GetControllerOf(&job)
		if controllerRef != nil && controllerRef.UID == ar.UID {
			if found != nil {
				c.t.Fatalf("Found multiple Jobs associated with AnalysisRun: %s", ar.Name)
			}
			found = &jobs.Items[i]
		}
	}
	if found == nil {
		c.t.Fatalf("Could not find Job from AnalysisRun: %s", ar.Name)
	}
	return found
}

func (c *Common) GetReplicaSetFromExperiment(exp *rov1.Experiment, templateName string) *appsv1.ReplicaSet {
	replicasets, err := c.kubeClient.AppsV1().ReplicaSets(c.namespace).List(c.Context, metav1.ListOptions{})
	c.CheckError(err)
	var found *appsv1.ReplicaSet
	for i, rs := range replicasets.Items {
		controllerRef := metav1.GetControllerOf(&rs)
		if controllerRef == nil || controllerRef.UID != exp.UID || rs.Annotations[rov1.ExperimentTemplateNameAnnotationKey] != templateName {
			continue
		}
		if found != nil {
			c.t.Fatalf("Found multiple ReplicaSet associated with experiment: %s, template: %s", exp.Name, templateName)
		}
		found = &replicasets.Items[i]
	}
	if found == nil {
		c.t.Fatalf("Could not find ReplicaSet from experiment: %s, template: %s", exp.Name, templateName)
	}
	return found
}

// ApplyManifests kubectl applys the given YAML string or file path:
// 1. A file name if it starts with "@"
// 2. Raw YAML.
func (c *Common) ApplyManifests(text string) {
	c.t.Helper()
	objs := c.parseTextToObjects(text)
	for _, obj := range objs {
		c.applyObject(obj)
	}
}

func (c *Common) parseTextToObjects(text string) []*unstructured.Unstructured {
	yamlBytes := c.yamlBytes(text)
	// Some E2E AnalysisTemplates use http://kubernetes.default.svc/version as a fake metric provider.
	// This doesn't work outside the cluster, so the following replaces it with the host from the
	// rest config.
	yamlString := strings.ReplaceAll(string(yamlBytes), "https://kubernetes.default.svc", c.kubernetesHost)
	objs, err := unstructuredutil.SplitYAML(yamlString)
	c.CheckError(err)
	return objs
}

// manifestRoot is the directory that "@file" manifest references are read
// from. Empty means the working directory.
var manifestRoot string

// SetManifestRoot sets the directory that "@file" manifest references are
// read from. Call it before any test starts, from TestMain, when the test
// process does not keep the package directory as its working directory.
func SetManifestRoot(dir string) {
	manifestRoot = dir
}

func (c *Common) yamlBytes(text string) []byte {
	var yamlBytes []byte
	var err error
	if strings.HasPrefix(text, "@") {
		file := strings.TrimPrefix(text, "@")
		if manifestRoot != "" && !filepath.IsAbs(file) {
			file = filepath.Join(manifestRoot, file)
		}
		yamlBytes, err = os.ReadFile(file)
		c.CheckError(err)
	} else {
		yamlBytes = []byte(text)
	}
	return yamlBytes
}

func (c *Common) applyObject(obj *unstructured.Unstructured) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	if E2ELabelValueInstanceID != "" {
		labels[rov1.LabelKeyControllerInstanceID] = E2ELabelValueInstanceID
	}
	testNameSplit := strings.SplitN(c.t.Name(), "/", 2)
	if len(testNameSplit) > 1 {
		// a subtest: TestName/SubtestName
		labels[E2ELabelKeyTestName] = testNameSplit[1]
	} else {
		// a top-level test
		labels[E2ELabelKeyTestName] = testNameSplit[0]
	}
	obj.SetLabels(labels)

	objBytes, err := json.Marshal(obj)
	c.CheckError(err)
	// The namespace is passed explicitly: with a namespace per test, the
	// kubeconfig's current namespace is not the one the test owns. An object
	// that names its own namespace keeps it; kubectl rejects a -n that
	// disagrees with the object.
	args := []string{"apply", "-f", "-"}
	if obj.GetNamespace() == "" {
		args = []string{"apply", "-n", c.namespace, "-f", "-"}
	}
	cmd := exec.Command("kubectl", args...)
	cmd.Env = os.Environ()
	cmd.Stdin = bytes.NewReader(objBytes)
	out, err := cmd.CombinedOutput()
	if err != nil {
		gvk := obj.GetObjectKind().GroupVersionKind()
		objMap, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
		un := unstructured.Unstructured{Object: objMap}
		c.log.Errorf("kubectl apply of %s %s failed: %s", gvk.Kind, un.GetName(), out)
		c.t.FailNow()
	}
	c.log.Info(string(out))
}

func (c *Common) deleteObject(kind, name string) {
	cmd := exec.Command("kubectl", "delete", "-n", c.namespace, kind, name)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		c.log.Errorf("kubectl delete of %s/%s failed: %s", kind, name, out)
		c.t.FailNow()
	}
	c.log.Info(string(out))
}

func (c *Common) SetLabels(obj *unstructured.Unstructured) {
	labels := obj.GetLabels()
	labels[E2ELabelKeyTestName] = c.t.Name()
	obj.SetLabels(labels)
}

// GetServices() returns the desired (aka preview/canary) and stable (aka active) services
func (c *Common) GetServices() (*corev1.Service, *corev1.Service) {
	var desiredName, stableName string
	ro := c.Rollout()
	if ro.Spec.Strategy.BlueGreen != nil {
		desiredName = ro.Spec.Strategy.BlueGreen.PreviewService
		stableName = ro.Spec.Strategy.BlueGreen.ActiveService
	} else {
		desiredName = ro.Spec.Strategy.Canary.CanaryService
		stableName = ro.Spec.Strategy.Canary.StableService
	}
	var desiredSvc, stableSvc *corev1.Service
	var err error
	if desiredName != "" {
		desiredSvc, err = c.kubeClient.CoreV1().Services(c.namespace).Get(c.Context, desiredName, metav1.GetOptions{})
		c.CheckError(err)
	}
	if stableName != "" {
		stableSvc, err = c.kubeClient.CoreV1().Services(c.namespace).Get(c.Context, stableName, metav1.GetOptions{})
		c.CheckError(err)
	}
	return desiredSvc, stableSvc
}

func (c *Common) GetALBIngress() *networkingv1.Ingress {
	ro := c.Rollout()
	name := ro.Spec.Strategy.Canary.TrafficRouting.ALB.Ingress
	ingress, err := c.kubeClient.NetworkingV1().Ingresses(c.namespace).Get(c.Context, name, metav1.GetOptions{})
	c.CheckError(err)
	return ingress
}

func (c *Common) GetALBIngresses() []*networkingv1.Ingress {
	ro := c.Rollout()
	names := ro.Spec.Strategy.Canary.TrafficRouting.ALB.Ingresses
	ingresses := []*networkingv1.Ingress{}
	for _, name := range names {
		ingress, err := c.kubeClient.NetworkingV1().Ingresses(c.namespace).Get(c.Context, name, metav1.GetOptions{})
		c.CheckError(err)
		ingresses = append(ingresses, ingress)
	}
	return ingresses
}

func (c *Common) GetNginxIngressStable() *networkingv1.Ingress {
	ro := c.Rollout()
	name := ro.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngress
	ingress, err := c.kubeClient.NetworkingV1().Ingresses(c.namespace).Get(c.Context, name, metav1.GetOptions{})
	c.CheckError(err)
	return ingress
}

func (c *Common) GetNginxIngressCanary() *networkingv1.Ingress {
	ro := c.Rollout()
	name := ro.Name + "-" + ro.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngress + "-canary"
	ingress, err := c.kubeClient.NetworkingV1().Ingresses(c.namespace).Get(c.Context, name, metav1.GetOptions{})
	c.CheckError(err)
	return ingress
}

func (c *Common) GetTrafficSplit() *smiv1alpha1.TrafficSplit {
	ro := c.Rollout()
	name := ro.Spec.Strategy.Canary.TrafficRouting.SMI.TrafficSplitName
	ts, err := c.smiClient.SplitV1alpha1().TrafficSplits(c.namespace).Get(c.Context, name, metav1.GetOptions{})
	c.CheckError(err)
	return ts
}

func (c *Common) GetVirtualService() *istio.VirtualService {
	ro := c.Rollout()
	name := ro.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService.Name
	vsvcClient := c.dynamicClient.Resource(istioutil.GetIstioVirtualServiceGVR()).Namespace(c.namespace)
	vsvcUn, err := vsvcClient.Get(c.Context, name, metav1.GetOptions{})
	c.CheckError(err)
	var vsvc istio.VirtualService
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(vsvcUn.Object, &vsvc)
	c.CheckError(err)
	return &vsvc
}

func (c *Common) GetApisixRoute() *unstructured.Unstructured {
	ro := c.Rollout()
	ctx := context.TODO()
	dyClient := a6util.NewDynamicClient(c.dynamicClient, c.namespace)
	name := ro.Spec.Strategy.Canary.TrafficRouting.Apisix.Route.Name
	a6Route, err := dyClient.Get(ctx, name, metav1.GetOptions{})
	c.CheckError(err)
	return a6Route
}

func (c *Common) GetApisixSetHeaderRoute() *unstructured.Unstructured {
	ctx := context.TODO()
	rollout, err := c.rolloutClient.ArgoprojV1alpha1().Rollouts(c.namespace).Get(ctx, c.Rollout().GetName(), metav1.GetOptions{})
	c.CheckError(err)
	dyClient := a6util.NewDynamicClient(c.dynamicClient, c.namespace)
	index := *rollout.Status.CurrentStepIndex
	if step := rollout.Spec.Strategy.Canary.Steps[index]; step.SetHeaderRoute != nil {
		name := step.SetHeaderRoute.Name
		a6Route, err := dyClient.Get(ctx, name, metav1.GetOptions{})
		c.CheckError(err)
		return a6Route
	}
	return nil
}

func (c *Common) GetAppMeshVirtualRouter() *unstructured.Unstructured {
	ro := c.Rollout()
	ctx := context.TODO()
	resClient := appmesh.NewResourceClient(c.dynamicClient)
	name := ro.Spec.Strategy.Canary.TrafficRouting.AppMesh.VirtualService.Name
	c.log.Infof("GetVirtualServiceCR with namespace(%s), name(%s)", c.namespace, name)
	uVsvc, err := resClient.GetVirtualServiceCR(ctx, c.namespace, name)
	c.CheckError(err)
	uVr, err := resClient.GetVirtualRouterCRForVirtualService(ctx, uVsvc)
	c.CheckError(err)
	return uVr
}

func (c *Common) GetDestinationRule() *istio.DestinationRule {
	ro := c.Rollout()
	name := ro.Spec.Strategy.Canary.TrafficRouting.Istio.DestinationRule.Name
	destRuleClient := c.dynamicClient.Resource(istioutil.GetIstioDestinationRuleGVR()).Namespace(c.namespace)
	destRuleUn, err := destRuleClient.Get(c.Context, name, metav1.GetOptions{})
	c.CheckError(err)
	var destRule istio.DestinationRule
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(destRuleUn.Object, &destRule)
	c.CheckError(err)
	return &destRule
}

// We use a watch to collect events (as opposed to listing them after-the-fact), because:
// 1. the kubernetes event recorder can dedupe multiple events into one Event object
// 2. listing events may return events out-of-order from when they were produced
func (c *Common) StartEventWatch(ctx context.Context) {
	watchEventsIf, err := c.kubeClient.CoreV1().Events(c.namespace).Watch(ctx, metav1.ListOptions{})
	c.events = nil
	c.CheckError(err)
	c.log.Infof("Event watcher started")
	events := &eventLog{}
	c.events = events
	logCtx := c.log

	// The goroutine must not touch c.t: failing a test from another goroutine is
	// not allowed, and the watcher can outlive the test. A broken watch is
	// recorded and reported by GetRolloutEventReasons instead.
	go func() {
		for {
			select {
			case watchEvent, open := <-watchEventsIf.ResultChan():
				if !open {
					if ctx.Err() == nil {
						events.fail(fmt.Errorf("event watch closed unexpectedly"))
					}
					return
				}
				event, ok := watchEvent.Object.(*corev1.Event)
				if !ok {
					events.fail(fmt.Errorf("received non-event from event watch: %v", watchEvent))
					return
				}
				events.add(*event)
			case <-ctx.Done():
				logCtx.Infof("Event watcher stopped")
				return
			}
		}
	}()
}

func (c *Common) GetRolloutEventReasons() []string {
	ro, err := c.rolloutClient.ArgoprojV1alpha1().Rollouts(c.namespace).Get(c.Context, c.rollout.GetName(), metav1.GetOptions{})
	c.CheckError(err)
	if c.events == nil {
		return nil
	}
	events, err := c.events.snapshot()
	c.CheckError(err)
	var reasons []string
	for _, event := range events {
		if event.InvolvedObject.UID == ro.UID {
			reasons = append(reasons, event.Reason)
		}
	}
	return reasons
}

// GetControllerConfig returns the controller's ConfigMap, or nil when there is
// none. It is read from the namespace the controller reads it from, which is
// not the namespace the test runs in.
func (c *Common) GetControllerConfig() *corev1.ConfigMap {
	configMap, err := c.kubeClient.CoreV1().ConfigMaps(defaults.Namespace()).Get(c.Context, "argo-rollouts-config", metav1.GetOptions{})
	if err != nil {
		if k8errors.IsNotFound(err) {
			return nil
		}
		c.CheckError(err)
	}
	return configMap
}

// PrintRolloutEvents prints all Kubernetes events associated with the given rollout.
func (c *Common) PrintRolloutEvents(ro *v1alpha1.Rollout) {
	opts := metav1.ListOptions{FieldSelector: fields.ParseSelectorOrDie(fmt.Sprintf("involvedObject.uid=%s", ro.UID)).String()}
	c.PrintObjectEvents(opts)
}

// PrintExperimentEvents prints all Kubernetes events associated with the given experiment.
func (c *Common) PrintExperimentEvents(ex *v1alpha1.Experiment) {
	opts := metav1.ListOptions{FieldSelector: fields.ParseSelectorOrDie(fmt.Sprintf("involvedObject.uid=%s", ex.UID)).String()}
	c.PrintObjectEvents(opts)
}

// PrintObjectEvents prints all Kubernetes events associated with the given object.
// Note that events may be deduplicated, or printed out-of-order from when they were emitted,
// so this function should only be used to assist with debugging and not correctness.
func (c *Common) PrintObjectEvents(opts metav1.ListOptions) {
	events, err := c.kubeClient.CoreV1().Events(c.namespace).List(c.Context, opts)
	c.CheckError(err)
	buf := bytes.NewBufferString("")

	w := tabwriter.NewWriter(buf, 0, 0, 4, ' ', 0)
	for _, event := range events.Items {
		timestamp := event.LastTimestamp.Format(time.RFC3339)
		if event.Count > 1 {
			timestamp = fmt.Sprintf("%s (x%d)", timestamp, event.Count)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", timestamp, event.Type, event.Reason, event.Message)
	}
	w.Flush()
	fmt.Fprintln(c.printOut(logrus.StandardLogger().Out), buf.String())
}

// waitForControllerQuiet waits until the in-process controller has made no
// write to the API server in the test's namespace for the given time, a write
// that was made to fail included. It fails the test when the controller does
// not stop writing within E2E_WAIT_TIMEOUT.
func (c *Common) waitForControllerQuiet(quiet time.Duration) {
	c.t.Helper()
	calls := inProcess.calls
	start := time.Now()
	for {
		writes := calls.writesIn(c.namespace)
		if len(writes) == 0 || time.Since(writes[len(writes)-1].Time) >= quiet {
			return
		}
		if time.Since(start) > E2EWaitTimeout {
			c.t.Fatalf("the controller did not stop writing in %s within %v. Its writes so far: %s",
				c.namespace, E2EWaitTimeout, summarizeWrites(calls.writesIn(c.namespace)))
		}
		time.Sleep(200 * time.Millisecond)
	}
}
