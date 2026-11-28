//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/argoproj/argo-rollouts/test/fixtures"
)

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	inProcess, err := fixtures.ControllerInProcess()
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e setup: %v\n", err)
		return 1
	}

	// Namespaces of tests that did not get to clean up (a crash, a timeout, or
	// E2E_DEBUG=true) would keep their rollouts reconciling during this run.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	err = fixtures.SweepTestNamespaces(ctx)
	cancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e setup: sweeping stale test namespaces: %v\n", err)
		return 1
	}
	if !inProcess {
		return m.Run()
	}

	// This process runs the controller as well. The controller looks for its
	// plugins relative to the working directory, as `make start-e2e` does
	// from the repository root; the tests read their manifests relative to
	// this directory.
	testDir, err := os.Getwd()
	if err == nil {
		fixtures.SetManifestRoot(testDir)
		err = os.Chdir(filepath.Join(testDir, "..", ".."))
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e setup: %v\n", err)
		return 1
	}
	ctrl, err := fixtures.StartController()
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e setup: %v\n", err)
		return 1
	}
	code := m.Run()
	if err := ctrl.Stop(); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	return code
}
