package vector

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/dgraph-io/badger/v4"
)

type BadgerStore struct {
	baseDir      string
	currentChatID string
	currentDB    *badger.DB
	mu           sync.RWMutex
}

func NewBadgerStore(baseDir string) (*BadgerStore, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create base directory: %w", err)
	}

	return &BadgerStore{
		baseDir: baseDir,
	}, nil
}

func (s *BadgerStore) OpenChat(ctx context.Context, chatID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Close existing chat if open
	if s.currentDB != nil {
		if err := s.currentDB.Close(); err != nil {
			return fmt.Errorf("failed to close current chat database: %w", err)
		}
		s.currentDB = nil
		s.currentChatID = ""
	}

	// Open database for the specified chat
	chatDBPath := filepath.Join(s.baseDir, chatID, "messages.db")
	if err := os.MkdirAll(filepath.Dir(chatDBPath), 0755); err != nil {
		return fmt.Errorf("failed to create chat directory: %w", err)
	}

	opts := badger.DefaultOptions(chatDBPath)
	opts.Logger = nil // Disable logging

	db, err := badger.Open(opts)
	if err != nil {
		return fmt.Errorf("failed to open chat database: %w", err)
	}

	s.currentDB = db
	s.currentChatID = chatID
	return nil
}

func (s *BadgerStore) CloseChat(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.currentDB == nil {
		return nil // No chat open
	}

	if err := s.currentDB.Close(); err != nil {
		return fmt.Errorf("failed to close chat database: %w", err)
	}

	s.currentDB = nil
	s.currentChatID = ""
	return nil
}

func (s *BadgerStore) StoreMessage(ctx context.Context, messageID, role, content string, embedding []float32, timestamp time.Time) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.currentDB == nil {
		return fmt.Errorf("no chat is currently open")
	}

	msg := Message{
		ID:        messageID,
		ChatID:    s.currentChatID,
		Role:      role,
		Content:   content,
		Embedding: embedding,
		Timestamp: timestamp,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	key := fmt.Sprintf("msg:%s", messageID)
	return s.currentDB.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(key), data)
	})
}

func (s *BadgerStore) SearchSimilar(ctx context.Context, queryEmbedding []float32, topK int) ([]Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.currentDB == nil {
		return nil, fmt.Errorf("no chat is currently open")
	}

	var messages []Message
	prefix := []byte("msg:")

	err := s.currentDB.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			err := item.Value(func(val []byte) error {
				var msg Message
				if err := json.Unmarshal(val, &msg); err != nil {
					return err
				}
				messages = append(messages, msg)
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to retrieve messages: %w", err)
	}

	// Calculate similarity scores
	type scoredMessage struct {
		message Message
		score   float32
	}

	scored := make([]scoredMessage, 0, len(messages))
	for _, msg := range messages {
		if len(msg.Embedding) > 0 {
			score := CosineSimilarity(queryEmbedding, msg.Embedding)
			scored = append(scored, scoredMessage{message: msg, score: score})
		}
	}

	// Sort by score descending
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	// Take top K
	if len(scored) > topK {
		scored = scored[:topK]
	}

	result := make([]Message, len(scored))
	for i, sm := range scored {
		result[i] = sm.message
	}

	return result, nil
}

func (s *BadgerStore) GetMessages(ctx context.Context) ([]Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.currentDB == nil {
		return nil, fmt.Errorf("no chat is currently open")
	}

	var messages []Message
	prefix := []byte("msg:")

	err := s.currentDB.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			err := item.Value(func(val []byte) error {
				var msg Message
				if err := json.Unmarshal(val, &msg); err != nil {
					return err
				}
				messages = append(messages, msg)
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to retrieve messages: %w", err)
	}

	// Sort by timestamp
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].Timestamp.Before(messages[j].Timestamp)
	})

	return messages, nil
}

func (s *BadgerStore) StoreChat(ctx context.Context, chat *Chat) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Create chat directory
	chatDir := filepath.Join(s.baseDir, chat.ID)
	if err := os.MkdirAll(chatDir, 0755); err != nil {
		return fmt.Errorf("failed to create chat directory: %w", err)
	}

	// Store chat metadata as JSON file
	metadataPath := filepath.Join(chatDir, "metadata.json")
	data, err := json.Marshal(chat)
	if err != nil {
		return fmt.Errorf("failed to marshal chat metadata: %w", err)
	}

	if err := os.WriteFile(metadataPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write chat metadata: %w", err)
	}

	return nil
}

func (s *BadgerStore) GetChat(ctx context.Context, chatID string) (*Chat, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	metadataPath := filepath.Join(s.baseDir, chatID, "metadata.json")
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("chat not found")
		}
		return nil, fmt.Errorf("failed to read chat metadata: %w", err)
	}

	var chat Chat
	if err := json.Unmarshal(data, &chat); err != nil {
		return nil, fmt.Errorf("failed to unmarshal chat metadata: %w", err)
	}

	return &chat, nil
}

