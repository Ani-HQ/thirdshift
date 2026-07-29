//go:build !windows

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
	path := socketPath(dataDir)
	_ = os.Remove(path)
	listener, err := net.Listen("unix", path)
	if err != nil {
		return fmt.Errorf("listen on control socket: %w", err)
	}
	defer listener.Close()
	defer os.Remove(path)

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
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", socketPath(dataDir))
	if err != nil {
		return Response{}, fmt.Errorf("connect to local control socket: %w", err)
	}
	defer conn.Close()
	if err := jsonEncoder(conn, command); err != nil {
		return Response{}, err
	}
	return decodeResponse(conn)
}

func socketPath(dataDir string) string {
	return filepath.Join(dataDir, "control.sock")
}
