package vector

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dgraph-io/badger/v4"
)

type BadgerStore struct {
	db *badger.DB
}

func NewBadgerStore(dbPath string) (*BadgerStore, error) {
	opts := badger.DefaultOptions(dbPath)
	opts.Logger = nil // Disable logging

	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open badger database: %w", err)
	}

	return &BadgerStore{db: db}, nil
}

func (s *BadgerStore) StoreMessage(ctx context.Context, chatID, messageID, role, content string, embedding []float32, timestamp time.Time) error {
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

	key := fmt.Sprintf("chat:%s:msg:%s", chatID, messageID)
	return s.db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(key), data)
	})
}

func (s *BadgerStore) SearchSimilar(ctx context.Context, chatID string, queryEmbedding []float32, topK int) ([]Message, error) {
	var messages []Message
	prefix := []byte(fmt.Sprintf("chat:%s:msg:", chatID))

	err := s.db.View(func(txn *badger.Txn) error {
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

func (s *BadgerStore) GetMessages(ctx context.Context, chatID string) ([]Message, error) {
	var messages []Message
	prefix := []byte(fmt.Sprintf("chat:%s:msg:", chatID))

	err := s.db.View(func(txn *badger.Txn) error {
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
	data, err := json.Marshal(chat)
	if err != nil {
		return fmt.Errorf("failed to marshal chat: %w", err)
	}

	key := fmt.Sprintf("metadata:chat:%s", chat.ID)
	return s.db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(key), data)
	})
}

func (s *BadgerStore) GetChat(ctx context.Context, chatID string) (*Chat, error) {
	var chat Chat
	key := []byte(fmt.Sprintf("metadata:chat:%s", chatID))

	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}

		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &chat)
		})
	})

	if err != nil {
		if err == badger.ErrKeyNotFound {
			return nil, fmt.Errorf("chat not found")
		}
		return nil, fmt.Errorf("failed to retrieve chat: %w", err)
	}

	return &chat, nil
}

func (s *BadgerStore) ListChats(ctx context.Context) ([]Chat, error) {
	var chats []Chat
	prefix := []byte("metadata:chat:")

	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			err := item.Value(func(val []byte) error {
				var chat Chat
				if err := json.Unmarshal(val, &chat); err != nil {
					return err
				}
				chats = append(chats, chat)
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to list chats: %w", err)
	}

	// Sort by creation date descending
	sort.Slice(chats, func(i, j int) bool {
		return chats[i].CreatedAt.After(chats[j].CreatedAt)
	})

	return chats, nil
}

func (s *BadgerStore) DeleteChat(ctx context.Context, chatID string) error {
	return s.db.Update(func(txn *badger.Txn) error {
		// Delete chat metadata
		chatKey := []byte(fmt.Sprintf("metadata:chat:%s", chatID))
		if err := txn.Delete(chatKey); err != nil && err != badger.ErrKeyNotFound {
			return fmt.Errorf("failed to delete chat metadata: %w", err)
		}

		// Delete all messages for this chat
		prefix := []byte(fmt.Sprintf("chat:%s:msg:", chatID))
		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			key := it.Item().KeyCopy(nil)
			if err := txn.Delete(key); err != nil {
				return fmt.Errorf("failed to delete message: %w", err)
			}
		}

		return nil
	})
}

func (s *BadgerStore) Close() error {
	return s.db.Close()
}

// Helper function to list all keys with a prefix (useful for debugging)
func (s *BadgerStore) listKeys(prefix string) ([]string, error) {
	var keys []string
	prefixBytes := []byte(prefix)

	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		if prefix != "" {
			opts.Prefix = prefixBytes
		}
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(prefixBytes); it.ValidForPrefix(prefixBytes); it.Next() {
			key := string(it.Item().KeyCopy(nil))
			// Remove internal badger keys
			if !strings.HasPrefix(key, "!badger!") {
				keys = append(keys, key)
			}
		}
		return nil
	})

	return keys, err
}
