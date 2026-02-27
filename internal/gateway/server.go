package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/greencode/greenforge/internal/agent"
	"github.com/greencode/greenforge/internal/audit"
	"github.com/greencode/greenforge/internal/autofix"
	"github.com/greencode/greenforge/internal/config"
	"github.com/greencode/greenforge/internal/digest"
	"github.com/greencode/greenforge/internal/index"
	"github.com/greencode/greenforge/internal/model"
	"github.com/greencode/greenforge/internal/rbac"
)

// Server is the main GreenForge gateway server handling WebSocket and REST.
type Server struct {
	cfg              *config.Config
	sessions         *SessionManager
	rbacEngine       *rbac.Engine
	auditor          *audit.Logger
	agentFn          func(cfg *config.Config) *agent.Runtime
	router           *model.Router
	webUI            *WebUIServer
	indexEngine      *index.Engine
	digestScheduler  *digest.Scheduler
	pipelineWatcher  *autofix.Watcher
	upgrader         websocket.Upgrader
	mu               sync.RWMutex
}

// NewServer creates a new gateway server.
func NewServer(cfg *config.Config, rbacEngine *rbac.Engine, auditor *audit.Logger) *Server {
	return &Server{
		cfg:        cfg,
		sessions:   NewSessionManager(),
		rbacEngine: rbacEngine,
		auditor:    auditor,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				// In production, validate origin properly
				return true
			},
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
	}
}

// SetAgentFactory sets the function used to create agent runtimes for new sessions.
func (s *Server) SetAgentFactory(fn func(cfg *config.Config) *agent.Runtime) {
	s.agentFn = fn
}

// SetRouter sets the model router for AI completions.
func (s *Server) SetRouter(r *model.Router) {
	s.router = r
}

// SetWebUI configures the web UI server.
func (s *Server) SetWebUI(webUI *WebUIServer) {
	s.webUI = webUI
}

// SetIndexEngine sets the codebase index engine reference.
func (s *Server) SetIndexEngine(engine *index.Engine) {
	s.indexEngine = engine
}

// SetDigestScheduler sets the digest scheduler reference.
func (s *Server) SetDigestScheduler(scheduler *digest.Scheduler) {
	s.digestScheduler = scheduler
}

// SetPipelineWatcher sets the pipeline watcher reference.
func (s *Server) SetPipelineWatcher(watcher *autofix.Watcher) {
	s.pipelineWatcher = watcher
}

// Start begins listening for connections.
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// WebSocket endpoint for agent sessions
	mux.HandleFunc("/ws", s.handleWebSocket)

	// REST API endpoints
	mux.HandleFunc("/api/v1/sessions", s.handleSessions)
	mux.HandleFunc("/api/v1/health", s.handleHealth)
	mux.HandleFunc("/api/v1/audit", s.handleAudit)

	// Web UI routes (models, config, chat, static files)
	if s.webUI != nil {
		s.webUI.SetupRoutes(mux)
	}

	addr := fmt.Sprintf("%s:%d", s.cfg.Gateway.Host, s.cfg.Gateway.Port)
	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 5 * time.Minute, // AI completions can take a while
		IdleTimeout:  120 * time.Second,
	}

	log.Printf("Gateway listening on %s", addr)

	// Start separate Web UI server on WebUIPort if configured
	if s.cfg.Gateway.WebUIPort > 0 && s.cfg.Gateway.WebUIPort != s.cfg.Gateway.Port {
		webMux := http.NewServeMux()
		// Proxy API and WS endpoints to gateway
		webMux.HandleFunc("/ws", s.handleWebSocket)
		webMux.HandleFunc("/api/v1/sessions", s.handleSessions)
		webMux.HandleFunc("/api/v1/health", s.handleHealth)
		webMux.HandleFunc("/api/v1/audit", s.handleAudit)
		if s.webUI != nil {
			s.webUI.SetupRoutes(webMux)
		}
		webAddr := fmt.Sprintf("%s:%d", s.cfg.Gateway.Host, s.cfg.Gateway.WebUIPort)
		webServer := &http.Server{
			Addr:         webAddr,
			Handler:      webMux,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 5 * time.Minute,
			IdleTimeout:  120 * time.Second,
		}
		go func() {
			log.Printf("Web UI listening on %s", webAddr)
			if err := webServer.ListenAndServe(); err != http.ErrServerClosed {
				log.Printf("Web UI server error: %v", err)
			}
		}()
		go func() {
			<-ctx.Done()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			webServer.Shutdown(shutdownCtx)
		}()
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		server.Shutdown(shutdownCtx)
	}()

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		return fmt.Errorf("gateway server error: %w", err)
	}
	return nil
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	// Extract session ID from query or create new
	sessionID := r.URL.Query().Get("session")
	project := r.URL.Query().Get("project")

	var session *Session
	if sessionID != "" {
		session = s.sessions.Get(sessionID)
		if session == nil {
			conn.WriteJSON(WSMessage{Type: "error", Data: "session not found"})
			conn.Close()
			return
		}
	} else {
		session = s.sessions.Create(project)
	}

	// Audit: session connected
	s.auditor.Log(audit.Event{
		Action:    "session.connect",
		SessionID: session.ID,
		Project:   project,
		Details:   map[string]string{"remote_addr": r.RemoteAddr},
	})

	client := &WSClient{
		conn:    conn,
		session: session,
		send:    make(chan WSMessage, 64),
	}

	session.AttachClient(client)

	go client.readPump(s)
	go client.writePump()
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		sessions := s.sessions.List()
		json.NewEncoder(w).Encode(sessions)
	case http.MethodPost:
		var req struct {
			Project  string   `json:"project"`
			Projects []string `json:"projects"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		session := s.sessions.Create(req.Project)
		if len(req.Projects) > 0 {
			session.Projects = req.Projects
		}
		json.NewEncoder(w).Encode(session)
	case http.MethodDelete:
		var req struct {
			ID string `json:"id"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.ID == "" {
			req.ID = r.URL.Query().Get("id")
		}
		if req.ID == "" {
			http.Error(w, `{"error":"missing session id"}`, http.StatusBadRequest)
			return
		}
		closed := s.sessions.Close(req.ID)
		json.NewEncoder(w).Encode(map[string]interface{}{"closed": closed, "id": req.ID})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"version": "0.1.0-dev",
	})
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	events, err := s.auditor.Query(audit.QueryFilter{Limit: 50})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(events)
}

