# GreenForge

Secure AI developer agent for JVM teams. Understands your Spring Boot project, watches pipelines, and helps from terminal and mobile.

## Highlights

- **JVM Intelligence** - Knows your modules, Spring beans, Kafka topics, DB schema
- **SSH Certificates** - Short-lived certs, zero static passwords, audit trail
- **Sandboxed Tools** - Every tool runs in an isolated Docker container
- **Remote Access** - CLI + mobile (QR provisioning) + WhatsApp/Telegram notifications
- **Proactive** - Morning digest, pipeline watcher, auto-fix

## Quick Start

### Docker (recommended)

```bash
# One-click install
git clone https://github.com/greencode/greenforge.git
cd greenforge
docker compose up -d

# Run setup wizard
docker exec -it greenforge greenforge init

# Start interactive session
docker exec -it greenforge greenforge run
```

### Windows

```powershell
.\scripts\install.ps1
```

### From source

```bash
# Requires Go 1.23+, CGO enabled (for SQLite)
make build
./bin/greenforge init
./bin/greenforge run
```

## Architecture

```
CLIENTS (CLI/TUI, PWA, Telegram, WhatsApp, Email)
        │ SSH cert auth
  GATEWAY (Go, gRPC/WS)
  ├── Cert Validator
  ├── Session Manager
  ├── RBAC Engine
  └── Audit Logger
        │
  ┌─────┼─────────┐
  │     │         │
SSH CA  AGENT    TOOL EXECUTION
        RUNTIME  ENGINE (Docker)
        │
  MODEL PROVIDER LAYER
  (Claude, GPT-4o, Ollama)

  CODEBASE INDEX ENGINE (zero-LLM, local)
  ├── Tree-sitter AST Parser
  ├── Build Graph Analyzer
  ├── Spring/Kafka Annotation Parser
  └── SQLite FTS5 + vector storage

  NOTIFICATION ENGINE
  (WhatsApp, Telegram, Email, SMS, CLI)
```

## Daily Usage

### Interactive Session
```bash
greenforge run --project cba-backend

> where is the VCF event processed?
> add retry logic to VcfEventListener
> list all kafka topics
> show endpoints for UserController
```

### Codebase Queries
```bash
greenforge query "list all kafka topics"
greenforge query "show endpoints"
greenforge query "where is VCF event processed?"
```

### Session Management (tmux-style)
```bash
greenforge session new --project cba-backend
greenforge session list
greenforge session attach s1
greenforge session detach
```

### Device Management
```bash
greenforge auth device add --name "iPhone"   # QR code for mobile
greenforge auth devices                      # List devices
greenforge auth device revoke "iPhone"       # Revoke access
```

### Morning Digest
```bash
greenforge digest
# Shows: pipeline status, PRs, commits, work items
```

## Configuration

Main config: `~/.greenforge/greenforge.toml`

```bash
greenforge config show    # Show current config
greenforge config edit    # Open in editor
```

See [configs/greenforge.toml](configs/greenforge.toml) for full reference.

### AI Model Policy

Control which AI providers are used per project:
```toml
[[ai.policies]]
project_pattern = "/c/GC/*"        # Company projects
allowed_providers = ["ollama"]      # Local AI only
```

### Auto-Fix Policy

Per-repo, per-branch pipeline failure handling:
```yaml
policies:
  - repo: "cba-backend"
    rules:
      - branch: "master"
        on_failure: notify_only
      - branch: "feature/*"
        on_failure: fix_and_merge
```

## Built-in Tools

| Tool | Category | Description |
|------|----------|-------------|
| `git` | VCS | Git operations (status, diff, commit, log, blame) |
| `shell` | System | Sandboxed command execution |
| `file` | Filesystem | Read, write, search (ripgrep), tree |
| `code_review` | Analysis | Review code for quality and JVM best practices |
| `build` | Build | Gradle/Maven build, test, dependencies |
| `spring_analyzer` | Analysis | Spring endpoints, beans, config, DI graph |
| `kafka_mapper` | Analysis | Kafka topic map, event trace, listeners |
| `database` | Database | SQL query, schema, migrations |
| `azure_devops` | CI/CD | Pipelines, PRs, work items |
| `logs` | Observability | Log search, tail, analysis |

Each tool runs in an isolated Docker container with:
- Network isolation (no network / restricted hosts only)
- Filesystem isolation (mounted workspace, read-only where possible)
- Resource limits (CPU, memory, timeout)
- Secret injection via environment variables (from OS keychain)

## Security Model

### 4-Layer Secret Protection

1. **Secret Isolation** - Secrets in OS keychain, never in config files
2. **AI Model Firewall** - Regex scrubbing before sending to LLM
3. **SSH Cert Scoped Access** - Certificates define which secrets are accessible
4. **Audit Trail** - Every secret access logged with hash chain integrity

### SSH Certificates

- Ed25519 certificates with configurable lifetime (8h default)
- Auto-renewal when 20% lifetime remaining
- Key Revocation List (KRL) for instant revocation
- Device certificates with restricted permissions
- QR code provisioning for mobile devices

## Project Structure

```
greenforge/
├── cmd/greenforge/          # CLI entry point
├── internal/
│   ├── ca/                  # SSH Certificate Authority
│   ├── gateway/             # WebSocket/gRPC server
│   ├── rbac/                # Role-based access control
│   ├── agent/               # Agent runtime (plan-execute loop)
│   ├── model/               # AI model router + firewall
│   │   └── providers/       # Ollama, Anthropic, OpenAI
│   ├── sandbox/             # Docker sandbox engine
│   ├── tools/               # Tool registry + executor
│   ├── index/               # Codebase index engine
│   ├── notify/              # Notification engine
│   ├── digest/              # Morning digest
│   ├── autofix/             # Auto-fix engine
│   ├── audit/               # Tamper-evident audit logger
│   └── config/              # Configuration management
├── tools/                   # Tool YAML manifests
├── configs/                 # Default config files
├── scripts/                 # Install scripts
├── Dockerfile               # Multi-stage build
└── docker-compose.yml       # One-click setup
```

## License

MIT
