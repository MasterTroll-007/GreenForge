package audit

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Logger provides tamper-evident audit logging with hash chain.
type Logger struct {
	mu       sync.Mutex
	db       *sql.DB
	lastHash string
}

// Event represents an auditable action.
type Event struct {
	ID        int64             `json:"id"`
	Timestamp time.Time         `json:"timestamp"`
	Action    string            `json:"action"`    // e.g. "tool.execute", "session.connect", "secret.access"
	User      string            `json:"user"`      // cert identity
	SessionID string            `json:"session_id"`
	Project   string            `json:"project"`
	Tool      string            `json:"tool,omitempty"`
	Details   map[string]string `json:"details,omitempty"`
	Hash      string            `json:"hash"` // SHA-256 hash chain
	PrevHash  string            `json:"prev_hash"`
}

// QueryFilter for querying audit events.
type QueryFilter struct {
	Limit     int
	User      string
	Tool      string
	Action    string
	SessionID string
	Since     *time.Time
	Until     *time.Time
}

// NewLogger creates a new audit logger with SQLite backend.
func NewLogger(dbPath string) (*Logger, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("opening audit db: %w", err)
	}

	if err := initAuditSchema(db); err != nil {
		db.Close()
		return nil, err
	}

	// Get last hash for chain continuity
	lastHash := getLastHash(db)

	return &Logger{db: db, lastHash: lastHash}, nil
}

func initAuditSchema(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS audit_events (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp  DATETIME NOT NULL,
			action     TEXT NOT NULL,
			user       TEXT DEFAULT '',
			session_id TEXT DEFAULT '',
			project    TEXT DEFAULT '',
			tool       TEXT DEFAULT '',
			details    TEXT DEFAULT '{}',
			hash       TEXT NOT NULL,
			prev_hash  TEXT NOT NULL
		);

		CREATE INDEX IF NOT EXISTS idx_audit_action ON audit_events(action);
		CREATE INDEX IF NOT EXISTS idx_audit_user ON audit_events(user);
		CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON audit_events(timestamp);
		CREATE INDEX IF NOT EXISTS idx_audit_tool ON audit_events(tool);
	`)
	return err
}

func getLastHash(db *sql.DB) string {
	var hash sql.NullString
	db.QueryRow("SELECT hash FROM audit_events ORDER BY id DESC LIMIT 1").Scan(&hash)
	if hash.Valid {
		return hash.String
	}
	return "genesis" // Initial hash for the chain
}

// Log records an audit event with hash chain.
func (l *Logger) Log(event Event) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	event.Timestamp = time.Now()
	event.PrevHash = l.lastHash

	// Calculate hash: SHA-256(prevHash + timestamp + action + user + details)
	detailsJSON, _ := json.Marshal(event.Details)
	hashInput := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s",
		event.PrevHash,
		event.Timestamp.Format(time.RFC3339Nano),
		event.Action,
		event.User,
		event.SessionID,
		event.Tool,
		string(detailsJSON),
	)
	hash := sha256.Sum256([]byte(hashInput))
	event.Hash = hex.EncodeToString(hash[:])

	_, err := l.db.Exec(`
		INSERT INTO audit_events (timestamp, action, user, session_id, project, tool, details, hash, prev_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.Timestamp,
		event.Action,
		event.User,
		event.SessionID,
		event.Project,
		event.Tool,
		string(detailsJSON),
		event.Hash,
		event.PrevHash,
	)
	if err != nil {
		return fmt.Errorf("inserting audit event: %w", err)
	}

	l.lastHash = event.Hash
	return nil
}

// Query retrieves audit events matching the filter.
func (l *Logger) Query(filter QueryFilter) ([]Event, error) {
	query := "SELECT id, timestamp, action, user, session_id, project, tool, details, hash, prev_hash FROM audit_events WHERE 1=1"
	var args []interface{}

	if filter.User != "" {
		query += " AND user = ?"
		args = append(args, filter.User)
	}
	if filter.Tool != "" {
		query += " AND tool = ?"
		args = append(args, filter.Tool)
	}
	if filter.Action != "" {
		query += " AND action = ?"
		args = append(args, filter.Action)
	}
	if filter.SessionID != "" {
		query += " AND session_id = ?"
		args = append(args, filter.SessionID)
	}
	if filter.Since != nil {
		query += " AND timestamp >= ?"
		args = append(args, *filter.Since)
	}
	if filter.Until != nil {
		query += " AND timestamp <= ?"
		args = append(args, *filter.Until)
	}

	query += " ORDER BY id DESC"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}

	rows, err := l.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		var detailsStr string
		if err := rows.Scan(
			&e.ID, &e.Timestamp, &e.Action, &e.User,
			&e.SessionID, &e.Project, &e.Tool,
			&detailsStr, &e.Hash, &e.PrevHash,
		); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(detailsStr), &e.Details)
		events = append(events, e)
	}
	return events, rows.Err()
}

// VerifyChain checks the integrity of the audit log hash chain.
func (l *Logger) VerifyChain() (bool, int64, error) {
	rows, err := l.db.Query(
		"SELECT id, timestamp, action, user, session_id, tool, details, hash, prev_hash FROM audit_events ORDER BY id ASC")
	if err != nil {
		return false, 0, err
	}
	defer rows.Close()

	expectedPrevHash := "genesis"
	var lastVerifiedID int64

	for rows.Next() {
		var e Event
		var detailsStr string
		if err := rows.Scan(
			&e.ID, &e.Timestamp, &e.Action, &e.User,
			&e.SessionID, &e.Tool,
			&detailsStr, &e.Hash, &e.PrevHash,
		); err != nil {
			return false, lastVerifiedID, err
		}

		// Verify prev_hash matches expected
		if e.PrevHash != expectedPrevHash {
			return false, e.ID, fmt.Errorf("chain broken at event %d: expected prev_hash %q, got %q",
				e.ID, expectedPrevHash, e.PrevHash)
		}

		// Verify hash
		hashInput := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s",
			e.PrevHash,
			e.Timestamp.Format(time.RFC3339Nano),
			e.Action,
			e.User,
			e.SessionID,
			e.Tool,
			detailsStr,
		)
		hash := sha256.Sum256([]byte(hashInput))
		expectedHash := hex.EncodeToString(hash[:])

		if e.Hash != expectedHash {
			return false, e.ID, fmt.Errorf("hash mismatch at event %d: expected %q, got %q",
				e.ID, expectedHash, e.Hash)
		}

		expectedPrevHash = e.Hash
		lastVerifiedID = e.ID
	}

	return true, lastVerifiedID, rows.Err()
}

// Close releases the database.
func (l *Logger) Close() error {
	return l.db.Close()
}
