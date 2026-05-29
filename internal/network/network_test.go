package network

import (
	"context"
	"io"
	"net"
	"testing"
	"time"
)

func TestGetLocalIP(t *testing.T) {
	ip := GetLocalIP()
	if ip == "" {
		t.Fatal("Expected local IP, got empty string")
	}
}

func TestGenerateTLSConfig(t *testing.T) {
	cfg, err := GenerateTLSConfig()
	if err != nil {
		t.Fatalf("Failed to generate TLS config: %v", err)
	}
	if cfg == nil {
		t.Fatal("Generated TLS config is nil")
	}
	if len(cfg.Certificates) == 0 {
		t.Fatal("No certificates generated in TLS config")
	}
	if len(cfg.NextProtos) == 0 || cfg.NextProtos[0] != "hayate" {
		t.Fatalf("Invalid NextProtos config: %v", cfg.NextProtos)
	}
}

func TestQUICLoopback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Create listener on dynamic port (0 binds to an available system port)
	listener, err := CreateListener(0)
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	_, portStr, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("Failed to split host port: %v", err)
	}
	addr := net.JoinHostPort("127.0.0.1", portStr)

	errChan := make(chan error, 1)
	msg := []byte("hayate-loopback-test")

	// Start server routine
	go func() {
		conn, err := listener.Accept(ctx)
		if err != nil {
			errChan <- err
			return
		}
		defer conn.CloseWithError(0, "done")

		stream, err := conn.AcceptStream(ctx)
		if err != nil {
			errChan <- err
			return
		}
		defer stream.Close()

		buf := make([]byte, len(msg))
		_, err = io.ReadFull(stream, buf)
		if err != nil {
			errChan <- err
			return
		}

		if string(buf) != string(msg) {
			errChan <- io.ErrUnexpectedEOF
			return
		}

		errChan <- nil
	}()

	// Dial client routine
	clientConn, err := DialPeer(ctx, addr)
	if err != nil {
		t.Fatalf("Client failed to dial peer: %v", err)
	}
	defer clientConn.CloseWithError(0, "done")

	clientStream, err := clientConn.OpenStreamSync(ctx)
	if err != nil {
		t.Fatalf("Client failed to open stream: %v", err)
	}
	defer clientStream.Close()

	_, err = clientStream.Write(msg)
	if err != nil {
		t.Fatalf("Client failed to write: %v", err)
	}

	select {
	case err := <-errChan:
		if err != nil {
			t.Fatalf("Server connection error: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("Test timed out")
	}
}
