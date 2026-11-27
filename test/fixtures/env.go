package fixtures

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
	"unicode"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	// AnnotationKeyTestName holds the full test name on a test namespace. The
	// label of the same purpose is truncated to fit a label value.
	AnnotationKeyTestName = "e2e-test-name"

	namespacePrefix = "e2e-"
	// A namespace name is at most 63 characters. GenerateName appends 5, and
	// the prefix and a separator take 5 more.
	namespaceNameBudget = 50
)

// Test is the environment of one e2e test written as a plain test function.
// It owns a namespace that no other test uses, so tests can run in parallel.
// Create it with New.
type Test struct {
	Common

	IstioEnabled   bool
	SMIEnabled     bool
	AppMeshEnabled bool
	ApisixEnabled  bool
}

type testOptions struct {
	serialReason string
}

// Option configures New.
type Option func(*testOptions)

// Serial opts a test out of running in parallel with other tests. It needs a
// reason, because every serial test adds its full duration to the run.
func Serial(reason string) Option {
	return func(o *testOptions) {
		o.serialReason = reason
	}
}

// New sets up the environment for one test: it marks the test parallel, creates
// a namespace for it and registers the cleanup. A test should call it first.
//
// When the test fails, the rollouts and experiments in its namespace are
// printed before the namespace is deleted. Set E2E_DEBUG=true to keep the
// namespace.
func New(t *testing.T, opts ...Option) *Test {
	t.Helper()
	var o testOptions
	for _, opt := range opts {
		opt(&o)
	}
	if o.serialReason == "" {
		t.Parallel()
	}

	sh, err := getShared()
	if err != nil {
		t.Fatalf("e2e setup: %v", err)
	}

	logger := log.New()
	logger.SetOutput(t.Output())
	logger.SetLevel(log.StandardLogger().GetLevel())
	logger.SetFormatter(log.StandardLogger().Formatter)

	e := &Test{
		Common: Common{
			Context:        t.Context(),
			t:              t,
			out:            t.Output(),
			kubernetesHost: sh.kubernetesHost,
			kubeClient:     sh.kubeClient,
			dynamicClient:  sh.dynamicClient,
			rolloutClient:  sh.rolloutClient,
			smiClient:      sh.smiClient,
		},
		IstioEnabled:   sh.istioEnabled,
		SMIEnabled:     sh.smiEnabled,
		AppMeshEnabled: sh.appMeshEnabled,
		ApisixEnabled:  sh.apisixEnabled,
	}
	e.namespace = createTestNamespace(t, sh)
	e.log = log.NewEntry(logger).WithField("namespace", e.namespace)
	if o.serialReason != "" {
		e.log.Infof("Running serially: %s", o.serialReason)
	}

	// Cleanups run last-registered first: the failure dump, then the deletion.
	t.Cleanup(e.deleteNamespace)
	t.Cleanup(e.dumpOnFailure)
	return e
}

// T returns the test this environment belongs to.
func (e *Test) T() *testing.T {
	return e.t
}

// Namespace returns the namespace owned by this test.
func (e *Test) Namespace() string {
	return e.namespace
}

// Given starts a scenario. Every call starts from a fresh copy of the state,
// so one test can set up more than one rollout.
func (e *Test) Given() *Given {
	c := e.Common
	return &Given{
		Common: &c,
	}
}

func createTestNamespace(t *testing.T, sh *sharedEnv) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), E2EWaitTimeout)
	defer cancel()

	ns, err := sh.kubeClient.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: namespacePrefix + dnsLabel(t.Name(), namespaceNameBudget) + "-",
			Labels:       map[string]string{E2ELabelKeyTestName: labelValue(t.Name())},
			Annotations:  map[string]string{AnnotationKeyTestName: t.Name()},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("e2e setup: creating namespace: %v", err)
	}

	// Pods are rejected until the namespace has its default ServiceAccount.
	err = wait.PollUntilContextCancel(ctx, 100*time.Millisecond, true, func(ctx context.Context) (bool, error) {
		_, err := sh.kubeClient.CoreV1().ServiceAccounts(ns.Name).Get(ctx, "default", metav1.GetOptions{})
		if k8serrors.IsNotFound(err) {
			return false, nil
		}
		return err == nil, err
	})
	if err != nil {
		t.Fatalf("e2e setup: waiting for the default ServiceAccount in %s: %v", ns.Name, err)
	}
	return ns.Name
}