// getIndexContext loads summaries from all indexed projects for AI context.
func (s *Server) getIndexContext() string {
	indexDir := filepath.Join(config.GreenForgeHome(), "index")
	entries, err := os.ReadDir(indexDir)
	if err != nil {
		return ""
	}

	var context string
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".db") {
			continue
		}
		projectName := strings.TrimSuffix(entry.Name(), ".db")
		dbPath := filepath.Join(indexDir, entry.Name())
		idx, err := index.NewEngine(dbPath)
		if err != nil {
			continue
		}
		summary := idx.GetContextSummary(projectName)
		idx.Close()
		if summary != "" {
			context += "\n" + summary
		}
	}

	if context != "" {
		return "\n\nBelow is your knowledge base from indexed codebases. This is YOUR data that YOU indexed and analyzed. " +
			"Answer questions about these projects confidently and directly based on this data. " +
			"Do NOT say the code is 'not available' or 'not accessible' - you HAVE the indexed data right here. " +
			"Present the information as your own knowledge.\n" + context
	}
	return ""
}

// --- WebSocket message types ---

// WSMessage is the wire format for WebSocket messages.
type WSMessage struct {
	Type string      `json:"type"`
	Data interface{} `json:"data,omitempty"`
	ID   string      `json:"id,omitempty"`
}

// WSClient represents a connected WebSocket client.
type WSClient struct {
	conn    *websocket.Conn
	session *Session
	send    chan WSMessage
}

func (c *WSClient) readPump(s *Server) {
	defer func() {
		c.session.DetachClient(c)
		c.conn.Close()
	}()

	for {
		var msg WSMessage
		if err := c.conn.ReadJSON(&msg); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("WebSocket read error: %v", err)
			}
			break
		}

		switch msg.Type {
		case "chat":
			// Process user message through agent
			if data, ok := msg.Data.(string); ok {
				go s.processMessage(c.session, c, data)
			}
		case "detach":
			return
		}
	}
}

func (c *WSClient) writePump() {
	defer c.conn.Close()

	for msg := range c.send {
		if err := c.conn.WriteJSON(msg); err != nil {
			log.Printf("WebSocket write error: %v", err)
			return
		}
	}
}

