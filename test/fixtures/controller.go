package fixtures

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/argoproj/argo-rollouts/controller"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

const (
	// E2E_CONTROLLER says where the controller under test runs. "inprocess",
	// the default, makes the test binary run it. "external" means a
	// controller that was started separately, with `make start-e2e`. "off"
	// means there is none, on purpose.
	EnvVarE2EController = "E2E_CONTROLLER"
	// E2E_CONTROLLER_LOG is the file an in-process controller appends its log
	// to. One run of the tests can be several test processes, when failed tests
	// are rerun, and each of them runs a controller. Without it every process
	// writes a file of its own in the temporary directory.
	EnvVarE2EControllerLog = "E2E_CONTROLLER_LOG"
	// E2E_CONTROLLER_FAILURES is a file an in-process controller appends a
	// line to when it recovers from a panic or does not stop cleanly. The
	// test process that sees such a thing fails, but that is not enough: when
	// failed tests are rerun, the reruns are processes of their own, they
	// pass, and the run as a whole passes with them. `make test-e2e` sets
	// this, and fails when the file is not empty after all the reruns.
	EnvVarE2EControllerFailures = "E2E_CONTROLLER_FAILURES"

	controllerInProcess = "inprocess"
	controllerExternal  = "external"
	controllerOff       = "off"
	// controllerStartedMessage is what the manager logs once its caches are
	// synced and its workers are running.
	controllerStartedMessage = "Started controller"

	// controllerStartTimeout is how long an in-process controller gets to sync
	// its caches and start its workers.
	controllerStartTimeout = 2 * time.Minute
	// controllerStopTimeout is how long it gets to shut down.
	controllerStopTimeout = time.Minute
	// capturedLogLines is how many of the controller's log lines are kept for
	// the failure dump of one test.
	capturedLogLines = 200
)

// testLogLevel is the level of the per-test loggers. It is read before an
// in-process controller raises the level of the shared logger for itself.
var testLogLevel = log.GetLevel()

// inProcess is the controller running in this process, if any.
var inProcess *InProcessController

// ControllerInProcess reports whether the test binary is to run the
// controller itself, which it does unless E2E_CONTROLLER says otherwise. A
// value it does not know is an error.
func ControllerInProcess() (bool, error) {
	switch mode := os.Getenv(EnvVarE2EController); mode {
	case "", controllerInProcess:
		return true, nil
	case controllerExternal, controllerOff:
		return false, nil
	default:
		return false, fmt.Errorf("%s=%q: it is %q, %q or %q", EnvVarE2EController, mode, controllerInProcess, controllerExternal, controllerOff)
	}
}

// InProcessController is an Argo Rollouts controller running inside the test
// process, built with the same wiring as the rollouts-controller binary.
type InProcessController struct {
	cancel  context.CancelFunc
	done    chan error
	logFile *os.File
	logPath string
	// logIsTemporary says that the log is a file made for this process and
	// not one the caller named.
	logIsTemporary bool
	klogLogger     *log.Logger
	hook           *controllerLogHook
	calls          *apiCalls
}

