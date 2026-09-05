package hubspot

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DefaultObjectTypes lists standard HubSpot CRM objects cataloged for lineage.
var DefaultObjectTypes = []string{
	"contacts",
	"companies",
	"deals",
	"tickets",
	"products",
	"line_items",
	"quotes",
}

// Config holds the HubSpot CRM connection configuration.
type Config struct {
	AccessToken           string   `json:"access_token"`
	BaseURL               string   `json:"base_url,omitempty"`
	ObjectTypes           []string `json:"object_types,omitempty"`
	IncludeCustomObjects  bool     `json:"include_custom_objects"`
	IncludeAssociations   bool     `json:"include_associations"`
}

// ParseConfig validates and applies defaults to HubSpot configuration.
func ParseConfig(configJSON string) (*Config, error) {
	if strings.TrimSpace(configJSON) == "" {
		return nil, fmt.Errorf("empty configuration JSON provided")
	}

	var cfg Config
	// Defaults
	cfg.IncludeCustomObjects = true
	cfg.IncludeAssociations = true

	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil, fmt.Errorf("invalid configuration JSON: %w", err)
	}

	if strings.TrimSpace(cfg.AccessToken) == "" {
		return nil, fmt.Errorf("missing required field: access_token")
	}

	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = "https://api.hubapi.com"
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")

	if len(cfg.ObjectTypes) == 0 {
		cfg.ObjectTypes = DefaultObjectTypes
	}

	return &cfg, nil
}
