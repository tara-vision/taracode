package ui

import "github.com/charmbracelet/lipgloss"

// Color palette
var (
	Primary = lipgloss.Color("#7C3AED") // Purple
	Success = lipgloss.Color("#10B981") // Green
	Error   = lipgloss.Color("#EF4444") // Red
	Warning = lipgloss.Color("#F59E0B") // Amber
	Muted   = lipgloss.Color("#6B7280") // Gray
	Info    = lipgloss.Color("#3B82F6") // Blue
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
)
