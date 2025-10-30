package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	DefaultConfigDir  = ".rag-terminal"
	DefaultConfigFile = "config.yaml"
)

// Config represents the application configuration
type Config struct {
	TokenBudget         TokenBudgetConfig `yaml:"token_budget"`
	CodeTokenBudget     TokenBudgetConfig `yaml:"code_token_budget"`
	EmbeddingDimensions int               `yaml:"embedding_dimensions"`
}

// TokenBudgetConfig defines how available input tokens are allocated
type TokenBudgetConfig struct {
	// InputRatio: percentage of context window allocated for input tokens (0.0-1.0)
	// The remainder is reserved for output
	// Default: 0.6 (60% for input, 40% for output)
	InputRatio float64 `yaml:"input_ratio"`

	// Excerpts: percentage of available input tokens for document excerpts (0.0-1.0)
	Excerpts float64 `yaml:"excerpts"`

	// History: percentage of available input tokens for conversation history (0.0-1.0)
	History float64 `yaml:"history"`
}

func DefaultConfig() *Config {
	return &Config{
		// Token budget for text/document files
		TokenBudget: TokenBudgetConfig{
			InputRatio: 0.6, // 60% of context window for input
			Excerpts:   0.3, // 30% of input for excerpts
			History:    0.1, // 10% of input for history
		},
		// Token budget for code files (SQL, Go, Python, etc.)
		CodeTokenBudget: TokenBudgetConfig{
			InputRatio: 0.7,  // 70% for input (code analysis needs more input, less output)
			Excerpts:   0.15, // 15% for excerpts (code uses syntax-aware extraction, less excerpt needed)
			History:    0.05, // 5% for history (prioritize code context over conversation)
		},
		EmbeddingDimensions: 786,
	}
}

// GetConfigPath returns the path to the config file
func GetConfigPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	configDir := filepath.Join(homeDir, DefaultConfigDir)
	return filepath.Join(configDir, DefaultConfigFile), nil
}

// EnsureConfigDir creates the config directory if it doesn't exist
func EnsureConfigDir() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	configDir := filepath.Join(homeDir, DefaultConfigDir)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	return nil
}

// Load loads the configuration from file, creating default if not exists
func Load() (*Config, error) {
	configPath, err := GetConfigPath()
	if err != nil {
		return nil, err
	}

	// Check if config file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Config file doesn't exist, create default
		cfg := DefaultConfig()
		if err := Save(cfg); err != nil {
			// If save fails, just return default config without error
			// This ensures the app works even if we can't write config
			return cfg, nil
		}
		return cfg, nil
	}

	// Read config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse YAML
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Validate config
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

// Save saves the configuration to file
func Save(cfg *Config) error {
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("cannot save invalid config: %w", err)
	}

	// Ensure config directory exists
	if err := EnsureConfigDir(); err != nil {
		return err
	}

	configPath, err := GetConfigPath()
	if err != nil {
		return err
	}

	// Marshal to YAML
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write to file
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// Validate validates the configuration values
func (c *Config) Validate() error {
	// Validate text token budget
	if err := c.validateTokenBudget("token_budget", &c.TokenBudget); err != nil {
		return err
	}

	// Validate code token budget
	if err := c.validateTokenBudget("code_token_budget", &c.CodeTokenBudget); err != nil {
		return err
	}

	// Validate EmbeddingDimensions
	if c.EmbeddingDimensions <= 0 {
		return fmt.Errorf("embedding_dimensions must be positive, got %d", c.EmbeddingDimensions)
	}

	return nil
}

// validateTokenBudget validates a token budget configuration
func (c *Config) validateTokenBudget(name string, budget *TokenBudgetConfig) error {
	// Validate InputRatio
	if budget.InputRatio < 0.0 || budget.InputRatio > 1.0 {
		return fmt.Errorf("%s.input_ratio must be between 0.0 and 1.0, got %f", name, budget.InputRatio)
	}

	// Validate Excerpts
	if budget.Excerpts < 0.0 || budget.Excerpts > 1.0 {
		return fmt.Errorf("%s.excerpts must be between 0.0 and 1.0, got %f", name, budget.Excerpts)
	}

	// Validate History
	if budget.History < 0.0 || budget.History > 1.0 {
		return fmt.Errorf("%s.history must be between 0.0 and 1.0, got %f", name, budget.History)
	}

	// Validate sum doesn't exceed 1.0
	sum := budget.Excerpts + budget.History
	if sum > 1.0 {
		return fmt.Errorf("%s.excerpts + %s.history must not exceed 1.0, got %f", name, name, sum)
	}

	return nil
}

// GetChunksBudget returns the calculated percentage for chunks
func (c *Config) GetChunksBudget() float64 {
	return 1.0 - c.TokenBudget.Excerpts - c.TokenBudget.History
}
