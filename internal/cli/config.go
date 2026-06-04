package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// Config holds user-level InferBolt CLI settings.
type Config struct {
	ServerURL string `mapstructure:"server_url" yaml:"server_url"`
	APIKey    string `mapstructure:"api_key"    yaml:"api_key"`
	TenantID  string `mapstructure:"tenant_id"  yaml:"tenant_id"`
	OutputFmt string `mapstructure:"output"     yaml:"output"` // "table" or "json"
}

// Load reads config from ~/.inferbolt/config.yaml, then env var overrides.
// Returns an error if no API key can be found.
func Load() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("user home dir: %w", err)
	}

	v := viper.New()
	v.SetConfigFile(filepath.Join(home, ".inferbolt", "config.yaml"))
	v.SetConfigType("yaml")

	v.BindEnv("server_url", "INFERBOLT_SERVER_URL") //nolint:errcheck
	v.BindEnv("api_key", "INFERBOLT_API_KEY")        //nolint:errcheck
	v.BindEnv("tenant_id", "INFERBOLT_TENANT_ID")    //nolint:errcheck
	v.BindEnv("output", "INFERBOLT_OUTPUT")           //nolint:errcheck

	v.SetDefault("server_url", "http://localhost:8080")
	v.SetDefault("output", "table")

	_ = v.ReadInConfig() // file absence is not an error

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	if cfg.APIKey == "" {
		return nil, errors.New("API key not configured; run 'inferbolt configure' or set INFERBOLT_API_KEY")
	}
	return &cfg, nil
}

// Save writes cfg to ~/.inferbolt/config.yaml (0600 permissions).
func Save(cfg *Config) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".inferbolt")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	f, err := os.OpenFile(filepath.Join(dir, "config.yaml"),
		os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := yaml.NewEncoder(f)
	defer enc.Close()
	return enc.Encode(cfg)
}

// NewClient creates an HTTP client from this config.
func (c *Config) NewClient() *Client {
	return NewClient(c.ServerURL, c.APIKey)
}
