package hubspot

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	protocolv1 "github.com/radicelle/kraal/pkg/protocol/v1"
	"github.com/radicelle/kraal/pkg/sdk"
)

// Ensure Connector implements sdk.Connector at compile time.
var _ sdk.Connector = (*Connector)(nil)

type Connector struct{}

func NewConnector() *Connector {
	return &Connector{}
}

func (c *Connector) Spec(ctx context.Context) (*protocolv1.SpecResponse, error) {
	schema := `{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"title": "HubSpotConfig",
		"type": "object",
		"required": ["access_token"],
		"properties": {
			"access_token": {
				"type": "string",
				"description": "HubSpot Private App Access Token (Bearer token)"
			},
			"base_url": {
				"type": "string",
				"default": "https://api.hubapi.com",
				"description": "Base URL of the HubSpot API"
			},
			"object_types": {
				"type": "array",
				"items": { "type": "string" },
				"default": ["contacts", "companies", "deals", "tickets", "products", "line_items", "quotes"],
				"description": "HubSpot CRM object types to catalog"
			},
			"include_custom_objects": {
				"type": "boolean",
				"default": true,
				"description": "Whether to discover and catalog custom object schemas"
			},
			"include_associations": {
				"type": "boolean",
				"default": true,
				"description": "Whether to extract object association lineage edges"
			}
		}
	}`

	return &protocolv1.SpecResponse{
		Name:             "hubspot",
		Version:          "1.0.0",
		Description:      "HubSpot CRM metadata & lineage connector extracting CRM object definitions, properties, and association graphs",
		ConfigSchemaJson: schema,
	}, nil
}

func (c *Connector) Check(ctx context.Context, configJSON string) (*protocolv1.CheckResponse, error) {
	cfg, err := ParseConfig(configJSON)
	if err != nil {
		return &protocolv1.CheckResponse{
			Status:  protocolv1.CheckStatus_CHECK_STATUS_FAILED,
			Message: fmt.Sprintf("invalid configuration: %v", err),
		}, nil
	}

	client := NewClient(cfg)
	if err := client.Ping(ctx); err != nil {
		return &protocolv1.CheckResponse{
			Status:  protocolv1.CheckStatus_CHECK_STATUS_FAILED,
			Message: fmt.Sprintf("HubSpot connection check failed: %v", err),
		}, nil
	}

	return &protocolv1.CheckResponse{
		Status:  protocolv1.CheckStatus_CHECK_STATUS_SUCCESS,
		Message: "successfully authenticated with HubSpot CRM API",
	}, nil
}

func (c *Connector) Discover(ctx context.Context, configJSON string) (*protocolv1.DiscoverResponse, error) {
	cfg, err := ParseConfig(configJSON)
	if err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	client := NewClient(cfg)
	var discoveredStreams []*protocolv1.StreamSchema
	var globalRelations []*protocolv1.RelationEdge

	// 1. Discover standard CRM objects
	for _, objType := range cfg.ObjectTypes {
		props, err := client.GetProperties(ctx, objType)
		if err != nil {
			// Continue with other objects if one fails or is not enabled for this portal
			continue
		}

		schema, fields := buildStreamSchemaFromProperties(objType, "CRM_OBJECT", props)
		discoveredStreams = append(discoveredStreams, schema)
		_ = fields
	}

	// 2. Discover custom objects if enabled
	if cfg.IncludeCustomObjects {
		customSchemas, err := client.GetCustomSchemas(ctx)
		if err == nil {
			for _, cs := range customSchemas {
				name := cs.Name
				if name == "" {
					name = cs.FullyQualifiedName
				}
				schema, _ := buildStreamSchemaFromProperties(name, "CUSTOM_OBJECT", cs.Properties)
				schema.Description = cs.Description

				// Extract custom object associations
				for _, assoc := range cs.Associations {
					edge := &protocolv1.RelationEdge{
						SourceEntity: name,
						SourceField:  "hs_object_id",
						TargetEntity: assoc.ToObjectTypeId,
						TargetField:  "hs_object_id",
						RelationType: "ASSOCIATION",
						Description:  fmt.Sprintf("Custom association: %s (%s -> %s)", assoc.Name, name, assoc.ToObjectTypeId),
					}
					schema.Relations = append(schema.Relations, edge)
					globalRelations = append(globalRelations, edge)
				}

				discoveredStreams = append(discoveredStreams, schema)
			}
		}
	}

	// 3. Attach standard association lineage edges
	if cfg.IncludeAssociations {
		stdRelations := StandardLineageRelations()
		globalRelations = append(globalRelations, stdRelations...)

		// Link outgoing edges to corresponding stream schemas
		edgeMap := make(map[string][]*protocolv1.RelationEdge)
		for _, rel := range stdRelations {
			edgeMap[rel.SourceEntity] = append(edgeMap[rel.SourceEntity], rel)
		}

		for _, stream := range discoveredStreams {
			if edges, exists := edgeMap[stream.Name]; exists {
				stream.Relations = append(stream.Relations, edges...)
			}
		}
	}

	return &protocolv1.DiscoverResponse{
		Streams:   discoveredStreams,
		Relations: globalRelations,
	}, nil
}

