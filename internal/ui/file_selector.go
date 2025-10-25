package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	overlay "github.com/rmhubbert/bubbletea-overlay"

	"rag-terminal/internal/vector"
)

// FileSelectorModel represents the file selector overlay foreground
type FileSelectorModel struct {
	files         []vector.Document
	filteredFiles []vector.Document
	filterInput   textinput.Model
	selectedIndex int
	width         int
	height        int
}

// FileSelected is sent when user selects a file
type FileSelected struct {
	FileName string
}

// FileSelectorClosed is sent when file selector is closed without selection
type FileSelectorClosed struct{}

func NewFileSelectorModel() FileSelectorModel {
	ti := textinput.New()
	ti.Placeholder = "Type to filter..."
	ti.CharLimit = 100
	ti.Width = 40

	return FileSelectorModel{
		files:         []vector.Document{},
		filteredFiles: []vector.Document{},
		filterInput:   ti,
		selectedIndex: 0,
	}
}

func (m FileSelectorModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m *FileSelectorModel) SetFiles(files []vector.Document) {
	m.files = files
	m.filterInput.SetValue("")
	m.filterInput.Focus()
	m.updateFilteredFiles()
	m.selectedIndex = 0
}

func (m *FileSelectorModel) updateFilteredFiles() {
	filterText := strings.ToLower(strings.TrimSpace(m.filterInput.Value()))

	if filterText == "" {
		m.filteredFiles = m.files
		return
	}

	m.filteredFiles = []vector.Document{}
	for _, doc := range m.files {
		if strings.Contains(strings.ToLower(doc.FileName), filterText) {
			m.filteredFiles = append(m.filteredFiles, doc)
		}
	}
}

func (m FileSelectorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("up"))):
			if m.selectedIndex > 0 {
				m.selectedIndex--
			}
			return m, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("down"))):
			if m.selectedIndex < len(m.filteredFiles)-1 {
				m.selectedIndex++
			}
			return m, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
			if len(m.filteredFiles) > 0 {
				selected := m.filteredFiles[m.selectedIndex]
				return m, func() tea.Msg {
					return FileSelected{FileName: selected.FileName}
				}
			}
			return m, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
			// If filter has text, clear it first
			if m.filterInput.Value() != "" {
				m.filterInput.SetValue("")
				m.updateFilteredFiles()
				m.selectedIndex = 0
				return m, nil
			}
			// Otherwise close the selector
			return m, func() tea.Msg {
				return FileSelectorClosed{}
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("backspace"))):
			// Handle backspace for filter input
			m.filterInput, cmd = m.filterInput.Update(msg)
			oldLen := len(m.filteredFiles)
			m.updateFilteredFiles()
			// Reset selection if filtered list changed
			if oldLen != len(m.filteredFiles) || m.selectedIndex >= len(m.filteredFiles) {
				m.selectedIndex = 0
			}
			return m, cmd

		default:
			// Handle text input for filtering
			if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
				m.filterInput, cmd = m.filterInput.Update(msg)
				oldLen := len(m.filteredFiles)
				m.updateFilteredFiles()
				// Reset selection if filtered list changed
				if oldLen != len(m.filteredFiles) || m.selectedIndex >= len(m.filteredFiles) {
					m.selectedIndex = 0
				}
				return m, cmd
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Calculate filter input width based on 50% overlay width
		overlayWidth := m.width / 2
		if overlayWidth < 40 {
			overlayWidth = 40
		}
		m.filterInput.Width = overlayWidth - 12
	}

	// Update textinput for cursor blinking
	m.filterInput, cmd = m.filterInput.Update(msg)
	return m, cmd
}

func (m FileSelectorModel) View() string {
	if len(m.files) == 0 {
		return m.renderEmptyOverlay()
	}

	return m.renderFileList()
}

func (m FileSelectorModel) renderEmptyOverlay() string {
	// Use 50% of window width
	overlayWidth := m.width / 2
	if overlayWidth < 40 {
		overlayWidth = 40 // Minimum width
	}

	// Build content
	var content strings.Builder
	content.WriteString(GetFileSelectorTitleStyle(true).Render("File Selector"))
	content.WriteString("\n\n")
	content.WriteString(GetFileSelectorMessageStyle(overlayWidth).Render("No files embedded in this chat"))
	content.WriteString("\n\n")
	content.WriteString(HelpTextSimpleStyle.Render("Press Esc to close"))

	return GetFileSelectorBorderStyle(overlayWidth, true).Render(content.String())
}

