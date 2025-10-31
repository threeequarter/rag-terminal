package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"

	"rag-terminal/internal/document"
	"rag-terminal/internal/logging"
	"rag-terminal/internal/rag"
	"rag-terminal/internal/vector"
)

const (
	titleHeight     = 5
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
	pipeline        rag.Pipeline
	documentManager *document.DocumentManager
	vectorStore     vector.VectorStore
	messages        []vector.Message
	viewport        viewport.Model
	textarea        textarea.Model
	spinner         spinner.Model
	fileSelector    FileSelectorOverlayModel
	width           int
	height          int
	processingState ProcessingState
	embeddedFiles   int
	totalFiles      int
	embeddedDocCount int // Number of embedded documents in current context
	hasQuery        bool // Track if current operation has a query (needs LLM)
	err             error
	ctx             context.Context
	cancelFunc      context.CancelFunc
	streamChan      <-chan string
	errChan         <-chan error
	streamBuffer       *strings.Builder
	lastRender         time.Time
	tokenCount         int
	thinkingStartTime  time.Time
	lastResponseTokens int
	lastResponseTPS    float64
	mdRenderer         *glamour.TermRenderer
	llmModel           string
	embedModel         string
}

type ChatMessageReceived struct {
	Token           string
	StreamChan      <-chan string
	ErrChan         <-chan error
	OriginalMessage string                        // Original user message with file paths
	PathResults     []document.PathDetectionResult // Detected file paths to replace
}

type ChatResponseComplete struct{}

type ChatResponseError struct {
	Err error
}

type DocumentLoadingComplete struct {
	OriginalMessage string
	PathResults     []document.PathDetectionResult
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

type FileOnlyEmbedding struct {
	Count int
}

// createMarkdownRenderer creates a markdown renderer with fallback handling
func createMarkdownRenderer(width int) *glamour.TermRenderer {
	// Try auto style first
	renderer, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width-10),
	)
	if err == nil {
		return renderer
	}

	logging.Error("Failed to create markdown renderer with auto style: %v, trying fallback", err)

	// Try basic style as fallback
	renderer, err = glamour.NewTermRenderer(
		glamour.WithWordWrap(width - 10),
	)
	if err == nil {
		return renderer
	}

	logging.Error("Failed to create markdown renderer with basic style: %v, using no style", err)

	// Last resort: try with no options (should never fail)
	renderer, err = glamour.NewTermRenderer()
	if err != nil {
		logging.Error("Critical: Failed to create basic markdown renderer: %v", err)
		return nil
	}

	return renderer
}

// safeRenderMarkdown safely renders markdown with panic recovery and fallback
func (m *ChatViewModel) safeRenderMarkdown(content string) string {
	defer func() {
		if r := recover(); r != nil {
			logging.Error("Panic in markdown rendering: %v", r)
		}
	}()

	// Fallback to plain text if no renderer
	if m.mdRenderer == nil {
		logging.Error("Markdown renderer is nil, falling back to plain text")
		return content
	}

	// Empty content returns as-is
	if content == "" {
		return content
	}

	rendered, err := m.mdRenderer.Render(content)
	if err != nil {
		logging.Error("Markdown rendering error: %v, falling back to plain text", err)
		return content
	}

	return strings.TrimRight(rendered, "\n")
}

