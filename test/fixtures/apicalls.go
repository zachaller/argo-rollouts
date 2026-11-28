package fixtures

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// APICall is one request of the controller to the API server about an object,
// or a kind of object, in a namespace.
type APICall struct {
	// Verb is get, list, watch, create, update, patch, delete or
	// deletecollection.
	Verb string
	// Group is the API group of the resource. It is empty for the core group.
	Group string
	// Resource is the plural name of the resource, such as "replicasets".
	Resource string
	// Subresource is, for example, "status". It is empty for the object itself.
	Subresource string
	Namespace   string
	// Name is empty for list, watch, create and deletecollection.
	Name string
	// Status is the HTTP status of the answer, or zero when there was none.
	Status int
	// Injected says that the answer was made up by a Fault: the request did
	// not reach the API server.
	Injected bool
	Time     time.Time
}

// IsWrite reports whether the call asks the API server to change something.
func (c APICall) IsWrite() bool {
	switch c.Verb {
	case "create", "update", "patch", "delete", "deletecollection":
		return true
	}
	return false
}

func (c APICall) String() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s %s %s", c.Time.UTC().Format("15:04:05.000"), c.Verb, c.Resource)
	if c.Group != "" {
		fmt.Fprintf(&sb, ".%s", c.Group)
	}
	if c.Name != "" {
		fmt.Fprintf(&sb, "/%s", c.Name)
	}
	if c.Subresource != "" {
		fmt.Fprintf(&sb, "/%s", c.Subresource)
	}
	fmt.Fprintf(&sb, " -> %d", c.Status)
	if c.Injected {
		sb.WriteString(" (injected)")
	}
	return sb.String()
}

// parseAPICall reads the verb and the object out of a request to the API
// server. It returns false for a request that is not about a resource, such as
// discovery or /version.
func parseAPICall(method string, u *url.URL) (APICall, bool) {
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	var call APICall
	switch {
	case len(parts) > 2 && parts[0] == "api":
		// /api/v1/...
		parts = parts[2:]
	case len(parts) > 3 && parts[0] == "apis":
		// /apis/<group>/<version>/...
		call.Group = parts[1]
		parts = parts[3:]
	default:
		return APICall{}, false
	}

	// What is left is [namespaces/<namespace>/]<resource>[/<name>[/<subresource>]].
	// A Namespace itself has the same beginning: namespaces/<name>[/status].
	if len(parts) >= 3 && parts[0] == "namespaces" && parts[2] != "status" && parts[2] != "finalize" {
		call.Namespace = parts[1]
		parts = parts[2:]
	}
	call.Resource = parts[0]
	if len(parts) > 1 {
		call.Name = parts[1]
	}
	if len(parts) > 2 {
		call.Subresource = strings.Join(parts[2:], "/")
	}

	switch method {
	case http.MethodGet:
		switch {
		case call.Name != "":
			call.Verb = "get"
		case u.Query().Get("watch") == "true" || u.Query().Get("watch") == "1":
			call.Verb = "watch"
		default:
			call.Verb = "list"
		}
	case http.MethodPost:
		call.Verb = "create"
	case http.MethodPut:
		call.Verb = "update"
	case http.MethodPatch:
		call.Verb = "patch"
	case http.MethodDelete:
		if call.Name == "" {
			call.Verb = "deletecollection"
		} else {
			call.Verb = "delete"
		}
	default:
		return APICall{}, false
	}
	return call, true
}

// Fault describes calls of the controller to the API server that fail instead
// of reaching it. A fault only ever applies to the namespace of the test that
// injected it, so tests running at the same time do not see it.
type Fault struct {
	// Verb is the verb of the calls: get, list, watch, create, update, patch,
	// delete or deletecollection. Required.
	Verb string
	// Resource is the plural name of the resource, such as "destinationrules".
	// Required.
	Resource string
	// Subresource is the subresource of the calls, such as "status". Empty
	// means calls for the object itself.
	Subresource string
	// Name is the name of the object. Empty means every object.
	Name string
	// Status is the HTTP status the controller gets, such as
	// http.StatusConflict. Required.
	Status int
	// Times is how many calls fail. Zero means every call, until the fault is
	// removed or the test ends.
	Times int
}

func (f Fault) validate() error {
	switch f.Verb {
	case "get", "list", "watch", "create", "update", "patch", "delete", "deletecollection":
	default:
		return fmt.Errorf("fault: %q is not a verb", f.Verb)
	}
	if f.Resource == "" {
		return fmt.Errorf("fault: the resource is required")
	}
	if f.Status < 400 || f.Status > 599 {
		return fmt.Errorf("fault: %d is not an error status", f.Status)
	}
	if f.Times < 0 {
		return fmt.Errorf("fault: times is negative")
	}
	return nil
}

func (f Fault) matches(call APICall) bool {
	return f.Verb == call.Verb &&
		f.Resource == call.Resource &&
		f.Subresource == call.Subresource &&
		(f.Name == "" || f.Name == call.Name)
}

