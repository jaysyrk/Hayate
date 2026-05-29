package tui

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"hayate/internal/network"
	"hayate/internal/transfer"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type State int

const (
	StateDiscover State = iota
	StateSelectPeer
	StateHandshake
	StateTransferring
	StateCompleted
	StateError
)

type PeerDiscoveredMsg []network.Peer

type StartTransferMsg struct {
	FileName string
	FileSize int64
	IsSend   bool
	PeerAddr string
}

type ProgressMsg int64

type DoneMsg struct {
	Hash           string
	CompressedSize int64
}

type ErrorMsg struct {
	Err error
}

// -- Color Palette --
// Primary:   #00E5FF (electric cyan)
// Accent:    #7C4DFF (vivid indigo)
// Success:   #00E676 (neon green)
// Error:     #FF3D00 (sharp red)
// Warning:   #FFC400 (amber)
// Surface:   #1A1A2E (deep navy)
// Muted:     #5C6370 (slate gray)
// Text:      #E8EAED (off-white)

var (
	cyan   = lipgloss.Color("#00E5FF")
	indigo = lipgloss.Color("#7C4DFF")
	green  = lipgloss.Color("#00E676")
	red    = lipgloss.Color("#FF3D00")
	amber  = lipgloss.Color("#FFC400")
	slate  = lipgloss.Color("#5C6370")
	text   = lipgloss.Color("#E8EAED")
	dim    = lipgloss.Color("#3A3A4E")

	logoStyle = lipgloss.NewStyle().
			Foreground(indigo).
			Bold(true)

	versionStyle = lipgloss.NewStyle().
			Foreground(slate).
			Italic(true)

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(cyan).
			PaddingLeft(1).
			PaddingRight(1)

	sectionStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(indigo).
			MarginTop(1)

	labelStyle = lipgloss.NewStyle().
			Foreground(slate).
			Width(14)

	valueStyle = lipgloss.NewStyle().
			Foreground(text)

	hashStyle = lipgloss.NewStyle().
			Foreground(amber)

	mutedStyle = lipgloss.NewStyle().
			Foreground(slate)

	successStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(green)

	errorStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(red)

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(indigo).
			Padding(1, 3)

	successBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(green).
			Padding(1, 3)

	errorBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(red).
			Padding(1, 3)

	tableHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(indigo)

	tableRowStyle = lipgloss.NewStyle().
			Foreground(text)

	tableSelectedStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(cyan)

	cursorStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(cyan)

	barFilledStyle = lipgloss.NewStyle().
			Foreground(cyan)

	barEmptyStyle = lipgloss.NewStyle().
			Foreground(dim)

	pctStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(cyan)

	speedStyle = lipgloss.NewStyle().
			Foreground(green)

	hintStyle = lipgloss.NewStyle().
			Foreground(slate).
			Italic(true).
			MarginTop(1)
)

type Model struct {
	Mode         string
	state        State
	err          error
	filePath     string
	fileName     string
	fileSize     int64
	peerAddr     string
	advertised   string
	localIP      string
	port         int
	peers        []network.Peer
	selectedIdx  int
	progress     int64
	startTime    time.Time
	lastTime     time.Time
	lastProgress int64
	speed        float64
	eta          time.Duration
	hash         string
	compSize     int64
	isSend       bool

	spinner          spinner.Model
	MsgChan          chan tea.Msg
	SelectedPeerChan chan network.Peer
	Ctx              context.Context
	Cancel           context.CancelFunc
}

func NewModel(ctx context.Context, cancel context.CancelFunc, mode string, filePath string, peerAddr string, port int, localIP string, advertised string) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(cyan)

	return Model{
		Mode:             mode,
		state:            StateDiscover,
		filePath:         filePath,
		peerAddr:         peerAddr,
		port:             port,
		localIP:          localIP,
		advertised:       advertised,
		spinner:          s,
		MsgChan:          make(chan tea.Msg, 64),
		SelectedPeerChan: make(chan network.Peer, 1),
		Ctx:              ctx,
		Cancel:           cancel,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.WaitForMsg(),
	)
}