// StartController starts the controller in this process and returns once its
// workers are running. It is configured as `make start-e2e` configures the
// binary by default: the e2e instance ID, debug logging, klog level 6 (or
// E2E_KLOG_LEVEL) and the shared analysis secrets in the argo-rollouts
// namespace. It has no counterpart of E2E_CONTROLLER_FLAGS. Leader election is
// off: the lease of an earlier test process would only make this one wait.
//
// The controller looks for its plugins relative to the working directory, so
// the caller has to make that the repository root first. Call Stop when the
// tests are done.
func StartController() (*InProcessController, error) {
	// Build the clients of the tests before the klog level is raised for the
	// controller: from level 6 on, client-go puts request tracing around a
	// new client. Built first, the tests' clients are the same as when no
	// controller runs in this process.
	if _, err := getShared(); err != nil {
		return nil, err
	}

	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{})
	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, err
	}
	restConfig.QPS = defaults.DefaultQPS
	restConfig.Burst = defaults.DefaultBurst
	// The API server names the manager of a field after the user agent. The
	// default is the name of the test binary, the same as for the tests.
	restConfig.UserAgent = "rollouts-controller/e2e"
	// Every request of the controller goes through calls, which records the
	// writes and lets a test make chosen calls fail. The tests' own clients
	// do not go through it.
	calls := newAPICalls(restConfig.Host)
	restConfig.Wrap(calls.wrap)

	opts := controller.DefaultOptions()
	opts.RESTConfig = restConfig
	opts.InstanceID = E2ELabelValueInstanceID
	opts.AnalysisSharedSecretNamespace = "argo-rollouts"
	opts.LeaderElection.LeaderElect = false

	// A second controller with the same instance ID would work on the same
	// rollouts. The manager only logs it when its ports are taken.
	for _, port := range []int{opts.HealthzPort, opts.MetricsPort} {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", port), time.Second)
		if err == nil {
			conn.Close()
			return nil, fmt.Errorf("port %d is in use: is a controller running already? The tests start their own unless %s=%s",
				port, EnvVarE2EController, controllerExternal)
		}
	}

	logFile, logIsTemporary, err := openControllerLog()
	if err != nil {
		return nil, fmt.Errorf("opening the controller log: %w", err)
	}
	logPath := logFile.Name()

	// The controller logs through the shared logrus logger, and client-go
	// through klog. Both go to the log file; the tests have loggers of their
	// own that write to the test output.
	hook := newControllerLogHook(os.Getenv(EnvVarE2EControllerFailures))
	log.SetOutput(logFile)
	log.SetLevel(log.DebugLevel)
	log.AddHook(hook)
	klogLogger := log.New()
	klogLogger.SetOutput(logFile)
	logutil.SetKLogLogger(klogLogger)
	klogLevel := 6
	if level, ok := klogLevelFromEnv(); ok {
		klogLevel = level
	}
	setKLogLevel(klogLevel)
	log.Infof("Starting the controller inside test process %d", os.Getpid())

	manager, err := controller.NewManagerFromOptions(opts)
	if err != nil {
		logFile.Close()
		return nil, fmt.Errorf("building the controller: %w (log: %s)", err, logPath)
	}

	ctx, cancel := context.WithCancel(context.Background())
	c := &InProcessController{
		cancel:         cancel,
		done:           make(chan error, 1),
		logFile:        logFile,
		logPath:        logPath,
		logIsTemporary: logIsTemporary,
		klogLogger:     klogLogger,
		hook:           hook,
		calls:          calls,
	}
	go func() { c.done <- manager.RunWithOptions(ctx, opts) }()

	select {
	case <-hook.started:
	case err := <-c.done:
		cancel()
		logFile.Close()
		if err == nil {
			err = errors.New("it stopped without an error")
		}
		return nil, fmt.Errorf("the controller did not start: %w (log: %s)", err, logPath)
	case <-time.After(controllerStartTimeout):
		cancel()
		return nil, fmt.Errorf("the controller did not log %q within %v (log: %s)", controllerStartedMessage, controllerStartTimeout, logPath)
	}
	inProcess = c
	return c, nil
}

// openControllerLog opens the file the controller logs to: the one
// E2E_CONTROLLER_LOG names, or a new one in the temporary directory. It says
// which of the two it is.
func openControllerLog() (f *os.File, temporary bool, err error) {
	if path := os.Getenv(EnvVarE2EControllerLog); path != "" {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		return f, false, err
	}
	f, err = os.CreateTemp("", "e2e-controller-*.log")
	return f, true, err
}

// Stop shuts the controller down and waits for it. It returns an error when
// the controller stopped with one, did not stop in time, or recovered from a
// panic while it ran: a reconcile that panics is logged and retried by the
// controller, which no test would notice on its own.
func (c *InProcessController) Stop() error {
	c.cancel()
	var errs []error
	select {
	case err := <-c.done:
		if err != nil {
			errs = append(errs, fmt.Errorf("the controller stopped with an error: %w", err))
		}
	case <-time.After(controllerStopTimeout):
		errs = append(errs, fmt.Errorf("the controller did not stop within %v", controllerStopTimeout))
	}
	inProcess = nil
	log.SetOutput(io.Discard)
	c.klogLogger.SetOutput(io.Discard)
	c.logFile.Close()

	// A panic was noted in the failures file when it happened. What went
	// wrong while stopping is only known now.
	for _, err := range errs {
		c.hook.noteFailure(err.Error())
	}
	if panics := c.hook.recoveredPanics(); len(panics) > 0 {
		errs = append(errs, fmt.Errorf("the controller recovered from %d panic(s) (log: %s); the first one:\n%s", len(panics), c.logPath, panics[0]))
	}
	return errors.Join(errs...)
}

