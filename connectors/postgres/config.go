package postgres

import (
	"encoding/json"
	"fmt"
	"net/url"
)

// Config represents the connection configuration for PostgreSQL.
type Config struct {
	Host        string   `json:"host"`
	Port        int      `json:"port"`
	User        string   `json:"user"`
	Password    string   `json:"password"`
	Database    string   `json:"database"`
	SSLMode     string   `json:"sslmode,omitempty"`
	Schemas     []string `json:"schemas,omitempty"`
	TableFilter []string `json:"table_filter,omitempty"`
}

// ParseConfig unmarshals and applies defaults to PostgreSQL configuration.
func ParseConfig(configJSON string) (*Config, error) {
	if configJSON == "" {
		return nil, fmt.Errorf("empty configuration JSON provided")
	}

	var cfg Config
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil, fmt.Errorf("invalid configuration JSON: %w", err)
	}

	if cfg.Host == "" {
		return nil, fmt.Errorf("missing required field: host")
	}
	if cfg.Database == "" {
		return nil, fmt.Errorf("missing required field: database")
	}
	if cfg.Port == 0 {
		cfg.Port = 5432
	}
	if cfg.SSLMode == "" {
		cfg.SSLMode = "disable"
	}
	if len(cfg.Schemas) == 0 {
		cfg.Schemas = []string{"public"}
	}

	return &cfg, nil
}

// ConnectionString formats the PostgreSQL connection URL.
func (c *Config) ConnectionString() string {
	userInfo := url.UserPassword(c.User, c.Password)
	hostPort := fmt.Sprintf("%s:%d", c.Host, c.Port)

	u := url.URL{
		Scheme:   "postgres",
		User:     userInfo,
		Host:     hostPort,
		Path:     c.Database,
		RawQuery: fmt.Sprintf("sslmode=%s", c.SSLMode),
	}
	return u.String()
}
