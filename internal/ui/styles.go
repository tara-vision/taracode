package ui

import "github.com/charmbracelet/lipgloss"

// Color palette (Claude Code pastel style)
var (
	Primary = lipgloss.Color("#A78BFA") // Soft violet
	Success = lipgloss.Color("#2dd4bf") // Soft mint green
	Error   = lipgloss.Color("#FCA5A5") // Soft coral
	Warning = lipgloss.Color("#FCD34D") // Soft amber
	Muted   = lipgloss.Color("#94A3B8") // Soft slate
	Info    = lipgloss.Color("#93C5FD") // Soft sky blue
)

// Text styles
var (
	Bold   = lipgloss.NewStyle().Bold(true)
	Italic = lipgloss.NewStyle().Italic(true)
	Subtle = lipgloss.NewStyle().Foreground(Muted)
)

// Tool status styles
var (
	ToolRead  = lipgloss.NewStyle().Foreground(Muted)
	ToolWrite = lipgloss.NewStyle().Foreground(Success)
	ToolError = lipgloss.NewStyle().Foreground(Error)
	ToolInfo  = lipgloss.NewStyle().Foreground(Info)
)

// UI element styles
var (
	// Prompt style
	PromptStyle = lipgloss.NewStyle().Foreground(Primary).Bold(true)

	// Title style for welcome message
	TitleStyle = lipgloss.NewStyle().Bold(true).Foreground(Primary)

	// Spinner style
	SpinnerStyle = lipgloss.NewStyle().Foreground(Primary)

	// Session info style
	SessionStyle = lipgloss.NewStyle().Foreground(Info)

	// Warning style
	WarningStyle = lipgloss.NewStyle().Foreground(Warning)

	// Success style
	SuccessStyle = lipgloss.NewStyle().Foreground(Success)

	// Error style
	ErrorStyle = lipgloss.NewStyle().Foreground(Error).Bold(true)
)

// Icon constants
const (
	IconSuccess  = "✓"
	IconError    = "✗"
	IconArrow    = "→"
	IconWarning  = "⚠"
	IconInfo     = "ℹ"
	IconFolder   = "📁"
	IconSession  = "📝"
	IconTip      = "💡"
	IconStar     = "🌟"
	IconThinking = "⠋"
	IconImage    = "📷"
	IconCloud    = "☁️"
	IconLock     = "🔒"
	IconShield   = "🛡"
	IconDanger   = "⛔"
)

// Display constants
const (
	// IDDisplayLength is the number of characters to show for truncated IDs
	IDDisplayLength = 8
	// MaxCommandDisplay is the max length for command display in tool status
	MaxCommandDisplay = 60
)

// TruncateID safely truncates an ID for display
func TruncateID(id string, length int) string {
	if length <= 0 {
		length = IDDisplayLength
	}
	if len(id) <= length {
		return id
	}
	return id[:length]
}

// TruncateString truncates a string with ellipsis
func TruncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