func NewChatViewModel(chat *vector.Chat, pipeline rag.Pipeline, vectorStore vector.VectorStore, llmModel, embedModel string, width, height int) ChatViewModel {
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
	vp := viewport.New(width-6, viewportHeight)
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

	fs := NewFileSelectorOverlayModel()
	// Initialize file selector with current dimensions
	fs.UpdateSize(width, height)

	// Initialize markdown renderer with dark theme
	mdRenderer := createMarkdownRenderer(width)

	return ChatViewModel{
		chat:            chat,
		pipeline:        pipeline,
		documentManager: pipeline.GetDocumentManager(),
		vectorStore:     vectorStore,
		viewport:     vp,
		textarea:     ta,
		spinner:      sp,
		fileSelector: fs,
		width:        width,
		height:       height,
		ctx:          ctx,
		cancelFunc:   cancel,
		lastRender:   time.Now(),
		mdRenderer:   mdRenderer,
		streamBuffer: &strings.Builder{},
		llmModel:     llmModel,
		embedModel:   embedModel,
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

	// Handle file selector closing and selection messages first
	switch msg := msg.(type) {
	case FileSelected:
		// Insert filename at cursor position in textarea
		currentValue := m.textarea.Value()
		m.textarea.SetValue(currentValue + msg.FileName)
		m.fileSelector.Hide()
		m.textarea.Focus()
		return m, nil

	case FileSelectorClosed:
		// Hide file selector and refocus textarea
		m.fileSelector.Hide()
		m.textarea.Focus()
		return m, nil
	}

	// Handle file selector updates if visible
	if m.fileSelector.IsVisible() {
		cmd := m.fileSelector.UpdateFileSelector(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		viewportHeight := msg.Height - titleHeight - textareaHeight - helpHeight - padding
		m.viewport.Width = msg.Width - 6
		m.viewport.Height = viewportHeight
		m.textarea.SetWidth(msg.Width - 4)
		m.fileSelector.UpdateSize(msg.Width, msg.Height)

		// Update markdown renderer word wrap width
		m.mdRenderer = createMarkdownRenderer(msg.Width)

		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+f":
			// Toggle file selector
			if m.processingState == StateIdle {
				if m.fileSelector.IsVisible() {
					// Close if already open
					m.fileSelector.Hide()
					m.textarea.Focus()
					return m, nil
				}
				// Open if closed
				return m, m.loadAndShowFileSelector()
			}
			return m, nil

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

				// Check if this is a file-only embedding (no query text)
				multiPathResult := document.DetectAllPaths(userMessage)
				isFileOnly := multiPathResult.HasPaths && strings.TrimSpace(multiPathResult.Query) == ""
				m.hasQuery = !isFileOnly // Track whether this operation has a query

				// Only add user message if there's actual query text
				if !isFileOnly {
					m.addUserMessage(userMessage)
					// Reset stats when sending new message with query
					m.embeddedDocCount = 0
					m.lastResponseTokens = 0
					m.lastResponseTPS = 0
				}

				// Only schedule state transition if there's a query (needs reranking/thinking)
				var cmds []tea.Cmd
				cmds = append(cmds, m.sendMessage(userMessage))
				if m.hasQuery {
					cmds = append(cmds, m.scheduleStateTransition())
				}

				return m, tea.Batch(cmds...)
			}
		}

	case MessagesLoaded:
		m.messages = msg.Messages
		m.renderMessages()
		return m, nil

	case DocumentsLoaded:
		m.fileSelector.SetFiles(msg.Documents)
		m.fileSelector.Show()
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
		// Only transition if: still embedding, reranking enabled, AND there's a query to process
		if m.processingState == StateEmbedding && m.chat.UseReranking && m.hasQuery {
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

				// Choose the correct continuation based on whether we're waiting for document loading + query
				var continueCmd tea.Cmd
				if msg.OriginalMessage != "" && len(msg.PathResults) > 0 {
					continueCmd = waitForDocumentLoadingAndProcessQuery(msg.StreamChan, msg.ErrChan, msg.OriginalMessage, msg.PathResults)
				} else {
					continueCmd = waitForStreamToken(msg.StreamChan, msg.ErrChan)
				}

				return m, tea.Batch(
					continueCmd,
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
			m.thinkingStartTime = time.Now()
		}

		// Filter out invalid UTF-8 replacement characters (�)
		cleanToken := strings.ReplaceAll(msg.Token, "\uFFFD", "")

		// Collect tokens in buffer - we'll render the complete message when done
		if cleanToken != "" {
			m.streamBuffer.WriteString(cleanToken)
		}
		m.tokenCount++

		// Continue with appropriate handler based on whether we're waiting for document loading
		if msg.OriginalMessage != "" && len(msg.PathResults) > 0 {
			return m, waitForDocumentLoadingAndProcessQuery(msg.StreamChan, msg.ErrChan, msg.OriginalMessage, msg.PathResults)
		}
		return m, waitForStreamToken(msg.StreamChan, msg.ErrChan)

	case DocumentLoadingComplete:
		// Document loading complete, now process the message with the LLM
		// Replace file paths with filenames in the message
		messageWithFilenames := replacePathsWithFilenames(msg.OriginalMessage, msg.PathResults)
		logging.Info("Document loading complete, processing message: '%s'", messageWithFilenames)

		// Flush any remaining buffer from document loading progress
		m.flushStreamBuffer()

		// Store the embedded document count
		if m.totalFiles > 0 {
			m.embeddedDocCount = m.totalFiles
		}

		// Reset document loading counters
		m.embeddedFiles = 0
		m.totalFiles = 0
		m.streamBuffer.Reset()

		// Process the message (with filenames instead of paths) through the pipeline
		streamChan, errChan, err := m.pipeline.ProcessUserMessage(m.ctx, m.chat, messageWithFilenames)
		if err != nil {
			logging.Error("ProcessUserMessage failed after document loading: %v", err)
			m.processingState = StateIdle
			m.hasQuery = false
			return m, func() tea.Msg { return ChatResponseError{Err: err} }
		}

		// Transition to reranking/thinking state
		var cmds []tea.Cmd
		cmds = append(cmds, waitForStreamToken(streamChan, errChan))
		if m.hasQuery {
			cmds = append(cmds, m.scheduleStateTransition())
		}

		return m, tea.Batch(cmds...)

	case ChatResponseComplete:
		m.flushStreamBuffer()
		m.processingState = StateIdle
		m.hasQuery = false // Reset query flag for next operation

		// Calculate tokens per second
		if m.tokenCount > 0 && !m.thinkingStartTime.IsZero() {
			duration := time.Since(m.thinkingStartTime).Seconds()
			if duration > 0 {
				m.lastResponseTokens = m.tokenCount
				m.lastResponseTPS = float64(m.tokenCount) / duration
			}
		}

		// Store the embedded document count before resetting
		// Only update if files were actually embedded (totalFiles > 0)
		if m.totalFiles > 0 {
			m.embeddedDocCount = m.totalFiles
		}

		m.streamBuffer.Reset()
		m.tokenCount = 0
		m.embeddedFiles = 0
		m.totalFiles = 0
		m.thinkingStartTime = time.Time{}
		return m, nil

	case ChatResponseError:
		m.err = msg.Err
		m.processingState = StateIdle
		m.hasQuery = false // Reset query flag for next operation
		m.streamBuffer.Reset()
		m.tokenCount = 0
		m.embeddedFiles = 0
		m.totalFiles = 0
		m.embeddedDocCount = 0
		m.thinkingStartTime = time.Time{}
		m.lastResponseTokens = 0
		m.lastResponseTPS = 0
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

	// Build status bar - 2 lines: models on line 1, properties on line 2
	rerankingStatus := "OFF"
	if m.chat.UseReranking {
		rerankingStatus = "ON"
	}

	// Line 1: Models
	modelLine := fmt.Sprintf("LLM: %s | Embedding: %s",
		m.llmModel,
		m.embedModel,
	)
	b.WriteString(statusBarStyle.Render(modelLine) + "\n")

	// Line 2: Chat properties
	var propertyLine string
	propertyLine = fmt.Sprintf("Temp: %.1f | TopK: %d | Ctx: %d | Reranking: %s",
		m.chat.Temperature,
		m.chat.TopK,
		m.chat.ContextWindow,
		rerankingStatus,
	)

	// Add files count if documents are embedded
	if m.embeddedDocCount > 0 {
		filesInfo := FilesCountStyle.Render(fmt.Sprintf("Files: %d", m.embeddedDocCount))
		propertyLine += " | " + filesInfo
	}

	// Add processing state indicators
	switch m.processingState {
	case StateEmbedding:
		if m.totalFiles > 0 {
			propertyLine += fmt.Sprintf(" | %s Embedding (%d/%d files)...", m.spinner.View(), m.embeddedFiles, m.totalFiles)
		} else {
			propertyLine += " | " + m.spinner.View() + " Embedding..."
		}
	case StateReranking:
		propertyLine += " | " + m.spinner.View() + " Reranking..."
	case StateThinking:
		propertyLine += fmt.Sprintf(" | %s Thinking... (%d tokens)", m.spinner.View(), m.tokenCount)
	case StateIdle:
		// Show last response statistics if available
		if m.lastResponseTokens > 0 {
			propertyLine += fmt.Sprintf(" | Last response: %d tokens, %.1f tok/s", m.lastResponseTokens, m.lastResponseTPS)
		}
	}

	b.WriteString(statusBarStyle.Render(propertyLine) + "\n\n")

	viewportWithBorder := RenderViewportWithBorder(m.viewport.View())
	b.WriteString(viewportWithBorder)
	b.WriteString("\n")

	scrollInfo := m.renderScrollIndicator()
	if scrollInfo != "" {
		b.WriteString(scrollInfo)
	}
	b.WriteString("\n\n")

	b.WriteString(m.textarea.View() + "\n")

	helpText := "Enter: Send • Ctrl+F: Files • ↑/↓: Scroll • PgUp/PgDn: Page Scroll • Esc: Back • Ctrl+X: Exit"
	b.WriteString(helpStyle.Render(helpText))

	baseView := b.String()

	// Render file selector overlay on top if visible using the overlay library
	return m.fileSelector.RenderOverlay(baseView)
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

	// Don't add assistant placeholder - we'll add it when response is complete
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
			streamChan, errChan, err := m.documentManager.LoadMultipleDocuments(m.ctx, m.chat, m.embedModel, multiPathResult.Paths)
			if err != nil {
				logging.Error("LoadMultipleDocuments failed: %v", err)
				return ChatResponseError{Err: err}
			}

			// If there's a query, wait for document loading to complete then process the query
			if strings.TrimSpace(query) != "" {
				logging.Info("Query detected, will process after document loading")
				return waitForDocumentLoadingAndProcessQuery(streamChan, errChan, userMessage, multiPathResult.Paths)()
			}

			// No query, just wait for document loading to complete
			return waitForStreamToken(streamChan, errChan)()
		}

		// No path detected, process as regular message with document support
		logging.Debug("No path detected, processing as regular message")
		streamChan, errChan, err := m.pipeline.ProcessUserMessage(m.ctx, m.chat, m.llmModel, m.embedModel, userMessage)
		if err != nil {
			logging.Error("ProcessUserMessage failed: %v", err)
			return ChatResponseError{Err: err}
		}

		return waitForStreamToken(streamChan, errChan)()
	}
}

