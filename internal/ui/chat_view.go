package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"rag-chat/internal/rag"
	"rag-chat/internal/vector"
)

type ChatViewModel struct {
	chat        *vector.Chat
	pipeline    *rag.Pipeline
	vectorStore vector.VectorStore
	messages    []vector.Message
	viewport    viewport.Model
	textarea    textarea.Model
	width       int
	height      int
	isThinking  bool
	err         error
	ctx         context.Context
	cancelFunc  context.CancelFunc
	streamChan  <-chan string
	errChan     <-chan error
}

type ChatMessageReceived struct {
	Token      string
	StreamChan <-chan string
	ErrChan    <-chan error
}

type ChatResponseComplete struct{}

type ChatResponseError struct {
	Err error
}

func NewChatViewModel(chat *vector.Chat, pipeline *rag.Pipeline, vectorStore vector.VectorStore, width, height int) ChatViewModel {
	ta := textarea.New()
	ta.Placeholder = "Type your message..."
	ta.Focus()
	ta.CharLimit = 2000
	ta.SetWidth(width - 4)
	ta.SetHeight(3)
	ta.ShowLineNumbers = false

	vp := viewport.New(width-2, height-10)
	vp.SetContent("")

	ctx, cancel := context.WithCancel(context.Background())

	return ChatViewModel{
		chat:        chat,
		pipeline:    pipeline,
		vectorStore: vectorStore,
		viewport:    vp,
		textarea:    ta,
		width:       width,
		height:      height,
		ctx:         ctx,
		cancelFunc:  cancel,
	}
}

func (m ChatViewModel) Init() tea.Cmd {
	return tea.Batch(
		textarea.Blink,
		m.loadMessages(),
	)
}

func (m ChatViewModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Width = msg.Width - 2
		m.viewport.Height = msg.Height - 10
		m.textarea.SetWidth(msg.Width - 4)
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "ctrl+x":
			m.cancelFunc()
			return m, tea.Quit

		case "esc":
			m.cancelFunc()
			return m, func() tea.Msg {
				return BackToChatList{}
			}

		case "enter":
			if !m.isThinking && m.textarea.Value() != "" {
				userMessage := m.textarea.Value()
				m.textarea.Reset()
				m.isThinking = true

				// Add user message to display immediately
				m.addUserMessage(userMessage)

				return m, m.sendMessage(userMessage)
			}
		}

	case MessagesLoaded:
		m.messages = msg.Messages
		m.renderMessages()
		return m, nil

	case ChatMessageReceived:
		// Append token to last message
		if len(m.messages) > 0 {
			lastMsg := &m.messages[len(m.messages)-1]
			if lastMsg.Role == "assistant" {
				lastMsg.Content += msg.Token
			}
		}
		m.renderMessages()
		m.viewport.GotoBottom()
		// Continue waiting for more tokens
		return m, waitForStreamToken(msg.StreamChan, msg.ErrChan)

	case ChatResponseComplete:
		m.isThinking = false
		return m, nil

	case ChatResponseError:
		m.err = msg.Err
		m.isThinking = false
		return m, nil
	}

	if !m.isThinking {
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		cmds = append(cmds, cmd)
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m ChatViewModel) View() string {
	if m.err != nil {
		return errorStyle.Render(fmt.Sprintf("Error: %v\n\nPress Esc to go back", m.err))
	}

	var b strings.Builder

	// Title bar
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("10")).
		Padding(0, 1).
		Render(m.chat.Name)
	b.WriteString(title + "\n")

	// Status bar
	status := fmt.Sprintf("Model: %s | RAG: %s | Temp: %.1f | TopK: %d",
		m.chat.LLMModel,
		map[bool]string{true: "ON", false: "OFF"}[m.chat.UseReranking],
		m.chat.Temperature,
		m.chat.TopK,
	)
	if m.isThinking {
		status += " | 🤔 Thinking..."
	}
	b.WriteString(statusBarStyle.Render(status) + "\n\n")

	// Messages viewport
	b.WriteString(lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(0, 1).
		Render(m.viewport.View()))
	b.WriteString("\n\n")

	// Input area
	b.WriteString(m.textarea.View() + "\n\n")

	// Help text
	helpText := "Enter: Send • Esc: Back to List • Ctrl+X: Quit"
	b.WriteString(helpStyle.Render(helpText))

	return b.String()
}

