package ui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"rag-chat/internal/nexa"
)

type modelSelectState int

const (
	selectingLLM modelSelectState = iota
	selectingEmbedding
)

type ModelSelectModel struct {
	list          list.Model
	llmModels     []nexa.Model
	embedModels   []nexa.Model
	state         modelSelectState
	selectedLLM   string
	selectedEmbed string
	width         int
	height        int
	err           error
}

type modelItem struct {
	model nexa.Model
}

func (i modelItem) Title() string       { return i.model.Name }
func (i modelItem) Description() string { return fmt.Sprintf("Type: %s", i.model.Type) }
func (i modelItem) FilterValue() string { return i.model.Name }

type ModelSelectionComplete struct {
	LLMModel   string
	EmbedModel string
}

func NewModelSelectModel(models []nexa.Model, width, height int) ModelSelectModel {
	// Separate models by type
	var llmModels, embedModels []nexa.Model
	for _, m := range models {
		if m.Type == "text-generation" {
			llmModels = append(llmModels, m)
		} else if m.Type == "embeddings" {
			embedModels = append(embedModels, m)
		}
	}

	// Create list items for LLM models
	items := make([]list.Item, len(llmModels))
	for i, m := range llmModels {
		items[i] = modelItem{model: m}
	}

	l := list.New(items, list.NewDefaultDelegate(), width, height-4)
	l.Title = "Select LLM Model"
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)

	return ModelSelectModel{
		list:        l,
		llmModels:   llmModels,
		embedModels: embedModels,
		state:       selectingLLM,
		width:       width,
		height:      height,
	}
}

func (m ModelSelectModel) Init() tea.Cmd {
	return nil
}

func (m ModelSelectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.list.SetSize(msg.Width, msg.Height-4)
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "ctrl+x":
			return m, tea.Quit

		case "enter":
			selectedItem := m.list.SelectedItem()
			if selectedItem == nil {
				return m, nil
			}

			if m.state == selectingLLM {
				// Save LLM selection and move to embedding selection
				m.selectedLLM = selectedItem.(modelItem).model.Name
				m.state = selectingEmbedding

				// Update list with embedding models
				items := make([]list.Item, len(m.embedModels))
				for i, model := range m.embedModels {
					items[i] = modelItem{model: model}
				}
				m.list.SetItems(items)
				m.list.Title = "Select Embedding Model"
				return m, nil
			} else if m.state == selectingEmbedding {
				// Save embedding selection and complete
				m.selectedEmbed = selectedItem.(modelItem).model.Name
				return m, func() tea.Msg {
					return ModelSelectionComplete{
						LLMModel:   m.selectedLLM,
						EmbedModel: m.selectedEmbed,
					}
				}
			}

		case "esc":
			if m.state == selectingEmbedding {
				// Go back to LLM selection
				m.state = selectingLLM
				items := make([]list.Item, len(m.llmModels))
				for i, model := range m.llmModels {
					items[i] = modelItem{model: model}
				}
				m.list.SetItems(items)
				m.list.Title = "Select LLM Model"
				return m, nil
			}
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m ModelSelectModel) View() string {
	if m.err != nil {
		return errorStyle.Render(fmt.Sprintf("Error: %v\n\nPress Ctrl+C to quit", m.err))
	}

	helpText := "↑/↓: Navigate • Enter: Select • Esc: Back • Ctrl+X: Quit"
	if m.state == selectingLLM {
		helpText = "↑/↓: Navigate • Enter: Select • Ctrl+X: Quit"
	}

	var status string
	if m.selectedLLM != "" {
		status = fmt.Sprintf("Selected LLM: %s", m.selectedLLM)
	}
	if m.selectedEmbed != "" {
		status += fmt.Sprintf(" | Embedding: %s", m.selectedEmbed)
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		m.list.View(),
		statusBarStyle.Render(status),
		helpStyle.Render(helpText),
	)
}

var (
	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("9")).
			Bold(true).
			Padding(1)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Padding(0, 1)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Padding(1, 0, 0, 1)
)
