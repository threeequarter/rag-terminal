package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"rag-chat/internal/vector"
)

type chatCreateField int

const (
	fieldName chatCreateField = iota
	fieldSystemPrompt
	fieldTemperature
	fieldTopK
	fieldReranking
)

type ChatCreateModel struct {
	nameInput        textinput.Model
	systemPromptArea textarea.Model
	temperatureInput textinput.Model
	topKInput        textinput.Model
	rerankingEnabled bool
	currentField     chatCreateField
	llmModel         string
	embedModel       string
	rerankModel      string
	width            int
	height           int
	err              error
}

type ChatCreated struct {
	Chat *vector.Chat
}

func NewChatCreateModel(llmModel, embedModel, rerankModel string, width, height int) ChatCreateModel {
	nameInput := textinput.New()
	nameInput.Placeholder = "My Awesome Chat"
	nameInput.Focus()
	nameInput.CharLimit = 100
	nameInput.Width = 50

	systemPromptArea := textarea.New()
	systemPromptArea.Placeholder = "You are a helpful assistant..."
	systemPromptArea.SetWidth(60)
	systemPromptArea.SetHeight(5)
	systemPromptArea.CharLimit = 1000

	tempInput := textinput.New()
	tempInput.Placeholder = "0.7"
	tempInput.CharLimit = 4
	tempInput.Width = 10

	topKInput := textinput.New()
	topKInput.Placeholder = "5"
	topKInput.CharLimit = 3
	topKInput.Width = 10

	// Enable reranking by default only if reranking model is available
	rerankingEnabled := rerankModel != ""

	return ChatCreateModel{
		nameInput:        nameInput,
		systemPromptArea: systemPromptArea,
		temperatureInput: tempInput,
		topKInput:        topKInput,
		rerankingEnabled: rerankingEnabled,
		currentField:     fieldName,
		llmModel:         llmModel,
		embedModel:       embedModel,
		rerankModel:      rerankModel,
		width:            width,
		height:           height,
	}
}

func (m ChatCreateModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m ChatCreateModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "ctrl+x":
			return m, tea.Quit

		case "esc":
			return m, func() tea.Msg {
				return BackToChatList{}
			}

		case "tab", "shift+tab":
			if msg.String() == "tab" {
				m.nextField()
			} else {
				m.prevField()
			}
			return m, nil

		case "enter":
			if m.currentField == fieldSystemPrompt {
				// Allow new lines in textarea
				var cmd tea.Cmd
				m.systemPromptArea, cmd = m.systemPromptArea.Update(msg)
				return m, cmd
			}

			// Submit on enter from other fields
			if m.currentField == fieldReranking {
				return m, m.createChat()
			}
			m.nextField()
			return m, nil

		case " ":
			if m.currentField == fieldReranking {
				m.rerankingEnabled = !m.rerankingEnabled
				return m, nil
			}
		}
	}

	// Update active field
	switch m.currentField {
	case fieldName:
		var cmd tea.Cmd
		m.nameInput, cmd = m.nameInput.Update(msg)
		cmds = append(cmds, cmd)
	case fieldSystemPrompt:
		var cmd tea.Cmd
		m.systemPromptArea, cmd = m.systemPromptArea.Update(msg)
		cmds = append(cmds, cmd)
	case fieldTemperature:
		var cmd tea.Cmd
		m.temperatureInput, cmd = m.temperatureInput.Update(msg)
		cmds = append(cmds, cmd)
	case fieldTopK:
		var cmd tea.Cmd
		m.topKInput, cmd = m.topKInput.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m ChatCreateModel) View() string {
	if m.err != nil {
		return errorStyle.Render(fmt.Sprintf("Error: %v\n\nPress Esc to go back", m.err))
	}

	var b strings.Builder

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("10")).
		Render("Create New Chat")

	b.WriteString(title + "\n\n")

	// Name field
	nameLabel := "Chat Name:"
	if m.currentField == fieldName {
		nameLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true).Render(nameLabel)
	}
	b.WriteString(nameLabel + "\n")
	b.WriteString(m.nameInput.View() + "\n\n")

	// System prompt field
	promptLabel := "System Prompt:"
	if m.currentField == fieldSystemPrompt {
		promptLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true).Render(promptLabel)
	}
	b.WriteString(promptLabel + "\n")
	b.WriteString(m.systemPromptArea.View() + "\n\n")

	// Temperature field
	tempLabel := "Temperature (0-1):"
	if m.currentField == fieldTemperature {
		tempLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true).Render(tempLabel)
	}
	b.WriteString(tempLabel + "\n")
	b.WriteString(m.temperatureInput.View() + "\n\n")

	// TopK field
	topKLabel := "Top K (retrieval count):"
	if m.currentField == fieldTopK {
		topKLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true).Render(topKLabel)
	}
	b.WriteString(topKLabel + "\n")
	b.WriteString(m.topKInput.View() + "\n\n")

	// Reranking checkbox
	rerankLabel := "Enable Reranking:"
	if m.currentField == fieldReranking {
		rerankLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true).Render(rerankLabel)
	}
	checkbox := "[ ]"
	if m.rerankingEnabled {
		checkbox = "[✓]"
	}
	b.WriteString(rerankLabel + " " + checkbox + "\n\n")

	// Model info
	rerankInfo := "None"
	if m.rerankModel != "" {
		rerankInfo = m.rerankModel
	}
	modelInfo := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render(
		fmt.Sprintf("LLM: %s | Embed: %s | Rerank: %s", m.llmModel, m.embedModel, rerankInfo))
	b.WriteString(modelInfo + "\n\n")

	helpText := "Tab/Shift+Tab: Navigate • Enter: Create • Space: Toggle • Esc: Cancel"
	b.WriteString(helpStyle.Render(helpText))

	return b.String()
}

