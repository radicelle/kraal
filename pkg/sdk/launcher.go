package sdk

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// SubprocessConnector represents a connector running as a child process (Desktop Mode).
type SubprocessConnector struct {
	Client  *Client
	Address string
	cmd     *exec.Cmd
}

// Close terminates the child process and closes the gRPC connection.
func (s *SubprocessConnector) Close() error {
	var clientErr error
	if s.Client != nil {
		clientErr = s.Client.Close()
	}

	var procErr error
	if s.cmd != nil && s.cmd.Process != nil {
		procErr = s.cmd.Process.Kill()
	}

	if clientErr != nil {
		return clientErr
	}
	return procErr
}

// LaunchSubprocess starts a connector binary in Desktop mode on an ephemeral port.
// It waits for the readiness signal, connects via gRPC, and returns the SubprocessConnector.
func LaunchSubprocess(ctx context.Context, binaryPath string, extraArgs ...string) (*SubprocessConnector, error) {
	args := append([]string{"--listen=127.0.0.1:0"}, extraArgs...)
	cmd := exec.CommandContext(ctx, binaryPath, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start connector process (%s): %w", binaryPath, err)
	}

	addrChan := make(chan string, 1)
	errChan := make(chan error, 1)

	// Monitor stdout for readiness signal
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, ReadySignalPrefix) {
				addr := strings.TrimPrefix(line, ReadySignalPrefix)
				addrChan <- strings.TrimSpace(addr)
				return
			}
		}
		if err := scanner.Err(); err != nil {
			errChan <- err
			return
		}
		errChan <- fmt.Errorf("connector process exited without emitting readiness signal")
	}()

	select {
	case addr := <-addrChan:
		client, err := NewClient(addr)
		if err != nil {
			_ = cmd.Process.Kill()
			return nil, fmt.Errorf("failed to connect to connector at %s: %w", addr, err)
		}
		return &SubprocessConnector{
			Client:  client,
			Address: addr,
			cmd:     cmd,
		}, nil

	case err := <-errChan:
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("connector failed to start: %w", err)

	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("timed out waiting for connector to start and bind port")

	case <-ctx.Done():
		_ = cmd.Process.Kill()
		return nil, ctx.Err()
	}
}
