package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	protocolv1 "github.com/radicelle/kraal/pkg/protocol/v1"
	"github.com/radicelle/kraal/pkg/sdk"
)

func main() {
	mode := flag.String("mode", "desktop", "Execution mode: 'desktop' (spawns local subprocess) or 'cloud' (connects to remote address)")
	connectorBin := flag.String("binary", "./connector-postgres", "Path to connector binary (Desktop mode)")
	remoteAddr := flag.String("remote", "127.0.0.1:50051", "Remote connector gRPC address (Cloud mode)")
	action := flag.String("action", "spec", "Action to perform: spec, check, discover, sync")
	configJSON := flag.String("config", "{}", "JSON connection configuration for check/discover/sync")
	streamName := flag.String("stream", "public.users", "Stream/table to read in sync action")
	cursorField := flag.String("cursor-field", "", "Cursor field name for incremental sync")
	cursorVal := flag.String("cursor-val", "", "Starting cursor value for incremental sync")
	limit := flag.Int64("limit", 10, "Record limit for sync (0 for unlimited)")

	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var client *sdk.Client
	var cleanup func()

	if *mode == "desktop" {
		log.Printf("[Desktop Mode] Spawning connector binary: %s on ephemeral local port...\n", *connectorBin)
		sub, err := sdk.LaunchSubprocess(ctx, *connectorBin)
		if err != nil {
			log.Fatalf("Failed to launch connector in desktop mode: %v", err)
		}
		client = sub.Client
		cleanup = func() { _ = sub.Close() }
		log.Printf("[Desktop Mode] Successfully connected to connector at %s\n", sub.Address)
	} else {
		log.Printf("[Cloud Mode] Connecting to remote connector at: %s...\n", *remoteAddr)
		c, err := sdk.NewClient(*remoteAddr)
		if err != nil {
			log.Fatalf("Failed to connect to remote connector: %v", err)
		}
		client = c
		cleanup = func() { _ = client.Close() }
		log.Println("[Cloud Mode] Successfully connected to remote connector")
	}
	defer cleanup()

	svc := client.Service()

	switch *action {
	case "spec":
		resp, err := svc.Spec(ctx, &protocolv1.SpecRequest{})
		if err != nil {
			log.Fatalf("Spec failed: %v", err)
		}
		fmt.Println("=== Connector Specification ===")
		fmt.Printf("Name:        %s\n", resp.GetName())
		fmt.Printf("Version:     %s\n", resp.GetVersion())
		fmt.Printf("Description: %s\n", resp.GetDescription())
		fmt.Printf("Config Schema:\n%s\n", resp.GetConfigSchemaJson())

	case "check":
		resp, err := svc.Check(ctx, &protocolv1.CheckRequest{ConfigJson: *configJSON})
		if err != nil {
			log.Fatalf("Check failed: %v", err)
		}
		fmt.Printf("Status:  %s\n", resp.GetStatus().String())
		fmt.Printf("Message: %s\n", resp.GetMessage())

	case "discover":
		resp, err := svc.Discover(ctx, &protocolv1.DiscoverRequest{ConfigJson: *configJSON})
		if err != nil {
			log.Fatalf("Discover failed: %v", err)
		}
		fmt.Printf("=== Discovered %d Entity Stream(s) ===\n", len(resp.GetStreams()))
		for _, s := range resp.GetStreams() {
			fmt.Printf("• %s [%s] (PKs: %v, Fields: %d, Outgoing Relations: %d)\n",
				s.GetName(), s.GetEntityType(), s.GetPrimaryKeys(), len(s.GetFields()), len(s.GetRelations()))
			for _, f := range s.GetFields() {
				pkTag := ""
				if f.GetIsPrimaryKey() {
					pkTag = " [PK]"
				}
				fmt.Printf("    - %s (%s)%s: %s\n", f.GetName(), f.GetDataType(), pkTag, f.GetDescription())
			}
			for _, r := range s.GetRelations() {
				fmt.Printf("    → Relation: %s.%s -> %s.%s (%s)\n",
					r.GetSourceEntity(), r.GetSourceField(), r.GetTargetEntity(), r.GetTargetField(), r.GetRelationType())
			}
		}

		if len(resp.GetRelations()) > 0 {
			fmt.Printf("\n=== Global Lineage Graph (%d Edges) ===\n", len(resp.GetRelations()))
			for _, r := range resp.GetRelations() {
				fmt.Printf("  %s.%s ---> %s.%s [%s]\n    Description: %s\n",
					r.GetSourceEntity(), r.GetSourceField(),
					r.GetTargetEntity(), r.GetTargetField(),
					r.GetRelationType(), r.GetDescription())
			}
		}

	case "sync":
		log.Printf("Starting lineage & metadata sync (filter='%s')...\n", *streamName)
		stream, err := svc.Read(ctx, &protocolv1.ReadRequest{
			ConfigJson:  *configJSON,
			StreamName:  *streamName,
			CursorField: *cursorField,
			CursorValue: *cursorVal,
			Limit:       *limit,
		})
		if err != nil {
			log.Fatalf("Read failed: %v", err)
		}

		var count int64
		for {
			record, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				log.Fatalf("Stream error: %v", err)
			}
			count++
			fmt.Printf("[%s #%d | Type=%s] %s\n",
				record.GetStream(),
				record.GetSequenceNumber(),
				record.GetRecordType(),
				string(record.GetDataJson()),
			)
		}
		log.Printf("Sync complete. Ingested %d metadata/lineage record(s) into Host catalog.\n", count)

	default:
		fmt.Fprintf(os.Stderr, "Unknown action: %s\n", *action)
		os.Exit(1)
	}
}
