package network

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"runtime"
	"strings"
	"time"

	"github.com/grandcat/zeroconf"
	"github.com/quic-go/quic-go"
)

const ServiceType = "_hayate._udp"

type Peer struct {
	Name string
	IP   string
	Port int
	OS   string
}

func GetLocalIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}

func GenerateTLSConfig() (*tls.Config, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating P-256 key: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Hayate"},
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(24 * 365 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("creating cert: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})

	privBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("marshalling key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privBytes})

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("building key pair: %w", err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"hayate"},
	}, nil
}

// highThroughputQUICConfig returns a quic.Config tuned for maximum throughput.
// Stream windows are expanded to 16MB/64MB and connection windows to 32MB/128MB
// to prevent flow-control throttling on high-bandwidth LAN and Wi-Fi 6E links.
func highThroughputQUICConfig() *quic.Config {
	cfg := &quic.Config{
		MaxIdleTimeout:                 60 * time.Second,
		KeepAlivePeriod:                15 * time.Second,
		InitialStreamReceiveWindow:     16 * 1024 * 1024,
		MaxStreamReceiveWindow:         64 * 1024 * 1024,
		InitialConnectionReceiveWindow: 32 * 1024 * 1024,
		MaxConnectionReceiveWindow:     128 * 1024 * 1024,
	}

	// Android Termux may drop QUIC PMTU probe packets; disable discovery to prevent stalls
	if runtime.GOOS == "android" {
		cfg.DisablePathMTUDiscovery = true
	}

	return cfg
}

func StartAdvertising(ctx context.Context, name string, port int) (*zeroconf.Server, error) {
	txt := []string{
		"os=" + runtime.GOOS,
		"app=Hayate",
	}

	server, err := zeroconf.Register(name, ServiceType, "local.", port, txt, nil)
	if err != nil {
		return nil, fmt.Errorf("registering zeroconf mdns: %w", err)
	}
	return server, nil
}

func DiscoverPeers(ctx context.Context, scanDuration time.Duration) ([]Peer, error) {
	resolver, err := zeroconf.NewResolver()
	if err != nil {
		if IsMulticastError(err) {
			return nil, fmt.Errorf("multicast UDP is not supported on this network interface. Use --peer <ip:port> to connect directly")
		}
		return nil, fmt.Errorf("creating resolver: %w", err)
	}

	entries := make(chan *zeroconf.ServiceEntry)
	var peers []Peer

	go func() {
		_ = resolver.Browse(ctx, ServiceType, "local.", entries)
	}()

	timeout := time.After(scanDuration)
	for {
		select {
		case entry, ok := <-entries:
			if !ok {
				return peers, nil
			}

			isHayate := false
			osVal := "Unknown"
			for _, item := range entry.Text {
				if item == "app=Hayate" {
					isHayate = true
				}
				if len(item) > 3 && item[:3] == "os=" {
					osVal = item[3:]
				}
			}

			if isHayate {
				ip := "127.0.0.1"
				if len(entry.AddrIPv4) > 0 {
					ip = entry.AddrIPv4[0].String()
				} else if len(entry.AddrIPv6) > 0 {
					ip = entry.AddrIPv6[0].String()
				}

				peers = append(peers, Peer{
					Name: entry.Instance,
					IP:   ip,
					Port: entry.Port,
					OS:   osVal,
				})
			}

		case <-timeout:
			return peers, nil
		case <-ctx.Done():
			return peers, ctx.Err()
		}
	}
}

func CreateListener(port int) (*quic.Listener, error) {
	tlsConf, err := GenerateTLSConfig()
	if err != nil {
		return nil, err
	}

	addr := fmt.Sprintf("0.0.0.0:%d", port)
	listener, err := quic.ListenAddr(addr, tlsConf, highThroughputQUICConfig())
	if err != nil {
		return nil, fmt.Errorf("starting QUIC listener on %s: %w", addr, err)
	}
	return listener, nil
}

func DialPeer(ctx context.Context, peerAddr string) (*quic.Conn, error) {
	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"hayate"},
	}

	conn, err := quic.DialAddr(ctx, peerAddr, tlsConf, highThroughputQUICConfig())
	if err != nil {
		return nil, fmt.Errorf("dialing peer %s: %w", peerAddr, err)
	}
	return conn, nil
}

func IsMulticastError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "failed to join") ||
		strings.Contains(s, "no such interface") ||
		strings.Contains(s, "failed to bind") ||
		strings.Contains(s, "multicast") ||
		strings.Contains(s, "determine host IP")
}
