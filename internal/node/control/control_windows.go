//go:build windows

package control

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

func Serve(ctx context.Context, dataDir string, handler Handler) error {
	if dataDir == "" {
		return fmt.Errorf("data dir is required")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen on local control port: %w", err)
	}
	defer listener.Close()
	if err := os.WriteFile(controlAddrPath(dataDir), []byte(listener.Addr().String()), 0o600); err != nil {
		return fmt.Errorf("write control address: %w", err)
	}
	defer os.Remove(controlAddrPath(dataDir))

	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("accept control connection: %w", err)
		}
		go handleConn(conn, handler)
	}
}

func Send(ctx context.Context, dataDir string, command Command) (Response, error) {
	addr, err := os.ReadFile(controlAddrPath(dataDir))
	if err != nil {
		return Response{}, fmt.Errorf("read local control address: %w", err)
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", string(addr))
	if err != nil {
		return Response{}, fmt.Errorf("connect to local control port: %w", err)
	}
	defer conn.Close()
	if err := jsonEncoder(conn, command); err != nil {
		return Response{}, err
	}
	return decodeResponse(conn)
}

func controlAddrPath(dataDir string) string {
	return filepath.Join(dataDir, "control.addr")
}
