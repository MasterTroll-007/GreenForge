# GreenForge - Progress Tracker

> Poslední aktualizace: 2026-02-27

---

## Celkový Stav

| Fáze | Stav | Pokrytí |
|------|------|---------|
| Phase 1: Core + Auth + Basic Tools | **~90%** | Většina funguje, chybí init wizard TUI |
| Phase 2: JVM Intelligence + CI/CD | **~75%** | Index engine + daemon hotový, CI/CD klienti hotové, chybí tree-sitter + tool images |
| Phase 3: Automation + Remote + Notifications | **~65%** | Pipeline watcher, digest, autofix, notif providers implementovány |
| Phase 4: Knowledge Base + Polish | **~5%** | Jen skills existují |

**Celkový odhad: ~75% plánu implementováno**

---

## Phase 1: Core + Auth + Basic Tools

### HOTOVO
- [x] Go scaffold + Cobra CLI — `cmd/greenforge/main.go` (11 příkazů)
- [x] SSH CA (Ed25519 sign/verify/revoke) — `internal/ca/authority.go` (349 LOC), `store.go`
- [x] Gateway WS server — `internal/gateway/server.go` (575 LOC)
- [x] Session manager (create/attach/detach/close) — `internal/gateway/server.go`
- [x] Web UI server — `internal/gateway/webui.go` (399 LOC)
- [x] Audit logger (SHA-256 hash chain) — `internal/audit/logger.go` (257 LOC)
- [x] TOML config (všechny sekce) — `internal/config/config.go` (372 LOC)
- [x] Agent runtime (plan→execute→observe loop) — `internal/agent/runtime.go` (251 LOC)
- [x] Agent memory — `internal/agent/memory.go`
- [x] Docker sandbox (container lifecycle, limits) — `internal/sandbox/docker.go` (253 LOC)
- [x] Secret injection do sandboxu — `internal/sandbox/secrets.go`
- [x] Model router (multi-provider, policy) — `internal/model/router.go` (283 LOC)
- [x] Anthropic provider (OAuth + API key) — `internal/model/anthropic.go` (621 LOC)
- [x] OpenAI provider — `internal/model/openai.go` (252 LOC)
- [x] Ollama provider — `internal/model/ollama.go` (275 LOC)
- [x] AI firewall (secret scrubbing) — `internal/model/firewall.go` (128 LOC)
- [x] RBAC engine (admin/dev/viewer) — `internal/rbac/engine.go`
- [x] Tool registry (YAML manifest loading) — `internal/tools/registry.go` (295 LOC)
- [x] Tool manifesty: git, shell, file, code_review — `tools/*/TOOL.yaml`
- [x] Dockerfile (multi-stage build) — `Dockerfile`
- [x] docker-compose.yml (one-click) — `docker-compose.yml`
- [x] Web UI (SPA) — `cmd/greenforge/web/index.html`
- [x] Claude proxy (OAuth, browse, workspace) — `claude-proxy.mjs`

### CHYBÍ
- [ ] **Init wizard TUI (bubbletea)** — 7-krokový interaktivní wizard. Cobra command existuje, ale bez TUI.
- [ ] **Cert validation middleware** — Gateway nemá plný cert auth middleware.
- [ ] **OS Keychain (Windows Credential Manager)** — Secrets jsou v env vars, ne v keychain.
- [ ] **gRPC (grpc-gateway)** — Pouze REST/WebSocket. gRPC není implementováno.

---

## Phase 2: JVM Intelligence + CI/CD

### HOTOVO
- [x] Codebase Index Engine (SQLite FTS5) — `internal/index/engine.go` (1042 LOC)
- [x] Java/Kotlin class/method indexing (regex-based)
- [x] Spring endpoint parsing (@RestController, @GetMapping, atd.)
- [x] Spring bean registry (@Service, @Repository, @Component, atd.)
- [x] Kafka topic/listener parsing (@KafkaListener)
- [x] JPA entity mapping (@Entity, @Table)
- [x] Build file parsing (Gradle/Maven)
- [x] Incremental git-diff based updates
- [x] Context summary generation pro AI (GetContextSummary)
- [x] Tool manifesty: build, spring_analyzer, kafka_mapper, database, logs, azure_devops, gitlab_ci — 15 TOOL.yaml
- [x] Multi-model support (Anthropic, OpenAI, Ollama)
- [x] RBAC engine

