package index

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Engine is the codebase index engine - zero-LLM, local-only.
type Engine struct {
	mu     sync.RWMutex
	db     *sql.DB
	dbPath string
}

// IndexedFile represents a file in the index.
type IndexedFile struct {
	Path       string    `json:"path"`
	Module     string    `json:"module"`
	Language   string    `json:"language"` // java, kotlin, gradle, xml, yaml
	Hash       string    `json:"hash"`
	IndexedAt  time.Time `json:"indexed_at"`
}

// IndexedClass represents a class/interface in the index.
type IndexedClass struct {
	Name        string   `json:"name"`
	Package     string   `json:"package"`
	File        string   `json:"file"`
	Module      string   `json:"module"`
	Kind        string   `json:"kind"` // class, interface, enum, data_class, sealed_class
	Annotations []string `json:"annotations"`
	Extends     string   `json:"extends"`
	Implements  []string `json:"implements"`
}

// IndexedMethod represents a method in the index.
type IndexedMethod struct {
	Name        string   `json:"name"`
	ClassName   string   `json:"class_name"`
	File        string   `json:"file"`
	Line        int      `json:"line"`
	ReturnType  string   `json:"return_type"`
	Params      string   `json:"params"`
	Annotations []string `json:"annotations"`
}

// Endpoint represents a Spring REST endpoint.
type Endpoint struct {
	Method     string `json:"method"` // GET, POST, PUT, DELETE
	Path       string `json:"path"`
	Handler    string `json:"handler"` // ClassName.methodName
	File       string `json:"file"`
	Line       int    `json:"line"`
}

// KafkaTopic represents a Kafka topic mapping.
type KafkaTopic struct {
	Topic     string `json:"topic"`
	GroupID   string `json:"group_id"`
	Type      string `json:"type"` // listener, producer
	Handler   string `json:"handler"`
	File      string `json:"file"`
	Line      int    `json:"line"`
}

// ModuleDep represents a dependency between modules.
type ModuleDep struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"` // implementation, api, compileOnly, etc.
}

// NewEngine creates a new codebase index engine.
func NewEngine(dbPath string) (*Engine, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("opening index db: %w", err)
	}

	if err := initIndexSchema(db); err != nil {
		db.Close()
		return nil, err
	}

	return &Engine{db: db, dbPath: dbPath}, nil
}

