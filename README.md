# Hayate

Fast encrypted file transfer for local networks, terminals, scripts, and Termux.

```text
    __ __                 __
   / // /___ ___ __ ___ _/ /____
  / _  / _ '/ // / _ '/ __/ -_)
 /_//_/\_,_/\_, /\_,_/\__/\___/
           /___/
```

Hayate is a single-binary Go CLI for sending files between machines over QUIC. It uses an ephemeral X25519 key exchange, ChaCha20-Poly1305 authenticated encryption, optional zstd compression, mDNS discovery, and a terminal UI that falls back cleanly to ASCII output for scripts and mobile terminals.

Current project version: **v2.0.0**.

This is the first open-source-ready protocol version. It introduces the v2 frame format with per-frame raw/zstd flags, adaptive compression, hardened metadata bounds, Termux-specific QUIC behavior, and strict remote filename sanitization.

## Features

- Encrypted by default with ephemeral X25519 session keys and ChaCha20-Poly1305 frames.
- QUIC transport over UDP using `quic-go`.
- Adaptive compression with `--compress auto`, `--compress always`, or `--compress never`.
- mDNS discovery for LAN receivers, with direct `--peer ip:port` mode for restricted networks.
- Termux-friendly direct connection flow and Path MTU discovery handling.
- ASCII-safe headless output for SSH, CI, scripts, narrow terminals, and non-color terminals.
- SHA-256 verification shown after every completed transfer.
- Single static release binaries for macOS, Linux, Windows, and Termux targets.

## Security Model

Hayate protects file contents in transit against passive network observers.

- Each connection negotiates a fresh X25519 shared secret.
- Payloads are authenticated and encrypted with ChaCha20-Poly1305.
- Metadata is encrypted before transfer.
- Incoming filenames are sanitized before writing to disk.
- Frame and metadata sizes are bounded before allocation.
- Release artifacts include SHA-256 checksums.

Hayate does not currently provide persistent peer identity or certificate pinning. Use direct `--peer` addresses on trusted local networks.

## Install

Download a release archive from the project releases page, then verify the checksum:

```bash
sha256sum -c SHA256SUMS
```

On macOS:

```bash
shasum -a 256 -c SHA256SUMS
```

Make the binary executable:

```bash
chmod +x hayate-linux-amd64
./hayate-linux-amd64 version
```

For Termux, use the Linux arm64/Termux build:

```bash
chmod +x hayate-termux-arm64
./hayate-termux-arm64 receive --no-tui --port 50001
```

## Quick Start

Start a receiver:

```bash
hayate receive --port 50001 --output ~/Downloads
```

Send a file using direct mode:

```bash
hayate send ./video.mp4 --peer 192.168.1.50:50001
```

Send a file and disable compression:

```bash
hayate send ./archive.zip --peer 192.168.1.50:50001 --compress never
```

Discover receivers on the LAN:

```bash
hayate discover --duration 5s
```

Use headless mode for Termux, SSH, and scripts:

```bash
hayate receive --port 50001 --output . --no-tui
hayate send ./file.bin --peer 192.168.1.50:50001 --no-tui
```

## Commands

```text
hayate send <file> [--peer ip:port] [--duration 3s] [--compress auto|always|never] [--no-tui]
hayate receive [--port 50001] [--name name] [--output dir] [--no-tui]
hayate discover [--duration 3s]
hayate version
```

Flags may be placed before or after positional arguments:

```bash
hayate send ./file.bin --peer 192.168.1.50:50001 --compress auto
hayate send --peer 192.168.1.50:50001 --compress auto ./file.bin
```

## Compression Modes

`auto` is the default. It skips known incompressible file types and also sends individual chunks raw when zstd does not reduce their size.

```text
auto    Use extension and per-chunk heuristics.
always  Force zstd frames.
never   Send raw encrypted frames.
```

Recommended choices:

```text
Photos, videos, APKs, ZIP/RAR/7z, PDFs:  --compress never or auto
Text, CSV, JSON, logs, source trees:     --compress auto
Known compressible backups:              --compress always
```

## Termux Notes

Android and Termux often restrict multicast discovery. Prefer direct mode:

```bash
# Phone
./hayate-termux-arm64 receive --port 50001 --output ~/storage/downloads --no-tui

# Computer
./hayate send ./file.bin --peer PHONE_IP:50001 --no-tui --compress auto
```

If mDNS discovery fails, it is usually a network or Android multicast restriction. Direct `--peer` mode is the reliable path.

## Build From Source

Requirements:

- Go 1.22 or newer.
- `zip` is optional; `tar.gz` archives are always produced by the release script.

Run tests:

```bash
go test ./...
go test -race ./...
```

Build a local binary:

```bash
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o hayate ./cmd/hayate
```

Build release artifacts:

```bash
scripts/release.sh
```

Run race tests during release:

```bash
HAYATE_RELEASE_RACE=1 scripts/release.sh
```

Optionally upload release archives over SSH:

```bash
HAYATE_RELEASE_SSH_TARGET=user@example.com:/var/www/releases/ scripts/release.sh
```

Artifacts are written to:

```text
dist/hayate-v2.0.0/
dist/hayate-v2.0.0.tar.gz
dist/hayate-v2.0.0.tar.gz.sha256
dist/hayate-v2.0.0.zip
dist/hayate-v2.0.0.zip.sha256
```

## Release Targets

The release script builds:

```text
hayate-darwin-amd64
hayate-darwin-arm64
hayate-linux-amd64
hayate-linux-arm64
hayate-termux-arm64
hayate-windows-amd64.exe
hayate-windows-arm64.exe
```

## Protocol Compatibility

v2.0.0 is not wire-compatible with v1.x peers because encrypted payload frames now include a compression flag.

Both sender and receiver should run the same major version.

## Contributing

Keep changes small, tested, and security-conscious.

Before opening a pull request:

```bash
gofmt -w .
go test ./...
go test -race ./...
```

Report security issues privately until a fix is available.

## License

MIT. See [LICENSE](LICENSE).