func (s *BadgerStore) ListChats(ctx context.Context) ([]Chat, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Chat{}, nil
		}
		return nil, fmt.Errorf("failed to read base directory: %w", err)
	}

	var chats []Chat
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		chatID := entry.Name()
		metadataPath := filepath.Join(s.baseDir, chatID, "metadata.json")

		data, err := os.ReadFile(metadataPath)
		if err != nil {
			// Skip directories without metadata
			continue
		}

		var chat Chat
		if err := json.Unmarshal(data, &chat); err != nil {
			// Skip invalid metadata files
			continue
		}

		chats = append(chats, chat)
	}

	// Sort by creation date descending
	sort.Slice(chats, func(i, j int) bool {
		return chats[i].CreatedAt.After(chats[j].CreatedAt)
	})

	return chats, nil
}

func (s *BadgerStore) DeleteChat(ctx context.Context, chatID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Close database if this is the currently open chat
	if s.currentChatID == chatID && s.currentDB != nil {
		if err := s.currentDB.Close(); err != nil {
			return fmt.Errorf("failed to close database before deletion: %w", err)
		}
		s.currentDB = nil
		s.currentChatID = ""
	}

	// Remove entire chat directory
	chatDir := filepath.Join(s.baseDir, chatID)
	if err := os.RemoveAll(chatDir); err != nil {
		return fmt.Errorf("failed to delete chat directory: %w", err)
	}

	return nil
}

// StoreMessageToChat stores a message to a specific chat without changing the current context
// This is useful for background operations that shouldn't interfere with the active chat
func (s *BadgerStore) StoreMessageToChat(ctx context.Context, chatID, messageID, role, content string, embedding []float32, timestamp time.Time) error {
	// Open a separate database connection for this specific chat
	chatDBPath := filepath.Join(s.baseDir, chatID, "messages.db")
	if err := os.MkdirAll(filepath.Dir(chatDBPath), 0755); err != nil {
		return fmt.Errorf("failed to create chat directory: %w", err)
	}

	opts := badger.DefaultOptions(chatDBPath)
	opts.Logger = nil // Disable logging

	db, err := badger.Open(opts)
	if err != nil {
		return fmt.Errorf("failed to open chat database: %w", err)
	}
	defer db.Close()

	msg := Message{
		ID:        messageID,
		ChatID:    chatID,
		Role:      role,
		Content:   content,
		Embedding: embedding,
		Timestamp: timestamp,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	key := fmt.Sprintf("msg:%s", messageID)
	return db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(key), data)
	})
}

func (s *BadgerStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.currentDB != nil {
		if err := s.currentDB.Close(); err != nil {
			return fmt.Errorf("failed to close database: %w", err)
		}
		s.currentDB = nil
		s.currentChatID = ""
	}

	return nil
}

// Document storage methods

// StoreDocument stores a document metadata in the chat context
func (s *BadgerStore) StoreDocument(ctx context.Context, doc *Document) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.currentDB == nil {
		return fmt.Errorf("no chat is currently open")
	}

	data, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("failed to marshal document: %w", err)
	}

	key := fmt.Sprintf("doc:%s", doc.ID)
	return s.currentDB.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(key), data)
	})
}

// StoreDocumentChunk stores a document chunk with its embedding
func (s *BadgerStore) StoreDocumentChunk(ctx context.Context, chunk *DocumentChunk) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.currentDB == nil {
		return fmt.Errorf("no chat is currently open")
	}

	data, err := json.Marshal(chunk)
	if err != nil {
		return fmt.Errorf("failed to marshal chunk: %w", err)
	}

	key := fmt.Sprintf("chunk:%s", chunk.ID)
	return s.currentDB.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(key), data)
	})
}

// GetDocuments retrieves all documents for the current chat
func (s *BadgerStore) GetDocuments(ctx context.Context) ([]Document, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.currentDB == nil {
		return nil, fmt.Errorf("no chat is currently open")
	}

	var documents []Document
	prefix := []byte("doc:")

	err := s.currentDB.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			err := item.Value(func(val []byte) error {
				var doc Document
				if err := json.Unmarshal(val, &doc); err != nil {
					return err
				}
				documents = append(documents, doc)
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to retrieve documents: %w", err)
	}

	// Sort by upload time
	sort.Slice(documents, func(i, j int) bool {
		return documents[i].UploadedAt.Before(documents[j].UploadedAt)
	})

	return documents, nil
}

// GetDocumentCount returns the number of documents in the current chat
func (s *BadgerStore) GetDocumentCount(ctx context.Context) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.currentDB == nil {
		return 0, fmt.Errorf("no chat is currently open")
	}

	count := 0
	prefix := []byte("doc:")

	err := s.currentDB.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefix
		opts.PrefetchValues = false // Only count keys
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			count++
		}
		return nil
	})

	if err != nil {
		return 0, fmt.Errorf("failed to count documents: %w", err)
	}

	return count, nil
}

