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
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

type State int

const (
	StateDiscover State = iota
	StateSelectPeer
	StatePasswordPrompt
	StateHandshake
	StateTransferring
	StateCompleted
	StateError
)

type PeerDiscoveredMsg []network.Peer

type ListeningMsg struct {
	Port      int
	LocalAddr string
}

type StartTransferMsg struct {
	FileName  string
	FileSize  int64
	IsSend    bool
	PeerAddr  string
	LocalAddr string
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

	asciiBorder = lipgloss.Border{
		Top:         "-",
		Bottom:      "-",
		Left:        "|",
		Right:       "|",
		TopLeft:     "+",
		TopRight:    "+",
		BottomLeft:  "+",
		BottomRight: "+",
	}

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
			Border(asciiBorder).
			BorderForeground(indigo).
			Padding(1, 3)

	successBoxStyle = lipgloss.NewStyle().
			Border(asciiBorder).
			BorderForeground(green).
			Padding(1, 3)

	errorBoxStyle = lipgloss.NewStyle().
			Border(asciiBorder).
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
	localAddr    string
	advertised   string
	localIP      string
	port         int
	width        int
	asciiOnly    bool
	peers        []network.Peer
	selectedIdx  int
	progress     int64
	startTime    time.Time
	lastTime     time.Time
	lastProgress int64
	lastSpeed    float64
	speed        float64
	eta          time.Duration
	hash         string
	compSize     int64
	isSend       bool
	password     string

	spinner          spinner.Model
	textInput        textinput.Model
	MsgChan          chan tea.Msg
	SelectedPeerChan chan network.Peer
	PasswordChan     chan string
	Ctx              context.Context
	Cancel           context.CancelFunc
}