func (s *Server) processMessage(session *Session, client *WSClient, message string) {
	client.send <- WSMessage{
		Type: "thinking",
		Data: "Processing...",
	}

	// Save user message to session history
	session.mu.Lock()
	session.history = append(session.history, ChatMessage{
		Role:      "user",
		Content:   message,
		Timestamp: time.Now(),
	})
	// Build messages from history
	var msgs []model.Message

	// Build system prompt with codebase index context
	systemPrompt := "You are GreenForge, an AI developer assistant for JVM teams. Be concise and helpful. Respond in the same language as the user.\n"
	systemPrompt += s.getIndexContext()

	// Tell AI about selected projects it can browse
	if len(session.Projects) > 0 {
		systemPrompt += "\n\nYou have FULL FILE ACCESS to these project directories. You can read, search, and explore any file in them:\n"
		for _, p := range session.Projects {
			systemPrompt += "- " + p + "\n"
		}
		systemPrompt += "Use your tools (Read, Grep, Glob) to explore files when answering questions about code.\n"
	}

	msgs = append(msgs, model.Message{
		Role:    "system",
		Content: systemPrompt,
	})
	for _, h := range session.history {
		if h.Role == "user" || h.Role == "assistant" {
			msgs = append(msgs, model.Message{Role: h.Role, Content: h.Content})
		}
	}

	// Resolve working directory for file access
	workingDir := ""
	if session.Project != "" {
		workingDir = session.Project
	} else if len(session.Projects) > 0 {
		// Use first project's parent as cwd
		workingDir = session.Projects[0]
	}
	session.mu.Unlock()

	if s.router == nil {
		client.send <- WSMessage{
			Type: "response",
			Data: "No AI router configured. Check your model settings.",
		}
		return
	}

	var responseText string
	ctx := context.Background()
	if session.Project != "" {
		ctx = model.WithProject(ctx, session.Project)
	}

	req := model.Request{
		Messages:   msgs,
		MaxTokens:  4096,
		WorkingDir: workingDir,
	}

	// Use streaming for real-time progress
	err := s.router.StreamComplete(ctx, req, func(chunk model.StreamChunk) {
		if chunk.Done {
			return
		}
		if len(chunk.ToolCalls) > 0 {
			for _, tc := range chunk.ToolCalls {
				client.send <- WSMessage{
					Type: "tool_call",
					Data: map[string]string{"name": tc.Name},
				}
			}
			return
		}
		if chunk.Content != "" {
			responseText += chunk.Content
			client.send <- WSMessage{
				Type: "stream",
				Data: chunk.Content,
			}
		}
	})
	if err != nil {
		client.send <- WSMessage{
			Type: "error",
			Data: fmt.Sprintf("AI error: %v", err),
		}
		s.auditor.Log(audit.Event{
			Action:    "chat.error",
			SessionID: session.ID,
			Details:   map[string]string{"error": err.Error()},
		})
		return
	}
	// Send final response (stream_end)
	client.send <- WSMessage{
		Type: "stream_end",
		Data: responseText,
	}

	// Save assistant response to history
	session.mu.Lock()
	session.history = append(session.history, ChatMessage{
		Role:      "assistant",
		Content:   responseText,
		Timestamp: time.Now(),
	})
	session.mu.Unlock()

	// Audit
	s.auditor.Log(audit.Event{
		Action:    "chat.complete",
		SessionID: session.ID,
		Details:   map[string]string{"message_length": fmt.Sprintf("%d", len(responseText))},
	})
}

// --- Session Manager ---

// SessionManager tracks all active sessions.
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*Session),
	}
}

// Session represents an AI agent session.
type Session struct {
	ID        string    `json:"id"`
	Project   string    `json:"project"`
	Projects  []string  `json:"projects,omitempty"` // Selected project folders
	Status    string    `json:"status"`             // active, idle, detached
	CreatedAt time.Time `json:"created_at"`
	Device    string    `json:"device,omitempty"`

	mu      sync.RWMutex
	clients []*WSClient
	history []ChatMessage
}

// ChatMessage represents a message in the session history.
type ChatMessage struct {
	Role      string    `json:"role"` // user, assistant, system, tool
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
	ToolName  string    `json:"tool_name,omitempty"`
	ToolInput string    `json:"tool_input,omitempty"`
}

func (sm *SessionManager) Create(project string) *Session {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	id := fmt.Sprintf("s%d", len(sm.sessions)+1)
	session := &Session{
		ID:        id,
		Project:   project,
		Status:    "active",
		CreatedAt: time.Now(),
	}
	sm.sessions[id] = session
	return session
}

func (sm *SessionManager) Get(id string) *Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.sessions[id]
}

func (sm *SessionManager) List() []*Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	list := make([]*Session, 0, len(sm.sessions))
	for _, s := range sm.sessions {
		list = append(list, s)
	}
	return list
}

func (sm *SessionManager) Close(id string) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if _, exists := sm.sessions[id]; exists {
		delete(sm.sessions, id)
		return true
	}
	return false
}

func (s *Session) AttachClient(client *WSClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients = append(s.clients, client)
	s.Status = "active"
}

func (s *Session) DetachClient(client *WSClient) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, c := range s.clients {
		if c == client {
			s.clients = append(s.clients[:i], s.clients[i+1:]...)
			break
		}
	}
	if len(s.clients) == 0 {
		s.Status = "detached"
	}
}

func (s *Session) Broadcast(msg WSMessage) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, client := range s.clients {
		select {
		case client.send <- msg:
		default:
			// Client buffer full, skip
		}
	}
}

// Sessions returns the session manager for external access.
func (s *Server) Sessions() *SessionManager {
	return s.sessions
}

// Used by tests and internal code
var _ = uuid.New // ensure uuid is used