func initIndexSchema(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS files (
			path       TEXT PRIMARY KEY,
			module     TEXT DEFAULT '',
			language   TEXT DEFAULT '',
			hash       TEXT DEFAULT '',
			indexed_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS classes (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			name        TEXT NOT NULL,
			package     TEXT DEFAULT '',
			file        TEXT NOT NULL,
			module      TEXT DEFAULT '',
			kind        TEXT DEFAULT 'class',
			annotations TEXT DEFAULT '[]',
			extends     TEXT DEFAULT '',
			implements  TEXT DEFAULT '[]'
		);
		CREATE INDEX IF NOT EXISTS idx_classes_name ON classes(name);
		CREATE INDEX IF NOT EXISTS idx_classes_file ON classes(file);

		CREATE TABLE IF NOT EXISTS methods (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			name        TEXT NOT NULL,
			class_name  TEXT NOT NULL,
			file        TEXT NOT NULL,
			line        INTEGER DEFAULT 0,
			return_type TEXT DEFAULT '',
			params      TEXT DEFAULT '',
			annotations TEXT DEFAULT '[]'
		);
		CREATE INDEX IF NOT EXISTS idx_methods_name ON methods(name);
		CREATE INDEX IF NOT EXISTS idx_methods_class ON methods(class_name);

		CREATE TABLE IF NOT EXISTS endpoints (
			id       INTEGER PRIMARY KEY AUTOINCREMENT,
			method   TEXT NOT NULL,
			path     TEXT NOT NULL,
			handler  TEXT NOT NULL,
			file     TEXT NOT NULL,
			line     INTEGER DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS idx_endpoints_path ON endpoints(path);

		CREATE TABLE IF NOT EXISTS kafka_topics (
			id       INTEGER PRIMARY KEY AUTOINCREMENT,
			topic    TEXT NOT NULL,
			group_id TEXT DEFAULT '',
			type     TEXT NOT NULL,
			handler  TEXT NOT NULL,
			file     TEXT NOT NULL,
			line     INTEGER DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS idx_kafka_topic ON kafka_topics(topic);

		CREATE TABLE IF NOT EXISTS module_deps (
			id    INTEGER PRIMARY KEY AUTOINCREMENT,
			from_module TEXT NOT NULL,
			to_module   TEXT NOT NULL,
			dep_type    TEXT DEFAULT 'implementation'
		);

		CREATE TABLE IF NOT EXISTS entities (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			name       TEXT NOT NULL,
			table_name TEXT DEFAULT '',
			file       TEXT NOT NULL,
			module     TEXT DEFAULT ''
		);

		CREATE TABLE IF NOT EXISTS spring_beans (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			name        TEXT NOT NULL,
			type        TEXT NOT NULL,
			class_name  TEXT NOT NULL,
			file        TEXT NOT NULL,
			module      TEXT DEFAULT '',
			profile     TEXT DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS idx_beans_type ON spring_beans(type);

		-- Full-Text Search for natural language queries
		CREATE VIRTUAL TABLE IF NOT EXISTS fts_index USING fts5(
			name, package, file, kind, annotations, content,
			tokenize='porter unicode61'
		);
	`)
	return err
}

// IndexProject performs a full index of a project directory.
func (e *Engine) IndexProject(ctx context.Context, projectPath string) (*IndexStats, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	stats := &IndexStats{StartTime: time.Now()}

	// Detect build system
	buildTool := detectBuildTool(projectPath)
	stats.BuildTool = buildTool

	// Walk all source files
	err := filepath.Walk(projectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Skip hidden dirs and build output
		name := info.Name()
		if info.IsDir() {
			if name == ".git" || name == "build" || name == "target" || name == ".gradle" || name == ".idea" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}

		ext := filepath.Ext(name)
		switch ext {
		case ".java":
			stats.JavaFiles++
			return e.indexJavaFile(path, projectPath)
		case ".kt", ".kts":
			if ext == ".kts" && strings.HasSuffix(name, ".gradle.kts") {
				stats.BuildFiles++
				return e.indexBuildFile(path, projectPath)
			}
			stats.KotlinFiles++
			return e.indexKotlinFile(path, projectPath)
		case ".xml":
			if name == "pom.xml" {
				stats.BuildFiles++
				return e.indexBuildFile(path, projectPath)
			}
		case ".yml", ".yaml":
			if strings.Contains(name, "application") {
				stats.ConfigFiles++
				return e.indexConfigFile(path, projectPath)
			}
		case ".properties":
			if strings.Contains(name, "application") {
				stats.ConfigFiles++
			}
		}
		return nil
	})

	stats.Duration = time.Since(stats.StartTime)
	return stats, err
}

// IncrementalUpdate re-indexes only changed files since last commit.
func (e *Engine) IncrementalUpdate(ctx context.Context, projectPath string) (*IndexStats, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	stats := &IndexStats{StartTime: time.Now(), Incremental: true}

	// Get changed files via git
	changedFiles, err := getGitChangedFiles(projectPath)
	if err != nil {
		log.Printf("Git diff failed, falling back to full index: %v", err)
		e.mu.Unlock()
		return e.IndexProject(ctx, projectPath)
	}

	for _, cf := range changedFiles {
		if ctx.Err() != nil {
			break
		}

		fullPath := filepath.Join(projectPath, cf.Path)

		switch cf.Status {
		case "D": // Deleted
			e.removeFileEntries(cf.Path)
		case "A", "M": // Added or Modified
			ext := filepath.Ext(cf.Path)
			switch ext {
			case ".java":
				stats.JavaFiles++
				e.removeFileEntries(cf.Path)
				e.indexJavaFile(fullPath, projectPath)
			case ".kt":
				stats.KotlinFiles++
				e.removeFileEntries(cf.Path)
				e.indexKotlinFile(fullPath, projectPath)
			}
		}
	}

	stats.Duration = time.Since(stats.StartTime)
	return stats, nil
}

// Search performs a full-text search across the index.
func (e *Engine) Search(query string) ([]SearchResult, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	rows, err := e.db.Query(`
		SELECT name, package, file, kind, annotations
		FROM fts_index
		WHERE fts_index MATCH ?
		ORDER BY rank
		LIMIT 20`, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.Name, &r.Package, &r.File, &r.Kind, &r.Annotations); err != nil {
			continue
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// ListEndpoints returns all indexed REST endpoints.
func (e *Engine) ListEndpoints(filter string) ([]Endpoint, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	query := "SELECT method, path, handler, file, line FROM endpoints"
	var args []interface{}
	if filter != "" {
		query += " WHERE path LIKE ?"
		args = append(args, "%"+filter+"%")
	}
	query += " ORDER BY path"

	rows, err := e.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var endpoints []Endpoint
	for rows.Next() {
		var ep Endpoint
		if err := rows.Scan(&ep.Method, &ep.Path, &ep.Handler, &ep.File, &ep.Line); err != nil {
			continue
		}
		endpoints = append(endpoints, ep)
	}
	return endpoints, rows.Err()
}

// ListKafkaTopics returns all indexed Kafka topics.
func (e *Engine) ListKafkaTopics() ([]KafkaTopic, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	rows, err := e.db.Query("SELECT topic, group_id, type, handler, file, line FROM kafka_topics ORDER BY topic")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var topics []KafkaTopic
	for rows.Next() {
		var t KafkaTopic
		if err := rows.Scan(&t.Topic, &t.GroupID, &t.Type, &t.Handler, &t.File, &t.Line); err != nil {
			continue
		}
		topics = append(topics, t)
	}
	return topics, rows.Err()
}

// GetStats returns index statistics.
func (e *Engine) GetStats() (*IndexStatus, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	status := &IndexStatus{}
	e.db.QueryRow("SELECT COUNT(*) FROM files").Scan(&status.Files)
	e.db.QueryRow("SELECT COUNT(*) FROM classes").Scan(&status.Classes)
	e.db.QueryRow("SELECT COUNT(*) FROM methods").Scan(&status.Methods)
	e.db.QueryRow("SELECT COUNT(*) FROM endpoints").Scan(&status.Endpoints)
	e.db.QueryRow("SELECT COUNT(*) FROM kafka_topics").Scan(&status.KafkaTopics)
	e.db.QueryRow("SELECT COUNT(*) FROM spring_beans").Scan(&status.SpringBeans)
	e.db.QueryRow("SELECT COUNT(*) FROM entities").Scan(&status.Entities)
	e.db.QueryRow("SELECT MAX(indexed_at) FROM files").Scan(&status.LastUpdate)
	return status, nil
}

// Close releases resources.
func (e *Engine) Close() error {
	return e.db.Close()
}

// --- Internal parsing methods (simplified - real impl would use tree-sitter) ---

func (e *Engine) indexJavaFile(path, projectRoot string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	relPath, _ := filepath.Rel(projectRoot, path)
	module := detectModule(relPath)
	text := string(content)

	// Store file
	e.db.Exec("INSERT OR REPLACE INTO files (path, module, language, indexed_at) VALUES (?, ?, 'java', CURRENT_TIMESTAMP)",
		relPath, module)

	// Parse package
	pkg := extractPackage(text)

	// Parse class declarations
	classes := extractJavaClasses(text, relPath, module, pkg)
	for _, c := range classes {
		e.db.Exec(`INSERT INTO classes (name, package, file, module, kind, annotations, extends, implements)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			c.Name, c.Package, c.File, c.Module, c.Kind,
			strings.Join(c.Annotations, ","), c.Extends, strings.Join(c.Implements, ","))

		// FTS entry
		e.db.Exec(`INSERT INTO fts_index (name, package, file, kind, annotations, content)
			VALUES (?, ?, ?, ?, ?, ?)`,
			c.Name, c.Package, c.File, c.Kind, strings.Join(c.Annotations, " "), "")
	}

	// Parse Spring endpoints
	endpoints := extractEndpoints(text, relPath)
	for _, ep := range endpoints {
		e.db.Exec("INSERT INTO endpoints (method, path, handler, file, line) VALUES (?, ?, ?, ?, ?)",
			ep.Method, ep.Path, ep.Handler, ep.File, ep.Line)
	}

	// Parse Kafka listeners
	kafkaTopics := extractKafkaListeners(text, relPath)
	for _, kt := range kafkaTopics {
		e.db.Exec("INSERT INTO kafka_topics (topic, group_id, type, handler, file, line) VALUES (?, ?, ?, ?, ?, ?)",
			kt.Topic, kt.GroupID, kt.Type, kt.Handler, kt.File, kt.Line)
	}

	// Parse Spring beans
	beans := extractSpringBeans(text, relPath, module)
	for _, b := range beans {
		e.db.Exec("INSERT INTO spring_beans (name, type, class_name, file, module) VALUES (?, ?, ?, ?, ?)",
			b.Name, b.Type, b.ClassName, b.File, b.Module)
	}

	// Parse JPA entities
	entities := extractJPAEntities(text, relPath, module)
	for _, ent := range entities {
		e.db.Exec("INSERT INTO entities (name, table_name, file, module) VALUES (?, ?, ?, ?)",
			ent.Name, ent.TableName, ent.File, ent.Module)
	}

	return nil
}

func (e *Engine) indexKotlinFile(path, projectRoot string) error {
	// Similar to Java but with Kotlin-specific syntax
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	relPath, _ := filepath.Rel(projectRoot, path)
	module := detectModule(relPath)

	e.db.Exec("INSERT OR REPLACE INTO files (path, module, language, indexed_at) VALUES (?, ?, 'kotlin', CURRENT_TIMESTAMP)",
		relPath, module)

	// Reuse Java parsing with minor differences (annotations are the same)
	text := string(content)
	pkg := extractPackage(text)
	classes := extractJavaClasses(text, relPath, module, pkg) // works for Kotlin too
	for _, c := range classes {
		e.db.Exec(`INSERT INTO classes (name, package, file, module, kind, annotations) VALUES (?, ?, ?, ?, ?, ?)`,
			c.Name, c.Package, c.File, c.Module, c.Kind, strings.Join(c.Annotations, ","))
	}
	return nil
}

func (e *Engine) indexBuildFile(path, projectRoot string) error {
	relPath, _ := filepath.Rel(projectRoot, path)
	e.db.Exec("INSERT OR REPLACE INTO files (path, module, language, indexed_at) VALUES (?, ?, 'build', CURRENT_TIMESTAMP)",
		relPath, detectModule(relPath))
	return nil
}

func (e *Engine) indexConfigFile(path, projectRoot string) error {
	relPath, _ := filepath.Rel(projectRoot, path)
	e.db.Exec("INSERT OR REPLACE INTO files (path, module, language, indexed_at) VALUES (?, ?, 'config', CURRENT_TIMESTAMP)",
		relPath, detectModule(relPath))
	return nil
}

func (e *Engine) removeFileEntries(relPath string) {
	e.db.Exec("DELETE FROM classes WHERE file = ?", relPath)
	e.db.Exec("DELETE FROM methods WHERE file = ?", relPath)
	e.db.Exec("DELETE FROM endpoints WHERE file = ?", relPath)
	e.db.Exec("DELETE FROM kafka_topics WHERE file = ?", relPath)
	e.db.Exec("DELETE FROM spring_beans WHERE file = ?", relPath)
	e.db.Exec("DELETE FROM entities WHERE file = ?", relPath)
	e.db.Exec("DELETE FROM files WHERE path = ?", relPath)
}

// --- Types ---

type SearchResult struct {
	Name        string `json:"name"`
	Package     string `json:"package"`
	File        string `json:"file"`
	Kind        string `json:"kind"`
	Annotations string `json:"annotations"`
}

type IndexStats struct {
	StartTime   time.Time
	Duration    time.Duration
	JavaFiles   int
	KotlinFiles int
	BuildFiles  int
	ConfigFiles int
	BuildTool   string
	Incremental bool
}

type IndexStatus struct {
	Files       int        `json:"files"`
	Classes     int        `json:"classes"`
	Methods     int        `json:"methods"`
	Endpoints   int        `json:"endpoints"`
	KafkaTopics int        `json:"kafka_topics"`
	SpringBeans int        `json:"spring_beans"`
	Entities    int        `json:"entities"`
	LastUpdate  *time.Time `json:"last_update"`
}

type SpringBean struct {
	Name      string
	Type      string // service, repository, component, controller, configuration
	ClassName string
	File      string
	Module    string
}

type JPAEntity struct {
	Name      string
	TableName string
	File      string
	Module    string
}

type changedFile struct {
	Status string
	Path   string
}

// --- Helper functions ---

func detectBuildTool(projectPath string) string {
	if _, err := os.Stat(filepath.Join(projectPath, "build.gradle.kts")); err == nil {
		return "gradle"
	}
	if _, err := os.Stat(filepath.Join(projectPath, "build.gradle")); err == nil {
		return "gradle"
	}
	if _, err := os.Stat(filepath.Join(projectPath, "pom.xml")); err == nil {
		return "maven"
	}
	return "unknown"
}

func detectModule(relPath string) string {
	parts := strings.Split(filepath.ToSlash(relPath), "/")
	if len(parts) > 1 && parts[0] != "src" {
		return parts[0]
	}
	return ""
}

func extractPackage(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "package ") {
			pkg := strings.TrimPrefix(line, "package ")
			pkg = strings.TrimSuffix(pkg, ";")
			return strings.TrimSpace(pkg)
		}
	}
	return ""
}

