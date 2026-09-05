package sdk

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	protocolv1 "github.com/radicelle/kraal/pkg/protocol/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// ReadySignalPrefix is printed to stdout when the connector is listening.
// The Host launcher listens for this signal in Desktop mode.
const ReadySignalPrefix = "[KRAAL_READY] address="

// GRPCServer bridges the Connector interface to gRPC.
type GRPCServer struct {
	protocolv1.UnimplementedConnectorServiceServer
	connector Connector
}

func NewGRPCServer(connector Connector) *GRPCServer {
	return &GRPCServer{connector: connector}
}

func (s *GRPCServer) Spec(ctx context.Context, _ *protocolv1.SpecRequest) (*protocolv1.SpecResponse, error) {
	return s.connector.Spec(ctx)
}

func (s *GRPCServer) Check(ctx context.Context, req *protocolv1.CheckRequest) (*protocolv1.CheckResponse, error) {
	return s.connector.Check(ctx, req.GetConfigJson())
}

func (s *GRPCServer) Discover(ctx context.Context, req *protocolv1.DiscoverRequest) (*protocolv1.DiscoverResponse, error) {
	return s.connector.Discover(ctx, req.GetConfigJson())
}

func (s *GRPCServer) Read(req *protocolv1.ReadRequest, stream grpc.ServerStreamingServer[protocolv1.RecordEnvelope]) error {
	return s.connector.Read(stream.Context(), req, func(record *protocolv1.RecordEnvelope) error {
		return stream.Send(record)
	})
}

func (s *GRPCServer) Write(stream grpc.ClientStreamingServer[protocolv1.RecordEnvelope, protocolv1.WriteResponse]) error {
	recordChan := make(chan *protocolv1.RecordEnvelope, 100)
	errChan := make(chan error, 1)

	go func() {
		defer close(recordChan)
		for {
			rec, err := stream.Recv()
			if err == io.EOF {
				return
			}
			if err != nil {
				errChan <- err
				return
			}
			recordChan <- rec
		}
	}()

	resp, err := s.connector.Write(stream.Context(), recordChan)
	if err != nil {
		return err
	}

	select {
	case streamErr := <-errChan:
		return streamErr
	default:
	}

	return stream.SendAndClose(resp)
}

// Serve starts the gRPC server on the specified address and blocks until terminated.
func Serve(connector Connector, address string) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", address, err)
	}

	grpcServer := grpc.NewServer()
	protocolv1.RegisterConnectorServiceServer(grpcServer, NewGRPCServer(connector))
	reflection.Register(grpcServer)

	// Emit ready signal with the actual assigned address (crucial for Desktop mode on ephemeral port)
	actualAddr := listener.Addr().String()
	fmt.Printf("%s%s\n", ReadySignalPrefix, actualAddr)
	_ = os.Stdout.Sync()

	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-stopChan
		log.Println("Shutting down connector server...")
		grpcServer.GracefulStop()
	}()

	log.Printf("Connector service listening on %s\n", actualAddr)
	if err := grpcServer.Serve(listener); err != nil && err != grpc.ErrServerStopped {
		return fmt.Errorf("gRPC server error: %w", err)
	}
	return nil
}

// Main is a helper for connector main() functions to parse CLI flags and run the server.
func Main(connector Connector) {
	listenAddr := flag.String("listen", "127.0.0.1:0", "TCP address to listen on (e.g. 0.0.0.0:50051 or 127.0.0.1:0 for ephemeral port)")
	flag.Parse()

	if err := Serve(connector, *listenAddr); err != nil {
		log.Fatalf("Connector terminated with error: %v", err)
	}
}
