package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"rag-chat/internal/nexa"
	"rag-chat/internal/rag"
	"rag-chat/internal/ui"
	"rag-chat/internal/vector"
)

type appState int

const (
	stateModelSelect appState = iota
	stateChatList
	stateChatCreate
	stateChatView
)

type model struct {
	state       appState
	nexaClient  *nexa.Client
	vectorStore vector.VectorStore
	pipeline    *rag.Pipeline

	// UI models
	modelSelectModel ui.ModelSelectModel
	chatListModel    ui.ChatListModel
	chatCreateModel  ui.ChatCreateModel
	chatViewModel    ui.ChatViewModel

	// Selected models
	llmModel    string
	embedModel  string
	rerankModel string

	// Current chat
	currentChat *vector.Chat

	// Screen size
	width  int
	height int

	// Error state
	err error
}

func main() {
	// Initialize BadgerDB
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("Failed to get user home directory: %v", err)
	}

	dbPath := filepath.Join(homeDir, ".rag-chat", "db")
	if err := os.MkdirAll(dbPath, 0755); err != nil {
		log.Fatalf("Failed to create database directory: %v", err)
	}

	vectorStore, err := vector.NewBadgerStore(dbPath)
	if err != nil {
		log.Fatalf("Failed to initialize vector store: %v", err)
	}
	defer vectorStore.Close()

	// Create Nexa client
	nexaClient := nexa.NewClient("")

	// Get available models
	models, err := nexaClient.GetModels()
	if err != nil {
		log.Fatalf("Failed to get models: %v", err)
	}

	// Create initial model
	initialModel := model{
		state:       stateModelSelect,
		nexaClient:  nexaClient,
		vectorStore: vectorStore,
		width:       80,
		height:      24,
	}

	// Create model select UI
	initialModel.modelSelectModel = ui.NewModelSelectModel(models, 80, 24)

	// Run the program
	p := tea.NewProgram(initialModel, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		log.Fatalf("Error running program: %v", err)
	}
}

func (m model) Init() tea.Cmd {
	switch m.state {
	case stateModelSelect:
		return m.modelSelectModel.Init()
	case stateChatList:
		return m.chatListModel.Init()
	case stateChatCreate:
		return m.chatCreateModel.Init()
	case stateChatView:
		return m.chatViewModel.Init()
	}
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// Update current screen
		switch m.state {
		case stateModelSelect:
			newModel, cmd := m.modelSelectModel.Update(msg)
			m.modelSelectModel = newModel.(ui.ModelSelectModel)
			return m, cmd
		case stateChatList:
			newModel, cmd := m.chatListModel.Update(msg)
			m.chatListModel = newModel.(ui.ChatListModel)
			return m, cmd
		case stateChatCreate:
			newModel, cmd := m.chatCreateModel.Update(msg)
			m.chatCreateModel = newModel.(ui.ChatCreateModel)
			return m, cmd
		case stateChatView:
			newModel, cmd := m.chatViewModel.Update(msg)
			m.chatViewModel = newModel.(ui.ChatViewModel)
			return m, cmd
		}

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" || msg.String() == "ctrl+x" {
			return m, tea.Quit
		}

	case ui.ModelSelectionComplete:
		// Transition to chat list
		m.llmModel = msg.LLMModel
		m.embedModel = msg.EmbedModel
		m.rerankModel = msg.RerankModel
		m.pipeline = rag.NewPipeline(m.nexaClient, m.vectorStore)

		chats, err := m.vectorStore.ListChats(context.Background())
		if err != nil {
			m.err = err
			return m, tea.Quit
		}

		m.state = stateChatList
		m.chatListModel = ui.NewChatListModel(chats, m.width, m.height)
		return m, m.chatListModel.Init()

	case ui.CreateNewChat:
		// Transition to chat create
		m.state = stateChatCreate
		m.chatCreateModel = ui.NewChatCreateModel(m.llmModel, m.embedModel, m.rerankModel, m.width, m.height)
		return m, m.chatCreateModel.Init()

	case ui.ChatCreated:
		// Save chat and transition to chat view
		if err := m.vectorStore.StoreChat(context.Background(), msg.Chat); err != nil {
			m.err = err
			return m, tea.Quit
		}

		m.currentChat = msg.Chat
		m.state = stateChatView
		m.chatViewModel = ui.NewChatViewModel(msg.Chat, m.pipeline, m.vectorStore, m.width, m.height)
		return m, m.chatViewModel.Init()

	case ui.ChatSelected:
		// Transition to chat view
		m.currentChat = &msg.Chat
		m.state = stateChatView
		m.chatViewModel = ui.NewChatViewModel(&msg.Chat, m.pipeline, m.vectorStore, m.width, m.height)
		return m, m.chatViewModel.Init()

	case ui.DeleteChat:
		// Delete chat and refresh list
		if err := m.vectorStore.DeleteChat(context.Background(), msg.ChatID); err != nil {
			m.err = err
			return m, nil
		}

		chats, err := m.vectorStore.ListChats(context.Background())
		if err != nil {
			m.err = err
			return m, nil
		}

		m.chatListModel.RefreshChats(chats)
		return m, nil

	case ui.BackToChatList:
		// Transition back to chat list
		chats, err := m.vectorStore.ListChats(context.Background())
		if err != nil {
			m.err = err
			return m, tea.Quit
		}

		m.state = stateChatList
		m.chatListModel = ui.NewChatListModel(chats, m.width, m.height)
		return m, m.chatListModel.Init()
	}

	// Delegate to current screen
	switch m.state {
	case stateModelSelect:
		newModel, cmd := m.modelSelectModel.Update(msg)
		m.modelSelectModel = newModel.(ui.ModelSelectModel)
		return m, cmd

	case stateChatList:
		newModel, cmd := m.chatListModel.Update(msg)
		m.chatListModel = newModel.(ui.ChatListModel)
		return m, cmd

	case stateChatCreate:
		newModel, cmd := m.chatCreateModel.Update(msg)
		m.chatCreateModel = newModel.(ui.ChatCreateModel)
		return m, cmd

	case stateChatView:
		newModel, cmd := m.chatViewModel.Update(msg)
		m.chatViewModel = newModel.(ui.ChatViewModel)
		return m, cmd
	}

	return m, nil
}

func (m model) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v\n\nPress Ctrl+C to quit", m.err)
	}

	switch m.state {
	case stateModelSelect:
		return m.modelSelectModel.View()
	case stateChatList:
		return m.chatListModel.View()
	case stateChatCreate:
		return m.chatCreateModel.View()
	case stateChatView:
		return m.chatViewModel.View()
	}

	return "Loading..."
}
