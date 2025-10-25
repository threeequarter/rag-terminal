package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"rag-terminal/internal/document"
	"rag-terminal/internal/logging"
	"rag-terminal/internal/rag"
	"rag-terminal/internal/vector"
)

const (
	titleHeight     = 3
	textareaHeight  = 5
	helpHeight      = 2
	padding         = 2
	renderInterval  = 50 * time.Millisecond
	gotoBottomEvery = 100
)

type ChatViewModel struct {
	chat         *vector.Chat
	pipeline     *rag.Pipeline
	vectorStore  vector.VectorStore
	messages     []vector.Message
	viewport     viewport.Model
	textarea     textarea.Model
	spinner      spinner.Model
	width        int
	height       int
	isThinking   bool
	err          error
	ctx          context.Context
	cancelFunc   context.CancelFunc
	streamChan   <-chan string
	errChan      <-chan error
	streamBuffer strings.Builder
	lastRender   time.Time
	tokenCount   int
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

type RenderTickMsg struct{}

func NewChatViewModel(chat *vector.Chat, pipeline *rag.Pipeline, vectorStore vector.VectorStore, width, height int) ChatViewModel {
	ta := textarea.New()
	ta.Placeholder = "Type your message..."
	ta.Focus()
	ta.CharLimit = 2000
	ta.SetWidth(width - 4)
	ta.SetHeight(3)
	ta.ShowLineNumbers = false

	viewportHeight := height - titleHeight - textareaHeight - helpHeight - padding
	vp := viewport.New(width-2, viewportHeight)
	vp.SetContent("")
	vp.MouseWheelDelta = 2

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	ctx, cancel := context.WithCancel(context.Background())

	return ChatViewModel{
		chat:        chat,
		pipeline:    pipeline,
		vectorStore: vectorStore,
		viewport:    vp,
		textarea:    ta,
		spinner:     sp,
		width:       width,
		height:      height,
		ctx:         ctx,
		cancelFunc:  cancel,
		lastRender:  time.Now(),
	}
}

func (m ChatViewModel) Init() tea.Cmd {
	return tea.Batch(
		textarea.Blink,
		m.spinner.Tick,
		m.loadMessages(),
	)
}

func (m ChatViewModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		viewportHeight := msg.Height - titleHeight - textareaHeight - helpHeight - padding
		m.viewport.Width = msg.Width - 2
		m.viewport.Height = viewportHeight
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

		case "ctrl+f":
			return m, nil

		case "enter":
			if !m.isThinking && m.textarea.Value() != "" {
				userMessage := m.textarea.Value()
				m.textarea.Reset()
				m.isThinking = true

				m.addUserMessage(userMessage)

				return m, m.sendMessage(userMessage)
			}
		}

	case MessagesLoaded:
		m.messages = msg.Messages
		m.renderMessages()
		return m, nil

	case ChatMessageReceived:
		m.streamBuffer.WriteString(msg.Token)
		m.tokenCount++

		if time.Since(m.lastRender) >= renderInterval {
			m.flushStreamBuffer()
			m.lastRender = time.Now()
		}

		return m, waitForStreamToken(msg.StreamChan, msg.ErrChan)

	case ChatResponseComplete:
		m.flushStreamBuffer()
		m.isThinking = false
		m.streamBuffer.Reset()
		m.tokenCount = 0
		return m, nil

	case ChatResponseError:
		m.err = msg.Err
		m.isThinking = false
		m.streamBuffer.Reset()
		m.tokenCount = 0
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
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

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("205")).
		Padding(0, 1).
		Render(m.chat.Name)
	b.WriteString(title + "\n")

	status := fmt.Sprintf("Model: %s | LLM reranking: %s | Temp: %.1f | TopK: %d",
		m.chat.LLMModel,
		map[bool]string{true: "ON", false: "OFF"}[m.chat.UseReranking],
		m.chat.Temperature,
		m.chat.TopK,
	)
	if m.isThinking {
		status += " | " + m.spinner.View() + " Thinking..."
	}
	b.WriteString(statusBarStyle.Render(status) + "\n\n")

	viewportWithBorder := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(0, 1).
		Render(m.viewport.View())

	b.WriteString(viewportWithBorder)
	b.WriteString("\n")

	scrollInfo := m.renderScrollIndicator()
	if scrollInfo != "" {
		b.WriteString(scrollInfo)
	}
	b.WriteString("\n\n")

	b.WriteString(m.textarea.View() + "\n")

	helpText := "Enter: Send • Ctrl+F: Files • Esc: Back to List • Ctrl+X: Quit"
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
		logging.Info("sendMessage called: message='%s', chatID=%s", userMessage, m.chat.ID)

		// Check if message contains file/folder paths (supports multiple paths)
		multiPathResult := document.DetectAllPaths(userMessage)

		logging.Debug("Path detection result: hasPaths=%v, count=%d, query='%s'",
			multiPathResult.HasPaths, len(multiPathResult.Paths), multiPathResult.Query)

		if multiPathResult.HasPaths {
			// Load all detected paths
			query := multiPathResult.Query
			logging.Info("Processing %d document path(s), query='%s'", len(multiPathResult.Paths), query)

			// Load multiple documents through pipeline
			streamChan, errChan, err := m.pipeline.LoadMultipleDocuments(m.ctx, m.chat, multiPathResult.Paths, query)
			if err != nil {
				logging.Error("LoadMultipleDocuments failed: %v", err)
				return ChatResponseError{Err: err}
			}

			return waitForStreamToken(streamChan, errChan)()
		}

		// No path detected, process as regular message with document support
		logging.Debug("No path detected, processing as regular message")
		streamChan, errChan, err := m.pipeline.ProcessUserMessageWithDocuments(m.ctx, m.chat, userMessage)
		if err != nil {
			logging.Error("ProcessUserMessageWithDocuments failed: %v", err)
			return ChatResponseError{Err: err}
		}

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
		messages, err := m.vectorStore.GetMessages(context.Background())
		if err != nil {
			return ChatResponseError{Err: err}
		}
		return MessagesLoaded{Messages: messages}
	}
}