func (c *Connector) Read(ctx context.Context, req *protocolv1.ReadRequest, emit func(record *protocolv1.RecordEnvelope) error) error {
	discoverResp, err := c.Discover(ctx, req.GetConfigJson())
	if err != nil {
		return fmt.Errorf("lineage discovery failed during read: %w", err)
	}

	var seq int64
	streamFilter := req.GetStreamName()

	// 1. Emit entity schemas
	for _, stream := range discoverResp.GetStreams() {
		if streamFilter != "" && streamFilter != "lineage" && streamFilter != "metadata" && streamFilter != stream.GetName() {
			continue
		}
		seq++
		dataBytes, err := json.Marshal(map[string]any{
			"entity":       stream.GetName(),
			"type":         stream.GetEntityType(),
			"description":  stream.GetDescription(),
			"primary_keys": stream.GetPrimaryKeys(),
			"fields":       stream.GetFields(),
			"relations":    stream.GetRelations(),
		})
		if err != nil {
			return err
		}

		envelope := &protocolv1.RecordEnvelope{
			Stream:         stream.GetName(),
			SequenceNumber: seq,
			EmittedAt:      time.Now().UTC().Format(time.RFC3339Nano),
			DataJson:       dataBytes,
			RecordType:     "ENTITY",
		}
		if err := emit(envelope); err != nil {
			return err
		}
	}

	// 2. Emit global lineage relationship edges
	if streamFilter == "" || streamFilter == "lineage" {
		for _, rel := range discoverResp.GetRelations() {
			seq++
			relBytes, err := json.Marshal(rel)
			if err != nil {
				return err
			}

			envelope := &protocolv1.RecordEnvelope{
				Stream:         "lineage.relations",
				SequenceNumber: seq,
				EmittedAt:      time.Now().UTC().Format(time.RFC3339Nano),
				DataJson:       relBytes,
				RecordType:     "RELATION",
			}
			if err := emit(envelope); err != nil {
				return err
			}
		}
	}

	return nil
}

func (c *Connector) Write(ctx context.Context, records <-chan *protocolv1.RecordEnvelope) (*protocolv1.WriteResponse, error) {
	var written, failed int64
	for record := range records {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if len(record.GetDataJson()) > 0 {
			written++
		} else {
			failed++
		}
	}

	return &protocolv1.WriteResponse{
		RecordsWritten: written,
		RecordsFailed:  failed,
		Message:        fmt.Sprintf("processed %d metadata records (%d failed)", written, failed),
	}, nil
}

func buildStreamSchemaFromProperties(objName, entityType string, props []PropertyDefinition) (*protocolv1.StreamSchema, []*protocolv1.FieldMetadata) {
	jsonProperties := make(map[string]map[string]any)
	var fieldMetas []*protocolv1.FieldMetadata

	// Standard primary key in HubSpot is hs_object_id
	jsonProperties["hs_object_id"] = map[string]any{
		"type":        "string",
		"description": "Unique identifier of the CRM record",
		"primary_key": true,
	}
	fieldMetas = append(fieldMetas, &protocolv1.FieldMetadata{
		Name:         "hs_object_id",
		DataType:     "string",
		NativeType:   "string",
		IsPrimaryKey: true,
		Description:  "Unique identifier of the CRM record",
	})

	for _, p := range props {
		if p.Archived || p.Name == "hs_object_id" {
			continue
		}
		dataType := MapHubSpotTypeToJSONType(p.Type)
		desc := p.Description
		if desc == "" {
			desc = p.Label
		}

		jsonProperties[p.Name] = map[string]any{
			"type":        dataType,
			"native_type": p.Type,
			"field_type":  p.FieldType,
			"label":       p.Label,
			"description": desc,
			"group_name":  p.GroupName,
			"calculated":  p.Calculated,
		}

		fieldMetas = append(fieldMetas, &protocolv1.FieldMetadata{
			Name:         p.Name,
			DataType:     dataType,
			NativeType:   p.Type,
			IsPrimaryKey: false,
			Description:  desc,
		})
	}

	schemaBytes, _ := json.Marshal(map[string]any{
		"type":       "object",
		"properties": jsonProperties,
	})

	return &protocolv1.StreamSchema{
		Name:               objName,
		PrimaryKeys:        []string{"hs_object_id"},
		JsonSchema:         string(schemaBytes),
		SupportedSyncModes: []string{"METADATA", "LINEAGE"},
		Fields:             fieldMetas,
		EntityType:         entityType,
	}, fieldMetas
}