func NewModel(ctx context.Context, cancel context.CancelFunc, mode string, filePath string, peerAddr string, port int, localIP string, advertised string, password string) Model {
	s := spinner.New()
	s.Spinner = spinner.Line
	s.Style = lipgloss.NewStyle().Foreground(cyan)

	ti := textinput.New()
	ti.Placeholder = "Enter 4-word passphrase (or empty for none)"
	ti.Focus()
	ti.CharLimit = 128
	ti.Width = 40

	initialState := StateDiscover
	if mode == "send" && peerAddr != "" {
		if password == "" {
			initialState = StatePasswordPrompt
		} else {
			initialState = StateHandshake
		}
	}

	return Model{
		Mode:             mode,
		state:            initialState,
		filePath:         filePath,
		peerAddr:         peerAddr,
		port:             port,
		localIP:          localIP,
		localAddr:        localIP,
		advertised:       advertised,
		password:         password,
		width:            80,
		asciiOnly:        !termenv.HasDarkBackground() || termenv.ColorProfile() == termenv.Ascii,
		spinner:          s,
		textInput:        ti,
		MsgChan:          make(chan tea.Msg, 64),
		SelectedPeerChan: make(chan network.Peer, 1),
		PasswordChan:     make(chan string, 1),
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
	case tea.WindowSizeMsg:
		m.width = msg.Width
		if msg.Width > 0 && msg.Width < 72 {
			m.asciiOnly = true
		}

	case tea.KeyMsg:
		if m.state == StatePasswordPrompt && msg.String() != "enter" && msg.String() != "ctrl+c" && msg.String() != "esc" {
			var cmd tea.Cmd
			m.textInput, cmd = m.textInput.Update(msg)
			return m, cmd
		}

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
				m.peerAddr = network.FormatAddress(selected.IP, selected.Port)
				select {
				case m.SelectedPeerChan <- selected:
				default:
				}
				if m.password == "" {
					m.state = StatePasswordPrompt
				} else {
					m.state = StateHandshake
				}
			} else if m.state == StatePasswordPrompt {
				m.password = strings.TrimSpace(m.textInput.Value())
				select {
				case m.PasswordChan <- m.password:
				default:
				}
				m.state = StateHandshake
			} else if m.state == StateCompleted || m.state == StateError {
				return m, tea.Quit
			}
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		var tiCmd tea.Cmd
		m.textInput, tiCmd = m.textInput.Update(msg)
		cmds = append(cmds, cmd, tiCmd)

	case PeerDiscoveredMsg:
		m.peers = msg
		if len(m.peers) == 0 {
			m.state = StateError
			m.err = fmt.Errorf("no peers discovered on the local network")
		} else {
			m.state = StateSelectPeer
		}

	case ListeningMsg:
		m.port = msg.Port
		if msg.LocalAddr != "" {
			m.localAddr = msg.LocalAddr
		}

	case StartTransferMsg:
		m.state = StateTransferring
		m.fileName = msg.FileName
		m.fileSize = msg.FileSize
		m.isSend = msg.IsSend
		if msg.PeerAddr != "" {
			m.peerAddr = msg.PeerAddr
		}
		if msg.LocalAddr != "" {
			m.localAddr = msg.LocalAddr
		}
		m.startTime = time.Now()
		m.lastTime = m.startTime
		m.progress = 0
		m.lastProgress = 0
		m.lastSpeed = 0
		m.speed = 0
		m.eta = 0

	case ProgressMsg:
		curr := int64(msg)
		if curr < m.progress {
			curr = m.progress
		}
		m.progress = curr
		now := time.Now()
		elapsed := now.Sub(m.lastTime)

		if elapsed >= 200*time.Millisecond {
			bytesDelta := curr - m.lastProgress
			instSpeed := float64(bytesDelta) / elapsed.Seconds()

			if m.lastSpeed == 0 {
				m.lastSpeed = instSpeed
			} else {
				m.lastSpeed = m.lastSpeed*0.55 + instSpeed*0.45
			}
			totalElapsed := now.Sub(m.startTime).Seconds()
			if totalElapsed > 0 {
				avgSpeed := float64(curr) / totalElapsed
				m.speed = avgSpeed*0.65 + m.lastSpeed*0.35
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

	switch msg.(type) {
	case PeerDiscoveredMsg, ListeningMsg, StartTransferMsg, ProgressMsg, DoneMsg, ErrorMsg:
		cmds = append(cmds, m.WaitForMsg())
	}
	return m, tea.Batch(cmds...)
}

func (m Model) View() string {
	var s strings.Builder

	s.WriteString(m.renderHeader())

	switch m.state {
	case StateDiscover:
		s.WriteString(m.viewDiscover())
	case StateSelectPeer:
		s.WriteString(m.viewSelectPeer())
	case StatePasswordPrompt:
		s.WriteString(m.viewPasswordPrompt())
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

func (m Model) renderHeader() string {
	logo := `    __ __                 __
   / // /___ ___ __ ___ _/ /____
  / _  / _ '/ // / _ '/ __/ -_)
 /_//_/\_,_/\_, /\_,_/\__/\___/
           /___/`
	if !m.asciiOnly {
		logo = logoStyle.Render(logo)
	}
	ver := " v" + transfer.Version
	if !m.asciiOnly {
		ver = versionStyle.Render(ver)
	}
	return logo + ver + "\n\n"
}

func (m Model) viewDiscover() string {
	var s strings.Builder

	mode := strings.ToUpper(m.Mode)
	s.WriteString(sectionStyle.Render("[ "+mode+" ]") + "\n\n")

	spin := "-"
	if !m.asciiOnly {
		spin = m.spinner.View()
	}
	s.WriteString(fmt.Sprintf("  %s ", spin))
	if m.Mode == "send" {
		s.WriteString("Scanning local network for active receivers...\n")
	} else {
		addr := m.localAddr
		if addr == "" {
			addr = network.FormatAddress(m.localIP, m.port)
		}
		s.WriteString(fmt.Sprintf("Listening on %s", addr))
		if m.advertised != "" {
			s.WriteString(fmt.Sprintf("  (advertised as %s)", m.advertised))
		}
		s.WriteString("\n\n")
		if m.password != "" {
			pw := m.password
			if !m.asciiOnly {
				pw = hashStyle.Render(pw)
			}
			s.WriteString("  Passphrase: " + pw + "\n")
		}
	}

	s.WriteString(hint("Esc to abort"))
	return s.String()
}

func (m Model) viewSelectPeer() string {
	var s strings.Builder

	s.WriteString(sectionStyle.Render("[ SELECT PEER ]") + "\n\n")

	// Table header
	nameW, addrW := m.peerTableWidths()
	hdr := fmt.Sprintf("    %s  %s  %s", padCell("NAME", nameW), padCell("ADDRESS", addrW), "OS")
	s.WriteString(tableHeaderStyle.Render(hdr) + "\n")
	s.WriteString(tableHeaderStyle.Render("    "+strings.Repeat("-", nameW+addrW+14)) + "\n")

	for i, p := range m.peers {
		addr := network.FormatAddress(p.IP, p.Port)
		name := fitString(p.Name, nameW)
		osName := fitString(p.OS, 12)
		if i == m.selectedIdx {
			line := fmt.Sprintf("  > %s  %s  %s", padCell(name, nameW), padCell(fitString(addr, addrW), addrW), osName)
			if !m.asciiOnly {
				line = tableSelectedStyle.Render(line)
			}
			s.WriteString(line + "\n")
		} else {
			line := fmt.Sprintf("    %s  %s  %s", padCell(name, nameW), padCell(fitString(addr, addrW), addrW), osName)
			s.WriteString(tableRowStyle.Render(line) + "\n")
		}
	}

	s.WriteString(tableHeaderStyle.Render("    "+strings.Repeat("-", nameW+addrW+14)) + "\n")
	s.WriteString(hint("j/k or Up/Down to navigate  |  Enter to select  |  Esc to cancel"))
	return s.String()
}

func (m Model) viewPasswordPrompt() string {
	var s strings.Builder
	s.WriteString(sectionStyle.Render("[ AUTHENTICATION ]") + "\n\n")
	s.WriteString("  Enter receiver's passphrase (leave empty if none):\n\n")
	s.WriteString("  " + m.textInput.View() + "\n\n")
	s.WriteString(hint("Enter to submit  |  Esc to cancel"))
	return s.String()
}

func (m Model) viewHandshake() string {
	var s strings.Builder

	s.WriteString(sectionStyle.Render("[ SECURING CONNECTION ]") + "\n\n")
	spin := "-"
	if !m.asciiOnly {
		spin = m.spinner.View()
	}
	s.WriteString(fmt.Sprintf("  %s Exchanging cryptographic keys with %s...\n",
		spin, m.renderValue(m.peerAddr)))
	s.WriteString(hint("Esc to abort"))
	return s.String()
}

func (m Model) viewTransferring() string {
	var s strings.Builder

	direction := "RECEIVING"
	arrow := "<-"
	if m.isSend {
		direction = "SENDING"
		arrow = "->"
	}

	s.WriteString(sectionStyle.Render("[ "+direction+" ]") + "\n\n")

	pct := 0.0
	if m.fileSize > 0 {
		pct = float64(m.progress) / float64(m.fileSize) * 100.0
	}

	valueW := m.valueWidth()
	s.WriteString(m.kv("File", m.fit(m.fileName, valueW)))
	s.WriteString(m.kv("Size", formatBytes(m.fileSize)))
	s.WriteString(m.kv("Peer", m.fit(fmt.Sprintf("%s %s %s", m.localAddr, arrow, m.peerAddr), valueW)))
	s.WriteString("\n")

	barWidth := m.progressWidth()
	bar := m.drawProgressBar(barWidth, m.progress, m.fileSize)
	pctStr := fmt.Sprintf("%5.1f%%", pct)
	if !m.asciiOnly {
		pctStr = pctStyle.Render(pctStr)
	}
	speed := m.fit(formatSpeed(m.speed), 12)
	if !m.asciiOnly {
		speed = speedStyle.Render(speed)
	}
	s.WriteString(fmt.Sprintf("  %s  %s  %s\n",
		bar, pctStr, speed))

	s.WriteString("\n")
	s.WriteString(m.kv("Transferred", fmt.Sprintf("%s / %s", formatBytes(m.progress), formatBytes(m.fileSize))))
	s.WriteString(m.kv("ETA", formatDuration(m.eta)))

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
	title := "  " + direction + " Successfully"
	if !m.asciiOnly {
		title = successStyle.Render(title)
	}
	b.WriteString(title + "\n\n")
	b.WriteString(m.kvInner("File", m.fit(m.fileName, m.innerValueWidth())))
	b.WriteString(m.kvInner("Size", formatBytes(m.fileSize)))
	hash := m.hash
	if m.width > 0 && m.width < 92 && len(hash) > 24 {
		hash = hash[:24]
	}
	if !m.asciiOnly {
		hash = hashStyle.Render(hash)
	}
	b.WriteString(m.kvInner("SHA-256", hash))
	speed := formatSpeed(avgSpeed)
	if !m.asciiOnly {
		speed = speedStyle.Render(speed)
	}
	b.WriteString(m.kvInner("Speed", speed))
	b.WriteString(m.kvInner("Duration", formatDuration(duration)))

	s.WriteString(m.box(b.String(), false))
	s.WriteString("\n")
	s.WriteString(hint("Enter or Q to exit"))
	return s.String()
}

func (m Model) viewError() string {
	var s strings.Builder

	var b strings.Builder
	title := "  Transfer Failed"
	if !m.asciiOnly {
		title = errorStyle.Render(title)
	}
	b.WriteString(title + "\n\n")
	errText := ""
	if m.err != nil {
		errText = m.err.Error()
	}
	b.WriteString(m.kvInner("Error", m.fit(errText, 60)))

	s.WriteString(m.box(b.String(), true))
	s.WriteString("\n")
	s.WriteString(hint("Enter or Q to exit"))
	return s.String()
}

// -- Helpers --

func (m Model) kv(label, value string) string {
	if m.asciiOnly {
		return "  " + rightPad(label, 14) + "  " + value + "\n"
	}
	return "  " + labelStyle.Render(label) + "  " + valueStyle.Render(value) + "\n"
}

func (m Model) kvInner(label, value string) string {
	if m.asciiOnly {
		return "  " + rightPad(label, 12) + "  " + value + "\n"
	}
	l := lipgloss.NewStyle().Foreground(slate).Width(12)
	return "  " + l.Render(label) + "  " + valueStyle.Render(value) + "\n"
}

func hint(text string) string {
	return "\n" + hintStyle.Render("  "+text) + "\n"
}

func (m Model) drawProgressBar(width int, progress, total int64) string {
	if total <= 0 {
		return "[" + strings.Repeat("-", width) + "]"
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

	filledText := strings.Repeat("#", filled)
	emptyText := strings.Repeat("-", empty)
	if !m.asciiOnly {
		filledText = barFilledStyle.Render(filledText)
		emptyText = barEmptyStyle.Render(emptyText)
	}
	return "[" + filledText + emptyText + "]"
}

func (m Model) renderValue(value string) string {
	if m.asciiOnly {
		return value
	}
	return valueStyle.Render(value)
}

func (m Model) progressWidth() int {
	width := 40
	if m.width > 0 {
		width = m.width - 32
	}
	if width < 12 {
		return 12
	}
	if width > 50 {
		return 50
	}
	return width
}

func (m Model) valueWidth() int {
	width := 48
	if m.width > 0 {
		width = m.width - 22
	}
	if width < 18 {
		return 18
	}
	if width > 64 {
		return 64
	}
	return width
}

func (m Model) innerValueWidth() int {
	width := 48
	if m.width > 0 {
		width = m.width - 28
	}
	if width < 18 {
		return 18
	}
	if width > 64 {
		return 64
	}
	return width
}

func (m Model) peerTableWidths() (int, int) {
	if m.width > 0 && m.width < 64 {
		return 14, 18
	}
	if m.width > 0 && m.width < 84 {
		return 18, 22
	}
	return 24, 28
}

func (m Model) box(content string, isError bool) string {
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	inner := 72
	if m.width > 0 {
		inner = m.width - 6
	}
	if inner < 36 {
		inner = 36
	}
	if inner > 84 {
		inner = 84
	}

	var b strings.Builder
	b.WriteString("+" + strings.Repeat("-", inner+2) + "+\n")
	for _, line := range lines {
		plain := stripANSI(line)
		if len(plain) > inner {
			line = m.fit(plain, inner)
			plain = line
		}
		b.WriteString("| " + line + strings.Repeat(" ", inner-len(plain)) + " |\n")
	}
	b.WriteString("+" + strings.Repeat("-", inner+2) + "+")

	if m.asciiOnly {
		return b.String()
	}
	if isError {
		return errorBoxStyle.Render(content)
	}
	return successBoxStyle.Render(content)
}

func (m Model) fit(value string, limit int) string {
	return fitString(value, limit)
}

func rightPad(value string, width int) string {
	if len(value) >= width {
		return value
	}
	return value + strings.Repeat(" ", width-len(value))
}

func padCell(value string, width int) string {
	value = fitString(value, width)
	if lipgloss.Width(value) >= width {
		return value
	}
	return value + strings.Repeat(" ", width-lipgloss.Width(value))
}

func fitString(value string, limit int) string {
	if limit <= 0 || lipgloss.Width(value) <= limit {
		return value
	}
	if limit <= 3 {
		return truncateCells(value, limit)
	}
	return truncateCells(value, limit-3) + "..."
}

func truncateCells(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	var b strings.Builder
	used := 0
	for _, r := range value {
		w := lipgloss.Width(string(r))
		if used+w > limit {
			break
		}
		b.WriteRune(r)
		used += w
	}
	return b.String()
}

func stripANSI(value string) string {
	var b strings.Builder
	inEsc := false
	for i := 0; i < len(value); i++ {
		c := value[i]
		if inEsc {
			if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
				inEsc = false
			}
			continue
		}
		if c == 0x1b {
			inEsc = true
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
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
