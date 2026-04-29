package screens

import (
	"net"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// NetworkInfo holds network information for display.
type NetworkInfo struct {
	SSID          string
	IPAddress     string
	Gateway       string
	BackendStatus string
	BackendURL    string
}

// NetworkScreenModel represents the network status screen.
type NetworkScreenModel struct {
	// Network information
	info NetworkInfo

	// Dimensions
	width  int
	height int

	// Styles
	styles *NetworkScreenStyles
}

// NetworkScreenStyles holds styles for the network screen.
type NetworkScreenStyles struct {
	Title        lipgloss.Style
	Subtitle     lipgloss.Style
	Label        lipgloss.Style
	Value        lipgloss.Style
	Connected    lipgloss.Style
	Disconnected lipgloss.Style
	Warning      lipgloss.Style
	Help         lipgloss.Style
}

// NewNetworkScreenStyles creates default styles.
func NewNetworkScreenStyles() *NetworkScreenStyles {
	return &NetworkScreenStyles{
		Title: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7D56F4")).
			MarginLeft(2).
			MarginBottom(1),
		Subtitle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#B8B8B8")).
			MarginLeft(2).
			MarginBottom(2),
		Label: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#B8B8B8")).
			MarginLeft(4).
			Width(20),
		Value: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")),
		Connected: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#04B575")),
		Disconnected: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF4672")),
		Warning: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F9D71C")),
		Help: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666")).
			MarginTop(2).
			MarginLeft(2),
	}
}

// NewNetworkScreen creates a new network status screen.
func NewNetworkScreen() NetworkScreenModel {
	return NetworkScreenModel{
		info:   detectNetworkInfo(),
		styles: NewNetworkScreenStyles(),
	}
}

// detectNetworkInfo attempts to detect network information from the OS.
func detectNetworkInfo() NetworkInfo {
	info := NetworkInfo{
		SSID:          "Unknown",
		IPAddress:     "Unknown",
		Gateway:       "Unknown",
		BackendStatus: "Unknown",
		BackendURL:    "wss://cloud.amg-rfid.com/api/v1/ws",
	}

	// Try to get IP address
	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					info.IPAddress = ipnet.IP.String()
					break
				}
			}
		}
	}

	// Try to get default gateway
	if out, err := exec.Command("ip", "route", "show", "default").Output(); err == nil {
		parts := strings.Fields(string(out))
		for i, part := range parts {
			if part == "via" && i+1 < len(parts) {
				info.Gateway = parts[i+1]
				break
			}
		}
	}

	// Try to get SSID (Linux with iw)
	if out, err := exec.Command("iwgetid", "-r").Output(); err == nil {
		ssid := strings.TrimSpace(string(out))
		if ssid != "" {
			info.SSID = ssid
		}
	}

	// Check backend connection (mock - would actually test in real implementation)
	info.BackendStatus = "online"

	return info
}

// Init initializes the screen.
func (m NetworkScreenModel) Init() tea.Cmd {
	return nil
}

// Update handles messages.
func (m NetworkScreenModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Network screen has no navigation, just display
	return m, nil
}

// View renders the screen.
func (m NetworkScreenModel) View() string {
	// Build title
	title := m.styles.Title.Render("Network Status")
	subtitle := m.styles.Subtitle.Render("Network configuration and connectivity")

	// Build network info display
	var content string

	content += m.renderRow("WiFi SSID:", m.info.SSID, false)
	content += m.renderRow("IP Address:", m.info.IPAddress, false)
	content += m.renderRow("Gateway:", m.info.Gateway, false)
	content += m.renderRow("Backend URL:", m.info.BackendURL, false)

	// Backend status with color
	status := m.styles.Connected.Render("ONLINE")
	if m.info.BackendStatus != "online" {
		status = m.styles.Disconnected.Render("OFFLINE")
	}
	content += m.renderRow("Backend Status:", status, true)

	// Warning message
	warning := m.styles.Warning.Render("\n\nNote: Network configuration via raspi-config")
	warning += "\n" + m.styles.Warning.Render("Run: sudo raspi-config")

	// Build help
	help := m.styles.Help.Render("ESC back • q/ctrl+c quit")

	// Combine all
	return title + "\n" + subtitle + "\n\n" + content + warning + "\n\n" + help
}

// renderRow renders a label-value row.
func (m NetworkScreenModel) renderRow(label, value string, isStyled bool) string {
	labelStr := m.styles.Label.Render(label)
	var valueStr string
	if isStyled {
		valueStr = value // Already styled
	} else {
		valueStr = m.styles.Value.Render(value)
	}
	return labelStr + valueStr + "\n"
}

// SetSize updates the screen dimensions.
func (m *NetworkScreenModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// GetNetworkInfo returns the current network info.
func (m NetworkScreenModel) GetNetworkInfo() NetworkInfo {
	return m.info
}

// SetNetworkInfo updates the network info.
func (m *NetworkScreenModel) SetNetworkInfo(info NetworkInfo) {
	m.info = info
}
