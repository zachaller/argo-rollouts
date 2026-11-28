package main

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/argoproj/argo-rollouts/controller"
)

func TestMetricsPortFlagCompatibility(t *testing.T) {
	cmd := newCommand()

	// Test that both flags are available
	metricsPortFlag := cmd.Flags().Lookup("metricsPort")
	assert.NotNil(t, metricsPortFlag, "metricsPort flag should exist")

	metricsportFlag := cmd.Flags().Lookup("metricsport")
	assert.NotNil(t, metricsportFlag, "metricsport flag should exist for backward compatibility")

	// Test that deprecated flag is marked as deprecated
	assert.True(t, metricsportFlag.Deprecated != "", "metricsport flag should be marked as deprecated")
}

// TestDefaultOptionsMatchFlagDefaults makes sure that controller.DefaultOptions keeps returning
// the defaults of the flags that the controller.Options are built from.
func TestDefaultOptionsMatchFlagDefaults(t *testing.T) {
	cmd := newCommand()
	opts := controller.DefaultOptions()

	flagDefaults := map[string]string{
		"namespaced":                            strconv.FormatBool(opts.Namespaced),
		"instance-id":                           opts.InstanceID,
		"rollout-resync":                        strconv.FormatInt(int64(opts.ResyncPeriod/time.Second), 10),
		"metricsPort":                           strconv.Itoa(opts.MetricsPort),
		"metricsport":                           strconv.Itoa(opts.MetricsPort),
		"healthzPort":                           strconv.Itoa(opts.HealthzPort),
		"rollout-threads":                       strconv.Itoa(opts.RolloutThreads),
		"experiment-threads":                    strconv.Itoa(opts.ExperimentThreads),
		"analysis-threads":                      strconv.Itoa(opts.AnalysisThreads),
		"service-threads":                       strconv.Itoa(opts.ServiceThreads),
		"ingress-threads":                       strconv.Itoa(opts.IngressThreads),
		"ephemeral-metadata-threads":            strconv.Itoa(opts.EphemeralMetadataThreads),
		"ephemeral-metadata-update-pod-retries": strconv.Itoa(opts.EphemeralMetadataPodRetries),
		"nginx-ingress-classes":                 fmt.Sprint(opts.NginxIngressClasses),
		"alb-ingress-classes":                   fmt.Sprint(opts.ALBIngressClasses),
		"ingress-api-version":                   opts.IngressAPIVersion,
		"self-service-notification-enabled":     strconv.FormatBool(opts.SelfServiceNotificationEnabled),
		"analysis-shared-secret-namespace":      opts.AnalysisSharedSecretNamespace,
		"analysis-shared-secret-mode":           string(opts.AnalysisSharedSecretMode),
		"leader-elect":                          strconv.FormatBool(opts.LeaderElection.LeaderElect),
		"leader-election-lease-duration":        opts.LeaderElection.LeaderElectionLeaseDuration.String(),
		"leader-election-renew-deadline":        opts.LeaderElection.LeaderElectionRenewDeadline.String(),
		"leader-election-retry-period":          opts.LeaderElection.LeaderElectionRetryPeriod.String(),
	}
	for name, expected := range flagDefaults {
		flag := cmd.Flags().Lookup(name)
		require.NotNil(t, flag, name)
		assert.Equal(t, expected, flag.DefValue, name)
	}

	// Without --namespaced the controller watches all the namespaces
	assert.Empty(t, opts.Namespace)
	// Without --controllers all the controllers run
	enabledControllers, err := getEnabledControllers(nil)
	require.NoError(t, err)
	assert.Equal(t, enabledControllers[controllerAnalysis], opts.AnalysisOnly)
	// There is no flag for the leader election namespace
	assert.Equal(t, *controller.NewLeaderElectionOptions(), opts.LeaderElection)
}
