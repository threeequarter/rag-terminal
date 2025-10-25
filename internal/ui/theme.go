package ui

import (
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"
	tint "github.com/lrstanley/bubbletint"
)

// Theme registry for the application
var Theme *tint.Registry

// Common style elements used across all views
var (
	// Title styles
	TitleStyle               lipgloss.Style
	TitleWithPaddingStyle    lipgloss.Style
	ActiveLabelStyle         lipgloss.Style
	InactiveLabelStyle       lipgloss.Style
	errorStyle               lipgloss.Style
	ErrorMessageStyle        lipgloss.Style
	statusBarStyle           lipgloss.Style
	helpStyle                lipgloss.Style
	HelpTextSimpleStyle      lipgloss.Style
	ActiveButtonStyle        lipgloss.Style
	InactiveButtonStyle      lipgloss.Style
	UserMessageLabelStyle    lipgloss.Style
	AssistantMessageLabelStyle lipgloss.Style
	UserMessageContentStyle  lipgloss.Style
	AssistantMessageContentStyle lipgloss.Style
	TimestampStyle           lipgloss.Style
	MetadataStyle            lipgloss.Style
	SpinnerStyle             lipgloss.Style
	ViewportBorderStyle      lipgloss.Style
	ScrollIndicatorStyle     lipgloss.Style
)

func init() {
	// Initialize with 3024 Night theme
	tint.NewDefaultRegistry()
	tint.SetTint(tint.Tint3024Night)
	Theme = tint.DefaultRegistry

	// Initialize styles after tint is set up
	TitleStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(tint.BrightPurple())

	TitleWithPaddingStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(tint.BrightPurple()).
		Padding(0, 1)

	// Label styles
	ActiveLabelStyle = lipgloss.NewStyle().
		Foreground(tint.Cyan()).
		Bold(true)

	InactiveLabelStyle = lipgloss.NewStyle().
		Foreground(tint.BrightBlack())

	// Error styles
	errorStyle = lipgloss.NewStyle().
		Foreground(tint.Red()).
		Bold(true).
		Padding(1)

	ErrorMessageStyle = lipgloss.NewStyle().
		Foreground(tint.Red())

	// Status bar styles
	statusBarStyle = lipgloss.NewStyle().
		Foreground(tint.BrightBlack()).
		Padding(0, 1)

	// Help text styles
	helpStyle = lipgloss.NewStyle().
		Foreground(tint.BrightBlack()).
		Padding(1, 0, 0, 1)

	HelpTextSimpleStyle = lipgloss.NewStyle().
		Foreground(tint.BrightBlack())

	// Button styles
	ActiveButtonStyle = lipgloss.NewStyle().
		Foreground(tint.Bg()).
		Background(tint.Green()).
		Bold(true)

	InactiveButtonStyle = lipgloss.NewStyle().
		Foreground(tint.Green())

	// Message styles (for chat messages)
	UserMessageLabelStyle = lipgloss.NewStyle().
		Foreground(tint.Blue()).
		Bold(true)

	AssistantMessageLabelStyle = lipgloss.NewStyle().
		Foreground(tint.Purple()).
		Bold(true)

	UserMessageContentStyle = lipgloss.NewStyle().
		Foreground(tint.Fg()).
		Padding(0, 1).
		MarginBottom(1)

	AssistantMessageContentStyle = lipgloss.NewStyle().
		Foreground(tint.Fg()).
		Padding(0, 1).
		MarginBottom(1)

	TimestampStyle = lipgloss.NewStyle().
		Foreground(tint.BrightBlack())

	// Metadata/info styles
	MetadataStyle = lipgloss.NewStyle().
		Foreground(tint.BrightBlack())

	// Spinner styles
	SpinnerStyle = lipgloss.NewStyle().
		Foreground(tint.BrightPurple())

	// Border styles
	ViewportBorderStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(tint.BrightBlue()).
		Padding(0, 1)

	// Scroll indicator style
	ScrollIndicatorStyle = lipgloss.NewStyle().
		Foreground(tint.Cyan()).
		Bold(false)
}

// Helper functions for dynamic styles

// ConfigureListTitle configures a list's title styles to match the application theme
func ConfigureListTitle(l *list.Model) {
	l.Styles.Title = TitleStyle
	l.Styles.TitleBar = lipgloss.NewStyle().
		Padding(0, 0, 1, 0)
}

