package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"hayate/internal/network"
	"hayate/internal/transfer"
	"hayate/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"
	"github.com/spf13/pflag"
)

const logo = `
    __ __                 __
   / // /___ ___ __ ___ _/ /____
  / _  / _ '/ // / _ '/ __/ -_)
 /_//_/\_,_/\_, /\_,_/\__/\___/
           /___/`

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	subcommand := os.Args[1]
	switch subcommand {
	case "send":
		handleSend()
	case "receive":
		handleReceive()
	case "discover":
		handleDiscover()
	case "version", "--version", "-v":
		fmt.Printf("hayate v%s\n", transfer.Version)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Printf("Unknown subcommand: %s\n\n", subcommand)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(logo)
	fmt.Printf("  v%s | Encrypted cross-device file transfer\n", transfer.Version)
	fmt.Println(strings.Repeat("-", 50))
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  hayate <command> [flags]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  send       Encrypt, compress, and send a file")
	fmt.Println("  receive    Listen for incoming file transfers")
	fmt.Println("  discover   Scan LAN for active Hayate peers")
	fmt.Println("  version    Print version information")
	fmt.Println()
	fmt.Println("Flags (send):")
	fmt.Println("  --peer <ip:port>    Direct connect (skip mDNS)")
	fmt.Println("  --duration <dur>    Discovery scan time (default 3s)")
	fmt.Println("  --compress <mode>   Compression mode: auto, always, never (default auto)")
	fmt.Println("  --no-tui            Headless mode")
	fmt.Println()
	fmt.Println("Flags (receive):")
	fmt.Println("  --port <port>       Listen port (default 50001)")
	fmt.Println("  --name <name>       mDNS display name")
	fmt.Println("  --output <dir>      Download directory (default .)")
	fmt.Println("  --no-tui            Headless mode")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  hayate send document.pdf")
	fmt.Println("  hayate send archive.tar.gz --peer 192.168.1.50:50001")
	fmt.Println("  hayate send archive.tar.gz --peer [fd00::50]:50001")
	fmt.Println("  hayate receive --port 5000 --output ~/Downloads")
}

