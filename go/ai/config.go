package ai

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/joho/godotenv"
)

var (
	envLoaded bool
	envMutex  sync.Mutex
)

// LoadEnv loads environment variables from ~/.atlas/.env if not already loaded.
// This is called automatically by GetAPIKey but can be called explicitly if needed.
func LoadEnv() error {
	envMutex.Lock()
	defer envMutex.Unlock()

	if envLoaded {
		return nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	envPath := filepath.Join(home, ".atlas", ".env")

	// Check if file exists
	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		return fmt.Errorf("config file not found: %s (create it with your API keys)", envPath)
	}

	// Load .env file
	if err := godotenv.Load(envPath); err != nil {
		return fmt.Errorf("failed to load %s: %w", envPath, err)
	}

	envLoaded = true
	return nil
}

// GetAPIKey retrieves the API key for the specified provider.
// It automatically loads ~/.atlas/.env if not already loaded.
// Supported providers: "anthropic", "openai".
func GetAPIKey(provider string) (string, error) {
	if err := LoadEnv(); err != nil {
		return "", err
	}

	var envVar string
	switch provider {
	case "anthropic":
		envVar = "ANTHROPIC_API_KEY"
	case "openai":
		envVar = "OPENAI_API_KEY"
	default:
		return "", fmt.Errorf("unknown provider: %s", provider)
	}

	key := os.Getenv(envVar)
	if key == "" {
		home, _ := os.UserHomeDir()
		envPath := filepath.Join(home, ".atlas", ".env")
		return "", fmt.Errorf("%s not set in %s", envVar, envPath)
	}

	return key, nil
}
