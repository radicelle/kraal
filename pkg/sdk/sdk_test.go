package sdk_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"testing"

	protocolv1 "github.com/radicelle/kraal/pkg/protocol/v1"
	"github.com/radicelle/kraal/pkg/sdk"
	"google.golang.org/grpc"
)

type mockConnector struct{}

func (m *mockConnector) Spec(ctx context.Context) (*protocolv1.SpecResponse, error) {
	return &protocolv1.SpecResponse{
		Name:             "mock",
		Version:          "0.1.0",
		Description:      "Mock connector for testing",
		ConfigSchemaJson: "{}",
	}, nil
}

func (m *mockConnector) Check(ctx context.Context, configJSON string) (*protocolv1.CheckResponse, error) {
	if configJSON == "valid" {
		return &protocolv1.CheckResponse{
			Status:  protocolv1.CheckStatus_CHECK_STATUS_SUCCESS,
			Message: "OK",
		}, nil
	}
	return &protocolv1.CheckResponse{
		Status:  protocolv1.CheckStatus_CHECK_STATUS_FAILED,
		Message: "Invalid config",
	}, nil
}

func (m *mockConnector) Discover(ctx context.Context, configJSON string) (*protocolv1.DiscoverResponse, error) {
	return &protocolv1.DiscoverResponse{
		Streams: []*protocolv1.StreamSchema{
			{Name: "users", PrimaryKeys: []string{"id"}},
		},
	}, nil
}

func (m *mockConnector) Read(ctx context.Context, req *protocolv1.ReadRequest, emit func(record *protocolv1.RecordEnvelope) error) error {
	for i := int64(1); i <= 3; i++ {
		err := emit(&protocolv1.RecordEnvelope{
			Stream:         req.GetStreamName(),
			SequenceNumber: i,
			DataJson:       []byte(fmt.Sprintf(`{"id": %d}`, i)),
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *mockConnector) Write(ctx context.Context, records <-chan *protocolv1.RecordEnvelope) (*protocolv1.WriteResponse, error) {
	var count int64
	for range records {
		count++
	}
	return &protocolv1.WriteResponse{
		RecordsWritten: count,
		Message:        "Written successfully",
	}, nil
}

func TestSDKServerAndClient(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	addr := listener.Addr().String()

	grpcServer := grpc.NewServer()
	protocolv1.RegisterConnectorServiceServer(grpcServer, sdk.NewGRPCServer(&mockConnector{}))

	go func() {
		_ = grpcServer.Serve(listener)
	}()
	defer grpcServer.Stop()

	client, err := sdk.NewClient(addr)
	if err != nil {
		t.Fatalf("Failed to connect client: %v", err)
	}
	defer client.Close()

	svc := client.Service()
	ctx := context.Background()

	// 1. Test Spec
	specResp, err := svc.Spec(ctx, &protocolv1.SpecRequest{})
	if err != nil {
		t.Fatalf("Spec failed: %v", err)
	}
	if specResp.GetName() != "mock" {
		t.Errorf("expected mock name, got %s", specResp.GetName())
	}

	// 2. Test Check
	checkResp, err := svc.Check(ctx, &protocolv1.CheckRequest{ConfigJson: "valid"})
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if checkResp.GetStatus() != protocolv1.CheckStatus_CHECK_STATUS_SUCCESS {
		t.Errorf("expected success, got %v", checkResp.GetStatus())
	}

	// 3. Test Read stream
	stream, err := svc.Read(ctx, &protocolv1.ReadRequest{StreamName: "users"})
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	var count int
	for {
		rec, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Recv failed: %v", err)
		}
		count++
		if rec.GetStream() != "users" {
			t.Errorf("unexpected stream: %s", rec.GetStream())
		}
	}
	if count != 3 {
		t.Errorf("expected 3 records, got %d", count)
	}
}
