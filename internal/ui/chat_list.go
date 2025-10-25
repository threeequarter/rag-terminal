package ui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"rag-terminal/internal/vector"
)

type ChatListModel struct {
	list   list.Model
	chats  []vector.Chat
	width  int
	height int
	err    error
}

type chatItem struct {
	chat vector.Chat
}

func (i chatItem) Title() string { return i.chat.Name }
func (i chatItem) Description() string {
	fileInfo := ""
	if i.chat.FileCount > 0 {
		fileInfo = fmt.Sprintf(" | Files: %d", i.chat.FileCount)
	}
	return fmt.Sprintf("Created: %s | Model: %s%s", i.chat.CreatedAt.Format("2006-01-02 15:04"), i.chat.LLMModel, fileInfo)
}
func (i chatItem) FilterValue() string { return i.chat.Name }

type ChatSelected struct {
	Chat vector.Chat
}

type CreateNewChat struct{}

type DeleteChat struct {
	ChatID string
}

func NewChatListModel(chats []vector.Chat, width, height int) ChatListModel {
	items := make([]list.Item, len(chats))
	for i, c := range chats {
		items[i] = chatItem{chat: c}
	}

	l := list.New(items, list.NewDefaultDelegate(), width, height-4)
	l.Title = "Chat Conversations"
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(false)

	// Disable all built-in key bindings except arrows and filter
	l.KeyMap.CursorUp = key.NewBinding(key.WithKeys("up"))
	l.KeyMap.CursorDown = key.NewBinding(key.WithKeys("down"))
	l.KeyMap.NextPage = key.NewBinding()
	l.KeyMap.PrevPage = key.NewBinding()
	l.KeyMap.GoToStart = key.NewBinding()
	l.KeyMap.GoToEnd = key.NewBinding()
	l.KeyMap.Filter = key.NewBinding(key.WithKeys("/"))
	l.KeyMap.ClearFilter = key.NewBinding(key.WithKeys("esc"))
	l.KeyMap.CancelWhileFiltering = key.NewBinding(key.WithKeys("esc"))
	l.KeyMap.AcceptWhileFiltering = key.NewBinding(key.WithKeys("enter"))
	l.KeyMap.ShowFullHelp = key.NewBinding()
	l.KeyMap.CloseFullHelp = key.NewBinding()
	l.KeyMap.Quit = key.NewBinding()
	l.KeyMap.ForceQuit = key.NewBinding()

	return ChatListModel{
		list:   l,
		chats:  chats,
		width:  width,
		height: height,
	}
}

func (m ChatListModel) Init() tea.Cmd {
	return nil
}

func (m ChatListModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.list.SetSize(msg.Width, msg.Height-4)
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+x":
			return m, tea.Quit

		case "enter":
			selectedItem := m.list.SelectedItem()
			if selectedItem == nil {
				return m, nil
			}
			chat := selectedItem.(chatItem).chat
			return m, func() tea.Msg {
				return ChatSelected{Chat: chat}
			}

		case "ctrl+n":
			return m, func() tea.Msg {
				return CreateNewChat{}
			}

		case "ctrl+d":
			selectedItem := m.list.SelectedItem()
			if selectedItem == nil {
				return m, nil
			}
			chat := selectedItem.(chatItem).chat
			return m, func() tea.Msg {
				return DeleteChat{ChatID: chat.ID}
			}
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m ChatListModel) View() string {
	if m.err != nil {
		return errorStyle.Render(fmt.Sprintf("Error: %v\n\nPress Ctrl+X to exit", m.err))
	}

	helpText := "↑/↓: Navigate • Enter: Open • /: Filter • Ctrl+N: New Chat • Ctrl+D: Delete • Ctrl+X: Exit"

	return lipgloss.JoinVertical(lipgloss.Left,
		m.list.View(),
		helpStyle.Render(helpText),
	)
}

func (m *ChatListModel) RefreshChats(chats []vector.Chat) {
	m.chats = chats
	items := make([]list.Item, len(chats))
	for i, c := range chats {
		items[i] = chatItem{chat: c}
	}
	m.list.SetItems(items)
}
