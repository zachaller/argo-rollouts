package main

import (
	"os"
	"strings"

	goPlugin "github.com/hashicorp/go-plugin"
	log "github.com/sirupsen/logrus"

	rolloutsPlugin "github.com/argoproj/argo-rollouts/rollout/steps/plugin/rpc"
	"github.com/argoproj/argo-rollouts/regional-traffic-demo/plugin/internal/plugin"
)

// handshakeConfigs are used to just do a basic handshake between
// a plugin and host. If the handshake fails, a user friendly error is shown.
// This prevents users from executing bad plugins or executing a plugin
// directory. It is a UX feature, not a security feature.
var handshakeConfig = goPlugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "ARGO_ROLLOUTS_RPC_PLUGIN",
	MagicCookieValue: "step",
}

func main() {
	logCtx := log.WithFields(log.Fields{"plugin": "regional-traffic-step"})

	setLogLevel("info")
	log.SetFormatter(createFormatter("text"))

	// pluginMap is the map of plugins we can dispense.
	var pluginMap = map[string]goPlugin.Plugin{
		"RpcStepPlugin": &rolloutsPlugin.RpcStepPlugin{Impl: plugin.New(logCtx)},
	}

	logCtx.Info("Regional Traffic Step Plugin starting")

	goPlugin.Serve(&goPlugin.ServeConfig{
		HandshakeConfig: handshakeConfig,
		Plugins:         pluginMap,
	})
}

func createFormatter(logFormat string) log.Formatter {
	var formatType log.Formatter
	switch strings.ToLower(logFormat) {
	case "json":
		formatType = &log.JSONFormatter{}
	case "text":
		formatType = &log.TextFormatter{
			FullTimestamp: true,
		}
	default:
		log.Infof("Unknown format: %s. Using text logformat", logFormat)
		formatType = &log.TextFormatter{
			FullTimestamp: true,
		}
	}

	return formatType
}

func setLogLevel(logLevel string) {
	level, err := log.ParseLevel(logLevel)
	if err != nil {
		log.Fatal(err)
	}
	log.SetLevel(level)

	// Also support LOG_LEVEL env var
	if envLevel := os.Getenv("LOG_LEVEL"); envLevel != "" {
		level, err := log.ParseLevel(envLevel)
		if err == nil {
			log.SetLevel(level)
		}
	}
}