func extractJavaClasses(text, file, module, pkg string) []IndexedClass {
	var classes []IndexedClass
	lines := strings.Split(text, "\n")
	var currentAnnotations []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Collect annotations
		if strings.HasPrefix(trimmed, "@") {
			ann := trimmed
			if idx := strings.IndexByte(ann, '('); idx >= 0 {
				ann = ann[:idx]
			}
			currentAnnotations = append(currentAnnotations, ann)
			continue
		}

		// Check for class/interface declaration
		kind := ""
		if strings.Contains(trimmed, "class ") {
			kind = "class"
		} else if strings.Contains(trimmed, "interface ") {
			kind = "interface"
		} else if strings.Contains(trimmed, "enum ") {
			kind = "enum"
		}

		if kind != "" && (strings.HasPrefix(trimmed, "public ") || strings.HasPrefix(trimmed, "abstract ") ||
			strings.HasPrefix(trimmed, "class ") || strings.HasPrefix(trimmed, "interface ") ||
			strings.HasPrefix(trimmed, "enum ") || strings.HasPrefix(trimmed, "sealed ") ||
			strings.HasPrefix(trimmed, "data ")) {
			name := extractClassName(trimmed, kind)
			if name != "" {
				classes = append(classes, IndexedClass{
					Name:        name,
					Package:     pkg,
					File:        file,
					Module:      module,
					Kind:        kind,
					Annotations: currentAnnotations,
				})
			}
			currentAnnotations = nil
		} else if !strings.HasPrefix(trimmed, "@") && trimmed != "" {
			currentAnnotations = nil
		}
	}
	return classes
}

