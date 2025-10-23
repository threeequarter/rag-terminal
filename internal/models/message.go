package models

import (
	"fmt"
	"time"
)

type Message struct {
	ID        string
	ChatID    string
	Role      string
	Content   string
	Timestamp time.Time
}

func NewMessage(chatID, role, content string) *Message {
	return &Message{
		ID:        generateMessageID(),
		ChatID:    chatID,
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
	}
}

func generateMessageID() string {
	return fmt.Sprintf("msg-%d", time.Now().UnixNano())
}
