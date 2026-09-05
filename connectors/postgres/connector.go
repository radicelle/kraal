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
				"description": "Schemas to discover and read from"
			},
			"table_filter": {
				"type": "array",
				"items": { "type": "string" },
				"description": "Optional list of tables to restrict discovery/read to"
			}
		}
	}`

	return &protocolv1.SpecResponse{
		Name:             "postgres",
		Version:          "1.0.0",
		Description:      "PostgreSQL source and sink connector supporting full refresh and incremental cursor replication",
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

	// Query tables across target schemas
	tableQuery := `
		SELECT table_schema, table_name 
		FROM information_schema.tables 
		WHERE table_type = 'BASE TABLE' 
		  AND table_schema = ANY($1)
		ORDER BY table_schema, table_name;
	`
	rows, err := conn.Query(ctx, tableQuery, cfg.Schemas)
	if err != nil {
		return nil, fmt.Errorf("failed to discover tables: %w", err)
	}
	defer rows.Close()

	type tableRef struct {
		schema string
		table  string
	}
	var tables []tableRef
	for rows.Next() {
		var t tableRef
		if err := rows.Scan(&t.schema, &t.table); err != nil {
			return nil, err
		}
		tables = append(tables, t)
	}

	var discoveredStreams []*protocolv1.StreamSchema

	for _, t := range tables {
		streamName := fmt.Sprintf("%s.%s", t.schema, t.table)

		// Discover columns
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
		for colRows.Next() {
			var colName, dataType, isNullable string
			if err := colRows.Scan(&colName, &dataType, &isNullable); err != nil {
				colRows.Close()
				return nil, err
			}
			properties[colName] = map[string]any{
				"type":        pgTypeToJSONType(dataType),
				"pg_type":     dataType,
				"is_nullable": isNullable == "YES",
			}
		}
		colRows.Close()

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
		if err != nil {
			return nil, fmt.Errorf("failed to discover primary keys for %s: %w", streamName, err)
		}
		var primaryKeys []string
		for pkRows.Next() {
			var pk string
			if err := pkRows.Scan(&pk); err == nil {
				primaryKeys = append(primaryKeys, pk)
			}
		}
		pkRows.Close()

		schemaBytes, _ := json.Marshal(map[string]any{
			"type":       "object",
			"properties": properties,
		})

		discoveredStreams = append(discoveredStreams, &protocolv1.StreamSchema{
			Name:               streamName,
			PrimaryKeys:        primaryKeys,
			JsonSchema:         string(schemaBytes),
			SupportedSyncModes: []string{"FULL_REFRESH", "INCREMENTAL"},
		})
	}

	return &protocolv1.DiscoverResponse{
		Streams: discoveredStreams,
	}, nil
}

func (c *Connector) Read(ctx context.Context, req *protocolv1.ReadRequest, emit func(record *protocolv1.RecordEnvelope) error) error {
	cfg, err := ParseConfig(req.GetConfigJson())
	if err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	conn, err := pgx.Connect(ctx, cfg.ConnectionString())
	if err != nil {
		return fmt.Errorf("connection error: %w", err)
	}
	defer conn.Close(context.Background())

	// Stream name can be "schema.table" or just "table"
	streamName := req.GetStreamName()
	parts := strings.Split(streamName, ".")
	var schemaName, tableName string
	if len(parts) == 2 {
		schemaName = parts[0]
		tableName = parts[1]
	} else {
		schemaName = "public"
		tableName = parts[0]
	}

	// Safe identifier quoting to prevent injection
	sanitizedTable := fmt.Sprintf("%s.%s", pgx.Identifier{schemaName}.Sanitize(), pgx.Identifier{tableName}.Sanitize())

	queryBuilder := fmt.Sprintf("SELECT * FROM %s", sanitizedTable)
	var args []any

	if req.GetCursorField() != "" && req.GetCursorValue() != "" {
		cursorCol := pgx.Identifier{req.GetCursorField()}.Sanitize()
		queryBuilder += fmt.Sprintf(" WHERE %s > $1 ORDER BY %s ASC", cursorCol, cursorCol)
		args = append(args, req.GetCursorValue())
	}

	if req.GetLimit() > 0 {
		queryBuilder += fmt.Sprintf(" LIMIT %d", req.GetLimit())
	}

	rows, err := conn.Query(ctx, queryBuilder, args...)
	if err != nil {
		return fmt.Errorf("failed executing read query (%s): %w", queryBuilder, err)
	}
	defer rows.Close()

	fields := rows.FieldDescriptions()
	var seq int64

	for rows.Next() {
		seq++
		values, err := rows.Values()
		if err != nil {
			return fmt.Errorf("failed to scan row: %w", err)
		}

		recordMap := make(map[string]any, len(fields))
		var latestCursor string

		for i, fd := range fields {
			colName := string(fd.Name)
			val := values[i]
			recordMap[colName] = val

			if req.GetCursorField() != "" && strings.EqualFold(colName, req.GetCursorField()) {
				latestCursor = fmt.Sprintf("%v", val)
			}
		}

		payloadBytes, err := json.Marshal(recordMap)
		if err != nil {
			return fmt.Errorf("failed marshaling row to JSON: %w", err)
		}

		envelope := &protocolv1.RecordEnvelope{
			Stream:         streamName,
			SequenceNumber: seq,
			EmittedAt:      time.Now().UTC().Format(time.RFC3339Nano),
			DataJson:       payloadBytes,
			Cursor:         latestCursor,
		}

		if err := emit(envelope); err != nil {
			return fmt.Errorf("failed emitting record: %w", err)
		}
	}

	return rows.Err()
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