// FindDocumentByHash checks if a document with the same content hash already exists
func (s *BadgerStore) FindDocumentByHash(ctx context.Context, contentHash string) (*Document, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.currentDB == nil {
		return nil, fmt.Errorf("no chat is currently open")
	}

	var foundDoc *Document
	prefix := []byte("doc:")

	err := s.currentDB.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			err := item.Value(func(val []byte) error {
				var doc Document
				if err := json.Unmarshal(val, &doc); err != nil {
					return err
				}
				if doc.ContentHash == contentHash {
					foundDoc = &doc
					return nil
				}
				return nil
			})
			if err != nil {
				return err
			}
			if foundDoc != nil {
				break
			}
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to search for document by hash: %w", err)
	}

	return foundDoc, nil
}

// SearchSimilarWithChunks searches for similar content including both messages and document chunks
func (s *BadgerStore) SearchSimilarWithChunks(ctx context.Context, queryEmbedding []float32, topK int) ([]Message, []DocumentChunk, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.currentDB == nil {
		return nil, nil, fmt.Errorf("no chat is currently open")
	}

	// Get messages
	var messages []Message
	msgPrefix := []byte("msg:")

	err := s.currentDB.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = msgPrefix
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(msgPrefix); it.ValidForPrefix(msgPrefix); it.Next() {
			item := it.Item()
			err := item.Value(func(val []byte) error {
				var msg Message
				if err := json.Unmarshal(val, &msg); err != nil {
					return err
				}
				messages = append(messages, msg)
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		return nil, nil, fmt.Errorf("failed to retrieve messages: %w", err)
	}

	// Get document chunks
	var chunks []DocumentChunk
	chunkPrefix := []byte("chunk:")

	err = s.currentDB.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = chunkPrefix
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(chunkPrefix); it.ValidForPrefix(chunkPrefix); it.Next() {
			item := it.Item()
			err := item.Value(func(val []byte) error {
				var chunk DocumentChunk
				if err := json.Unmarshal(val, &chunk); err != nil {
					return err
				}
				chunks = append(chunks, chunk)
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		return nil, nil, fmt.Errorf("failed to retrieve chunks: %w", err)
	}

	// Calculate similarity scores for messages
	type scoredMessage struct {
		message Message
		score   float32
	}

	scoredMessages := make([]scoredMessage, 0, len(messages))
	for _, msg := range messages {
		if len(msg.Embedding) > 0 {
			score := CosineSimilarity(queryEmbedding, msg.Embedding)
			scoredMessages = append(scoredMessages, scoredMessage{message: msg, score: score})
		}
	}

	// Calculate similarity scores for chunks
	type scoredChunk struct {
		chunk DocumentChunk
		score float32
	}

	scoredChunks := make([]scoredChunk, 0, len(chunks))
	for _, chunk := range chunks {
		if len(chunk.Embedding) > 0 {
			score := CosineSimilarity(queryEmbedding, chunk.Embedding)
			scoredChunks = append(scoredChunks, scoredChunk{chunk: chunk, score: score})
		}
	}

	// Sort both by score descending
	sort.Slice(scoredMessages, func(i, j int) bool {
		return scoredMessages[i].score > scoredMessages[j].score
	})

	sort.Slice(scoredChunks, func(i, j int) bool {
		return scoredChunks[i].score > scoredChunks[j].score
	})

	// Interleave results: take top items from both, preferring higher scores
	// Strategy: Take topK/2 from each, or adjust based on availability
	messageCount := topK / 2
	chunkCount := topK - messageCount

	if len(scoredMessages) < messageCount {
		// Not enough messages, allocate more to chunks
		chunkCount += messageCount - len(scoredMessages)
		messageCount = len(scoredMessages)
	}

	if len(scoredChunks) < chunkCount {
		// Not enough chunks, allocate more to messages
		messageCount += chunkCount - len(scoredChunks)
		chunkCount = len(scoredChunks)
	}

	// Cap to available items
	if messageCount > len(scoredMessages) {
		messageCount = len(scoredMessages)
	}
	if chunkCount > len(scoredChunks) {
		chunkCount = len(scoredChunks)
	}

	// Extract results
	resultMessages := make([]Message, messageCount)
	for i := 0; i < messageCount; i++ {
		resultMessages[i] = scoredMessages[i].message
	}

	resultChunks := make([]DocumentChunk, chunkCount)
	for i := 0; i < chunkCount; i++ {
		resultChunks[i] = scoredChunks[i].chunk
	}

	return resultMessages, resultChunks, nil
}