func extractClassName(line, kind string) string {
	idx := strings.Index(line, kind+" ")
	if idx < 0 {
		return ""
	}
	rest := line[idx+len(kind)+1:]
	// Get first word (class name)
	for i, ch := range rest {
		if ch == ' ' || ch == '{' || ch == '<' || ch == '(' {
			return rest[:i]
		}
	}
	return strings.TrimSpace(rest)
}

func extractEndpoints(text, file string) []Endpoint {
	var endpoints []Endpoint
	lines := strings.Split(text, "\n")

	// Find class-level @RequestMapping
	classPath := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "@RequestMapping") {
			classPath = extractAnnotationValue(trimmed)
		}
	}

	mappings := map[string]string{
		"@GetMapping":    "GET",
		"@PostMapping":   "POST",
		"@PutMapping":    "PUT",
		"@DeleteMapping": "DELETE",
		"@PatchMapping":  "PATCH",
	}

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		for ann, method := range mappings {
			if strings.HasPrefix(trimmed, ann) {
				path := classPath + extractAnnotationValue(trimmed)
				handler := extractNextMethodName(lines, i+1)
				endpoints = append(endpoints, Endpoint{
					Method:  method,
					Path:    path,
					Handler: handler,
					File:    file,
					Line:    i + 1,
				})
			}
		}
	}
	return endpoints
}