func (m *ChatCreateModel) nextField() {
	m.currentField++
	if m.currentField > fieldReranking {
		m.currentField = fieldName
	}
	m.updateFocus()
}

func (m *ChatCreateModel) prevField() {
	m.currentField--
	if m.currentField < fieldName {
		m.currentField = fieldReranking
	}
	m.updateFocus()
}

func (m *ChatCreateModel) updateFocus() {
	m.nameInput.Blur()
	m.systemPromptArea.Blur()
	m.temperatureInput.Blur()
	m.topKInput.Blur()

	switch m.currentField {
	case fieldName:
		m.nameInput.Focus()
	case fieldSystemPrompt:
		m.systemPromptArea.Focus()
	case fieldTemperature:
		m.temperatureInput.Focus()
	case fieldTopK:
		m.topKInput.Focus()
	}
}

func (m ChatCreateModel) createChat() tea.Cmd {
	return func() tea.Msg {
		// Validate and parse inputs
		name := m.nameInput.Value()
		if name == "" {
			name = "Untitled Chat"
		}

		systemPrompt := m.systemPromptArea.Value()
		if systemPrompt == "" {
			systemPrompt = "You are a helpful assistant."
		}

		temperature := 0.7
		if m.temperatureInput.Value() != "" {
			if temp, err := strconv.ParseFloat(m.temperatureInput.Value(), 64); err == nil {
				if temp >= 0 && temp <= 1 {
					temperature = temp
				}
			}
		}

		topK := 5
		if m.topKInput.Value() != "" {
			if k, err := strconv.Atoi(m.topKInput.Value()); err == nil {
				if k > 0 {
					topK = k
				}
			}
		}

		chat := &vector.Chat{
			ID:           fmt.Sprintf("chat-%d", time.Now().Unix()),
			Name:         name,
			SystemPrompt: systemPrompt,
			LLMModel:     m.llmModel,
			EmbedModel:   m.embedModel,
			RerankModel:  m.rerankModel,
			CreatedAt:    time.Now(),
			Temperature:  temperature,
			TopK:         topK,
			UseReranking: m.rerankingEnabled,
			MaxTokens:    2048,
		}

		return ChatCreated{Chat: chat}
	}
}

type BackToChatList struct{}