### CHYBÍ
- [ ] **Tree-sitter AST parsing** — Plán říká tree-sitter Go bindings. Aktuálně regex-based.
- [ ] **sqlite-vec (vector embeddings)** — FTS5 je, vectors ne.
- [ ] **Module dependency graph** — Základní build parsing je, ne plný graph.
- [ ] **Spring DI graph** — Manifest definuje funkci, implementace chybí.
- [ ] **Kafka flow tracing (trace_event)** — Manifest definuje, Go kód chybí.
- [ ] **JPA relations (@ManyToOne, @OneToMany)** — Neparsovány.
- [ ] **Liquibase/Flyway migration tracking** — Naimplementováno.
- [ ] **Tool Docker images/scripts** — Manifesty existují, ale Docker images pro sandbox execution chybí.
- [x] **Background index daemon (file watcher)** — `internal/index/daemon.go` — Git-based change detection, auto reindex

---

## Phase 2.5: CI/CD Klient Vrstva (NOVÉ)

### HOTOVO
- [x] CI/CD common interface — `internal/cicd/client.go` — Pipeline, PullRequest, CreatePR
- [x] Azure DevOps REST API klient — `internal/cicd/azuredevops.go` — Builds, timeline, PRs, CreatePR
- [x] GitLab REST API klient — `internal/cicd/gitlab.go` — Pipelines, MRs, CreateMR

---

## Phase 3: Automation + Remote + Notifications

### HOTOVO
- [x] Notification engine (dispatcher) — `internal/notify/engine.go` (190 LOC)
- [x] Telegram provider (plná impl.) — `internal/notify/telegram.go` (115 LOC)
- [x] Email provider (SMTP + STARTTLS + TLS) — `internal/notify/email.go` — MIME multipart, HTML formát
- [x] WhatsApp provider (Cloud API + webhook) — `internal/notify/providers.go` — Meta Graph API + custom Baileys webhook
- [x] SMS provider (Twilio) — `internal/notify/providers.go` — Twilio REST API
- [x] CLI provider — `internal/notify/providers.go`
- [x] Digest collector (CI/CD + git log) — `internal/digest/collector.go` — Real data z CI/CD klientů + git log
- [x] Digest formatter (text + HTML) — `internal/digest/collector.go` — Format() + FormatHTML()
- [x] Digest scheduler (cron) — `internal/digest/scheduler.go` — Automatic/on-demand/both modes
- [x] Pipeline Watcher (polling + dedup) — `internal/autofix/watcher.go` — Multi-client polling, failure dedup, policy resolution
- [x] Auto-fix analyzer — `internal/autofix/analyzer.go` — Pattern matching: test, compile, dependency, config, OOM, timeout
- [x] Auto-fix fixer (PR creation) — `internal/autofix/fixer.go` — Analysis → fix branch → PR creation
- [x] Web UI Settings rozšíření — Notifications, Auto-fix, CI/CD, Index, Watcher panels

### CHYBÍ
- [ ] **Tailscale/WireGuard integrace** — Neimplementováno
- [ ] **QR code device provisioning** — Chybí
- [ ] **Web Push (PWA service worker)** — Chybí

---

## Phase 4: Knowledge Base + Polish

### HOTOVO
- [x] Skills (SKILL.md) — 4 definice (spring-boot-debug, kafka-event-trace, jvm-code-review, migration-helper)

### CHYBÍ
- [ ] Persistent knowledge base
- [ ] Docker/K8s tool implementace
- [ ] Onboarding mode
- [ ] Team-sharable skills/configs
- [ ] VS Code extension

---

## Web UI Stav