// ConfigureListStyles configures all list styles to match the application theme
func ConfigureListStyles(l *list.Model) {
	// Title styles
	l.Styles.Title = TitleStyle
	l.Styles.TitleBar = lipgloss.NewStyle().
		Padding(0, 0, 1, 0)

	// Pagination styles
	l.Styles.PaginationStyle = lipgloss.NewStyle().
		Foreground(tint.BrightBlack())

	// Help styles
	l.Styles.HelpStyle = helpStyle

	// Filter styles
	l.Styles.FilterPrompt = lipgloss.NewStyle().
		Foreground(tint.Cyan())
	l.Styles.FilterCursor = lipgloss.NewStyle().
		Foreground(tint.BrightPurple())

	// Status bar
	l.Styles.StatusBar = lipgloss.NewStyle().
		Foreground(tint.BrightBlack()).
		Padding(0, 0, 1, 0)

	// Divider
	l.Styles.DividerDot = lipgloss.NewStyle().
		Foreground(tint.BrightBlack()).
		SetString(" • ")
}

// CreateThemedDelegate creates a themed list delegate with application colors
func CreateThemedDelegate() list.DefaultDelegate {
	d := list.NewDefaultDelegate()

	// Configure item styles
	d.Styles.SelectedTitle = lipgloss.NewStyle().
		Foreground(tint.BrightPurple()).
		Bold(true).
		BorderLeft(true).
		BorderForeground(tint.BrightPurple()).
		Padding(0, 0, 0, 1)

	d.Styles.SelectedDesc = lipgloss.NewStyle().
		Foreground(tint.Cyan()).
		BorderLeft(true).
		BorderForeground(tint.BrightPurple()).
		Padding(0, 0, 0, 1)

	d.Styles.NormalTitle = lipgloss.NewStyle().
		Foreground(tint.Fg()).
		Padding(0, 0, 0, 2)

	d.Styles.NormalDesc = lipgloss.NewStyle().
		Foreground(tint.BrightBlack()).
		Padding(0, 0, 0, 2)

	d.Styles.DimmedTitle = lipgloss.NewStyle().
		Foreground(tint.BrightBlack()).
		Padding(0, 0, 0, 2)

	d.Styles.DimmedDesc = lipgloss.NewStyle().
		Foreground(tint.BrightBlack()).
		Padding(0, 0, 0, 2)

	return d
}

// GetFieldLabelStyle returns the appropriate style for a field label based on whether it's active
func GetFieldLabelStyle(isActive bool) lipgloss.Style {
	if isActive {
		return ActiveLabelStyle
	}
	return InactiveLabelStyle
}

// GetButtonStyle returns the appropriate style for a button based on whether it's active
func GetButtonStyle(isActive bool) lipgloss.Style {
	if isActive {
		return ActiveButtonStyle
	}
	return InactiveButtonStyle
}

// RenderFieldLabel renders a field label with the appropriate style
func RenderFieldLabel(label string, isActive bool) string {
	return GetFieldLabelStyle(isActive).Render(label)
}

// RenderButton renders a button with the appropriate style
func RenderButton(label string, isActive bool) string {
	if isActive {
		return ActiveButtonStyle.Render(" " + label + " ")
	}
	return InactiveButtonStyle.Render("[ " + label + " ]")
}

// RenderError renders an error message
func RenderError(msg string) string {
	return ErrorMessageStyle.Render("  ✗ " + msg)
}

// RenderViewportWithBorder renders content with a viewport border style
func RenderViewportWithBorder(content string) string {
	return ViewportBorderStyle.Render(content)
}

// GetUserMessageContentStyle returns a style for user message content with given width
func GetUserMessageContentStyle(width int) lipgloss.Style {
	return UserMessageContentStyle.
		Width(width - 10).
		Align(lipgloss.Right)
}

// GetAssistantMessageContentStyle returns a style for assistant message content with given width
func GetAssistantMessageContentStyle(width int) lipgloss.Style {
	return AssistantMessageContentStyle.
		Width(width - 10)
}

// GetTimestampStyle returns a style for timestamp with given width
func GetTimestampStyle(width int) lipgloss.Style {
	return TimestampStyle.
		Align(lipgloss.Right).
		Width(width - 10)
}
