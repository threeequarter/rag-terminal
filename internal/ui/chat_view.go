package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

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

type ProcessingState int

const (
	StateIdle ProcessingState = iota
	StateEmbedding
	StateReranking
	StateThinking
)

type ChatViewModel struct {
	chat            *vector.Chat
	pipeline        *rag.Pipeline
	vectorStore     vector.VectorStore
	messages        []vector.Message
	viewport        viewport.Model
	textarea        textarea.Model
	spinner         spinner.Model
	width           int
	height          int
	processingState ProcessingState
	embeddedFiles   int
	totalFiles      int
	err             error
	ctx             context.Context
	cancelFunc      context.CancelFunc
	streamChan      <-chan string
	errChan         <-chan error
	streamBuffer    strings.Builder
	lastRender      time.Time
	tokenCount      int
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

type StateChange struct {
	State ProcessingState
}

type FileEmbeddingProgress struct {
	Embedded int
	Total    int
}

type RenderTickMsg struct{}

type StateTransitionMsg struct{}

func NewChatViewModel(chat *vector.Chat, pipeline *rag.Pipeline, vectorStore vector.VectorStore, width, height int) ChatViewModel {
	ta := textarea.New()
	ta.Placeholder = "Type your message, drop file or folder..."
	ta.Focus()
	ta.CharLimit = 2000
	ta.SetWidth(width - 4)
	ta.SetHeight(3)
	ta.ShowLineNumbers = false

	// Configure textarea key bindings - keep only essential editing keys
	ta.KeyMap.CharacterForward = key.NewBinding(key.WithKeys("right"))
	ta.KeyMap.CharacterBackward = key.NewBinding(key.WithKeys("left"))
	ta.KeyMap.LineStart = key.NewBinding(key.WithKeys("home"))
	ta.KeyMap.LineEnd = key.NewBinding(key.WithKeys("end"))
	ta.KeyMap.DeleteCharacterBackward = key.NewBinding(key.WithKeys("backspace"))
	ta.KeyMap.DeleteCharacterForward = key.NewBinding(key.WithKeys("delete"))
	ta.KeyMap.LineNext = key.NewBinding()
	ta.KeyMap.LinePrevious = key.NewBinding()
	ta.KeyMap.WordForward = key.NewBinding()
	ta.KeyMap.WordBackward = key.NewBinding()
	ta.KeyMap.DeleteWordBackward = key.NewBinding()
	ta.KeyMap.DeleteWordForward = key.NewBinding()
	ta.KeyMap.DeleteAfterCursor = key.NewBinding()
	ta.KeyMap.DeleteBeforeCursor = key.NewBinding()
	ta.KeyMap.InsertNewline = key.NewBinding()
	ta.KeyMap.Paste = key.NewBinding()

	viewportHeight := height - titleHeight - textareaHeight - helpHeight - padding
	vp := viewport.New(width-2, viewportHeight)
	vp.SetContent("")
	vp.MouseWheelDelta = 2

	// Configure viewport key bindings - keep arrows and page up/down
	vp.KeyMap.Down = key.NewBinding(key.WithKeys("down"))
	vp.KeyMap.Up = key.NewBinding(key.WithKeys("up"))
	vp.KeyMap.PageDown = key.NewBinding(key.WithKeys("pgdown"))
	vp.KeyMap.PageUp = key.NewBinding(key.WithKeys("pgup"))
	vp.KeyMap.HalfPageDown = key.NewBinding()
	vp.KeyMap.HalfPageUp = key.NewBinding()

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = SpinnerStyle

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
		case "ctrl+x":
			m.cancelFunc()
			return m, tea.Quit

		case "esc":
			m.cancelFunc()
			return m, func() tea.Msg {
				return BackToChatList{}
			}

		case "enter":
			if m.processingState == StateIdle && m.textarea.Value() != "" {
				userMessage := m.textarea.Value()
				m.textarea.Reset()
				m.processingState = StateEmbedding

				m.addUserMessage(userMessage)

				return m, tea.Batch(
					m.sendMessage(userMessage),
					m.scheduleStateTransition(),
				)
			}
		}

	case MessagesLoaded:
		m.messages = msg.Messages
		m.renderMessages()
		return m, nil

	case StateChange:
		m.processingState = msg.State
		return m, nil

	case FileEmbeddingProgress:
		m.embeddedFiles = msg.Embedded
		m.totalFiles = msg.Total
		return m, nil

	case StateTransitionMsg:
		// Auto-transition from Embedding to Reranking if appropriate
		if m.processingState == StateEmbedding && m.chat.UseReranking {
			m.processingState = StateReranking
		}
		return m, nil

	case ChatMessageReceived:
		// Check if this is a progress token
		if strings.HasPrefix(msg.Token, "@@PROGRESS:") && strings.HasSuffix(msg.Token, "@@") {
			// Parse progress: @@PROGRESS:X/Y@@
			progressStr := strings.TrimPrefix(msg.Token, "@@PROGRESS:")
			progressStr = strings.TrimSuffix(progressStr, "@@")
			parts := strings.Split(progressStr, "/")
			if len(parts) == 2 {
				var embedded, total int
				fmt.Sscanf(parts[0], "%d", &embedded)
				fmt.Sscanf(parts[1], "%d", &total)
				return m, tea.Batch(
					waitForStreamToken(msg.StreamChan, msg.ErrChan),
					func() tea.Msg {
						return FileEmbeddingProgress{
							Embedded: embedded,
							Total:    total,
						}
					},
				)
			}
		}

		// Transition to thinking state when we start receiving tokens
		if m.processingState != StateThinking {
			m.processingState = StateThinking
		}

		m.streamBuffer.WriteString(msg.Token)
		m.tokenCount++

		if time.Since(m.lastRender) >= renderInterval {
			m.flushStreamBuffer()
			m.lastRender = time.Now()
		}

		return m, waitForStreamToken(msg.StreamChan, msg.ErrChan)

	case ChatResponseComplete:
		m.flushStreamBuffer()
		m.processingState = StateIdle
		m.streamBuffer.Reset()
		m.tokenCount = 0
		m.embeddedFiles = 0
		m.totalFiles = 0
		return m, nil

	case ChatResponseError:
		m.err = msg.Err
		m.processingState = StateIdle
		m.streamBuffer.Reset()
		m.tokenCount = 0
		m.embeddedFiles = 0
		m.totalFiles = 0
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	if m.processingState == StateIdle {
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

	title := TitleWithPaddingStyle.Render(m.chat.Name)
	b.WriteString(title + "\n")

	status := fmt.Sprintf("Model: %s | LLM reranking: %s | Temp: %.1f | TopK: %d",
		m.chat.LLMModel,
		map[bool]string{true: "ON", false: "OFF"}[m.chat.UseReranking],
		m.chat.Temperature,
		m.chat.TopK,
	)

	switch m.processingState {
	case StateEmbedding:
		if m.totalFiles > 0 {
			status += fmt.Sprintf(" | %s Embedding (%d/%d files)...", m.spinner.View(), m.embeddedFiles, m.totalFiles)
		} else {
			status += " | " + m.spinner.View() + " Embedding..."
		}
	case StateReranking:
		status += " | " + m.spinner.View() + " Reranking..."
	case StateThinking:
		status += " | " + m.spinner.View() + " Thinking..."
	}

	b.WriteString(statusBarStyle.Render(status) + "\n\n")

	viewportWithBorder := RenderViewportWithBorder(m.viewport.View())
	b.WriteString(viewportWithBorder)
	b.WriteString("\n")

	scrollInfo := m.renderScrollIndicator()
	if scrollInfo != "" {
		b.WriteString(scrollInfo)
	}
	b.WriteString("\n\n")

	b.WriteString(m.textarea.View() + "\n")

	helpText := "Enter: Send • ↑/↓: Scroll • PgUp/PgDn: Page Scroll • Esc: Back • Ctrl+X: Exit"
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

func (m ChatViewModel) scheduleStateTransition() tea.Cmd {
	return tea.Tick(800*time.Millisecond, func(t time.Time) tea.Msg {
		return StateTransitionMsg{}
	})
}

func (m *ChatViewModel) renderMessages() {
	var b strings.Builder

	for _, msg := range m.messages {
		if msg.Role == "user" {
			timestamp := msg.Timestamp.Format("15:04:05")
			label := UserMessageLabelStyle.Render("You:")

			b.WriteString(GetUserMessageContentStyle(m.width).Render(label + "\n" + msg.Content))
			b.WriteString(GetTimestampStyle(m.width).Render(timestamp))
			b.WriteString("\n\n")
		} else {
			label := AssistantMessageLabelStyle.Render("Assistant:")

			b.WriteString(GetAssistantMessageContentStyle(m.width).Render(label + "\n" + msg.Content))
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

	return ScrollIndicatorStyle.Render(indicator)
}

type MessagesLoaded struct {
	Messages []vector.Message
}