// InjectedFault is a Fault that is in effect.
type InjectedFault struct {
	fault     Fault
	namespace string
	calls     *apiCalls
	hits      int // guarded by calls.mu
}

// Hits returns how many calls the fault has made fail so far. A test should
// assert on it: a fault that was never hit has tested nothing.
func (f *InjectedFault) Hits() int {
	f.calls.mu.Lock()
	defer f.calls.mu.Unlock()
	return f.hits
}

// Remove ends the fault. Faults also end with the test that injected them.
func (f *InjectedFault) Remove() {
	f.calls.mu.Lock()
	defer f.calls.mu.Unlock()
	faults := f.calls.faults[f.namespace]
	for i, other := range faults {
		if other == f {
			f.calls.faults[f.namespace] = append(faults[:i:i], faults[i+1:]...)
			return
		}
	}
}

// apiCalls sits in the transport of the in-process controller's clients. It
// records what the controller writes in the namespaces of running tests, and
// makes the calls fail that a test injected a fault for.
type apiCalls struct {
	mu     sync.Mutex
	writes map[string][]APICall
	faults map[string][]*InjectedFault
}

func newAPICalls() *apiCalls {
	return &apiCalls{
		writes: map[string][]APICall{},
		faults: map[string][]*InjectedFault{},
	}
}

// watch starts recording the controller's writes in a namespace.
func (a *apiCalls) watch(namespace string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.writes[namespace] = []APICall{}
}

// forget drops what was recorded for a namespace, and its faults.
func (a *apiCalls) forget(namespace string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.writes, namespace)
	delete(a.faults, namespace)
}

// writesIn returns a copy of the writes recorded for a namespace so far.
func (a *apiCalls) writesIn(namespace string) []APICall {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]APICall(nil), a.writes[namespace]...)
}

func (a *apiCalls) inject(namespace string, fault Fault) *InjectedFault {
	a.mu.Lock()
	defer a.mu.Unlock()
	injected := &InjectedFault{fault: fault, namespace: namespace, calls: a}
	a.faults[namespace] = append(a.faults[namespace], injected)
	return injected
}

// faultFor returns the status a call has to fail with, if a fault says so,
// and counts the call against that fault.
func (a *apiCalls) faultFor(call APICall) (int, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, injected := range a.faults[call.Namespace] {
		if !injected.fault.matches(call) {
			continue
		}
		if injected.fault.Times > 0 && injected.hits >= injected.fault.Times {
			continue
		}
		injected.hits++
		return injected.fault.Status, true
	}
	return 0, false
}

func (a *apiCalls) record(call APICall) {
	if !call.IsWrite() {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if writes, watched := a.writes[call.Namespace]; watched {
		a.writes[call.Namespace] = append(writes, call)
	}
}

// wrap is a transport wrapper for rest.Config.Wrap.
func (a *apiCalls) wrap(next http.RoundTripper) http.RoundTripper {
	return &apiCallTransport{next: next, calls: a}
}

type apiCallTransport struct {
	next  http.RoundTripper
	calls *apiCalls
}

func (t *apiCallTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	call, ok := parseAPICall(req.Method, req.URL)
	if !ok || call.Namespace == "" {
		return t.next.RoundTrip(req)
	}
	call.Time = time.Now()

	if status, fail := t.calls.faultFor(call); fail {
		// A RoundTripper closes the body of a request it does not send.
		if req.Body != nil {
			req.Body.Close()
		}
		call.Status = status
		call.Injected = true
		t.calls.record(call)
		return faultResponse(req, call), nil
	}

	resp, err := t.next.RoundTrip(req)
	if resp != nil {
		call.Status = resp.StatusCode
	}
	t.calls.record(call)
	return resp, err
}

// WrappedRoundTripper lets client-go reach the transport underneath, which it
// does to cancel requests.
func (t *apiCallTransport) WrappedRoundTripper() http.RoundTripper {
	return t.next
}

// faultResponse builds the answer the API server gives with that status: a
// Status object, which the clients turn into the matching error, so that
// apierrors.IsConflict and the like hold for it.
func faultResponse(req *http.Request, call APICall) *http.Response {
	// NewGenericServerResponse knows the reason that goes with each status.
	statusErr := apierrors.NewGenericServerResponse(call.Status, call.Verb,
		schema.GroupResource{Group: call.Group, Resource: call.Resource}, call.Name, "", 0, false)
	status := statusErr.Status()
	status.Kind = "Status"
	status.APIVersion = "v1"
	// The message is what the controller logs: say where the failure is from.
	status.Message = fmt.Sprintf("injected by the e2e test: %s %s %q fails with %d", call.Verb, call.Resource, call.Name, call.Status)
	body, _ := json.Marshal(status)
	return &http.Response{
		Status:        fmt.Sprintf("%d %s", call.Status, http.StatusText(call.Status)),
		StatusCode:    call.Status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        http.Header{"Content-Type": []string{"application/json"}},
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       req,
	}
}
