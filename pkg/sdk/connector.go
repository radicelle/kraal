package sdk

import (
	"context"

	protocolv1 "github.com/radicelle/kraal/pkg/protocol/v1"
)

// Connector defines the standard contract that all Kraal connectors must implement.
// It is completely agnostic of Kraal's internal database models, migrations, and storage logic.
type Connector interface {
	// Spec returns connector metadata and JSON Schema for configuration.
	Spec(ctx context.Context) (*protocolv1.SpecResponse, error)

	// Check verifies connectivity with external data source.
	Check(ctx context.Context, configJSON string) (*protocolv1.CheckResponse, error)

	// Discover inspects data source and returns available streams/tables and schemas.
	Discover(ctx context.Context, configJSON string) (*protocolv1.DiscoverResponse, error)

	// Read extracts records from the data source and calls emit for each record.
	Read(ctx context.Context, req *protocolv1.ReadRequest, emit func(record *protocolv1.RecordEnvelope) error) error

	// Write ingests records into the external target (sink mode).
	Write(ctx context.Context, records <-chan *protocolv1.RecordEnvelope) (*protocolv1.WriteResponse, error)
}