// replacePathsWithFilenames replaces full file paths with just filenames in the message
func replacePathsWithFilenames(originalMessage string, pathResults []document.PathDetectionResult) string {
	if len(pathResults) == 0 {
		return originalMessage
	}

	// Sort paths by start position in reverse order to replace from end to beginning
	// This ensures indices remain valid after replacements
	type pathWithIndices struct {
		path      string
		filename  string
		startIdx  int
		endIdx    int
	}

	var paths []pathWithIndices
	currentIdx := 0
	for _, pathResult := range pathResults {
		// Find the path in the message starting from currentIdx
		idx := strings.Index(originalMessage[currentIdx:], pathResult.Path)
		if idx >= 0 {
			actualIdx := currentIdx + idx
			filename := filepath.Base(pathResult.Path)
			paths = append(paths, pathWithIndices{
				path:     pathResult.Path,
				filename: filename,
				startIdx: actualIdx,
				endIdx:   actualIdx + len(pathResult.Path),
			})
			currentIdx = actualIdx + len(pathResult.Path)
		}
	}

	// Sort in reverse order by start index
	for i := 0; i < len(paths); i++ {
		for j := i + 1; j < len(paths); j++ {
			if paths[j].startIdx > paths[i].startIdx {
				paths[i], paths[j] = paths[j], paths[i]
			}
		}
	}

	// Replace paths with filenames from end to beginning
	result := originalMessage
	for _, p := range paths {
		result = result[:p.startIdx] + p.filename + result[p.endIdx:]
	}

	return result
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

// waitForDocumentLoadingAndProcessQuery waits for document loading to complete, then triggers query processing
func waitForDocumentLoadingAndProcessQuery(streamChan <-chan string, errChan <-chan error, originalMessage string, pathResults []document.PathDetectionResult) tea.Cmd {
	return func() tea.Msg {
		select {
		case token, ok := <-streamChan:
			if !ok {
				// Stream closed, document loading complete
				return DocumentLoadingComplete{
					OriginalMessage: originalMessage,
					PathResults:     pathResults,
				}
			}
			// Return token with channels for continuation
			return ChatMessageReceived{
				Token:           token,
				StreamChan:      streamChan,
				ErrChan:         errChan,
				OriginalMessage: originalMessage,
				PathResults:     pathResults,
			}

		case err := <-errChan:
			if err != nil {
				return ChatResponseError{Err: err}
			}
			// Continue waiting for tokens
			return waitForDocumentLoadingAndProcessQuery(streamChan, errChan, originalMessage, pathResults)()
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
		// Skip "context" messages - they're only for retrieval, not display
		if msg.Role == "context" {
			continue
		}

		if msg.Role == "user" {
			label := UserMessageLabelStyle.Render("You:")

			// Use safe markdown rendering for user messages
			renderedContent := m.safeRenderMarkdown(msg.Content)

			b.WriteString(GetUserMessageContentStyle(m.width).Render(label + "\n" + renderedContent))
			b.WriteString("\n\n")
		} else {
			label := AssistantMessageLabelStyle.Render("Assistant:")

			// Use safe markdown rendering for assistant messages
			renderedContent := m.safeRenderMarkdown(msg.Content)

			b.WriteString(GetAssistantMessageContentStyle(m.width).Render(label + "\n" + renderedContent))
			b.WriteString("\n\n")
		}
	}

	m.viewport.SetContent(b.String())
}

func (m *ChatViewModel) flushStreamBuffer() {
	if m.streamBuffer.Len() == 0 {
		return
	}

	// Add the complete assistant message
	assistantMsg := vector.Message{
		ID:        fmt.Sprintf("msg-%d", time.Now().UnixNano()),
		ChatID:    m.chat.ID,
		Role:      "assistant",
		Content:   m.streamBuffer.String(),
		Timestamp: time.Now(),
	}
	m.messages = append(m.messages, assistantMsg)

	m.streamBuffer.Reset()
	m.renderMessages()
	m.viewport.GotoBottom()
}

func (m ChatViewModel) renderScrollIndicator() string {
	if m.viewport.TotalLineCount() <= m.viewport.Height {
		return ""
	}

	scrollPercent := int(m.viewport.ScrollPercent() * 100)
	indicator := fmt.Sprintf("Scroll: %d%% ↕", scrollPercent)

	return ScrollIndicatorStyle.Render(indicator)
}

func (m ChatViewModel) loadAndShowFileSelector() tea.Cmd {
	return func() tea.Msg {
		docs, err := m.vectorStore.GetDocuments(context.Background())
		if err != nil {
			logging.Error("Failed to load documents for file selector: %v", err)
			return FileSelectorClosed{}
		}

		return DocumentsLoaded{Documents: docs}
	}
}

type MessagesLoaded struct {
	Messages []vector.Message
}

type DocumentsLoaded struct {
	Documents []vector.Document
}