func (m *ChatViewModel) addUserMessage(content string) {
	msg := vector.Message{
		ID:        fmt.Sprintf("msg-%d", time.Now().UnixNano()),
		ChatID:    m.chat.ID,
		Role:      "user",
		Content:   content,
		Timestamp: time.Now(),
	}
	m.messages = append(m.messages, msg)

	// Add placeholder for assistant response
	assistantMsg := vector.Message{
		ID:        fmt.Sprintf("msg-%d", time.Now().UnixNano()+1),
		ChatID:    m.chat.ID,
		Role:      "assistant",
		Content:   "",
		Timestamp: time.Now(),
	}
	m.messages = append(m.messages, assistantMsg)

	m.renderMessages()
	m.viewport.GotoBottom()
}

func (m ChatViewModel) sendMessage(userMessage string) tea.Cmd {
	return func() tea.Msg {
		// Process message through RAG pipeline
		streamChan, errChan, err := m.pipeline.ProcessUserMessage(m.ctx, m.chat, userMessage)
		if err != nil {
			return ChatResponseError{Err: err}
		}

		// Call waitForStreamToken as a command and execute it to get the first message
		return waitForStreamToken(streamChan, errChan)()
	}
}

// waitForStreamToken creates a command that waits for the next stream token
func waitForStreamToken(streamChan <-chan string, errChan <-chan error) tea.Cmd {
	return func() tea.Msg {
		select {
		case token, ok := <-streamChan:
			if !ok {
				// Stream closed
				return ChatResponseComplete{}
			}
			// Return token with channels for continuation
			return ChatMessageReceived{
				Token:      token,
				StreamChan: streamChan,
				ErrChan:    errChan,
			}

		case err := <-errChan:
			if err != nil {
				return ChatResponseError{Err: err}
			}
			// Continue waiting for tokens
			return waitForStreamToken(streamChan, errChan)()
		}
	}
}

func (m ChatViewModel) loadMessages() tea.Cmd {
	return func() tea.Msg {
		messages, err := m.vectorStore.GetMessages(context.Background(), m.chat.ID)
		if err != nil {
			return ChatResponseError{Err: err}
		}
		return MessagesLoaded{Messages: messages}
	}
}

func (m *ChatViewModel) renderMessages() {
	var b strings.Builder

	for _, msg := range m.messages {
		timestamp := msg.Timestamp.Format("15:04:05")

		if msg.Role == "user" {
			// User message - right aligned, blue
			content := lipgloss.NewStyle().
				Foreground(lipgloss.Color("12")).
				Bold(true).
				Render("You: ") + msg.Content

			b.WriteString(lipgloss.NewStyle().
				Foreground(lipgloss.Color("231")).
				Padding(0, 1).
				MarginBottom(1).
				Width(m.width - 10).
				Align(lipgloss.Right).
				Render(content))
			b.WriteString(lipgloss.NewStyle().
				Foreground(lipgloss.Color("241")).
				Align(lipgloss.Right).
				Width(m.width - 10).
				Render(timestamp))
			b.WriteString("\n\n")
		} else {
			// Assistant message - left aligned, gray
			content := lipgloss.NewStyle().
				Foreground(lipgloss.Color("10")).
				Bold(true).
				Render("Assistant: ") + msg.Content

			b.WriteString(lipgloss.NewStyle().
				Foreground(lipgloss.Color("231")).
				Padding(0, 1).
				MarginBottom(1).
				Width(m.width - 10).
				Render(content))
			b.WriteString(lipgloss.NewStyle().
				Foreground(lipgloss.Color("241")).
				Width(m.width - 10).
				Render(timestamp))
			b.WriteString("\n\n")
		}
	}

	m.viewport.SetContent(b.String())
}

type MessagesLoaded struct {
	Messages []vector.Message
}
