package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"rag-terminal/internal/vector"
)

type chatCreateField int

const (
	fieldName chatCreateField = iota
	fieldSystemPrompt
	fieldTemperature
	fieldTopK
	fieldContextWindow
	fieldReranking
	fieldCreateButton
)

type ChatCreateModel struct {
	nameInput           textinput.Model
	systemPromptArea    textarea.Model
	temperatureInput    textinput.Model
	topKInput           textinput.Model
	contextWindowInput  textinput.Model
	rerankingEnabled    bool
	currentField        chatCreateField
	llmModel            string
	embedModel          string
	width               int
	height              int
	err                 error
	temperatureError    string
	topKError           string
	contextWindowError  string
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

	contextWindowInput := textinput.New()
	contextWindowInput.Placeholder = "4096"
	contextWindowInput.SetValue("4096")
	contextWindowInput.CharLimit = 6
	contextWindowInput.Width = 10

	// Enable LLM reranking by default
	rerankingEnabled := true

	return ChatCreateModel{
		nameInput:          nameInput,
		systemPromptArea:   systemPromptArea,
		temperatureInput:   tempInput,
		topKInput:          topKInput,
		contextWindowInput: contextWindowInput,
		rerankingEnabled:   rerankingEnabled,
		currentField:       fieldName,
		llmModel:           llmModel,
		embedModel:         embedModel,
		width:              width,
		height:             height,
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
		m.contextWindowError = msg.ContextWindowError
		m.validationAttempted = true
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+x":
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
	case fieldContextWindow:
		var cmd tea.Cmd
		m.contextWindowInput, cmd = m.contextWindowInput.Update(msg)
		cmds = append(cmds, cmd)
		// Clear context window error when user types
		if m.validationAttempted {
			m.contextWindowError = ""
		}
	}

	return m, tea.Batch(cmds...)
}

func (m ChatCreateModel) View() string {
	if m.err != nil {
		return errorStyle.Render(fmt.Sprintf("Error: %v\n\nPress Esc to go back", m.err))
	}

	var b strings.Builder

	title := TitleStyle.Render("Create New Chat")
	b.WriteString(title + "\n\n")

	// Name field
	b.WriteString(RenderFieldLabel("Chat Name:", m.currentField == fieldName) + "\n")
	b.WriteString(m.nameInput.View() + "\n\n")

	// System prompt field
	b.WriteString(RenderFieldLabel("System Prompt:", m.currentField == fieldSystemPrompt) + "\n")
	b.WriteString(m.systemPromptArea.View() + "\n\n")

	// Temperature field
	b.WriteString(RenderFieldLabel("Temperature (0-2):", m.currentField == fieldTemperature) + "\n")
	b.WriteString(m.temperatureInput.View() + "\n")
	if m.temperatureError != "" {
		b.WriteString(RenderError(m.temperatureError) + "\n")
	}
	b.WriteString("\n")

	// TopK field
	b.WriteString(RenderFieldLabel("Top K (1-100):", m.currentField == fieldTopK) + "\n")
	b.WriteString(m.topKInput.View() + "\n")
	if m.topKError != "" {
		b.WriteString(RenderError(m.topKError) + "\n")
	}
	b.WriteString("\n")

	// Context Window field
	b.WriteString(RenderFieldLabel("Context Window (1024-32768):", m.currentField == fieldContextWindow) + "\n")
	b.WriteString(m.contextWindowInput.View() + "\n")
	if m.contextWindowError != "" {
		b.WriteString(RenderError(m.contextWindowError) + "\n")
	}
	b.WriteString("\n")

	// LLM Reranking checkbox
	rerankLabel := RenderFieldLabel("Use LLM Reranking:", m.currentField == fieldReranking)
	checkbox := "[ ]"
	if m.rerankingEnabled {
		checkbox = "[✓]"
	}
	b.WriteString(rerankLabel + " " + checkbox + "\n\n")

	// Model info
	modelInfo := MetadataStyle.Render(
		fmt.Sprintf("LLM: %s | Embed: %s", m.llmModel, m.embedModel))
	b.WriteString(modelInfo + "\n\n")

	// Create button
	b.WriteString(RenderButton("Create", m.currentField == fieldCreateButton) + "\n\n")

	helpText := "Tab/Shift+Tab: Navigate • Enter: Next/Create • Space: Toggle • Esc: Back • Ctrl+X: Exit"
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
	m.contextWindowInput.Blur()

	switch m.currentField {
	case fieldName:
		m.nameInput.Focus()
	case fieldSystemPrompt:
		m.systemPromptArea.Focus()
	case fieldTemperature:
		m.temperatureInput.Focus()
	case fieldTopK:
		m.topKInput.Focus()
	case fieldContextWindow:
		m.contextWindowInput.Focus()
	}
}

type ValidationFailed struct {
	TemperatureError   string
	TopKError          string
	ContextWindowError string
}

func (m ChatCreateModel) createChat() tea.Cmd {
	return func() tea.Msg {
		// Validate temperature
		var temperatureError, topKError, contextWindowError string
		var temperature float64
		var topK int
		var contextWindow int

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

		// Validate Context Window
		contextWindowValue := m.contextWindowInput.Value()
		if contextWindowValue == "" {
			contextWindowError = "Context Window is required"
		} else {
			cw, err := strconv.Atoi(contextWindowValue)
			if err != nil {
				contextWindowError = "Context Window must be an integer"
			} else if cw <= 0 {
				contextWindowError = "Context Window must be between positive"
			} else {
				contextWindow = cw
			}
		}

		// If validation failed, return validation errors
		if temperatureError != "" || topKError != "" || contextWindowError != "" {
			return ValidationFailed{
				TemperatureError:   temperatureError,
				TopKError:          topKError,
				ContextWindowError: contextWindowError,
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
			ID:            fmt.Sprintf("chat-%d", time.Now().Unix()),
			Name:          name,
			SystemPrompt:  systemPrompt,
			CreatedAt:     time.Now(),
			Temperature:   temperature,
			TopK:          topK,
			UseReranking:  m.rerankingEnabled,
			MaxTokens:     2048,
			ContextWindow: contextWindow,
		}

		return ChatCreated{Chat: chat}
	}
}

type BackToChatList struct{}
