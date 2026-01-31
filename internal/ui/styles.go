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
	IconShield      = "🛡"
	IconDanger      = "⛔"
	IconDiagnostics = "🔬"
	IconAgent       = "🤖"
)

// Security audit styles - category-specific colors (softer pastel variants)
var (
	// Destructive operations - high risk (soft red on light red)
	SecurityDestructive = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#991B1B")).
		Background(lipgloss.Color("#FEE2E2")).
		Bold(true).
		Padding(0, 1)

	// Execute operations - elevated risk (soft orange on light orange)
	SecurityExecute = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#9A3412")).
		Background(lipgloss.Color("#FFEDD5")).
		Bold(true).
		Padding(0, 1)

	// Write/Git operations - moderate risk (soft amber on light amber)
	SecurityWrite = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#92400E")).
		Background(lipgloss.Color("#FEF3C7")).
		Bold(true).
		Padding(0, 1)

	// Security audit box style
	SecurityAuditBox = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(Warning).
		Padding(0, 1).
		Width(60)

	// Security audit header
	SecurityAuditHeader = lipgloss.NewStyle().
		Foreground(Warning).
		Bold(true)

	// Security audit label style
	SecurityAuditLabel = lipgloss.NewStyle().
		Foreground(Muted).
		Width(12)

	// Security audit value style
	SecurityAuditValue = lipgloss.NewStyle().
		Bold(true)

	// Security implication style
	SecurityImplication = lipgloss.NewStyle().
		Foreground(Warning).
		Italic(true)

	// Batch indicator style
	BatchIndicator = lipgloss.NewStyle().
		Foreground(Info).
		Bold(true)
)

// Security mode colors (softer pastel variants)
var (
	SecurityRed    = lipgloss.Color("#F87171") // Soft red
	SecurityOrange = lipgloss.Color("#FB923C") // Soft orange
)

// Security mode banner styles
var (
	// Security mode banner box - prominent red/orange border
	SecurityModeBannerBox = lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(SecurityOrange).
		Padding(0, 2).
		Width(64)

	// Security mode title - bold with shield icon
	SecurityModeTitle = lipgloss.NewStyle().
		Foreground(SecurityOrange).
		Bold(true)

	// Security mode subtitle
	SecurityModeSubtitle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FBBF24")).
		Italic(true)

	// Security mode feature bullet
	SecurityModeBullet = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#F97316"))

	// Security mode prompt indicator
	SecurityModePrompt = lipgloss.NewStyle().
		Foreground(SecurityOrange).
		Bold(true)
)

// Display constants
const (
	// IDDisplayLength is the number of characters to show for truncated IDs
	IDDisplayLength = 8
	// MaxCommandDisplay is the max length for command display in tool status
	MaxCommandDisplay = 40
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
