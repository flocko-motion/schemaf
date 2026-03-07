package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// loadConfig loads config.toml from homeDir
func loadConfig(homeDir string) (*Config, error) {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("toml")
	v.AddConfigPath(homeDir)

	// Read config file
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("failed to read config: %w", err)
		}
		// Config file not found is OK - use defaults
	}

	return &Config{v: v}, nil
}

// loadState loads state.toml from homeDir
// If state.json exists, migrates it to TOML automatically
func loadState(homeDir string) (*State, error) {
	statePath := filepath.Join(homeDir, "state.toml")

	// Check for old state.json and migrate
	oldStatePath := filepath.Join(homeDir, "state.json")
	if _, err := os.Stat(oldStatePath); err == nil {
		if err := migrateStateJSONToTOML(oldStatePath, statePath); err != nil {
			return nil, fmt.Errorf("failed to migrate state.json: %w", err)
		}
	}

	v := viper.New()
	v.SetConfigFile(statePath)
	v.SetConfigType("toml")

	// Create state file if it doesn't exist
	if _, err := os.Stat(statePath); os.IsNotExist(err) {
		// Ensure directory exists
		if err := os.MkdirAll(homeDir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create config directory: %w", err)
		}

		// Create empty state file
		v.Set("_created", "true") // Add a dummy key to ensure file is created
		if err := v.WriteConfigAs(statePath); err != nil {
			return nil, fmt.Errorf("failed to create state file: %w", err)
		}
	} else {
		// Read existing state
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("failed to read state: %w", err)
		}
	}

	return &State{v: v, path: statePath}, nil
}

// migrateStateJSONToTOML migrates old state.json to state.toml
func migrateStateJSONToTOML(jsonPath, tomlPath string) error {
	// Read JSON state
	oldV := viper.New()
	oldV.SetConfigFile(jsonPath)
	oldV.SetConfigType("json")

	if err := oldV.ReadInConfig(); err != nil {
		return fmt.Errorf("failed to read old state.json: %w", err)
	}

	// Write as TOML
	newV := viper.New()
	newV.SetConfigFile(tomlPath)
	newV.SetConfigType("toml")

	// Copy all settings
	for _, key := range oldV.AllKeys() {
		newV.Set(key, oldV.Get(key))
	}

	if err := newV.WriteConfig(); err != nil {
		return fmt.Errorf("failed to write state.toml: %w", err)
	}

	// Remove old JSON file
	if err := os.Remove(jsonPath); err != nil {
		// Non-fatal - log but continue
		Errorf(fmt.Errorf("failed to remove old state.json: %w", err))
	}

	Success("Migrated state.json → state.toml")
	return nil
}