func extractKafkaListeners(text, file string) []KafkaTopic {
	var topics []KafkaTopic
	lines := strings.Split(text, "\n")

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "@KafkaListener") {
			topic := extractNamedParam(trimmed, "topics")
			groupID := extractNamedParam(trimmed, "groupId")
			handler := extractNextMethodName(lines, i+1)
			if topic != "" {
				topics = append(topics, KafkaTopic{
					Topic:   topic,
					GroupID: groupID,
					Type:    "listener",
					Handler: handler,
					File:    file,
					Line:    i + 1,
				})
			}
		}
	}
	return topics
}

func extractSpringBeans(text, file, module string) []SpringBean {
	var beans []SpringBean
	lines := strings.Split(text, "\n")

	beanAnnotations := map[string]string{
		"@Service":       "service",
		"@Repository":    "repository",
		"@Component":     "component",
		"@RestController": "controller",
		"@Controller":    "controller",
		"@Configuration": "configuration",
	}

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		for ann, beanType := range beanAnnotations {
			if strings.HasPrefix(trimmed, ann) {
				className := findNextClassName(lines, i+1)
				if className != "" {
					beans = append(beans, SpringBean{
						Name:      strings.ToLower(className[:1]) + className[1:],
						Type:      beanType,
						ClassName: className,
						File:      file,
						Module:    module,
					})
				}
			}
		}
	}
	return beans
}

