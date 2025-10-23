package models

import (
	"time"
)

type Chat struct {
	ID           string
	Name         string
	SystemPrompt string
	LLMModel     string
	EmbedModel   string
	CreatedAt    time.Time

	// RAG parameters
	Temperature  float64
	TopK         int
	UseReranking bool
	MaxTokens    int
}

func NewChat(name, systemPrompt, llmModel, embedModel string) *Chat {
	return &Chat{
		ID:           generateID(),
		Name:         name,
		SystemPrompt: systemPrompt,
		LLMModel:     llmModel,
		EmbedModel:   embedModel,
		CreatedAt:    time.Now(),
		Temperature:  0.7,
		TopK:         5,
		UseReranking: true,
		MaxTokens:    2048,
	}
}

func generateID() string {
	return time.Now().Format("20060102-150405")
}
