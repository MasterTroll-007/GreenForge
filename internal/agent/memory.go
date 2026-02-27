package agent

import (
	"sync"
	"time"

	"github.com/greencode/greenforge/internal/model"
)

// Memory stores conversation history per session.
type Memory struct {
	mu       sync.RWMutex
	sessions map[string][]Message
	maxSize  int // max messages per session before summarization
}

// Message represents a conversation message.
type Message struct {
	Role       string           `json:"role"` // user, assistant, system, tool
	Content    string           `json:"content"`
	Timestamp  time.Time        `json:"timestamp"`
	ToolCalls  []model.ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolName   string           `json:"tool_name,omitempty"`
}

// NewMemory creates a new session memory store.
func NewMemory() *Memory {
	return &Memory{
		sessions: make(map[string][]Message),
		maxSize:  200,
	}
}

// Add appends a message to a session's history.
func (m *Memory) Add(sessionID string, msg Message) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sessions[sessionID] = append(m.sessions[sessionID], msg)

	// Trim if exceeding max size (keep system + recent messages)
	if len(m.sessions[sessionID]) > m.maxSize {
		history := m.sessions[sessionID]
		// Keep first 10 (system/early context) + last 150 messages
		trimmed := make([]Message, 0, 160)
		trimmed = append(trimmed, history[:10]...)
		trimmed = append(trimmed, history[len(history)-150:]...)
		m.sessions[sessionID] = trimmed
	}
}

// Get returns the conversation history for a session.
func (m *Memory) Get(sessionID string) []Message {
	m.mu.RLock()
	defer m.mu.RUnlock()

	msgs := m.sessions[sessionID]
	result := make([]Message, len(msgs))
	copy(result, msgs)
	return result
}

// Clear removes all messages for a session.
func (m *Memory) Clear(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, sessionID)
}

// SessionCount returns the number of active sessions.
func (m *Memory) SessionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// MessageCount returns the number of messages in a session.
func (m *Memory) MessageCount(sessionID string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions[sessionID])
}
