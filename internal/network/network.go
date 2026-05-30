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
	"os"
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
	ips := GetLocalIPs()
	if len(ips) > 0 {
		return ips[0]
	}
	return "127.0.0.1"
}

func GetLocalIPs() []string {
	var v4 []string
	var v6 []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ip := ipFromAddr(addr)
			if ip == nil {
				continue
			}
			if ip4 := ip.To4(); ip4 != nil && isPrivateIPv4(ip4) {
				v4 = append(v4, ip4.String())
				continue
			}
			if ip16 := ip.To16(); ip16 != nil && ip.To4() == nil && isPreferredIPv6(ip16) {
				v6 = append(v6, ip16.String())
			}
		}
	}
	return append(v4, v6...)
}

func ipFromAddr(addr net.Addr) net.IP {
	switch v := addr.(type) {
	case *net.IPNet:
		return v.IP
	case *net.IPAddr:
		return v.IP
	default:
		return nil
	}
}

func isPrivateIPv4(ip net.IP) bool {
	if len(ip) != net.IPv4len {
		return false
	}
	return ip[0] == 10 ||
		(ip[0] == 172 && ip[1] >= 16 && ip[1] <= 31) ||
		(ip[0] == 192 && ip[1] == 168) ||
		(ip[0] == 169 && ip[1] == 254)
}

func isPreferredIPv6(ip net.IP) bool {
	return ip.IsGlobalUnicast() && !ip.IsLinkLocalUnicast() && !ip.IsLoopback()
}

func FormatAddress(host string, port int) string {
	return net.JoinHostPort(host, fmt.Sprintf("%d", port))
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

// highThroughputQUICConfig returns a quic.Config tuned for maximum LAN throughput.
// Large flow-control windows keep Wi-Fi 6/6E and wired LAN paths saturated when
// disk and CPU can keep up.
func highThroughputQUICConfig() *quic.Config {
	cfg := &quic.Config{
		MaxIdleTimeout:                 60 * time.Second,
		KeepAlivePeriod:                15 * time.Second,
		HandshakeIdleTimeout:           10 * time.Second,
		InitialStreamReceiveWindow:     32 * 1024 * 1024,
		MaxStreamReceiveWindow:         256 * 1024 * 1024,
		InitialConnectionReceiveWindow: 64 * 1024 * 1024,
		MaxConnectionReceiveWindow:     512 * 1024 * 1024,
		MaxIncomingStreams:             8,
		MaxIncomingUniStreams:          8,
	}

	// Android and Termux environments may drop QUIC PMTU probe packets.
	if isAndroid() {
		cfg.DisablePathMTUDiscovery = true
	}

	return cfg
}

func isAndroid() bool {
	if runtime.GOOS == "android" {
		return true
	}
	return strings.Contains(strings.ToLower(os.Getenv("PREFIX")), "com.termux")
}

func StartAdvertising(ctx context.Context, name string, port int) (*zeroconf.Server, error) {
	txt := []string{
		"os=" + runtime.GOOS,
		"app=Hayate",
	}

	server, err := zeroconf.Register(name, ServiceType, "local.", port, txt, multicastInterfaces())
	if err != nil {
		return nil, fmt.Errorf("registering zeroconf mdns: %w", err)
	}
	return server, nil
}

func DiscoverPeers(ctx context.Context, scanDuration time.Duration) ([]Peer, error) {
	resolver, err := zeroconf.NewResolver(
		zeroconf.SelectIPTraffic(zeroconf.IPv4AndIPv6),
		zeroconf.SelectIfaces(multicastInterfaces()),
	)
	if err != nil {
		if IsMulticastError(err) {
			return nil, fmt.Errorf("multicast UDP is not supported on this network interface. Use --peer <ip:port> to connect directly")
		}
		return nil, fmt.Errorf("creating resolver: %w", err)
	}

	entries := make(chan *zeroconf.ServiceEntry)
	browseErr := make(chan error, 1)
	var peers []Peer

	go func() {
		browseErr <- resolver.Browse(ctx, ServiceType, "local.", entries)
	}()

	timeout := time.After(scanDuration)
	for {
		select {
		case err := <-browseErr:
			if err != nil {
				if IsMulticastError(err) {
					return peers, fmt.Errorf("multicast UDP is not supported on this network interface. Use --peer <ip:port> to connect directly")
				}
				return peers, fmt.Errorf("browsing mdns: %w", err)
			}
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
				ip := bestServiceIP(entry)
				if ip == "" {
					continue
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

func multicastInterfaces() []net.Interface {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	selected := make([]net.Interface, 0, len(ifaces))
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 ||
			iface.Flags&net.FlagLoopback != 0 ||
			iface.Flags&net.FlagMulticast == 0 {
			continue
		}
		if hasUsableIP(iface) {
			selected = append(selected, iface)
		}
	}
	if len(selected) == 0 {
		return nil
	}
	return selected
}

func hasUsableIP(iface net.Interface) bool {
	addrs, err := iface.Addrs()
	if err != nil {
		return false
	}
	for _, addr := range addrs {
		ip := ipFromAddr(addr)
		if ip == nil {
			continue
		}
		if ip.To4() != nil || isPreferredIPv6(ip) {
			return true
		}
	}
	return false
}

func bestServiceIP(entry *zeroconf.ServiceEntry) string {
	for _, ip := range entry.AddrIPv4 {
		if ip4 := ip.To4(); ip4 != nil {
			return ip4.String()
		}
	}
	for _, ip := range entry.AddrIPv6 {
		if ip16 := ip.To16(); ip16 != nil && ip.To4() == nil && isPreferredIPv6(ip16) {
			return ip16.String()
		}
	}
	return ""
}

func CreateListener(port int) (*quic.Listener, error) {
	tlsConf, err := GenerateTLSConfig()
	if err != nil {
		return nil, err
	}

	addr := FormatAddress("::", port)
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
