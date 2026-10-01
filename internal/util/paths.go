package util

import (
	"os"
	"path/filepath"
	"sync/atomic"
)

// PlatformPaths is the immutable path configuration installed by a process at
// startup, after loading JSON/.env/environment settings and before opening stores.
// Explicit environment overrides remain authoritative.
type PlatformPaths struct {
	HomeDir        string
	AgentDefsDir   string
	HistoryDir     string
	LogDir         string
	ReflectionsDir string
}

var configuredPlatformPaths atomic.Pointer[PlatformPaths]

// ConfigurePlatformPaths publishes a value copy. The returned restore function
// is for sequential test scopes; running services configure paths once at startup.
func ConfigurePlatformPaths(paths PlatformPaths) func() {
	previous := configuredPlatformPaths.Swap(&paths)
	return func() { configuredPlatformPaths.Store(previous) }
}

func currentPlatformPaths() PlatformPaths {
	if paths := configuredPlatformPaths.Load(); paths != nil {
		return *paths
	}
	return PlatformPaths{}
}

// PlatformPathsConfigured distinguishes an installed startup snapshot from a
// standalone library caller that resolves configuration for its own owners.
func PlatformPathsConfigured() bool { return configuredPlatformPaths.Load() != nil }

// RuntimePlatformHome uses the loaded startup configuration as the fallback.
// PlatformHome remains a pure resolver for configuration loading itself.
func RuntimePlatformHome() string {
	if root := os.Getenv("BT_AGENT_HOME"); root != "" {
		return root
	}
	if root := os.Getenv("BT_HOME"); root != "" {
		return root
	}
	paths := currentPlatformPaths()
	defs := os.Getenv("BT_AGENT_DEFS_DIR")
	if defs == "" {
		defs = paths.AgentDefsDir
	}
	if defs == "" && paths.HomeDir != "" {
		return paths.HomeDir
	}
	return PlatformHome(defs)
}

func PlatformAgentDefinitionsDir() string {
	if dir := os.Getenv("BT_AGENT_DEFS_DIR"); dir != "" {
		return dir
	}
	if dir := currentPlatformPaths().AgentDefsDir; dir != "" {
		return dir
	}
	return platformSubdir("agents")
}

func PlatformHistoryDir() string {
	if dir := os.Getenv("BT_HISTORY_DIR"); dir != "" {
		return dir
	}
	if dir := currentPlatformPaths().HistoryDir; dir != "" {
		return dir
	}
	return platformSubdir("history")
}

func PlatformLogDir() string {
	if dir := os.Getenv("BT_LOG_DIR"); dir != "" {
		return dir
	}
	if dir := currentPlatformPaths().LogDir; dir != "" {
		return dir
	}
	return platformSubdir("logs")
}

func platformSubdir(name string) string {
	root := RuntimePlatformHome()
	if root == "" {
		return ""
	}
	return filepath.Join(root, name)
}

// ConfiguredReflectionsDir returns the startup root when a process has installed
// one. An empty value means a library caller must resolve its own configuration.
func ConfiguredReflectionsDir() string {
	if dir := os.Getenv("BT_REFLECTIONS_DIR"); dir != "" {
		return dir
	}
	return currentPlatformPaths().ReflectionsDir
}

// PlatformHome resolves the shared platform state root. An explicit home wins
// over the caller's configured agent-definition directory. Keeping this below
// engine/config/agent avoids import cycles and divergent persistence roots.
func PlatformHome(agentDefsDir string) string {
	if root := os.Getenv("BT_AGENT_HOME"); root != "" {
		return root
	}
	if root := os.Getenv("BT_HOME"); root != "" {
		return root
	}
	if agentDefsDir != "" {
		return filepath.Dir(agentDefsDir)
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".go-bt-evolve")
}
