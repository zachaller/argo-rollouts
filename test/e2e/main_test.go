//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/argoproj/argo-rollouts/test/fixtures"
)

func TestMain(m *testing.M) {
	// Namespaces of tests that did not get to clean up (a crash, a timeout, or
	// E2E_DEBUG=true) would keep their rollouts reconciling during this run.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	err := fixtures.SweepTestNamespaces(ctx)
	cancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e setup: sweeping stale test namespaces: %v\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}
