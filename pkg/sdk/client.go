package sdk

import (
	"fmt"

	protocolv1 "github.com/radicelle/kraal/pkg/protocol/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Client wraps the gRPC client for interacting with any Kraal connector.
type Client struct {
	conn    *grpc.ClientConn
	service protocolv1.ConnectorServiceClient
}

// NewClient connects to a connector at targetAddress (used in Cloud mode or when address is known).
func NewClient(targetAddress string) (*Client, error) {
	conn, err := grpc.NewClient(targetAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to connector at %s: %w", targetAddress, err)
	}

	return &Client{
		conn:    conn,
		service: protocolv1.NewConnectorServiceClient(conn),
	}, nil
}

func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *Client) Service() protocolv1.ConnectorServiceClient {
	return c.service
}