func (m *ChatViewModel) renderMessages() {
	var b strings.Builder

	for _, msg := range m.messages {
		if msg.Role == "user" {
			timestamp := msg.Timestamp.Format("15:04:05")
			label := lipgloss.NewStyle().
				Foreground(lipgloss.Color("12")).
				Bold(true).
				Render("You:")

			b.WriteString(lipgloss.NewStyle().
				Foreground(lipgloss.Color("231")).
				Padding(0, 1).
				MarginBottom(1).
				Width(m.width - 10).
				Align(lipgloss.Right).
				Render(label + "\n" + msg.Content))
			b.WriteString(lipgloss.NewStyle().
				Foreground(lipgloss.Color("241")).
				Align(lipgloss.Right).
				Width(m.width - 10).
				Render(timestamp))
			b.WriteString("\n\n")
		} else {
			label := lipgloss.NewStyle().
				Foreground(lipgloss.Color("205")).
				Bold(true).
				Render("Assistant:")

			b.WriteString(lipgloss.NewStyle().
				Foreground(lipgloss.Color("231")).
				Padding(0, 1).
				MarginBottom(1).
				Width(m.width - 10).
				Render(label + "\n" + msg.Content))
			b.WriteString("\n\n")
		}
	}

	m.viewport.SetContent(b.String())
}

func (m *ChatViewModel) flushStreamBuffer() {
	if m.streamBuffer.Len() == 0 {
		return
	}

	if len(m.messages) > 0 {
		lastMsg := &m.messages[len(m.messages)-1]
		if lastMsg.Role == "assistant" {
			lastMsg.Content += m.streamBuffer.String()
		}
	}

	m.streamBuffer.Reset()
	m.renderMessages()

	if m.tokenCount%gotoBottomEvery == 0 {
		m.viewport.GotoBottom()
	}
}

func (m ChatViewModel) renderScrollIndicator() string {
	if m.viewport.TotalLineCount() <= m.viewport.Height {
		return ""
	}

	scrollPercent := int(m.viewport.ScrollPercent() * 100)
	indicator := fmt.Sprintf("Scroll: %d%% ↕", scrollPercent)

	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("62")).
		Bold(false).
		Render(indicator)
}

type MessagesLoaded struct {
	Messages []vector.Message
}