func extractJPAEntities(text, file, module string) []JPAEntity {
	var entities []JPAEntity
	lines := strings.Split(text, "\n")

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "@Entity") {
			tableName := ""
			// Check next line for @Table
			if i+1 < len(lines) && strings.Contains(lines[i+1], "@Table") {
				tableName = extractNamedParam(lines[i+1], "name")
			}
			className := findNextClassName(lines, i+1)
			if className != "" {
				entities = append(entities, JPAEntity{
					Name:      className,
					TableName: tableName,
					File:      file,
					Module:    module,
				})
			}
		}
	}
	return entities
}

func extractAnnotationValue(ann string) string {
	// @GetMapping("/api/users") or @GetMapping(value = "/api/users")
	start := strings.IndexByte(ann, '(')
	if start < 0 {
		return ""
	}
	end := strings.LastIndexByte(ann, ')')
	if end < 0 {
		return ""
	}
	value := ann[start+1 : end]
	value = strings.TrimPrefix(value, "value = ")
	value = strings.TrimPrefix(value, "value=")
	value = strings.Trim(value, `"'`)
	return value
}

func extractNamedParam(ann, param string) string {
	idx := strings.Index(ann, param)
	if idx < 0 {
		return ""
	}
	rest := ann[idx+len(param):]
	rest = strings.TrimLeft(rest, " =")
	rest = strings.TrimLeft(rest, " ")
	if strings.HasPrefix(rest, `"`) {
		end := strings.IndexByte(rest[1:], '"')
		if end >= 0 {
			return rest[1 : end+1]
		}
	}
	return ""
}

func extractNextMethodName(lines []string, startLine int) string {
	for i := startLine; i < len(lines) && i < startLine+5; i++ {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "@") {
			continue
		}
		// Look for method signature: ... methodName(
		if idx := strings.IndexByte(line, '('); idx > 0 {
			before := line[:idx]
			parts := strings.Fields(before)
			if len(parts) > 0 {
				return parts[len(parts)-1]
			}
		}
	}
	return ""
}

func findNextClassName(lines []string, startLine int) string {
	for i := startLine; i < len(lines) && i < startLine+5; i++ {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "@") {
			continue
		}
		for _, keyword := range []string{"class ", "interface ", "enum "} {
			if idx := strings.Index(line, keyword); idx >= 0 {
				return extractClassName(line, strings.TrimSpace(keyword))
			}
		}
	}
	return ""
}

func getGitChangedFiles(projectPath string) ([]changedFile, error) {
	cmd := exec.Command("git", "diff", "--name-status", "HEAD@{1}..HEAD")
	cmd.Dir = projectPath
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var files []changedFile
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			files = append(files, changedFile{
				Status: parts[0],
				Path:   parts[1],
			})
		}
	}
	return files, nil
}
