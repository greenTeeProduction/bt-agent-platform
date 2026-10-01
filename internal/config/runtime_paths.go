package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nico/go-bt-evolve/internal/util"
)

// ConfigureRuntimePaths must run before a process initializes logging or state
// owners. Loading/reloading a Config alone never redirects active stores.
// The returned restore function supports sequential test scopes.
func (c *Config) ConfigureRuntimePaths() (func(), error) {
	if util.PlatformHome(c.AgentDefsDir) == "" {
		return nil, fmt.Errorf("platform home: set BT_AGENT_HOME or supply a user home/agent definition directory")
	}
	if _, err := c.SharedReflectionsDir(); err != nil {
		return nil, fmt.Errorf("reflection home: %w", err)
	}
	c.ResolvePaths()
	return util.ConfigurePlatformPaths(util.PlatformPaths{
		HomeDir:        c.Paths.HomeDir,
		AgentDefsDir:   c.AgentDefsDir,
		HistoryDir:     c.HistoryDir,
		LogDir:         c.LogDir,
		ReflectionsDir: c.Paths.ReflectionsDir,
	}), nil
}

// LoadRuntime validates and installs startup paths before owners can open state.
// Use Load for pure configuration reads and hot reload.
func LoadRuntime() (*Config, error) {
	cfg, err := Load()
	if err != nil {
		return nil, err
	}
	if _, err := cfg.ConfigureRuntimePaths(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// SharedReflectionsDir resolves the independently configured shared reflection,
// tree and block store; its default remains ~/.go-bt-reflections.
func (c *Config) SharedReflectionsDir() (string, error) {
	if c.ReflectionsDir != "" {
		return c.ReflectionsDir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".go-bt-reflections"), nil
}
