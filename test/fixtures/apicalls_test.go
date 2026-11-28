package fixtures

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func TestParseAPICall(t *testing.T) {
	tests := []struct {
		method string
		url    string
		want   APICall
	}{
		{"GET", "/api/v1/namespaces/ns/services/svc", APICall{Verb: "get", Resource: "services", Namespace: "ns", Name: "svc"}},
		{"GET", "/api/v1/namespaces/ns/services", APICall{Verb: "list", Resource: "services", Namespace: "ns"}},
		{"GET", "/api/v1/namespaces/ns/services?watch=true&resourceVersion=5", APICall{Verb: "watch", Resource: "services", Namespace: "ns"}},
		{"GET", "/api/v1/services?watch=1", APICall{Verb: "watch", Resource: "services"}},
		{"POST", "/api/v1/namespaces/ns/events", APICall{Verb: "create", Resource: "events", Namespace: "ns"}},
		{"PUT", "/apis/apps/v1/namespaces/ns/replicasets/rs", APICall{Verb: "update", Group: "apps", Resource: "replicasets", Namespace: "ns", Name: "rs"}},
		{"PUT", "/apis/apps/v1/namespaces/ns/replicasets/rs/scale", APICall{Verb: "update", Group: "apps", Resource: "replicasets", Namespace: "ns", Name: "rs", Subresource: "scale"}},
		{"PATCH", "/apis/argoproj.io/v1alpha1/namespaces/ns/rollouts/ro/status", APICall{Verb: "patch", Group: "argoproj.io", Resource: "rollouts", Namespace: "ns", Name: "ro", Subresource: "status"}},
		{"PUT", "/apis/networking.istio.io/v1alpha3/namespaces/ns/destinationrules/dr", APICall{Verb: "update", Group: "networking.istio.io", Resource: "destinationrules", Namespace: "ns", Name: "dr"}},
		{"DELETE", "/api/v1/namespaces/ns/pods/pod", APICall{Verb: "delete", Resource: "pods", Namespace: "ns", Name: "pod"}},
		{"DELETE", "/api/v1/namespaces/ns/pods", APICall{Verb: "deletecollection", Resource: "pods", Namespace: "ns"}},
		// Cluster-scoped, and the Namespace itself.
		{"GET", "/apis/argoproj.io/v1alpha1/clusteranalysistemplates/t", APICall{Verb: "get", Group: "argoproj.io", Resource: "clusteranalysistemplates", Name: "t"}},
		{"GET", "/api/v1/namespaces/ns", APICall{Verb: "get", Resource: "namespaces", Name: "ns"}},
		{"PUT", "/api/v1/namespaces/ns/finalize", APICall{Verb: "update", Resource: "namespaces", Name: "ns", Subresource: "finalize"}},
		{"GET", "/api/v1/namespaces", APICall{Verb: "list", Resource: "namespaces"}},
	}
	for _, tt := range tests {
		u, err := url.Parse("https://127.0.0.1:6443" + tt.url)
		require.NoError(t, err)
		got, ok := parseAPICall(tt.method, u)
		assert.True(t, ok, "%s %s", tt.method, tt.url)
		assert.Equal(t, tt.want, got, "%s %s", tt.method, tt.url)
	}

	for _, notAResource := range []string{"/version", "/healthz", "/api", "/api/v1", "/apis", "/apis/apps", "/apis/apps/v1", "/openapi/v2"} {
		u, err := url.Parse("https://127.0.0.1:6443" + notAResource)
		require.NoError(t, err)
		_, ok := parseAPICall("GET", u)
		assert.False(t, ok, notAResource)
	}
}

func TestFaultValidate(t *testing.T) {
	valid := Fault{Verb: "update", Resource: "destinationrules", Status: http.StatusConflict}
	assert.NoError(t, valid.validate())

	for name, change := range map[string]func(*Fault){
		"no verb":         func(f *Fault) { f.Verb = "" },
		"not a verb":      func(f *Fault) { f.Verb = "PUT" },
		"no resource":     func(f *Fault) { f.Resource = "" },
		"no status":       func(f *Fault) { f.Status = 0 },
		"a success":       func(f *Fault) { f.Status = http.StatusOK },
		"negative number": func(f *Fault) { f.Times = -1 },
	} {
		f := valid
		change(&f)
		assert.Error(t, f.validate(), name)
	}
}

func TestFaultMatchingAndCounting(t *testing.T) {
	calls := newAPICalls()
	update := APICall{Verb: "update", Resource: "destinationrules", Namespace: "a", Name: "rule"}

	twice := calls.inject("a", Fault{Verb: "update", Resource: "destinationrules", Status: http.StatusConflict, Times: 2})

	// Other namespaces, verbs, resources and subresources are left alone.
	for _, other := range []APICall{
		{Verb: "update", Resource: "destinationrules", Namespace: "b", Name: "rule"},
		{Verb: "patch", Resource: "destinationrules", Namespace: "a", Name: "rule"},
		{Verb: "update", Resource: "virtualservices", Namespace: "a", Name: "rule"},
		{Verb: "update", Resource: "destinationrules", Namespace: "a", Name: "rule", Subresource: "status"},
	} {
		_, fail := calls.faultFor(other)
		assert.False(t, fail, "%+v", other)
	}
	assert.Equal(t, 0, twice.Hits())

	for i := 1; i <= 2; i++ {
		status, fail := calls.faultFor(update)
		assert.True(t, fail)
		assert.Equal(t, http.StatusConflict, status)
		assert.Equal(t, i, twice.Hits())
	}
	_, fail := calls.faultFor(update)
	assert.False(t, fail, "the fault is used up")
	assert.Equal(t, 2, twice.Hits())

	// By name, without a limit, until it is removed.
	byName := calls.inject("a", Fault{Verb: "update", Resource: "destinationrules", Name: "other", Status: http.StatusInternalServerError})
	_, fail = calls.faultFor(update)
	assert.False(t, fail)
	other := update
	other.Name = "other"
	for i := 0; i < 5; i++ {
		status, fail := calls.faultFor(other)
		assert.True(t, fail)
		assert.Equal(t, http.StatusInternalServerError, status)
	}
	byName.Remove()
	_, fail = calls.faultFor(other)
	assert.False(t, fail, "the fault is removed")
	assert.Equal(t, 5, byName.Hits())

	// Faults end with the namespace.
	forever := calls.inject("a", Fault{Verb: "update", Resource: "destinationrules", Status: http.StatusConflict})
	calls.forget("a")
	_, fail = calls.faultFor(update)
	assert.False(t, fail)
	forever.Remove() // must not panic
}

