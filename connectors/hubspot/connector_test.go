package hubspot_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/radicelle/kraal/connectors/hubspot"
	protocolv1 "github.com/radicelle/kraal/pkg/protocol/v1"
)

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name      string
		json      string
		expectErr bool
	}{
		{
			name:      "valid minimal config",
			json:      `{"access_token": "pat-na1-12345"}`,
			expectErr: false,
		},
		{
			name:      "missing access_token",
			json:      `{"base_url": "https://api.hubapi.com"}`,
			expectErr: true,
		},
		{
			name:      "empty json",
			json:      ``,
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := hubspot.ParseConfig(tt.json)
			if tt.expectErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if !tt.expectErr && cfg != nil {
				if cfg.BaseURL != "https://api.hubapi.com" {
					t.Errorf("expected default base URL, got %s", cfg.BaseURL)
				}
				if len(cfg.ObjectTypes) == 0 {
					t.Errorf("expected default object types to be populated")
				}
			}
		})
	}
}

func TestConnectorSpec(t *testing.T) {
	connector := hubspot.NewConnector()
	spec, err := connector.Spec(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.GetName() != "hubspot" {
		t.Errorf("expected name hubspot, got %s", spec.GetName())
	}
	if !strings.Contains(spec.GetConfigSchemaJson(), "access_token") {
		t.Errorf("expected access_token in config schema: %s", spec.GetConfigSchemaJson())
	}
}

func TestConnectorCheck(t *testing.T) {
	// Mock server that succeeds on valid bearer token
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer valid-token" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"message": "Invalid token"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"results": []}`))
	}))
	defer server.Close()

	connector := hubspot.NewConnector()

	// 1. Success check
	cfgValid := fmt.Sprintf(`{"access_token": "valid-token", "base_url": "%s"}`, server.URL)
	resp, err := connector.Check(context.Background(), cfgValid)
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if resp.GetStatus() != protocolv1.CheckStatus_CHECK_STATUS_SUCCESS {
		t.Errorf("expected success, got %v: %s", resp.GetStatus(), resp.GetMessage())
	}

	// 2. Failure check
	cfgInvalid := fmt.Sprintf(`{"access_token": "bad-token", "base_url": "%s"}`, server.URL)
	respFail, err := connector.Check(context.Background(), cfgInvalid)
	if err != nil {
		t.Fatalf("Check returned error instead of failed response: %v", err)
	}
	if respFail.GetStatus() != protocolv1.CheckStatus_CHECK_STATUS_FAILED {
		t.Errorf("expected failure status, got %v", respFail.GetStatus())
	}
}

func TestConnectorDiscoverAndRead(t *testing.T) {
	// Mock server simulating properties and custom schemas
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/crm/v3/properties/contacts"):
			resp := map[string]any{
				"results": []map[string]any{
					{
						"name":        "firstname",
						"label":       "First Name",
						"type":        "string",
						"fieldType":   "text",
						"description": "Contact's first name",
						"archived":    false,
					},
					{
						"name":        "email",
						"label":       "Email",
						"type":        "string",
						"fieldType":   "text",
						"description": "Contact's email",
						"archived":    false,
					},
				},
			}
			json.NewEncoder(w).Encode(resp)

		case strings.HasPrefix(r.URL.Path, "/crm/v3/properties/"):
			// Return empty properties for other objects
			json.NewEncoder(w).Encode(map[string]any{"results": []any{}})

		case r.URL.Path == "/crm/v3/schemas":
			resp := map[string]any{
				"results": []map[string]any{
					{
						"id":                 "2-12345",
						"fullyQualifiedName": "p_appliances",
						"name":               "appliances",
						"description":        "Custom appliance object",
						"properties": []map[string]any{
							{"name": "serial_number", "label": "Serial Number", "type": "string"},
						},
						"associations": []map[string]any{
							{
								"fromObjectTypeId": "appliances",
								"toObjectTypeId":   "contacts",
								"name":             "appliance_to_contact",
							},
						},
					},
				},
			}
			json.NewEncoder(w).Encode(resp)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	connector := hubspot.NewConnector()
	cfg := fmt.Sprintf(`{
		"access_token": "test-pat",
		"base_url": "%s",
		"object_types": ["contacts", "deals"],
		"include_custom_objects": true,
		"include_associations": true
	}`, server.URL)

	// Test Discover
	discoverResp, err := connector.Discover(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	// Should have contacts, deals, and appliances
	if len(discoverResp.GetStreams()) < 3 {
		t.Errorf("expected at least 3 streams, got %d", len(discoverResp.GetStreams()))
	}

	var foundContacts, foundCustom bool
	for _, stream := range discoverResp.GetStreams() {
		if stream.GetName() == "contacts" {
			foundContacts = true
			if len(stream.GetFields()) < 3 { // hs_object_id + firstname + email
				t.Errorf("expected at least 3 fields on contacts, got %d", len(stream.GetFields()))
			}
		}
		if stream.GetName() == "appliances" {
			foundCustom = true
			if stream.GetEntityType() != "CUSTOM_OBJECT" {
				t.Errorf("expected CUSTOM_OBJECT entity type, got %s", stream.GetEntityType())
			}
		}
	}
	if !foundContacts {
		t.Errorf("contacts stream not found in discover response")
	}
	if !foundCustom {
		t.Errorf("custom object stream not found in discover response")
	}

	// Test relations
	if len(discoverResp.GetRelations()) == 0 {
		t.Errorf("expected association relations, got 0")
	}

	// Test Read stream
	var envelopes []*protocolv1.RecordEnvelope
	err = connector.Read(context.Background(), &protocolv1.ReadRequest{
		ConfigJson: cfg,
	}, func(record *protocolv1.RecordEnvelope) error {
		envelopes = append(envelopes, record)
		return nil
	})
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	var hasEntity, hasRelation bool
	for _, env := range envelopes {
		if env.GetRecordType() == "ENTITY" {
			hasEntity = true
		}
		if env.GetRecordType() == "RELATION" {
			hasRelation = true
		}
	}
	if !hasEntity {
		t.Errorf("expected entity records in read stream")
	}
	if !hasRelation {
		t.Errorf("expected relation records in read stream")
	}
}

func TestConnectorWrite(t *testing.T) {
	connector := hubspot.NewConnector()
	ch := make(chan *protocolv1.RecordEnvelope, 2)
	ch <- &protocolv1.RecordEnvelope{DataJson: []byte(`{"id":"hs_1"}`)}
	ch <- &protocolv1.RecordEnvelope{DataJson: []byte(`{"id":"hs_2"}`)}
	close(ch)

	resp, err := connector.Write(context.Background(), ch)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if resp.GetRecordsWritten() != 2 {
		t.Errorf("expected 2 records written, got %d", resp.GetRecordsWritten())
	}
}
