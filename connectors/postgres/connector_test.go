package postgres_test

import (
	"context"
	"strings"
	"testing"

	"github.com/radicelle/kraal/connectors/postgres"
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
			json:      `{"host": "localhost", "database": "testdb"}`,
			expectErr: false,
		},
		{
			name:      "missing host",
			json:      `{"database": "testdb"}`,
			expectErr: true,
		},
		{
			name:      "missing database",
			json:      `{"host": "localhost"}`,
			expectErr: true,
		},
		{
			name:      "invalid json",
			json:      `not json`,
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := postgres.ParseConfig(tt.json)
			if tt.expectErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if !tt.expectErr && cfg != nil {
				if cfg.Port != 5432 {
					t.Errorf("expected default port 5432, got %d", cfg.Port)
				}
				if cfg.SSLMode != "disable" {
					t.Errorf("expected default sslmode disable, got %s", cfg.SSLMode)
				}
			}
		})
	}
}

func TestConnectionString(t *testing.T) {
	cfg := &postgres.Config{
		Host:     "db.example.com",
		Port:     5432,
		User:     "myuser",
		Password: "mypassword",
		Database: "proddb",
		SSLMode:  "require",
	}

	connStr := cfg.ConnectionString()
	if !strings.Contains(connStr, "db.example.com:5432") {
		t.Errorf("expected host in connection string: %s", connStr)
	}
	if !strings.Contains(connStr, "sslmode=require") {
		t.Errorf("expected sslmode in connection string: %s", connStr)
	}
}

func TestConnectorSpec(t *testing.T) {
	connector := postgres.NewConnector()
	spec, err := connector.Spec(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.GetName() != "postgres" {
		t.Errorf("expected name postgres, got %s", spec.GetName())
	}
	if !strings.Contains(spec.GetConfigSchemaJson(), "PostgresConfig") {
		t.Errorf("expected PostgresConfig in schema: %s", spec.GetConfigSchemaJson())
	}
}

func TestConnectorWrite(t *testing.T) {
	connector := postgres.NewConnector()
	ch := make(chan *protocolv1.RecordEnvelope, 2)
	ch <- &protocolv1.RecordEnvelope{DataJson: []byte(`{"id":1}`)}
	ch <- &protocolv1.RecordEnvelope{DataJson: []byte(`{"id":2}`)}
	close(ch)

	resp, err := connector.Write(context.Background(), ch)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if resp.GetRecordsWritten() != 2 {
		t.Errorf("expected 2 records written, got %d", resp.GetRecordsWritten())
	}
}