func handleSend() {
	sendCmd := pflag.NewFlagSet("send", pflag.ExitOnError)
	peerFlag := sendCmd.String("peer", "", "Target peer address (e.g. 192.168.1.50:50001)")
	durationFlag := sendCmd.Duration("duration", 3*time.Second, "Scan duration for peer discovery")
	compressFlag := sendCmd.String("compress", transfer.CompressAuto, "Compression mode: auto, always, never")
	noTuiFlag := sendCmd.Bool("no-tui", false, "Disable interactive TUI")

	_ = sendCmd.Parse(os.Args[2:])

	args := sendCmd.Args()
	if len(args) < 1 {
		fmt.Println("Error: file path required")
		fmt.Println("Usage: hayate send <file> [--peer ip:port] [--compress auto|always|never] [--no-tui]")
		os.Exit(1)
	}
	compressMode, ok := transfer.NormalizeCompressionMode(*compressFlag)
	if !ok {
		fmt.Printf("Error: invalid --compress mode %q; expected auto, always, or never\n", *compressFlag)
		os.Exit(1)
	}

	absPath, err := filepath.Abs(args[0])
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	stat, err := os.Stat(absPath)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	if stat.IsDir() {
		fmt.Println("Error: directories not supported, transfer a single file")
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if *noTuiFlag || !isatty.IsTerminal(os.Stdout.Fd()) {
		runSenderHeadless(ctx, absPath, *peerFlag, *durationFlag, compressMode)
		return
	}

	localIP := network.GetLocalIP()
	model := tui.NewModel(ctx, cancel, "send", absPath, *peerFlag, 0, localIP, "")
	go runSenderOrchestrator(ctx, &model, absPath, *peerFlag, *durationFlag, compressMode)

	p := tea.NewProgram(model)
	if _, err := p.Run(); err != nil {
		fmt.Printf("TUI error: %v\n", err)
		os.Exit(1)
	}
}

func handleReceive() {
	recvCmd := pflag.NewFlagSet("receive", pflag.ExitOnError)
	portFlag := recvCmd.Int("port", 50001, "Listen port (0 = random)")
	nameFlag := recvCmd.String("name", "", "mDNS display name")
	noTuiFlag := recvCmd.Bool("no-tui", false, "Disable interactive TUI")
	outputFlag := recvCmd.String("output", ".", "Download directory")

	_ = recvCmd.Parse(os.Args[2:])

	hostname := *nameFlag
	if hostname == "" {
		h, err := os.Hostname()
		if err != nil || h == "" {
			hostname = "hayate-peer"
		} else {
			if idx := strings.Index(h, "."); idx != -1 {
				hostname = h[:idx]
			} else {
				hostname = h
			}
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if *noTuiFlag || !isatty.IsTerminal(os.Stdout.Fd()) {
		runReceiverHeadless(ctx, *portFlag, hostname, *outputFlag)
		return
	}

	localIP := network.GetLocalIP()
	model := tui.NewModel(ctx, cancel, "receive", "", "", *portFlag, localIP, hostname)
	go runReceiverOrchestrator(ctx, &model, *portFlag, hostname, *outputFlag)

	p := tea.NewProgram(model)
	if _, err := p.Run(); err != nil {
		fmt.Printf("TUI error: %v\n", err)
		os.Exit(1)
	}
}

func handleDiscover() {
	discoverCmd := pflag.NewFlagSet("discover", pflag.ExitOnError)
	durationFlag := discoverCmd.Duration("duration", 3*time.Second, "Scan duration")
	_ = discoverCmd.Parse(os.Args[2:])

	printBanner()
	step("Scanning local network for %s...", *durationFlag)

	ctx, cancel := context.WithTimeout(context.Background(), *durationFlag)
	defer cancel()

	peers, err := network.DiscoverPeers(ctx, *durationFlag)
	if err != nil {
		fail("Discovery failed: %v", err)
	}

	if len(peers) == 0 {
		fmt.Println("  No peers found.")
		return
	}

	fmt.Printf("  Found %d peer(s):\n\n", len(peers))
	fmt.Printf("  %-20s  %-22s  %s\n", "NAME", "ADDRESS", "OS")
	fmt.Printf("  %s\n", strings.Repeat("-", 54))
	for _, p := range peers {
		fmt.Printf("  %-20s  %-22s  %s\n", p.Name, network.FormatAddress(p.IP, p.Port), p.OS)
	}
	fmt.Println()
}

// resolveOutputPath generates a unique filename with incrementing counter on collision.
func resolveOutputPath(outputDir, filename string) string {
	outPath := filepath.Join(outputDir, filename)
	if _, err := os.Stat(outPath); os.IsNotExist(err) {
		return outPath
	}
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)
	for i := 1; ; i++ {
		candidate := filepath.Join(outputDir, fmt.Sprintf("%s (%d)%s", base, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

func sanitizeRemoteFilename(filename string) (string, error) {
	if filename == "" {
		return "", fmt.Errorf("empty filename")
	}
	if strings.ContainsAny(filename, `/\`) {
		return "", fmt.Errorf("filename contains path separators")
	}
	base := filepath.Base(filename)
	if base != filename || base == "." || base == ".." {
		return "", fmt.Errorf("unsafe filename")
	}
	return base, nil
}

// -- Headless Output Primitives --

func printBanner() {
	fmt.Println(logo)
	fmt.Printf("  v%s\n\n", transfer.Version)
}

func step(format string, a ...any) {
	fmt.Printf("  [*] "+format+"\n", a...)
}

func done(format string, a ...any) {
	fmt.Printf("  [+] "+format+"\n", a...)
}

func warn(format string, a ...any) {
	fmt.Printf("  [!] "+format+"\n", a...)
}

func fail(format string, a ...any) {
	fmt.Printf("  [-] "+format+"\n", a...)
	os.Exit(1)
}

func printSummaryBox(title string, rows [][2]string) {
	// Determine max label width for alignment
	maxLabel := 0
	for _, r := range rows {
		if len(r[0]) > maxLabel {
			maxLabel = len(r[0])
		}
	}

	// Calculate inner width
	innerWidth := maxLabel + 4 + 66
	if innerWidth < 56 {
		innerWidth = 56
	}

	fmt.Println()
	fmt.Printf("  +%s+\n", strings.Repeat("-", innerWidth))
	fmt.Printf("  |  %-*s|\n", innerWidth-2, title)
	fmt.Printf("  |%s|\n", strings.Repeat("-", innerWidth))

	for _, r := range rows {
		val := r[1]
		padding := innerWidth - 4 - maxLabel - len(val)
		if padding < 0 {
			padding = 0
		}
		fmt.Printf("  |  %-*s  %s%*s|\n", maxLabel, r[0], val, padding, "")
	}

	fmt.Printf("  +%s+\n", strings.Repeat("-", innerWidth))
	fmt.Println()
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// -- TUI Orchestrators --

func runSenderOrchestrator(ctx context.Context, m *tui.Model, filePath string, peerAddr string, scanDuration time.Duration, compressMode string) {
	stat, err := os.Stat(filePath)
	if err != nil {
		m.MsgChan <- tui.ErrorMsg{Err: fmt.Errorf("stat: %w", err)}
		return
	}
	filename := filepath.Base(filePath)
	fileSize := stat.Size()
	var targetAddr string

	if peerAddr == "" {
		peers, err := network.DiscoverPeers(ctx, scanDuration)
		if err != nil {
			m.MsgChan <- tui.ErrorMsg{Err: fmt.Errorf("discovery: %w", err)}
			return
		}
		select {
		case m.MsgChan <- tui.PeerDiscoveredMsg(peers):
		case <-ctx.Done():
			return
		}
		select {
		case sel := <-m.SelectedPeerChan:
			targetAddr = network.FormatAddress(sel.IP, sel.Port)
		case <-ctx.Done():
			return
		}
	} else {
		targetAddr = peerAddr
	}

	conn, err := network.DialPeer(ctx, targetAddr)
	if err != nil {
		m.MsgChan <- tui.ErrorMsg{Err: fmt.Errorf("connect: %w", err)}
		return
	}
	defer conn.CloseWithError(0, "done")

	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		m.MsgChan <- tui.ErrorMsg{Err: fmt.Errorf("stream: %w", err)}
		return
	}
	defer stream.Close()

	key, err := transfer.EstablishSecureStreamSender(ctx, stream, filename, fileSize)
	if err != nil {
		m.MsgChan <- tui.ErrorMsg{Err: fmt.Errorf("handshake: %w", err)}
		return
	}

	select {
	case m.MsgChan <- tui.StartTransferMsg{FileName: filename, FileSize: fileSize, IsSend: true, PeerAddr: targetAddr, LocalAddr: conn.LocalAddr().String()}:
	case <-ctx.Done():
		return
	}

	hash, err := transfer.SendFile(ctx, filePath, stream, key, compressMode, func(p int64) {
		select {
		case m.MsgChan <- tui.ProgressMsg(p):
		default:
		}
	})
	if err != nil {
		m.MsgChan <- tui.ErrorMsg{Err: fmt.Errorf("transfer: %w", err)}
		return
	}

	_ = stream.Close()
	dummy := make([]byte, 1)
	_, _ = stream.Read(dummy)

	select {
	case m.MsgChan <- tui.DoneMsg{Hash: hash}:
	case <-ctx.Done():
	}
}

func runReceiverOrchestrator(ctx context.Context, m *tui.Model, listenPort int, advertisedName string, outputDir string) {
	listener, err := network.CreateListener(listenPort)
	if err != nil {
		m.MsgChan <- tui.ErrorMsg{Err: fmt.Errorf("listener: %w", err)}
		return
	}
	defer listener.Close()

	_, portStr, err := net.SplitHostPort(listener.Addr().String())
	if err == nil {
		if p, err := strconv.Atoi(portStr); err == nil {
			listenPort = p
		}
	}

	advServer, err := network.StartAdvertising(ctx, advertisedName, listenPort)
	if err != nil {
		if !network.IsMulticastError(err) {
			m.MsgChan <- tui.ErrorMsg{Err: fmt.Errorf("mDNS: %w", err)}
			return
		}
	} else {
		defer advServer.Shutdown()
	}

	listenAddr := network.FormatAddress(network.GetLocalIP(), listenPort)
	select {
	case m.MsgChan <- tui.ListeningMsg{Port: listenPort, LocalAddr: listenAddr}:
	case <-ctx.Done():
		return
	}

	conn, err := listener.Accept(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		m.MsgChan <- tui.ErrorMsg{Err: fmt.Errorf("accept: %w", err)}
		return
	}
	defer conn.CloseWithError(0, "done")

	peerAddr := conn.RemoteAddr().String()

	stream, err := conn.AcceptStream(ctx)
	if err != nil {
		m.MsgChan <- tui.ErrorMsg{Err: fmt.Errorf("stream: %w", err)}
		return
	}
	defer stream.Close()

	key, filename, fileSize, err := transfer.EstablishSecureStreamReceiver(ctx, stream)
	if err != nil {
		m.MsgChan <- tui.ErrorMsg{Err: fmt.Errorf("handshake: %w", err)}
		return
	}

	safeFilename, err := sanitizeRemoteFilename(filename)
	if err != nil {
		m.MsgChan <- tui.ErrorMsg{Err: fmt.Errorf("metadata: %w", err)}
		return
	}

	select {
	case m.MsgChan <- tui.StartTransferMsg{FileName: safeFilename, FileSize: fileSize, IsSend: false, PeerAddr: peerAddr, LocalAddr: listener.Addr().String()}:
	case <-ctx.Done():
		return
	}

	outPath := resolveOutputPath(outputDir, safeFilename)
	hash, err := transfer.ReceiveFile(ctx, outPath, stream, key, fileSize, func(p int64) {
		select {
		case m.MsgChan <- tui.ProgressMsg(p):
		default:
		}
	})
	if err != nil {
		m.MsgChan <- tui.ErrorMsg{Err: fmt.Errorf("transfer: %w", err)}
		return
	}

	select {
	case m.MsgChan <- tui.DoneMsg{Hash: hash}:
	case <-ctx.Done():
	}
}

// -- Headless Flows --

func runSenderHeadless(ctx context.Context, filePath string, peerAddr string, scanDuration time.Duration, compressMode string) {
	printBanner()

	stat, err := os.Stat(filePath)
	if err != nil {
		fail("Cannot stat file: %v", err)
	}
	filename := filepath.Base(filePath)
	fileSize := stat.Size()

	step("File: %s (%s)", filename, formatBytes(fileSize))

	var targetAddr string
	if peerAddr == "" {
		step("Scanning for peers...")
		peers, err := network.DiscoverPeers(ctx, scanDuration)
		if err != nil {
			fail("Discovery: %v", err)
		}
		if len(peers) == 0 {
			fail("No peers found on the local network")
		}
		selected := peers[0]
		targetAddr = network.FormatAddress(selected.IP, selected.Port)
		done("Selected peer: %s (%s)", selected.Name, targetAddr)
	} else {
		targetAddr = peerAddr
	}

	step("Connecting to %s...", targetAddr)
	conn, err := network.DialPeer(ctx, targetAddr)
	if err != nil {
		fail("Connection failed: %v", err)
	}
	defer conn.CloseWithError(0, "done")

	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		fail("Stream error: %v", err)
	}
	defer stream.Close()

	step("Performing cryptographic handshake...")
	key, err := transfer.EstablishSecureStreamSender(ctx, stream, filename, fileSize)
	if err != nil {
		fail("Handshake failed: %v", err)
	}
	done("Secure channel established")

	step("Sending %s...", filename)
	step("Compression: %s", compressMode)
	startTime := time.Now()
	lastPrint := time.Now()

	hash, err := transfer.SendFile(ctx, filePath, stream, key, compressMode, func(progress int64) {
		now := time.Now()
		if now.Sub(lastPrint) < 200*time.Millisecond {
			return
		}
		lastPrint = now
		pct := 0.0
		if fileSize > 0 {
			pct = float64(progress) / float64(fileSize) * 100.0
		}
		elapsed := now.Sub(startTime).Seconds()
		speed := 0.0
		if elapsed > 0 {
			speed = float64(progress) / elapsed
		}
		bar := headlessBar(30, progress, fileSize)
		fmt.Printf("\r  [%s] %5.1f%%  %s/s  %s/%s",
			bar, pct, formatBytes(int64(speed)),
			formatBytes(progress), formatBytes(fileSize))
	})
	if err != nil {
		fmt.Println()
		fail("Transfer failed: %v", err)
	}

	_ = stream.Close()
	dummy := make([]byte, 1)
	_, _ = stream.Read(dummy)

	duration := time.Since(startTime)
	speed := 0.0
	if duration.Seconds() > 0 {
		speed = float64(fileSize) / duration.Seconds()
	}

	fmt.Println()
	printSummaryBox("TRANSFER COMPLETE", [][2]string{
		{"File", filename},
		{"Size", formatBytes(fileSize)},
		{"SHA-256", hash},
		{"Speed", fmt.Sprintf("%.2f MB/s", speed/(1024*1024))},
		{"Duration", duration.Round(time.Millisecond).String()},
		{"Peer", targetAddr},
	})
}

func runReceiverHeadless(ctx context.Context, listenPort int, advertisedName string, outputDir string) {
	printBanner()

	listener, err := network.CreateListener(listenPort)
	if err != nil {
		fail("Listener error: %v", err)
	}
	defer listener.Close()

	_, portStr, err := net.SplitHostPort(listener.Addr().String())
	if err == nil {
		if p, err := strconv.Atoi(portStr); err == nil {
			listenPort = p
		}
	}

	advServer, err := network.StartAdvertising(ctx, advertisedName, listenPort)
	if err != nil {
		if network.IsMulticastError(err) {
			warn("mDNS unavailable (%v)", err)
			step("Peers must connect directly via --peer flag")
		} else {
			fail("mDNS error: %v", err)
		}
	} else {
		defer advServer.Shutdown()
	}

	localIP := network.GetLocalIP()
	step("Listening on %s:%d (advertised as %s)", localIP, listenPort, advertisedName)
	step("Waiting for incoming connection...")

	conn, err := listener.Accept(ctx)
	if err != nil {
		fail("Accept error: %v", err)
	}
	defer conn.CloseWithError(0, "done")

	peerAddr := conn.RemoteAddr().String()
	done("Connection from %s", peerAddr)

	stream, err := conn.AcceptStream(ctx)
	if err != nil {
		fail("Stream error: %v", err)
	}
	defer stream.Close()

	step("Performing cryptographic handshake...")
	key, filename, fileSize, err := transfer.EstablishSecureStreamReceiver(ctx, stream)
	if err != nil {
		fail("Handshake failed: %v", err)
	}
	safeFilename, err := sanitizeRemoteFilename(filename)
	if err != nil {
		fail("Unsafe remote filename: %v", err)
	}
	done("Secure channel established")

	step("Receiving: %s (%s)", safeFilename, formatBytes(fileSize))

	outPath := resolveOutputPath(outputDir, safeFilename)

	startTime := time.Now()
	lastPrint := time.Now()

	hash, err := transfer.ReceiveFile(ctx, outPath, stream, key, fileSize, func(progress int64) {
		now := time.Now()
		if now.Sub(lastPrint) < 200*time.Millisecond {
			return
		}
		lastPrint = now
		pct := 0.0
		if fileSize > 0 {
			pct = float64(progress) / float64(fileSize) * 100.0
		}
		elapsed := now.Sub(startTime).Seconds()
		speed := 0.0
		if elapsed > 0 {
			speed = float64(progress) / elapsed
		}
		bar := headlessBar(30, progress, fileSize)
		fmt.Printf("\r  [%s] %5.1f%%  %s/s  %s/%s",
			bar, pct, formatBytes(int64(speed)),
			formatBytes(progress), formatBytes(fileSize))
	})
	if err != nil {
		fmt.Println()
		fail("Transfer failed: %v", err)
	}

	duration := time.Since(startTime)
	speed := 0.0
	if duration.Seconds() > 0 {
		speed = float64(fileSize) / duration.Seconds()
	}

	fmt.Println()
	printSummaryBox("FILE RECEIVED", [][2]string{
		{"File", filepath.Base(outPath)},
		{"Saved to", outPath},
		{"Size", formatBytes(fileSize)},
		{"SHA-256", hash},
		{"Speed", fmt.Sprintf("%.2f MB/s", speed/(1024*1024))},
		{"Duration", duration.Round(time.Millisecond).String()},
		{"From", peerAddr},
	})
}

func headlessBar(width int, progress, total int64) string {
	if total <= 0 {
		return strings.Repeat("-", width)
	}
	ratio := float64(progress) / float64(total)
	if ratio > 1.0 {
		ratio = 1.0
	}
	filled := int(ratio * float64(width))
	empty := width - filled
	if empty < 0 {
		empty = 0
	}
	return strings.Repeat("=", filled) + strings.Repeat("-", empty)
}