// DiscardLog removes the controller's log if it is the file that was made for
// this process. Call it after Stop when nothing failed: nobody will read the
// log of a run that passed, and every test process would leave one behind. A
// log that the caller named with E2E_CONTROLLER_LOG is left alone.
func (c *InProcessController) DiscardLog() {
	if c.logIsTemporary {
		os.Remove(c.logPath)
	}
}

// watchNamespace starts keeping, for the namespace of a test, the controller's
// log lines about it and the API writes the controller makes in it.
func (c *InProcessController) watchNamespace(namespace string) {
	c.hook.capture(namespace)
	c.calls.watch(namespace)
}

// releaseNamespace returns what was kept for a namespace and stops keeping
// it. Faults injected for the namespace end here too.
func (c *InProcessController) releaseNamespace(namespace string) (logLines []string, writes []APICall) {
	writes = c.calls.writesIn(namespace)
	c.calls.forget(namespace)
	return c.hook.release(namespace), writes
}

// controllerLogHook sees every entry that is logged through the shared logrus
// logger. It tells when the controller has started, remembers recovered
// panics, and keeps the latest lines about the namespaces of running tests for
// their failure dumps.
//
// It does not see everything in the log file. The analysis controller logs
// its per-metric lines through loggers of its own (logutil.WithRedactor), and
// client-go logs through klog, for the tests' clients as well as the
// controller's. Those lines are in the file and not in a failure dump. The
// places that recover from a panic all log through the shared logger.
type controllerLogHook struct {
	started   chan struct{}
	startOnce sync.Once
	// failuresPath is the file recovered panics are noted in as they happen,
	// or empty.
	failuresPath string

	mu          sync.Mutex
	panics      []string
	byNamespace map[string][]string
}

func newControllerLogHook(failuresPath string) *controllerLogHook {
	return &controllerLogHook{
		started:      make(chan struct{}),
		failuresPath: failuresPath,
		byNamespace:  map[string][]string{},
	}
}

// noteFailure appends one line to the failures file, if there is one. The
// file is what makes a problem of the controller outlast this process.
func (h *controllerLogHook) noteFailure(what string) {
	if h.failuresPath == "" {
		return
	}
	f, err := os.OpenFile(h.failuresPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e: could not note a controller failure in %s: %v\n", h.failuresPath, err)
		return
	}
	defer f.Close()
	line, _, _ := strings.Cut(strings.TrimSpace(what), "\n")
	fmt.Fprintf(f, "%s test process %d: %s\n", time.Now().UTC().Format(time.RFC3339), os.Getpid(), line)
}

func (h *controllerLogHook) Levels() []log.Level {
	return log.AllLevels
}

func (h *controllerLogHook) Fire(entry *log.Entry) error {
	switch {
	case entry.Message == controllerStartedMessage:
		h.startOnce.Do(func() { close(h.started) })
	case strings.HasPrefix(entry.Message, "Recovered from panic"):
		h.mu.Lock()
		h.panics = append(h.panics, entry.Message)
		h.mu.Unlock()
		namespace, _ := entry.Data[logutil.NamespaceKey].(string)
		h.noteFailure(fmt.Sprintf("the controller recovered from a panic in namespace %q: %s", namespace, entry.Message))
	}

	// Debug lines stay in the log file only: there are far too many of them.
	if entry.Level > log.InfoLevel {
		return nil
	}
	namespace, _ := entry.Data[logutil.NamespaceKey].(string)
	if namespace == "" {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	lines, wanted := h.byNamespace[namespace]
	if !wanted {
		return nil
	}
	if len(lines) >= capturedLogLines {
		lines = lines[1:]
	}
	h.byNamespace[namespace] = append(lines, formatLogEntry(entry))
	return nil
}

func (h *controllerLogHook) capture(namespace string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.byNamespace[namespace] = []string{}
}

func (h *controllerLogHook) release(namespace string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	lines := h.byNamespace[namespace]
	delete(h.byNamespace, namespace)
	return lines
}

func (h *controllerLogHook) recoveredPanics() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.panics...)
}

// formatLogEntry renders an entry as one line: time, level, message, then the
// fields in a fixed order. The namespace is left out; it is the same on every
// line of a dump.
func formatLogEntry(entry *log.Entry) string {
	keys := make([]string, 0, len(entry.Data))
	for key := range entry.Data {
		if key != logutil.NamespaceKey {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s %-5s %s", entry.Time.UTC().Format("15:04:05.000"), entry.Level, strings.TrimRight(entry.Message, "\n"))
	for _, key := range keys {
		fmt.Fprintf(&sb, " %s=%v", key, entry.Data[key])
	}
	return sb.String()
}
