package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	overlay "github.com/rmhubbert/bubbletea-overlay"

	"rag-terminal/internal/logging"
	"rag-terminal/internal/vector"
)

// FactsViewerModel represents the facts viewer overlay foreground
type FactsViewerModel struct {
	facts         []vector.ProfileFact
	filteredFacts []vector.ProfileFact
	filterInput   textinput.Model
	selectedIndex int
	width         int
	height        int
	vectorStore   vector.VectorStore
	chatID        string
}

// FactSelected is sent when user selects a fact
type FactSelected struct {
	Key string
}

// FactDeleted is sent when a fact is deleted
type FactDeleted struct {
	Key string
}

// FactsViewerClosed is sent when facts viewer is closed
type FactsViewerClosed struct{}

func NewFactsViewerModel(vectorStore vector.VectorStore) FactsViewerModel {
	ti := textinput.New()
	ti.Placeholder = "Type to filter..."
	ti.CharLimit = 100
	ti.Width = 40

	return FactsViewerModel{
		facts:         []vector.ProfileFact{},
		filteredFacts: []vector.ProfileFact{},
		filterInput:   ti,
		selectedIndex: 0,
		vectorStore:   vectorStore,
	}
}

func (m FactsViewerModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m *FactsViewerModel) SetFacts(chatID string, profile *vector.UserProfile) {
	m.chatID = chatID
	m.facts = []vector.ProfileFact{}

	// Extract facts from profile
	if profile != nil {
		for _, fact := range profile.Facts {
			m.facts = append(m.facts, fact)
		}
	}

	m.filterInput.SetValue("")
	m.filterInput.Focus()
	m.updateFilteredFacts()
	m.selectedIndex = 0

	// Update filter input width based on current overlay width
	if m.width > 0 {
		overlayWidth := m.width / 2
		if overlayWidth < 50 {
			overlayWidth = 50
		}
		m.filterInput.Width = overlayWidth - 12
	}
}

func (m *FactsViewerModel) updateFilteredFacts() {
	filterText := strings.ToLower(strings.TrimSpace(m.filterInput.Value()))

	if filterText == "" {
		m.filteredFacts = m.facts
		return
	}

	m.filteredFacts = []vector.ProfileFact{}
	for _, fact := range m.facts {
		// Search in key and value
		if strings.Contains(strings.ToLower(fact.Key), filterText) ||
			strings.Contains(strings.ToLower(fact.Value), filterText) {
			m.filteredFacts = append(m.filteredFacts, fact)
		}
	}
}

func (m FactsViewerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			if m.selectedIndex < len(m.filteredFacts)-1 {
				m.selectedIndex++
			}
			return m, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("delete"))):
			if len(m.filteredFacts) > 0 && m.selectedIndex < len(m.filteredFacts) {
				selectedFact := m.filteredFacts[m.selectedIndex]
				return m, func() tea.Msg {
					return FactDeleted{Key: selectedFact.Key}
				}
			}
			return m, nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
			// If filter has text, clear it first
			if m.filterInput.Value() != "" {
				m.filterInput.SetValue("")
				m.updateFilteredFacts()
				m.selectedIndex = 0
				return m, nil
			}
			// Otherwise close the viewer
			return m, func() tea.Msg {
				return FactsViewerClosed{}
			}

		case key.Matches(msg, key.NewBinding(key.WithKeys("backspace"))):
			// Handle backspace for filter input
			m.filterInput, cmd = m.filterInput.Update(msg)
			oldLen := len(m.filteredFacts)
			m.updateFilteredFacts()
			// Reset selection if filtered list changed
			if oldLen != len(m.filteredFacts) || m.selectedIndex >= len(m.filteredFacts) {
				m.selectedIndex = 0
			}
			return m, cmd

		default:
			// Handle text input for filtering
			if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
				m.filterInput, cmd = m.filterInput.Update(msg)
				oldLen := len(m.filteredFacts)
				m.updateFilteredFacts()
				// Reset selection if filtered list changed
				if oldLen != len(m.filteredFacts) || m.selectedIndex >= len(m.filteredFacts) {
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
		if overlayWidth < 50 {
			overlayWidth = 50
		}
		m.filterInput.Width = overlayWidth - 12
	}

	// Update textinput for cursor blinking
	m.filterInput, cmd = m.filterInput.Update(msg)
	return m, cmd
}

func (m FactsViewerModel) View() string {
	if len(m.facts) == 0 {
		return m.renderEmptyOverlay()
	}

	return m.renderFactsList()
}

func (m FactsViewerModel) renderEmptyOverlay() string {
	// Use 50% of window width
	overlayWidth := m.width / 2
	if overlayWidth < 50 {
		overlayWidth = 50 // Minimum width
	}

	// Build content
	var content strings.Builder
	content.WriteString(GetFileSelectorTitleStyle(true).Render("User Facts"))
	content.WriteString("\n\n")
	content.WriteString(GetFileSelectorMessageStyle(overlayWidth).Render("No facts extracted yet"))
	content.WriteString("\n\n")
	content.WriteString(HelpTextSimpleStyle.Render("Press Esc to close"))

	return GetFileSelectorBorderStyle(overlayWidth, true).Render(content.String())
}

