package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
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
		"title": "PostgresConfig",
		"type": "object",
		"required": ["host", "database"],
		"properties": {
			"host": { "type": "string", "description": "Hostname of the PostgreSQL server" },
			"port": { "type": "integer", "default": 5432, "description": "Port of the PostgreSQL server" },
			"user": { "type": "string", "description": "Database username" },
			"password": { "type": "string", "description": "Database password" },
			"database": { "type": "string", "description": "Database name" },
			"sslmode": { 
				"type": "string", 
				"enum": ["disable", "require", "verify-ca", "verify-full"], 
				"default": "disable",
				"description": "SSL connection mode"
			},
			"schemas": { 
				"type": "array", 
				"items": { "type": "string" }, 
				"default": ["public"],
				"description": "Schemas to discover and read metadata from"
			},
			"table_filter": {
				"type": "array",
				"items": { "type": "string" },
				"description": "Optional list of tables to restrict discovery to"
			}
		}
	}`

	return &protocolv1.SpecResponse{
		Name:             "postgres",
		Version:          "1.1.0",
		Description:      "PostgreSQL lineage & metadata connector extracting tables, views, columns, and foreign key lineage graph",
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

	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	conn, err := pgx.Connect(checkCtx, cfg.ConnectionString())
	if err != nil {
		return &protocolv1.CheckResponse{
			Status:  protocolv1.CheckStatus_CHECK_STATUS_FAILED,
			Message: fmt.Sprintf("failed to connect to postgres: %v", err),
		}, nil
	}
	defer conn.Close(context.Background())

	var result int
	if err := conn.QueryRow(checkCtx, "SELECT 1;").Scan(&result); err != nil {
		return &protocolv1.CheckResponse{
			Status:  protocolv1.CheckStatus_CHECK_STATUS_FAILED,
			Message: fmt.Sprintf("ping query failed: %v", err),
		}, nil
	}

	return &protocolv1.CheckResponse{
		Status:  protocolv1.CheckStatus_CHECK_STATUS_SUCCESS,
		Message: "successfully connected to PostgreSQL database",
	}, nil
}

func (c *Connector) Discover(ctx context.Context, configJSON string) (*protocolv1.DiscoverResponse, error) {
	cfg, err := ParseConfig(configJSON)
	if err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	conn, err := pgx.Connect(ctx, cfg.ConnectionString())
	if err != nil {
		return nil, fmt.Errorf("connection error: %w", err)
	}
	defer conn.Close(context.Background())

	// Query tables and views across target schemas
	tableQuery := `
		SELECT table_schema, table_name, table_type 
		FROM information_schema.tables 
		WHERE table_type IN ('BASE TABLE', 'VIEW') 
		  AND table_schema = ANY($1)
		ORDER BY table_schema, table_name;
	`
	rows, err := conn.Query(ctx, tableQuery, cfg.Schemas)
	if err != nil {
		return nil, fmt.Errorf("failed to discover tables: %w", err)
	}
	defer rows.Close()

	type tableRef struct {
		schema    string
		table     string
		tableType string
	}
	var tables []tableRef
	for rows.Next() {
		var t tableRef
		if err := rows.Scan(&t.schema, &t.table, &t.tableType); err != nil {
			return nil, err
		}
		tables = append(tables, t)
	}

	// Query all Foreign Key relations across target schemas
	fkQuery := `
		SELECT
			tc.table_schema AS src_schema,
			tc.table_name AS src_table,
			kcu.column_name AS src_column,
			ccu.table_schema AS tgt_schema,
			ccu.table_name AS tgt_table,
			ccu.column_name AS tgt_column,
			tc.constraint_name
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
		  ON tc.constraint_name = kcu.constraint_name
		  AND tc.table_schema = kcu.table_schema
		JOIN information_schema.constraint_column_usage ccu
		  ON ccu.constraint_name = tc.constraint_name
		  AND ccu.table_schema = tc.table_schema
		WHERE tc.constraint_type = 'FOREIGN KEY'
		  AND tc.table_schema = ANY($1)
		ORDER BY tc.table_schema, tc.table_name, kcu.ordinal_position;
	`
	var globalRelations []*protocolv1.RelationEdge
	tableRelationsMap := make(map[string][]*protocolv1.RelationEdge)

	fkRows, err := conn.Query(ctx, fkQuery, cfg.Schemas)
	if err == nil {
		for fkRows.Next() {
			var srcSchema, srcTable, srcCol, tgtSchema, tgtTable, tgtCol, constraintName string
			if err := fkRows.Scan(&srcSchema, &srcTable, &srcCol, &tgtSchema, &tgtTable, &tgtCol, &constraintName); err == nil {
				srcEntity := fmt.Sprintf("%s.%s", srcSchema, srcTable)
				tgtEntity := fmt.Sprintf("%s.%s", tgtSchema, tgtTable)
				edge := &protocolv1.RelationEdge{
					SourceEntity: srcEntity,
					SourceField:  srcCol,
					TargetEntity: tgtEntity,
					TargetField:  tgtCol,
					RelationType: "FOREIGN_KEY",
					Description:  fmt.Sprintf("Foreign key %s linking %s.%s to %s.%s", constraintName, srcEntity, srcCol, tgtEntity, tgtCol),
				}
				globalRelations = append(globalRelations, edge)
				tableRelationsMap[srcEntity] = append(tableRelationsMap[srcEntity], edge)
			}
		}
		fkRows.Close()
	}

	// Query View dependencies for lineage
	viewQuery := `
		SELECT 
			view_schema, 
			view_name, 
			table_schema, 
			table_name 
		FROM information_schema.view_table_usage
		WHERE view_schema = ANY($1);
	`
	viewRows, err := conn.Query(ctx, viewQuery, cfg.Schemas)
	if err == nil {
		for viewRows.Next() {
			var vSchema, vName, tSchema, tName string
			if err := viewRows.Scan(&vSchema, &vName, &tSchema, &tName); err == nil {
				vEntity := fmt.Sprintf("%s.%s", vSchema, vName)
				tEntity := fmt.Sprintf("%s.%s", tSchema, tName)
				edge := &protocolv1.RelationEdge{
					SourceEntity: vEntity,
					SourceField:  "*",
					TargetEntity: tEntity,
					TargetField:  "*",
					RelationType: "VIEW_DEPENDENCY",
					Description:  fmt.Sprintf("View %s derives data from %s", vEntity, tEntity),
				}
				globalRelations = append(globalRelations, edge)
				tableRelationsMap[vEntity] = append(tableRelationsMap[vEntity], edge)
			}
		}
		viewRows.Close()
	}

	var discoveredStreams []*protocolv1.StreamSchema

	for _, t := range tables {
		streamName := fmt.Sprintf("%s.%s", t.schema, t.table)

		// Discover Primary Keys
		pkQuery := `
			SELECT kcu.column_name
			FROM information_schema.table_constraints tc
			JOIN information_schema.key_column_usage kcu
			  ON tc.constraint_name = kcu.constraint_name
			  AND tc.table_schema = kcu.table_schema
			WHERE tc.constraint_type = 'PRIMARY KEY'
			  AND tc.table_schema = $1 AND tc.table_name = $2
			ORDER BY kcu.ordinal_position;
		`
		pkRows, err := conn.Query(ctx, pkQuery, t.schema, t.table)
		pkSet := make(map[string]bool)
		var primaryKeys []string
		if err == nil {
			for pkRows.Next() {
				var pk string
				if err := pkRows.Scan(&pk); err == nil {
					primaryKeys = append(primaryKeys, pk)
					pkSet[pk] = true
				}
			}
			pkRows.Close()
		}

		// Discover columns and attributes
		colQuery := `
			SELECT column_name, data_type, is_nullable
			FROM information_schema.columns
			WHERE table_schema = $1 AND table_name = $2
			ORDER BY ordinal_position;
		`
		colRows, err := conn.Query(ctx, colQuery, t.schema, t.table)
		if err != nil {
			return nil, fmt.Errorf("failed to discover columns for %s: %w", streamName, err)
		}

		properties := make(map[string]map[string]any)
		var fieldMetas []*protocolv1.FieldMetadata

		for colRows.Next() {
			var colName, dataType, isNullable string
			if err := colRows.Scan(&colName, &dataType, &isNullable); err != nil {
				colRows.Close()
				return nil, err
			}
			jsonType := pgTypeToJSONType(dataType)
			isNullableBool := isNullable == "YES"
			isPK := pkSet[colName]

			properties[colName] = map[string]any{
				"type":        jsonType,
				"pg_type":     dataType,
				"is_nullable": isNullableBool,
				"primary_key": isPK,
			}

			fieldMetas = append(fieldMetas, &protocolv1.FieldMetadata{
				Name:         colName,
				DataType:     jsonType,
				NativeType:   dataType,
				IsPrimaryKey: isPK,
				IsNullable:   isNullableBool,
			})
		}
		colRows.Close()

		schemaBytes, _ := json.Marshal(map[string]any{
			"type":       "object",
			"properties": properties,
		})

		entityKind := "TABLE"
		if t.tableType == "VIEW" {
			entityKind = "VIEW"
		}

		discoveredStreams = append(discoveredStreams, &protocolv1.StreamSchema{
			Name:               streamName,
			PrimaryKeys:        primaryKeys,
			JsonSchema:         string(schemaBytes),
			SupportedSyncModes: []string{"METADATA", "LINEAGE"},
			Fields:             fieldMetas,
			Relations:          tableRelationsMap[streamName],
			EntityType:         entityKind,
		})
	}

	return &protocolv1.DiscoverResponse{
		Streams:   discoveredStreams,
		Relations: globalRelations,
	}, nil
}

func (c *Connector) Read(ctx context.Context, req *protocolv1.ReadRequest, emit func(record *protocolv1.RecordEnvelope) error) error {
	// Discover all metadata and lineage
	discoverResp, err := c.Discover(ctx, req.GetConfigJson())
	if err != nil {
		return fmt.Errorf("lineage discovery failed during read: %w", err)
	}

	var seq int64
	streamFilter := req.GetStreamName()

	// 1. Emit entity metadata
	for _, stream := range discoverResp.GetStreams() {
		if streamFilter != "" && streamFilter != "lineage" && streamFilter != "metadata" && streamFilter != stream.GetName() {
			continue
		}
		seq++
		dataBytes, err := json.Marshal(map[string]any{
			"entity":       stream.GetName(),
			"type":         stream.GetEntityType(),
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

	// Consume and process records from the channel
	for record := range records {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// A full sink implementation would batch insert/upsert into destination table.
		// For demonstration and testing of the contract:
		if len(record.GetDataJson()) > 0 {
			written++
		} else {
			failed++
		}
	}

	return &protocolv1.WriteResponse{
		RecordsWritten: written,
		RecordsFailed:  failed,
		Message:        fmt.Sprintf("successfully processed %d records (%d failed)", written, failed),
	}, nil
}

func pgTypeToJSONType(pgType string) string {
	lower := strings.ToLower(pgType)
	switch {
	case strings.Contains(lower, "int"), strings.Contains(lower, "serial"):
		return "integer"
	case strings.Contains(lower, "numeric"), strings.Contains(lower, "decimal"),
		strings.Contains(lower, "real"), strings.Contains(lower, "double"), strings.Contains(lower, "float"):
		return "number"
	case strings.Contains(lower, "bool"):
		return "boolean"
	case strings.Contains(lower, "json"):
		return "object"
	default:
		return "string"
	}
}
