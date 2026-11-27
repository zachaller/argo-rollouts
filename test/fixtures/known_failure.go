package fixtures

import (
	"flag"
	"os"
	"strings"
	"testing"
)

// EnvVarE2ERunKnownFailures makes tests marked with KnownFailure run when set to "true".
const EnvVarE2ERunKnownFailures = "E2E_RUN_KNOWN_FAILURES"

// KnownFailure skips a test that fails because of a known defect in the code
// under test, so that it does not block CI. Call it first in the test, and give
// the reason and where the defect is tracked.
//
// The test still runs when it is asked for: by name with -run, or with
// E2E_RUN_KNOWN_FAILURES=true. Remove the call once the defect is fixed.
func KnownFailure(t *testing.T, reason string) {
	t.Helper()
	if os.Getenv(EnvVarE2ERunKnownFailures) == "true" {
		return
	}
	if f := flag.Lookup("test.run"); f != nil && runPatternNames(f.Value.String(), t.Name()) {
		return
	}
	t.Skipf("known failure, skipped by default: %s", reason)
}

// runPatternNames reports whether a -run pattern asks for the test by name.
// An empty pattern, or one that merely matches the test along with others
// (such as "Test" or "TestCanary"), does not count.
func runPatternNames(pattern, testName string) bool {
	for _, alternative := range strings.Split(pattern, "|") {
		// "^(TestX)$", "^TestX$" and "TestX" all name TestX.
		if strings.Trim(alternative, "^$()") == testName {
			return true
		}
	}
	return false
}
