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

	"rag-terminal/internal/vector"
)

type chatCreateField int

const (
	fieldName chatCreateField = iota
	fieldSystemPrompt
	fieldTemperature
	fieldTopK
	fieldReranking
	fieldCreateButton
)

type ChatCreateModel struct {
	nameInput           textinput.Model
	systemPromptArea    textarea.Model
	temperatureInput    textinput.Model
	topKInput           textinput.Model
	rerankingEnabled    bool
	currentField        chatCreateField
	llmModel            string
	embedModel          string
	width               int
	height              int
	err                 error
	temperatureError    string
	topKError           string
	validationAttempted bool
}

type ChatCreated struct {
	Chat *vector.Chat
}

func NewChatCreateModel(llmModel, embedModel string, width, height int) ChatCreateModel {
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
	tempInput.SetValue("0.7")
	tempInput.CharLimit = 4
	tempInput.Width = 10

	topKInput := textinput.New()
	topKInput.Placeholder = "5"
	topKInput.SetValue("5")
	topKInput.CharLimit = 3
	topKInput.Width = 10

	// Enable LLM reranking by default
	rerankingEnabled := true

	return ChatCreateModel{
		nameInput:        nameInput,
		systemPromptArea: systemPromptArea,
		temperatureInput: tempInput,
		topKInput:        topKInput,
		rerankingEnabled: rerankingEnabled,
		currentField:     fieldName,
		llmModel:         llmModel,
		embedModel:       embedModel,
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

	case ValidationFailed:
		m.temperatureError = msg.TemperatureError
		m.topKError = msg.TopKError
		m.validationAttempted = true
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

			// Create chat only when on Create button
			if m.currentField == fieldCreateButton {
				return m, m.createChat()
			}

			// Move to next field for all other fields
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
		// Clear temperature error when user types
		if m.validationAttempted {
			m.temperatureError = ""
		}
	case fieldTopK:
		var cmd tea.Cmd
		m.topKInput, cmd = m.topKInput.Update(msg)
		cmds = append(cmds, cmd)
		// Clear topK error when user types
		if m.validationAttempted {
			m.topKError = ""
		}
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
		Foreground(lipgloss.Color("205")).
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
	tempLabel := "Temperature (0-2):"
	if m.currentField == fieldTemperature {
		tempLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true).Render(tempLabel)
	}
	b.WriteString(tempLabel + "\n")
	b.WriteString(m.temperatureInput.View() + "\n")
	if m.temperatureError != "" {
		errorMsg := lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("  ✗ " + m.temperatureError)
		b.WriteString(errorMsg + "\n")
	}
	b.WriteString("\n")

	// TopK field
	topKLabel := "Top K (1-100):"
	if m.currentField == fieldTopK {
		topKLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true).Render(topKLabel)
	}
	b.WriteString(topKLabel + "\n")
	b.WriteString(m.topKInput.View() + "\n")
	if m.topKError != "" {
		errorMsg := lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("  ✗ " + m.topKError)
		b.WriteString(errorMsg + "\n")
	}
	b.WriteString("\n")

	// LLM Reranking checkbox
	rerankLabel := "Use LLM Reranking:"
	if m.currentField == fieldReranking {
		rerankLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true).Render(rerankLabel)
	}
	checkbox := "[ ]"
	if m.rerankingEnabled {
		checkbox = "[✓]"
	}
	b.WriteString(rerankLabel + " " + checkbox + "\n\n")

	// Model info
	modelInfo := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render(
		fmt.Sprintf("LLM: %s | Embed: %s", m.llmModel, m.embedModel))
	b.WriteString(modelInfo + "\n\n")

	// Create button
	createButton := "[ Create ]"
	if m.currentField == fieldCreateButton {
		createButton = lipgloss.NewStyle().
			Foreground(lipgloss.Color("0")).
			Background(lipgloss.Color("205")).
			Bold(true).
			Render(" Create ")
	} else {
		createButton = lipgloss.NewStyle().
			Foreground(lipgloss.Color("205")).
			Render("[ Create ]")
	}
	b.WriteString(createButton + "\n\n")

	helpText := "Tab/Shift+Tab: Navigate • Enter: Next/Create • Space: Toggle • Esc: Cancel"
	b.WriteString(helpStyle.Render(helpText))

	return b.String()
}

func (m *ChatCreateModel) nextField() {
	m.currentField++
	if m.currentField > fieldCreateButton {
		m.currentField = fieldName
	}
	m.updateFocus()
}

func (m *ChatCreateModel) prevField() {
	m.currentField--
	if m.currentField < fieldName {
		m.currentField = fieldCreateButton
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

type ValidationFailed struct {
	TemperatureError string
	TopKError        string
}

func (m ChatCreateModel) createChat() tea.Cmd {
	return func() tea.Msg {
		// Validate temperature
		var temperatureError, topKError string
		var temperature float64
		var topK int

		tempValue := m.temperatureInput.Value()
		if tempValue == "" {
			temperatureError = "Temperature is required"
		} else {
			temp, err := strconv.ParseFloat(tempValue, 64)
			if err != nil {
				temperatureError = "Temperature must be a number"
			} else if temp < 0 || temp > 2 {
				temperatureError = "Temperature must be between 0 and 2"
			} else {
				temperature = temp
			}
		}

		// Validate TopK
		topKValue := m.topKInput.Value()
		if topKValue == "" {
			topKError = "TopK is required"
		} else {
			k, err := strconv.Atoi(topKValue)
			if err != nil {
				topKError = "TopK must be an integer"
			} else if k < 1 || k > 100 {
				topKError = "TopK must be between 1 and 100"
			} else {
				topK = k
			}
		}

		// If validation failed, return validation errors
		if temperatureError != "" || topKError != "" {
			return ValidationFailed{
				TemperatureError: temperatureError,
				TopKError:        topKError,
			}
		}

		// Validation passed, create chat
		name := m.nameInput.Value()
		if name == "" {
			name = "Untitled Chat"
		}

		systemPrompt := m.systemPromptArea.Value()
		if systemPrompt == "" {
			systemPrompt = "You are a helpful assistant."
		}

		chat := &vector.Chat{
			ID:           fmt.Sprintf("chat-%d", time.Now().Unix()),
			Name:         name,
			SystemPrompt: systemPrompt,
			LLMModel:     m.llmModel,
			EmbedModel:   m.embedModel,
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