func (m FileSelectorModel) renderFileList() string {
	// Calculate overlay dimensions based on number of files
	maxFiles := 10
	fileCount := len(m.filteredFiles)
	if fileCount > maxFiles {
		fileCount = maxFiles
	}

	// Use 50% of window width
	overlayWidth := m.width / 2
	if overlayWidth < 40 {
		overlayWidth = 40 // Minimum width
	}

	// Build content
	var content strings.Builder

	// Title with filtered count
	title := fmt.Sprintf("Embedded Files (%d)", len(m.files))
	if len(m.filteredFiles) != len(m.files) {
		title = fmt.Sprintf("Embedded Files (%d of %d)", len(m.filteredFiles), len(m.files))
	}
	content.WriteString(GetFileSelectorTitleStyle(false).Render(title))
	content.WriteString("\n\n")

	// Render filter input
	content.WriteString(GetFileSelectorFilterLabelStyle().Render("Filter: "))
	content.WriteString(GetFileSelectorFilterInputStyle().Render(m.filterInput.View()))
	content.WriteString("\n\n")

	// Handle no matches case
	if len(m.filteredFiles) == 0 {
		content.WriteString(GetFileSelectorMessageStyle(overlayWidth).Render("No files match your filter"))
		content.WriteString("\n\n")
		content.WriteString(HelpTextSimpleStyle.Render("Type to filter • Esc: Clear filter"))
		return GetFileSelectorBorderStyle(overlayWidth, false).Render(content.String())
	}

	// Calculate visible range for scrolling
	visibleStart := 0
	visibleEnd := len(m.filteredFiles)
	if len(m.filteredFiles) > maxFiles {
		// Scroll to keep selected item in view
		visibleStart = m.selectedIndex - maxFiles/2
		if visibleStart < 0 {
			visibleStart = 0
		}
		visibleEnd = visibleStart + maxFiles
		if visibleEnd > len(m.filteredFiles) {
			visibleEnd = len(m.filteredFiles)
			visibleStart = visibleEnd - maxFiles
			if visibleStart < 0 {
				visibleStart = 0
			}
		}
	}

	// Render file list
	for i := visibleStart; i < visibleEnd; i++ {
		doc := m.filteredFiles[i]
		fileName := doc.FileName

		// Truncate long filenames
		maxNameLength := overlayWidth - 12
		if len(fileName) > maxNameLength {
			ext := filepath.Ext(fileName)
			nameWithoutExt := strings.TrimSuffix(fileName, ext)
			if len(nameWithoutExt) > maxNameLength-len(ext)-3 {
				fileName = nameWithoutExt[:maxNameLength-len(ext)-3] + "..." + ext
			}
		}

		indicator := "  "
		if i == m.selectedIndex {
			indicator = "▶ "
			content.WriteString(GetFileSelectorItemStyle(overlayWidth, "selected").Render(indicator + fileName))
		} else {
			content.WriteString(GetFileSelectorItemStyle(overlayWidth, "normal").Render(indicator + fileName))
		}
		content.WriteString("\n")
	}

	// Show scroll indicator if there are more files
	if len(m.filteredFiles) > maxFiles {
		scrollInfo := fmt.Sprintf("\n%s", GetFileSelectorItemStyle(overlayWidth, "dimmed").Render(
			fmt.Sprintf("Showing %d-%d of %d files", visibleStart+1, visibleEnd, len(m.filteredFiles)),
		))
		content.WriteString(scrollInfo)
	}

	content.WriteString("\n")

	// Update help text based on filter state
	helpText := "Type to filter • ↑/↓: Navigate • Enter: Select • Esc: "
	if m.filterInput.Value() != "" {
		helpText += "Clear filter"
	} else {
		helpText += "Cancel"
	}
	content.WriteString(HelpTextSimpleStyle.Render(helpText))

	return GetFileSelectorBorderStyle(overlayWidth, false).Render(content.String())
}

// FileSelectorOverlayModel wraps the file selector with the overlay library
type FileSelectorOverlayModel struct {
	overlayModel *overlay.Model
	fileSelector FileSelectorModel
	visible      bool
}

func NewFileSelectorOverlayModel() FileSelectorOverlayModel {
	return FileSelectorOverlayModel{
		fileSelector: NewFileSelectorModel(),
		visible:      false,
	}
}

func (m *FileSelectorOverlayModel) SetFiles(files []vector.Document) {
	m.fileSelector.SetFiles(files)
}

func (m *FileSelectorOverlayModel) Show() {
	m.visible = true
}

func (m *FileSelectorOverlayModel) Hide() {
	m.visible = false
}

func (m *FileSelectorOverlayModel) IsVisible() bool {
	return m.visible
}

func (m *FileSelectorOverlayModel) UpdateSize(width, height int) {
	m.fileSelector.width = width
	m.fileSelector.height = height
}

func (m *FileSelectorOverlayModel) UpdateFileSelector(msg tea.Msg) tea.Cmd {
	if !m.visible {
		return nil
	}

	var cmd tea.Cmd
	var mdl tea.Model
	mdl, cmd = m.fileSelector.Update(msg)
	m.fileSelector = mdl.(FileSelectorModel)
	return cmd
}

func (m FileSelectorOverlayModel) RenderOverlay(backgroundView string) string {
	if !m.visible {
		return backgroundView
	}

	// Create overlay with file selector as foreground and background view
	// Position at top center with slight vertical offset
	overlayModel := overlay.New(
		m.fileSelector,
		&staticViewModel{content: backgroundView},
		overlay.Center, // horizontal position
		overlay.Top,    // vertical position
		0,              // x offset
		1,              // y offset (minimal top margin)
	)

	return overlayModel.View()
}

// staticViewModel is a simple model that renders static content (background)
type staticViewModel struct {
	content string
}

func (m staticViewModel) Init() tea.Cmd {
	return nil
}

func (m staticViewModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m, nil
}

func (m staticViewModel) View() string {
	return m.content
}
