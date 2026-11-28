package fixtures

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

// newHookedLogger returns a logger like the one the controller logs through,
// with the hook on it.
func newHookedLogger() (*log.Logger, *controllerLogHook) {
	return newHookedLoggerNoting("")
}

// newHookedLoggerNoting is newHookedLogger with a failures file.
func newHookedLoggerNoting(failuresPath string) (*log.Logger, *controllerLogHook) {
	hook := newControllerLogHook(failuresPath)
	logger := log.New()
	logger.SetOutput(io.Discard)
	logger.SetLevel(log.DebugLevel)
	logger.AddHook(hook)
	return logger, hook
}

func TestControllerLogHookStarted(t *testing.T) {
	logger, hook := newHookedLogger()

	logger.Info("Starting Controllers")
	select {
	case <-hook.started:
		t.Fatal("started before the controller said so")
	default:
	}

	logger.Info(controllerStartedMessage)
	logger.Info(controllerStartedMessage) // a second one must not panic
	select {
	case <-hook.started:
	default:
		t.Fatal("not started after the controller said so")
	}
}

func TestControllerLogHookRecoveredPanics(t *testing.T) {
	logger, hook := newHookedLogger()
	assert.Empty(t, hook.recoveredPanics())

	// The line as utils/controller logs it, with the fields of the work item.
	logger.WithField(logutil.NamespaceKey, "ns").WithField("rollout", "ro").
		Errorf("Recovered from panic: %+v\n%s", "boom", "goroutine 1 [running]:")
	logger.Error("rollout syncHandler error: boom")

	panics := hook.recoveredPanics()
	require.Len(t, panics, 1)
	assert.Contains(t, panics[0], "boom")
	assert.Contains(t, panics[0], "goroutine 1")
}

func TestControllerLogHookCapture(t *testing.T) {
	logger, hook := newHookedLogger()
	inNamespace := func(namespace string) *log.Entry {
		return logger.WithField(logutil.NamespaceKey, namespace).WithField("rollout", "ro")
	}

	inNamespace("a").Info("before the capture")
	hook.capture("a")
	inNamespace("a").Info("kept")
	inNamespace("a").Debug("debug lines are not kept")
	inNamespace("a").Warn("warnings are kept")
	inNamespace("b").Info("another namespace")
	logger.Info("no namespace")

	lines := hook.release("a")
	require.Len(t, lines, 2)
	assert.Regexp(t, `^\d\d:\d\d:\d\d\.\d{3} info  kept rollout=ro$`, lines[0])
	assert.Regexp(t, `warning warnings are kept rollout=ro$`, lines[1])

	// Released: nothing is kept any more, and nothing is left behind.
	inNamespace("a").Info("after the release")
	assert.Empty(t, hook.release("a"))
	assert.Empty(t, hook.byNamespace)
}

func TestControllerLogHookKeepsTheLatestLines(t *testing.T) {
	logger, hook := newHookedLogger()
	hook.capture("a")
	for i := 0; i < capturedLogLines+10; i++ {
		logger.WithField(logutil.NamespaceKey, "a").Info(fmt.Sprintf("line %d", i))
	}

	lines := hook.release("a")
	require.Len(t, lines, capturedLogLines)
	assert.Contains(t, lines[0], "line 10")
	assert.Contains(t, lines[len(lines)-1], fmt.Sprintf("line %d", capturedLogLines+9))
}

func TestControllerLogHookNotesFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "failures.txt")
	logger, hook := newHookedLoggerNoting(path)

	logger.Info("nothing wrong")
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "no failure, no file")

	logger.WithField(logutil.NamespaceKey, "ns").WithField("rollout", "ro").
		Errorf("Recovered from panic: %+v\n%s", "boom", "goroutine 1 [running]:")
	hook.noteFailure("the controller did not stop within 1m0s")

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	require.Len(t, lines, 2, "one line each, without the stack")
	assert.Regexp(t, `^\S+ test process \d+: the controller recovered from a panic in namespace "ns": Recovered from panic: boom$`, lines[0])
	assert.Contains(t, lines[1], "the controller did not stop within 1m0s")

	// Without a file nothing is written anywhere.
	_, plain := newHookedLogger()
	plain.noteFailure("ignored")
}
