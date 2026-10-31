// Package config locates forge's home directory (~/.forge) and loads config.yaml from it.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// Config is forge's user configuration.
type Config struct {
	Home           string   // ~/.forge, or $FORGE_HOME
	DBPath         string   // SQLite database, default <Home>/forge.db
	ContextTargets []string // `forge context sync` targets; empty = auto
}

const defaultConfig = `# forge configuration — https://github.com/AnwarMestycer/forge
# db_path: /absolute/path/to/forge.db   # default: forge.db next to this file
# context:
#   targets: [agents, claude]   # files "forge context sync" writes: agents (AGENTS.md), claude (CLAUDE.md)
#                               # default: AGENTS.md, plus CLAUDE.md if the project already has one
`

// Home returns forge's home directory: $FORGE_HOME if set, else ~/.forge.
func Home() (string, error) {
	if h := os.Getenv("FORGE_HOME"); h != "" {
		return h, nil
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}
	return filepath.Join(userHome, ".forge"), nil
}

// Load creates the forge home directory (and a default config.yaml) if missing,
// then reads the config. FORGE_* environment variables override the file.
func Load() (*Config, error) {
	home, err := Home()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, fmt.Errorf("create %s: %w", home, err)
	}

	path := filepath.Join(home, "config.yaml")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(path, []byte(defaultConfig), 0o600); err != nil {
			return nil, fmt.Errorf("write %s: %w", path, err)
		}
	}

	v := viper.New()
	v.SetConfigFile(path)
	v.SetEnvPrefix("FORGE")
	v.AutomaticEnv()
	v.SetDefault("db_path", filepath.Join(home, "forge.db"))
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	return &Config{
		Home:           home,
		DBPath:         v.GetString("db_path"),
		ContextTargets: v.GetStringSlice("context.targets"),
	}, nil
}