func (m Model) WaitForMsg() tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-m.MsgChan
		if !ok {
			return nil
		}
		return msg
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.Cancel()
			return m, tea.Quit
		case "q":
			if m.state == StateCompleted || m.state == StateError {
				return m, tea.Quit
			}
		case "up", "k":
			if m.state == StateSelectPeer && m.selectedIdx > 0 {
				m.selectedIdx--
			}
		case "down", "j":
			if m.state == StateSelectPeer && m.selectedIdx < len(m.peers)-1 {
				m.selectedIdx++
			}
		case "enter":
			if m.state == StateSelectPeer && len(m.peers) > 0 {
				selected := m.peers[m.selectedIdx]
				m.state = StateHandshake
				m.peerAddr = fmt.Sprintf("%s:%d", selected.IP, selected.Port)
				select {
				case m.SelectedPeerChan <- selected:
				default:
				}
			} else if m.state == StateCompleted || m.state == StateError {
				return m, tea.Quit
			}
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case PeerDiscoveredMsg:
		m.peers = msg
		if len(m.peers) == 0 {
			m.state = StateError
			m.err = fmt.Errorf("no peers discovered on the local network")
		} else {
			m.state = StateSelectPeer
		}

	case StartTransferMsg:
		m.state = StateTransferring
		m.fileName = msg.FileName
		m.fileSize = msg.FileSize
		m.isSend = msg.IsSend
		if msg.PeerAddr != "" {
			m.peerAddr = msg.PeerAddr
		}
		m.startTime = time.Now()
		m.lastTime = m.startTime
		m.lastProgress = 0

	case ProgressMsg:
		curr := int64(msg)
		m.progress = curr
		now := time.Now()
		elapsed := now.Sub(m.lastTime)

		if elapsed >= 200*time.Millisecond {
			bytesDelta := curr - m.lastProgress
			instSpeed := float64(bytesDelta) / elapsed.Seconds()

			if m.speed == 0 {
				m.speed = instSpeed
			} else {
				m.speed = m.speed*0.65 + instSpeed*0.35
			}
			m.lastTime = now
			m.lastProgress = curr

			if m.speed > 0 && m.fileSize > curr {
				remaining := m.fileSize - curr
				m.eta = time.Duration(float64(remaining)/m.speed) * time.Second
			} else {
				m.eta = 0
			}
		}

	case DoneMsg:
		m.state = StateCompleted
		m.hash = msg.Hash
		m.compSize = msg.CompressedSize

	case ErrorMsg:
		m.state = StateError
		m.err = msg.Err

	case nil:
		return m, nil
	}

	cmds = append(cmds, m.WaitForMsg())
	return m, tea.Batch(cmds...)
}

func (m Model) View() string {
	var s strings.Builder

	s.WriteString(renderHeader())

	switch m.state {
	case StateDiscover:
		s.WriteString(m.viewDiscover())
	case StateSelectPeer:
		s.WriteString(m.viewSelectPeer())
	case StateHandshake:
		s.WriteString(m.viewHandshake())
	case StateTransferring:
		s.WriteString(m.viewTransferring())
	case StateCompleted:
		s.WriteString(m.viewCompleted())
	case StateError:
		s.WriteString(m.viewError())
	}

	return s.String()
}

func renderHeader() string {
	logo := logoStyle.Render(`    __ __                 __
   / // /___ ___ __ ___ _/ /____
  / _  / _ '/ // / _ '/ __/ -_)
 /_//_/\_,_/\_, /\_,_/\__/\___/
           /___/`)
	ver := versionStyle.Render(" v" + transfer.Version)
	return logo + ver + "\n\n"
}

func (m Model) viewDiscover() string {
	var s strings.Builder

	mode := strings.ToUpper(m.Mode)
	s.WriteString(sectionStyle.Render("[ "+mode+" ]") + "\n\n")

	s.WriteString(fmt.Sprintf("  %s ", m.spinner.View()))
	if m.Mode == "send" {
		s.WriteString("Scanning local network for active receivers...\n")
	} else {
		s.WriteString(fmt.Sprintf("Listening on %s:%d", m.localIP, m.port))
		if m.advertised != "" {
			s.WriteString(fmt.Sprintf("  (advertised as %s)", m.advertised))
		}
		s.WriteString("\n")
	}

	s.WriteString(hint("Esc to abort"))
	return s.String()
}

func (m Model) viewSelectPeer() string {
	var s strings.Builder

	s.WriteString(sectionStyle.Render("[ SELECT PEER ]") + "\n\n")

	// Table header
	hdr := fmt.Sprintf("    %-20s  %-22s  %s", "NAME", "ADDRESS", "OS")
	s.WriteString(tableHeaderStyle.Render(hdr) + "\n")
	s.WriteString(tableHeaderStyle.Render("    " + strings.Repeat("-", 54)) + "\n")

	for i, p := range m.peers {
		addr := fmt.Sprintf("%s:%d", p.IP, p.Port)
		if i == m.selectedIdx {
			line := fmt.Sprintf("  %s %-20s  %-22s  %s",
				cursorStyle.Render(">"),
				tableSelectedStyle.Render(p.Name),
				tableSelectedStyle.Render(addr),
				tableSelectedStyle.Render(p.OS))
			s.WriteString(line + "\n")
		} else {
			line := fmt.Sprintf("    %-20s  %-22s  %s", p.Name, addr, p.OS)
			s.WriteString(tableRowStyle.Render(line) + "\n")
		}
	}

	s.WriteString(tableHeaderStyle.Render("    " + strings.Repeat("-", 54)) + "\n")
	s.WriteString(hint("j/k or Up/Down to navigate  |  Enter to select  |  Esc to cancel"))
	return s.String()
}

