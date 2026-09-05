package hubspot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	protocolv1 "github.com/radicelle/kraal/pkg/protocol/v1"
)

// PropertyDefinition models a property returned by HubSpot CRM Properties API.
type PropertyDefinition struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Type        string `json:"type"`      // string, number, date, datetime, enumeration, bool
	FieldType   string `json:"fieldType"` // text, textarea, select, booleancheckbox, number
	Description string `json:"description"`
	GroupName   string `json:"groupName"`
	Calculated  bool   `json:"calculated"`
	Archived    bool   `json:"archived"`
}

type propertiesAPIResponse struct {
	Results []PropertyDefinition `json:"results"`
}

// CustomObjectSchema models a custom object returned by HubSpot Schemas API.
type CustomObjectSchema struct {
	ID                 string               `json:"id"`
	FullyQualifiedName string               `json:"fullyQualifiedName"`
	Name               string               `json:"name"`
	Labels             map[string]string    `json:"labels"`
	Description        string               `json:"description"`
	Properties         []PropertyDefinition `json:"properties"`
	Associations       []SchemaAssociation  `json:"associations"`
}

type SchemaAssociation struct {
	FromObjectTypeId string `json:"fromObjectTypeId"`
	ToObjectTypeId   string `json:"toObjectTypeId"`
	Name             string `json:"name"`
}

type customSchemasAPIResponse struct {
	Results []CustomObjectSchema `json:"results"`
}

// Client interacts with the HubSpot CRM API.
type Client struct {
	cfg        *Config
	httpClient *http.Client
}

func NewClient(cfg *Config) *Client {
	return &Client{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (c *Client) doRequest(ctx context.Context, method, endpoint string) (*http.Response, error) {
	reqURL := fmt.Sprintf("%s%s", c.cfg.BaseURL, endpoint)
	req, err := http.NewRequestWithContext(ctx, method, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.cfg.AccessToken))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Kraal-HubSpot-Connector/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request to %s failed: %w", reqURL, err)
	}
	return resp, nil
}

// Ping checks connectivity and token validity against HubSpot CRM.
func (c *Client) Ping(ctx context.Context) error {
	resp, err := c.doRequest(ctx, http.MethodGet, "/crm/v3/properties/contacts?archived=false")
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("authentication failed: invalid or unauthorized access token (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HubSpot API returned error status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// GetProperties retrieves property definitions for a specific CRM object type.
func (c *Client) GetProperties(ctx context.Context, objectType string) ([]PropertyDefinition, error) {
	endpoint := fmt.Sprintf("/crm/v3/properties/%s?archived=false", objectType)
	resp, err := c.doRequest(ctx, http.MethodGet, endpoint)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed fetching properties for %s (HTTP %d): %s", objectType, resp.StatusCode, string(body))
	}

	var res propertiesAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("failed decoding properties for %s: %w", objectType, err)
	}
	return res.Results, nil
}

// GetCustomSchemas retrieves custom object definitions and custom association schemas.
func (c *Client) GetCustomSchemas(ctx context.Context) ([]CustomObjectSchema, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/crm/v3/schemas")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Custom schemas might not be accessible if portal lacks custom objects feature
		return nil, nil
	}

	var res customSchemasAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("failed decoding custom schemas: %w", err)
	}
	return res.Results, nil
}

// StandardLineageRelations provides the core HubSpot CRM association graph.
func StandardLineageRelations() []*protocolv1.RelationEdge {
	return []*protocolv1.RelationEdge{
		{
			SourceEntity: "deals",
			SourceField:  "associated_contact_id",
			TargetEntity: "contacts",
			TargetField:  "hs_object_id",
			RelationType: "ASSOCIATION",
			Description:  "Deal associated to Contact",
		},
		{
			SourceEntity: "deals",
			SourceField:  "associated_company_id",
			TargetEntity: "companies",
			TargetField:  "hs_object_id",
			RelationType: "ASSOCIATION",
			Description:  "Deal associated to Company",
		},
		{
			SourceEntity: "contacts",
			SourceField:  "associated_company_id",
			TargetEntity: "companies",
			TargetField:  "hs_object_id",
			RelationType: "ASSOCIATION",
			Description:  "Contact works at Company",
		},
		{
			SourceEntity: "tickets",
			SourceField:  "associated_contact_id",
			TargetEntity: "contacts",
			TargetField:  "hs_object_id",
			RelationType: "ASSOCIATION",
			Description:  "Ticket raised by Contact",
		},
		{
			SourceEntity: "tickets",
			SourceField:  "associated_company_id",
			TargetEntity: "companies",
			TargetField:  "hs_object_id",
			RelationType: "ASSOCIATION",
			Description:  "Ticket associated with Company",
		},
		{
			SourceEntity: "line_items",
			SourceField:  "associated_deal_id",
			TargetEntity: "deals",
			TargetField:  "hs_object_id",
			RelationType: "ASSOCIATION",
			Description:  "Line item attached to Deal",
		},
		{
			SourceEntity: "quotes",
			SourceField:  "associated_deal_id",
			TargetEntity: "deals",
			TargetField:  "hs_object_id",
			RelationType: "ASSOCIATION",
			Description:  "Quote generated for Deal",
		},
	}
}

// MapHubSpotTypeToJSONType converts HubSpot property types into JSON Schema types.
func MapHubSpotTypeToJSONType(hsType string) string {
	switch strings.ToLower(hsType) {
	case "number":
		return "number"
	case "bool", "boolean":
		return "boolean"
	case "date", "datetime":
		return "string"
	default:
		return "string"
	}
}