// TestAPICallTransport runs real clients through the transport, against a
// server that answers everything with an empty object.
func TestAPICallTransport(t *testing.T) {
	var served atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"kind":"ConfigMap","apiVersion":"v1","metadata":{"name":"cm","namespace":"a"}}`))
	}))
	defer server.Close()

	calls := newAPICalls()
	config := &rest.Config{Host: server.URL}
	config.Wrap(calls.wrap)
	kube, err := kubernetes.NewForConfig(config)
	require.NoError(t, err)
	dyn, err := dynamic.NewForConfig(config)
	require.NoError(t, err)
	ctx := context.Background()
	configMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: "a"}}

	// Nothing is recorded for a namespace until it is watched.
	_, err = kube.CoreV1().ConfigMaps("a").Update(ctx, configMap, metav1.UpdateOptions{})
	require.NoError(t, err)
	assert.Empty(t, calls.writesIn("a"))

	calls.watch("a")
	_, err = kube.CoreV1().ConfigMaps("a").Get(ctx, "cm", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Empty(t, calls.writesIn("a"), "a read is not a write")

	_, err = kube.CoreV1().ConfigMaps("a").Update(ctx, configMap, metav1.UpdateOptions{})
	require.NoError(t, err)
	_, err = kube.CoreV1().ConfigMaps("b").Update(ctx, configMap, metav1.UpdateOptions{})
	require.NoError(t, err)
	writes := calls.writesIn("a")
	require.Len(t, writes, 1)
	assert.Equal(t, "update", writes[0].Verb)
	assert.Equal(t, "configmaps", writes[0].Resource)
	assert.Equal(t, "cm", writes[0].Name)
	assert.Equal(t, http.StatusOK, writes[0].Status)
	assert.False(t, writes[0].Injected)
	assert.Empty(t, calls.writesIn("b"))

	// A fault: the typed and the dynamic client both get the error the API
	// server would have given, and the server is not asked.
	fault := calls.inject("a", Fault{Verb: "update", Resource: "configmaps", Status: http.StatusConflict, Times: 2})
	before := served.Load()

	_, err = kube.CoreV1().ConfigMaps("a").Update(ctx, configMap, metav1.UpdateOptions{})
	require.Error(t, err)
	assert.True(t, apierrors.IsConflict(err), "%v", err)
	assert.Contains(t, err.Error(), "injected by the e2e test")

	gvr := schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
	un := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "cm", "namespace": "a"},
	}}
	_, err = dyn.Resource(gvr).Namespace("a").Update(ctx, un, metav1.UpdateOptions{})
	require.Error(t, err)
	assert.True(t, apierrors.IsConflict(err), "%v", err)

	assert.Equal(t, before, served.Load(), "an injected failure does not reach the server")
	assert.Equal(t, 2, fault.Hits())

	// Used up: the next call goes through.
	_, err = kube.CoreV1().ConfigMaps("a").Update(ctx, configMap, metav1.UpdateOptions{})
	require.NoError(t, err)

	writes = calls.writesIn("a")
	require.Len(t, writes, 4)
	assert.True(t, writes[1].Injected)
	assert.Equal(t, http.StatusConflict, writes[1].Status)
	assert.True(t, writes[2].Injected)
	assert.False(t, writes[3].Injected)
	assert.Regexp(t, `^\d\d:\d\d:\d\d\.\d{3} update configmaps/cm -> 409 \(injected\)$`, writes[1].String())

	// Other statuses come out as the matching errors too.
	calls.inject("a", Fault{Verb: "get", Resource: "configmaps", Status: http.StatusInternalServerError, Times: 1})
	_, err = kube.CoreV1().ConfigMaps("a").Get(ctx, "cm", metav1.GetOptions{})
	assert.True(t, apierrors.IsInternalError(err), "%v", err)
	calls.inject("a", Fault{Verb: "get", Resource: "configmaps", Status: http.StatusNotFound, Times: 1})
	_, err = kube.CoreV1().ConfigMaps("a").Get(ctx, "cm", metav1.GetOptions{})
	assert.True(t, apierrors.IsNotFound(err), "%v", err)

	calls.forget("a")
	assert.Empty(t, calls.writesIn("a"))
}

func TestSummarizeWrites(t *testing.T) {
	assert.Equal(t, "none", summarizeWrites(nil))

	writes := []APICall{
		{Verb: "patch", Resource: "rollouts"},
		{Verb: "update", Resource: "replicasets"},
		{Verb: "patch", Resource: "rollouts"},
		{Verb: "patch", Resource: "rollouts", Subresource: "status"},
	}
	assert.Equal(t, "4 (patch rollouts x2, patch rollouts/status x1, update replicasets x1)", summarizeWrites(writes))
}