func (m Model) viewHandshake() string {
	var s strings.Builder

	s.WriteString(sectionStyle.Render("[ SECURING CONNECTION ]") + "\n\n")
	s.WriteString(fmt.Sprintf("  %s Exchanging cryptographic keys with %s...\n",
		m.spinner.View(), valueStyle.Render(m.peerAddr)))
	s.WriteString(hint("Esc to abort"))
	return s.String()
}

func (m Model) viewTransferring() string {
	var s strings.Builder

	direction := "RECEIVING"
	arrow := "<--"
	if m.isSend {
		direction = "SENDING"
		arrow = "-->"
	}

	s.WriteString(sectionStyle.Render("[ "+direction+" ]") + "\n\n")

	pct := 0.0
	if m.fileSize > 0 {
		pct = float64(m.progress) / float64(m.fileSize) * 100.0
	}

	s.WriteString(kv("File", m.fileName))
	s.WriteString(kv("Size", formatBytes(m.fileSize)))
	s.WriteString(kv("Peer", fmt.Sprintf("localhost %s %s", arrow, m.peerAddr)))
	s.WriteString("\n")

	// Large, prominent progress bar
	bar := drawProgressBar(40, m.progress, m.fileSize)
	pctStr := pctStyle.Render(fmt.Sprintf("%5.1f%%", pct))
	s.WriteString(fmt.Sprintf("  %s  %s  %s\n",
		bar, pctStr, speedStyle.Render(formatSpeed(m.speed))))

	s.WriteString("\n")
	s.WriteString(kv("Transferred", fmt.Sprintf("%s / %s", formatBytes(m.progress), formatBytes(m.fileSize))))
	s.WriteString(kv("ETA", formatDuration(m.eta)))

	s.WriteString(hint("Esc to abort"))
	return s.String()
}

func (m Model) viewCompleted() string {
	var s strings.Builder

	avgSpeed := 0.0
	duration := time.Since(m.startTime)
	if duration.Seconds() > 0 {
		avgSpeed = float64(m.fileSize) / duration.Seconds()
	}

	direction := "File Received"
	if m.isSend {
		direction = "File Sent"
	}

	var b strings.Builder
	b.WriteString(successStyle.Render("  "+direction+" Successfully") + "\n\n")
	b.WriteString(kvInner("File", m.fileName))
	b.WriteString(kvInner("Size", formatBytes(m.fileSize)))
	b.WriteString(kvInner("SHA-256", hashStyle.Render(m.hash)))
	b.WriteString(kvInner("Speed", speedStyle.Render(formatSpeed(avgSpeed))))
	b.WriteString(kvInner("Duration", formatDuration(duration)))

	s.WriteString(successBoxStyle.Render(b.String()))
	s.WriteString("\n")
	s.WriteString(hint("Enter or Q to exit"))
	return s.String()
}

func (m Model) viewError() string {
	var s strings.Builder

	var b strings.Builder
	b.WriteString(errorStyle.Render("  Transfer Failed") + "\n\n")
	b.WriteString(kvInner("Error", m.err.Error()))

	s.WriteString(errorBoxStyle.Render(b.String()))
	s.WriteString("\n")
	s.WriteString(hint("Enter or Q to exit"))
	return s.String()
}

// -- Helpers --

func kv(label, value string) string {
	return "  " + labelStyle.Render(label) + "  " + valueStyle.Render(value) + "\n"
}

func kvInner(label, value string) string {
	l := lipgloss.NewStyle().Foreground(slate).Width(12)
	return "  " + l.Render(label) + "  " + valueStyle.Render(value) + "\n"
}

func hint(text string) string {
	return "\n" + hintStyle.Render("  "+text) + "\n"
}

func drawProgressBar(width int, progress, total int64) string {
	if total <= 0 {
		return barEmptyStyle.Render(strings.Repeat("-", width))
	}
	ratio := float64(progress) / float64(total)
	if ratio > 1.0 {
		ratio = 1.0
	}
	if ratio < 0 {
		ratio = 0
	}

	filled := int(math.Round(ratio * float64(width)))
	empty := width - filled
	if empty < 0 {
		empty = 0
	}

	var bar strings.Builder
	if filled > 0 {
		bar.WriteString(barFilledStyle.Render(strings.Repeat("=", filled)))
	}
	if empty > 0 {
		bar.WriteString(barEmptyStyle.Render(strings.Repeat("-", empty)))
	}
	return bar.String()
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

func formatSpeed(bps float64) string {
	if bps <= 0 {
		return "-- B/s"
	}
	return formatBytes(int64(bps)) + "/s"
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "--"
	}
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	sec := d / time.Second

	if h > 0 {
		return fmt.Sprintf("%dh %dm %ds", h, m, sec)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, sec)
	}
	return fmt.Sprintf("%ds", sec)
}
