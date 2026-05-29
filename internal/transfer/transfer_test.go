package transfer

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hayate/internal/crypto"
	"hayate/internal/network"
)

func TestFileTransferPipeline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create a temporary directory for test files
	tempDir, err := os.MkdirTemp("", "hayate-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Generate 10MB of random test data
	srcData := make([]byte, 10*1024*1024)
	if _, err := io.ReadFull(rand.Reader, srcData); err != nil {
		t.Fatalf("Failed to generate source random data: %v", err)
	}

	srcHash := sha256.Sum256(srcData)
	srcHashStr := hex.EncodeToString(srcHash[:])

	srcPath := filepath.Join(tempDir, "src.bin")
	if err := os.WriteFile(srcPath, srcData, 0600); err != nil {
		t.Fatalf("Failed to write source test file: %v", err)
	}

	dstPath := filepath.Join(tempDir, "dst.bin")

	// Generate ephemeral ECDH keys for the session
	privA, pubA, err := crypto.GenerateEphemeralKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate ECDH A: %v", err)
	}
	privB, pubB, err := crypto.GenerateEphemeralKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate ECDH B: %v", err)
	}

	keyA, err := crypto.DeriveSharedSecret(privA, pubB)
	if err != nil {
		t.Fatalf("Failed to derive key A: %v", err)
	}
	keyB, err := crypto.DeriveSharedSecret(privB, pubA)
	if err != nil {
		t.Fatalf("Failed to derive key B: %v", err)
	}

	// Create loopback listener
	listener, err := network.CreateListener(0)
	if err != nil {
		t.Fatalf("Failed to create QUIC listener: %v", err)
	}
	defer listener.Close()

	_, portStr, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("Failed to parse listener port: %v", err)
	}
	addr := net.JoinHostPort("127.0.0.1", portStr)

	recvErrChan := make(chan error, 1)
	var recvHash string

	// Spawn receiver routine
	go func() {
		conn, err := listener.Accept(ctx)
		if err != nil {
			recvErrChan <- err
			return
		}
		defer conn.CloseWithError(0, "done")

		stream, err := conn.AcceptStream(ctx)
		if err != nil {
			recvErrChan <- err
			return
		}
		defer stream.Close()

		hash, err := ReceiveFile(ctx, dstPath, stream, keyB, int64(len(srcData)), func(progress int64) {})
		if err != nil {
			recvErrChan <- err
			return
		}

		recvHash = hash
		recvErrChan <- nil
	}()

	// Dial from client
	clientConn, err := network.DialPeer(ctx, addr)
	if err != nil {
		t.Fatalf("Client failed to dial: %v", err)
	}
	defer clientConn.CloseWithError(0, "done")

	clientStream, err := clientConn.OpenStreamSync(ctx)
	if err != nil {
		t.Fatalf("Client failed to open stream: %v", err)
	}
	defer clientStream.Close()

	sendHash, err := SendFile(ctx, srcPath, clientStream, keyA, func(progress int64) {})
	if err != nil {
		t.Fatalf("SendFile failed: %v", err)
	}

	if sendHash != srcHashStr {
		t.Fatalf("Sender computed hash mismatch: expected %s, got %s", srcHashStr, sendHash)
	}

	select {
	case err := <-recvErrChan:
		if err != nil {
			t.Fatalf("Receiver failed: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("Transfer timed out")
	}

	if recvHash != srcHashStr {
		t.Fatalf("Receiver computed hash mismatch: expected %s, got %s", srcHashStr, recvHash)
	}

	// Verify target file bytes on disk
	dstData, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("Failed to read received destination file: %v", err)
	}

	if !bytes.Equal(srcData, dstData) {
		t.Fatal("Destination file bytes do not match source file bytes!")
	}
}
