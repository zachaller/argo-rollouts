package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	kubetesting "k8s.io/client-go/testing"
)

// newTestAPIServer returns an API server that only knows its version: a request for anything else
// is answered with the status of failingPath when its path ends with failingPath, and with a
// 404 NotFound otherwise.
func newTestAPIServer(t *testing.T, failingPath string, status int) *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/version":
			fmt.Fprint(w, `{"major":"1","minor":"30","gitVersion":"v1.30.0"}`)
		case failingPath != "" && strings.HasSuffix(r.URL.Path, failingPath):
			w.WriteHeader(status)
			fmt.Fprintf(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","code":%d}`, status)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"NotFound","code":404}`)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestNewManagerFromOptions(t *testing.T) {
	for _, analysisOnly := range []bool{false, true} {
		t.Run(fmt.Sprintf("analysisOnly=%v", analysisOnly), func(t *testing.T) {
			server := newTestAPIServer(t, "", 0)
			config := &rest.Config{Host: server.URL}

			opts := DefaultOptions()
			opts.RESTConfig = config
			opts.AnalysisOnly = analysisOnly
			cm, err := NewManagerFromOptions(opts)

			require.NoError(t, err)
			require.NotNil(t, cm)
			assert.Equal(t, analysisOnly, cm.onlyAnalysisMode)
			assert.NotNil(t, cm.analysisController)
			assert.Equal(t, !analysisOnly, cm.rolloutController != nil)
			// The config of the caller is left as it was, without the metrics transport wrapper.
			assert.Equal(t, &rest.Config{Host: server.URL}, config)
		})
	}
}

func TestNewManagerFromOptionsReturnsErrors(t *testing.T) {
	closedServer := newTestAPIServer(t, "", 0)
	closedServer.Close()

	tests := []struct {
		name        string
		restConfig  *rest.Config
		mutate      func(opts *Options)
		expectedErr string
	}{
		{
			name:        "no REST config",
			restConfig:  nil,
			expectedErr: "RESTConfig is required",
		},
		{
			name:        "namespaced mode without a namespace",
			restConfig:  &rest.Config{Host: closedServer.URL},
			mutate:      func(opts *Options) { opts.Namespaced = true },
			expectedErr: "Namespace is required in namespaced mode",
		},
		{
			name:        "instance ID that is not a label value",
			restConfig:  &rest.Config{Host: closedServer.URL},
			mutate:      func(opts *Options) { opts.InstanceID = "not/a/label/value" },
			expectedErr: `invalid instance ID "not/a/label/value"`,
		},
		{
			name:        "API server that cannot be reached",
			restConfig:  &rest.Config{Host: closedServer.URL},
			expectedErr: "connection refused",
		},
		{
			name:        "controller config that cannot be read",
			restConfig:  &rest.Config{Host: newTestAPIServer(t, "/configmaps/argo-rollouts-config", http.StatusInternalServerError).URL},
			expectedErr: "Failed to init config",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := DefaultOptions()
			opts.RESTConfig = tt.restConfig
			if tt.mutate != nil {
				tt.mutate(&opts)
			}
			cm, err := NewManagerFromOptions(opts)

			assert.Nil(t, cm)
			assert.ErrorContains(t, err, tt.expectedErr)
		})
	}
}

// runManager runs the manager, with or without leader election, and returns what Run returned.
func runManager(t *testing.T, ctx context.Context, cm *Manager, electOpts *LeaderElectionOptions) error {
	t.Helper()
	opts := DefaultOptions()
	opts.LeaderElection = *electOpts
	errCh := make(chan error, 1)
	go func() { errCh <- cm.RunWithOptions(ctx, opts) }()
	select {
	case err := <-errCh:
		return err
	case <-time.After(30 * time.Second):
		require.FailNow(t, "Run did not return")
		return nil
	}
}

func TestRunReturnsLeaderElectionErrors(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(electOpts *LeaderElectionOptions)
		expectedErr string
	}{
		{
			name:        "empty namespace",
			mutate:      func(electOpts *LeaderElectionOptions) { electOpts.LeaderElectionNamespace = "" },
			expectedErr: "Error LeaderElectionNamespace is empty",
		},
		{
			name: "renew deadline that is not shorter than the lease duration",
			mutate: func(electOpts *LeaderElectionOptions) {
				electOpts.LeaderElectionRenewDeadline = electOpts.LeaderElectionLeaseDuration
			},
			expectedErr: "leaseDuration must be greater than renewDeadline",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			cm := f.newManager(t)
			electOpts := NewLeaderElectionOptions()
			tt.mutate(electOpts)

			// The context is never cancelled: Run has to return because of the error.
			err := runManager(t, context.Background(), cm, electOpts)

			assert.ErrorContains(t, err, tt.expectedErr)
			// Run shut down the servers that it started.
			assert.Equal(t, http.ErrServerClosed, cm.healthzServer.ListenAndServe())
			assert.Equal(t, http.ErrServerClosed, cm.metricsServer.ListenAndServe())
		})
	}
}

func TestRunReturnsErrorWhenCachesDoNotSync(t *testing.T) {
	for _, leaderElect := range []bool{false, true} {
		t.Run(fmt.Sprintf("leaderElect=%v", leaderElect), func(t *testing.T) {
			f := newFixture(t)
			cm := f.newManager(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			// The rollout cache never syncs, and the context is cancelled as soon as the manager
			// waits for it. The manager then takes a while to notice, which is longer than the
			// leader election takes to stop: Run has to wait for the error.
			var waiting sync.Once
			cm.rolloutSynced = func() bool {
				waiting.Do(func() {
					cancel()
					time.Sleep(200 * time.Millisecond)
				})
				return false
			}
			electOpts := NewLeaderElectionOptions()
			electOpts.LeaderElect = leaderElect

			err := runManager(t, ctx, cm, electOpts)

			assert.ErrorContains(t, err, "failed to wait for caches to sync")
			assert.ErrorIs(t, err, context.Canceled)
			assert.Equal(t, http.ErrServerClosed, cm.healthzServer.ListenAndServe())
			assert.Equal(t, http.ErrServerClosed, cm.metricsServer.ListenAndServe())
		})
	}
}

func TestRunReturnsNilWhenLeaseIsLost(t *testing.T) {
	f := newFixture(t)
	cm := f.newManager(t)
	// The lease cannot be renewed anymore once the caches are synced, which is when the manager
	// starts the controllers.
	var synced atomic.Bool
	f.kubeclient.PrependReactor("update", "leases", func(kubetesting.Action) (bool, runtime.Object, error) {
		if synced.Load() {
			return true, nil, errors.New("the lease cannot be renewed")
		}
		return false, nil, nil
	})
	cm.clusterAnalysisTemplateSynced = func() bool {
		synced.Store(true)
		return true
	}
	electOpts := NewLeaderElectionOptions()
	electOpts.LeaderElectionLeaseDuration = time.Second
	electOpts.LeaderElectionRenewDeadline = 500 * time.Millisecond
	electOpts.LeaderElectionRetryPeriod = 100 * time.Millisecond

	// The context is never cancelled: Run has to return because the lease is lost.
	err := runManager(t, context.Background(), cm, electOpts)

	assert.NoError(t, err)
}