// dumpOnFailure prints the rollouts and experiments of a failed test. It
// writes to the test's own output, so the dump stays with the test that
// failed when tests run in parallel.
func (e *Test) dumpOnFailure() {
	if !e.t.Failed() {
		return
	}

	// The test's own context is cancelled by the time cleanups run.
	ctx, cancel := context.WithTimeout(context.Background(), E2EWaitTimeout)
	defer cancel()
	c := e.Common
	c.Context = ctx

	out := c.out
	fmt.Fprintf(out, "\n=== FAILED %s (namespace %s)\n", e.t.Name(), e.namespace)
	roList, err := c.rolloutClient.ArgoprojV1alpha1().Rollouts(c.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(out, "could not list rollouts: %v\n", err)
	} else {
		for _, ro := range roList.Items {
			c.PrintRollout(ro.Name)
			c.PrintRolloutYAML(&ro)
			c.PrintRolloutEvents(&ro)
		}
	}
	exList, err := c.rolloutClient.ArgoprojV1alpha1().Experiments(c.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(out, "could not list experiments: %v\n", err)
	} else {
		for _, ex := range exList.Items {
			c.PrintExperiment(ex.Name)
			c.PrintExperimentYAML(&ex)
			c.PrintExperimentEvents(&ex)
		}
	}
	fmt.Fprintf(out, "=== END %s\n\n", e.t.Name())
}

// DeleteResources deletes every object the test has applied so far and waits
// until they are gone. A test that applies several sets of manifests one
// after another calls it between them. With E2E_DEBUG=true it does nothing,
// like the namespace deletion at the end of the test.
func (e *Test) DeleteResources() {
	if os.Getenv(EnvVarE2EDebug) == "true" {
		e.log.Info("skipping resource cleanup")
		return
	}
	// The label value applyObject gives the objects of a top-level test.
	// Matching it, and not just the label, keeps this off the cluster-scoped
	// objects of tests that run at the same time.
	req, err := labels.NewRequirement(E2ELabelKeyTestName, selection.Equals, []string{e.t.Name()})
	e.CheckError(err)
	e.deleteResources(req, metav1.DeletePropagationBackground)
}

// deleteNamespace removes everything the test created. It does not wait for
// the deletion to finish.
func (e *Test) deleteNamespace() {
	if os.Getenv(EnvVarE2EDebug) == "true" {
		e.log.Infof("E2E_DEBUG is set: keeping namespace %s", e.namespace)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), E2EWaitTimeout)
	defer cancel()

	// Delete the rollouts and experiments first so the controller stops
	// creating objects in a namespace that is going away.
	background := metav1.DeletePropagationBackground
	deleteOpts := metav1.DeleteOptions{PropagationPolicy: &background}
	if err := e.rolloutClient.ArgoprojV1alpha1().Rollouts(e.namespace).DeleteCollection(ctx, deleteOpts, metav1.ListOptions{}); err != nil {
		e.log.Warnf("could not delete rollouts in %s: %v", e.namespace, err)
	}
	if err := e.rolloutClient.ArgoprojV1alpha1().Experiments(e.namespace).DeleteCollection(ctx, deleteOpts, metav1.ListOptions{}); err != nil {
		e.log.Warnf("could not delete experiments in %s: %v", e.namespace, err)
	}
	if err := e.kubeClient.CoreV1().Namespaces().Delete(ctx, e.namespace, deleteOpts); err != nil && !k8serrors.IsNotFound(err) {
		e.t.Errorf("could not delete namespace %s: %v", e.namespace, err)
	}
}

// SweepTestNamespaces deletes the namespaces earlier runs left behind, for
// example after a crash or with E2E_DEBUG=true. It does not wait for the
// deletions to finish. Call it once before the tests start.
func SweepTestNamespaces(ctx context.Context) error {
	sh, err := getShared()
	if err != nil {
		return err
	}
	list, err := sh.kubeClient.CoreV1().Namespaces().List(ctx, metav1.ListOptions{LabelSelector: E2ELabelKeyTestName})
	if err != nil {
		return err
	}
	background := metav1.DeletePropagationBackground
	for _, ns := range list.Items {
		if ns.DeletionTimestamp != nil || !strings.HasPrefix(ns.Name, namespacePrefix) {
			continue
		}
		err := sh.kubeClient.CoreV1().Namespaces().Delete(ctx, ns.Name, metav1.DeleteOptions{PropagationPolicy: &background})
		if err != nil && !k8serrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

// dnsLabel turns a test name into a lowercase DNS label fragment of at most
// max characters: "TestCanary/With Pause" becomes "canary-with-pause".
func dnsLabel(name string, max int) string {
	name = strings.TrimPrefix(name, "Test")
	var sb strings.Builder
	dash := true // suppress a leading dash
	for _, r := range strings.ToLower(name) {
		if r < unicode.MaxASCII && (unicode.IsLower(r) || unicode.IsDigit(r)) {
			sb.WriteRune(r)
			dash = false
		} else if !dash {
			sb.WriteByte('-')
			dash = true
		}
	}
	out := sb.String()
	if len(out) > max {
		out = out[:max]
	}
	out = strings.Trim(out, "-")
	if out == "" {
		out = "test"
	}
	return out
}

// labelValue makes a test name usable as a label value: at most 63 characters
// of letters, digits, '-', '_' and '.', starting and ending with a letter or digit.
func labelValue(name string) string {
	var sb strings.Builder
	for _, r := range name {
		switch {
		case r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)), r == '-', r == '_', r == '.':
			sb.WriteRune(r)
		default:
			sb.WriteByte('_')
		}
	}
	out := sb.String()
	if len(out) > 63 {
		out = out[:63]
	}
	return strings.Trim(out, "-_.")
}
