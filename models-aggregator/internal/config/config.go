// Package config loads the aggregator's runtime configuration from the environment (wired by the
// Helm chart's Deployment).
package config

import (
	"os"
	"strconv"
	"time"
)

// Config is the aggregator's runtime configuration.
type Config struct {
	ListenAddr        string        // AGG_LISTEN_ADDR
	PublisherPrefix   string        // PUBLISHER_PREFIX — first segment of the fully-qualified id
	BackendPort       string        // BACKEND_PORT — OpenAI port probed on KServe backends
	SlurmSelector     string        // SLURM_LABEL_SELECTOR — label selector for slurm-models routes
	KServeSvcTemplate string        // KSERVE_SVC_TEMPLATE — DNS template ({name}/{namespace})
	RequestTimeout    time.Duration // REQUEST_TIMEOUT_SECONDS — per-backend probe timeout
	RefreshInterval   time.Duration // REFRESH_INTERVAL_SECONDS — background poll interval
}

// Load reads the configuration from the environment, applying defaults for any unset variable.
func Load() Config {
	return Config{
		ListenAddr:        env("AGG_LISTEN_ADDR", ":8080"),
		PublisherPrefix:   env("PUBLISHER_PREFIX", "publishers"),
		BackendPort:       env("BACKEND_PORT", "8000"),
		SlurmSelector:     env("SLURM_LABEL_SELECTOR", "app.kubernetes.io/name=slurm-models"),
		KServeSvcTemplate: env("KSERVE_SVC_TEMPLATE", "{name}.{namespace}.svc.cluster.local"),
		RequestTimeout:    time.Duration(envInt("REQUEST_TIMEOUT_SECONDS", 3)) * time.Second,
		RefreshInterval:   time.Duration(envInt("REFRESH_INTERVAL_SECONDS", 30)) * time.Second,
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