func (m FactsViewerModel) renderFactsList() string {
	// Calculate overlay dimensions
	maxFacts := 12
	factCount := len(m.filteredFacts)
	if factCount > maxFacts {
		factCount = maxFacts
	}

	// Use 50% of window width
	overlayWidth := m.width / 2
	if overlayWidth < 50 {
		overlayWidth = 50 // Minimum width
	}

	// Build content
	var content strings.Builder

	// Title with filtered count
	title := fmt.Sprintf("User Facts (%d)", len(m.facts))
	if len(m.filteredFacts) != len(m.facts) {
		title = fmt.Sprintf("User Facts (%d of %d)", len(m.filteredFacts), len(m.facts))
	}
	content.WriteString(GetFileSelectorTitleStyle(false).Render(title))
	content.WriteString("\n\n")

	// Render filter input
	content.WriteString(GetFileSelectorFilterLabelStyle().Render("Filter: "))
	content.WriteString(GetFileSelectorFilterInputStyle().Render(m.filterInput.View()))
	content.WriteString("\n\n")

	// Handle no matches case
	if len(m.filteredFacts) == 0 {
		content.WriteString(GetFileSelectorMessageStyle(overlayWidth).Render("No facts match your filter"))
		content.WriteString("\n\n")
		content.WriteString(HelpTextSimpleStyle.Render("Type to filter • Esc: Clear filter"))
		return GetFileSelectorBorderStyle(overlayWidth, false).Render(content.String())
	}

	// Calculate visible range for scrolling
	visibleStart := 0
	visibleEnd := len(m.filteredFacts)
	if len(m.filteredFacts) > maxFacts {
		// Scroll to keep selected item in view
		visibleStart = m.selectedIndex - maxFacts/2
		if visibleStart < 0 {
			visibleStart = 0
		}
		visibleEnd = visibleStart + maxFacts
		if visibleEnd > len(m.filteredFacts) {
			visibleEnd = len(m.filteredFacts)
			visibleStart = visibleEnd - maxFacts
			if visibleStart < 0 {
				visibleStart = 0
			}
		}
	}

	// Render facts list
	for i := visibleStart; i < visibleEnd; i++ {
		fact := m.filteredFacts[i]

		// Format: [confidence] key: value
		displayText := fmt.Sprintf("[%.0f%%] %s: %s", fact.Confidence*100, fact.Key, fact.Value)

		// Truncate long text
		maxTextLength := overlayWidth - 12
		if len(displayText) > maxTextLength {
			displayText = displayText[:maxTextLength-3] + "..."
		}

		indicator := "  "
		if i == m.selectedIndex {
			indicator = "▶ "
			content.WriteString(GetFileSelectorItemStyle(overlayWidth, "selected").Render(indicator + displayText))
		} else {
			content.WriteString(GetFileSelectorItemStyle(overlayWidth, "normal").Render(indicator + displayText))
		}
		content.WriteString("\n")
	}

	// Show scroll indicator if there are more facts
	if len(m.filteredFacts) > maxFacts {
		scrollInfo := fmt.Sprintf("\n%s", GetFileSelectorItemStyle(overlayWidth, "dimmed").Render(
			fmt.Sprintf("Showing %d-%d of %d facts", visibleStart+1, visibleEnd, len(m.filteredFacts)),
		))
		content.WriteString(scrollInfo)
		content.WriteString("\n")
	}

	// Help text
	content.WriteString("\n")
	content.WriteString(HelpTextSimpleStyle.Render("↑/↓: Navigate • Del: Delete • Esc: Close"))

	return GetFileSelectorBorderStyle(overlayWidth, false).Render(content.String())
}

// FactsViewerOverlayModel wraps the facts viewer with the overlay library
type FactsViewerOverlayModel struct {
	overlayModel  *overlay.Model
	factsViewer   FactsViewerModel
	visible       bool
}

func NewFactsViewerOverlayModel(vectorStore vector.VectorStore) FactsViewerOverlayModel {
	return FactsViewerOverlayModel{
		factsViewer: NewFactsViewerModel(vectorStore),
		visible:     false,
	}
}

func (m *FactsViewerOverlayModel) SetFacts(chatID string, profile *vector.UserProfile) {
	m.factsViewer.SetFacts(chatID, profile)
}

func (m *FactsViewerOverlayModel) Show() {
	m.visible = true
}

func (m *FactsViewerOverlayModel) Hide() {
	m.visible = false
}

func (m *FactsViewerOverlayModel) IsVisible() bool {
	return m.visible
}

func (m *FactsViewerOverlayModel) UpdateSize(width, height int) {
	m.factsViewer.width = width
	m.factsViewer.height = height
}

func (m *FactsViewerOverlayModel) UpdateFactsViewer(msg tea.Msg) tea.Cmd {
	if !m.visible {
		return nil
	}

	var cmd tea.Cmd
	var mdl tea.Model
	mdl, cmd = m.factsViewer.Update(msg)
	m.factsViewer = mdl.(FactsViewerModel)
	return cmd
}

func (m *FactsViewerOverlayModel) DeleteSelectedFact(ctx context.Context, key string) error {
	if err := m.factsViewer.vectorStore.DeleteProfileFact(ctx, m.factsViewer.chatID, key); err != nil {
		logging.Error("Failed to delete fact %s: %v", key, err)
		return err
	}

	// Update local state
	newFacts := []vector.ProfileFact{}
	for _, fact := range m.factsViewer.facts {
		if fact.Key != key {
			newFacts = append(newFacts, fact)
		}
	}
	m.factsViewer.facts = newFacts

	m.factsViewer.updateFilteredFacts()
	if m.factsViewer.selectedIndex >= len(m.factsViewer.filteredFacts) && m.factsViewer.selectedIndex > 0 {
		m.factsViewer.selectedIndex--
	}

	return nil
}

func (m FactsViewerOverlayModel) RenderOverlay(backgroundView string) string {
	if !m.visible {
		return backgroundView
	}

	// Create overlay with facts viewer as foreground and background view
	// Position at top center with slight vertical offset
	overlayModel := overlay.New(
		m.factsViewer,
		&staticViewModel{content: backgroundView},
		overlay.Center, // horizontal position
		overlay.Top,    // vertical position
		0,              // x offset
		1,              // y offset (minimal top margin)
	)

	return overlayModel.View()
}