| Sekce | Stav |
|-------|------|
| Chat (streaming, WebSocket, tool viz) | HOTOVO |
| Session management (project picker) | HOTOVO |
| Settings - Workspace paths | HOTOVO |
| Settings - AI Models (family filtering, persistence) | HOTOVO |
| Dashboard (pipeline/index overview) | ČÁSTEČNĚ |
| Audit Log (filtrovatelný) | ČÁSTEČNĚ |
| Settings - Security (rozšířené) | HOTOVO |
| Settings - Auto-fix Policy | HOTOVO |
| Settings - Notifications | HOTOVO |
| Settings - CI/CD Integrations | HOTOVO |
| Settings - Pipeline Watcher status | HOTOVO |
| Settings - Codebase Index (stats + reindex) | HOTOVO |
| Settings - Digest trigger | HOTOVO |
| Settings - RBAC | CHYBÍ |
| Settings - Tools | CHYBÍ |
| Devices (cert status, QR, revoke) | CHYBÍ |

---

## Dokumentace Stav

| Dokument | Stav |
|----------|------|
| README.md | EXISTUJE (základní) |
| GREENFORGE_PLAN.md | EXISTUJE (kompletní) |
| docs/getting-started.md | CHYBÍ |
| docs/architecture.md | CHYBÍ |
| docs/auth-flow.md | CHYBÍ |
| docs/tool-development.md | CHYBÍ |
| docs/security-model.md | CHYBÍ |
| docs/configuration-reference.md | CHYBÍ |
| docs/api-reference.md | CHYBÍ |

---

## Soubory a LOC

| Komponenta | Soubory | ~LOC |
|-----------|---------|------|
| CLI + main | 1 | 400 |
| SSH CA | 2 | 500 |
| Gateway | 2 | 975 |
| RBAC | 1 | 150 |
| Agent | 2 | 350 |
| Model (router + providers + firewall) | 5 | 1560 |
| Index Engine + Daemon | 2 | 1250 |
| Sandbox | 2 | 350 |
| Tools Registry | 1 | 295 |
| Audit | 1 | 257 |
| Config | 1 | 372 |
| CI/CD Klienti (AzDO + GitLab) | 3 | 650 |
| Notify (engine + 4 providers) | 4 | 700 |
| Digest (collector + scheduler) | 2 | 350 |
| AutoFix (watcher + analyzer + fixer) | 3 | 500 |
| **Celkem Go** | **~32** | **~8,650** |
| Web UI (HTML/CSS/JS) | 1 | ~2,400 |
| Claude Proxy (Node.js) | 1 | ~350 |
| Tool manifesty (YAML) | 15 | ~1,500 |
| Skills (Markdown) | 4 | ~400 |

---

## TOP Priorit (co implementovat další)

| # | Co | Důvod | Effort |
|---|-----|-------|--------|
| 1 | Tool Docker images/scripts | Bez nich tools nefungují v sandboxu | VELKÝ |
| 2 | ~~Pipeline Watcher (CI/CD polling)~~ | ~~Core use-case~~ | HOTOVO |
| 3 | ~~Morning Digest implementace~~ | ~~Core use-case~~ | HOTOVO |
| 4 | ~~Auto-fix engine~~ | ~~Core use-case~~ | HOTOVO |
| 5 | ~~Notification providers (WhatsApp, Email, SMS)~~ | ~~Pro digest + alerts~~ | HOTOVO |
| 6 | ~~Background index daemon~~ | ~~Auto-update indexu~~ | HOTOVO |
| 7 | Init wizard TUI (bubbletea) | Lepší onboarding | STŘEDNÍ |
| 8 | Tree-sitter místo regex | Přesnější parsing | VELKÝ |
| 9 | ~~Web UI Settings rozšíření~~ | ~~Security, notifications, auto-fix~~ | HOTOVO |
| 10 | Go backend API endpointy pro nové UI panely | Config, digest, index, watcher API | MALÝ |
| 11 | Dokumentace (docs/) | Pro další uživatele | MALÝ |
| 12 | Web UI - RBAC + Tools settings | Zbývající settings panely | STŘEDNÍ |
