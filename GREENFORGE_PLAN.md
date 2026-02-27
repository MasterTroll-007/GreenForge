# GreenForge - Secure AI Developer Agent pro JVM Týmy

## Context & Vize

**GreenForge** je open-source AI agent pro Java/Kotlin vývojáře, vytvořený pro JVM kompetenci v GreenCode s potenciálem rozšíření na celou firmu.

**Problém:** Vývojáři v enterprise JVM prostředí (Spring Boot, Kafka, multi-module Gradle/Maven, Azure DevOps) tráví hodně času:
- Context switchingem mezi 20+ modulovými projekty
- Onboardingem do cizích repo (kde co je, jaká architektura)
- Repetitivními tasky (boilerplate, CRUD, migrace)
- Debuggingem produkčních issues (korelace logů, tracing)
- Manuálním monitoringem pipeline a PR

**Řešení:** AI agent, který:
- **Rozumí celému JVM projektu** - indexuje codebase, zná moduly, Spring beany, Kafka topiky, DB schéma
- **Je dostupný odkudkoliv** - CLI doma, z mobilu přes SSH/Tailscale, notifikace přes WhatsApp/Telegram/email
- **Je bezpečný** - SSH certifikáty, sandbox izolace, secrets nikdy neopustí stroj
- **Proaktivně pomáhá** - morning digest, auto-fix failing tests, pipeline watcher

**Inspirace:** [Teleport](https://goteleport.com) (SSH certifikáty), [Claude Code](https://claude.ai/claude-code) (agent loop)

---

## Unikátní Přidaná Hodnota (co neexistuje)

### 1. JVM Project Intelligence
Žádný AI agent dnes hluboce nerozumí multi-module JVM projektům. GreenForge:
- Parsuje `build.gradle.kts` / `pom.xml` → **dependency graph** mezi moduly
- Indexuje všechny třídy/interfaces via tree-sitter → **okamžité vyhledávání**
- Parsuje Spring anotace → **bean registry, endpoint map, config properties**
- Parsuje Kafka listener anotace → **topic/consumer/producer flow map**
- Parsuje JPA/Hibernate entity → **DB schema model**
- Vše v SQLite → **okamžité queries, persistentní mezi sessions**

### 2. Eficientní Inkrementální Index
Index se nebuduje celý znovu po každém pushi:
- **Git-diff based:** Po pull/push agent zjistí `git diff --name-only` → reindexuje jen změněné soubory
- **AST-level diffing:** Porovnává AST stromu (ne text) → ví že "přejmenování metody" != "nová metoda"
- **Zero-LLM indexing:** Index se buduje lokálně bez volání AI → žádné tokeny spotřebovány
- **Background update:** Daemon sleduje git hooks, indexuje na pozadí

### 3. Proaktivní Automation
- **Morning digest:** Souhrn co se stalo (commity, PRs, pipeline status, work items, logy)
  - **Konfigurovatelný trigger:** automaticky (cron, např. 7:30) NEBO on-demand (`greenforge digest` / `/digest` command / button v UI)
- **Pipeline watcher:** Sleduje Azure DevOps/GitLab, při failure analyzuje → notifikace + optional auto-fix PR
- **Auto-fix policy:** Konfigurovatelné **per-repo + per-branch** (pattern matching: `feature/*`, `master`, `release/*`):
  - `notify_only` - jen upozornění
  - `fix_and_pr` - vytvoří fix branch + PR k review
  - `fix_and_merge` - fix + auto-merge (pokud testy projdou)
  - `max_auto_fixes` limit per branch, `escalate_after` timeout

### 4. Multi-Channel Notifikace
Konfigurovatelný notification backend:
- WhatsApp (via WhatsApp Business API / Baileys)
- Telegram (via Bot API)
- Email (SMTP)
- SMS (via Twilio/custom)
- Web push (PWA)
- CLI toast (local)

### 5. Multi-Session Management (tmux-style)
GreenForge podporuje **více současných AI sessions** - lokálně i remote:
- **Více sessions najednou:** Každá session má vlastní kontext/projekt/historii
- **Persistent sessions:** Session běží na serveru, přežije odpojení klienta
- **Attach/detach:** Jako tmux - odpojíš se z CLI, připojíš se z mobilu ke stejné session
- **Remote sessions:** Z mobilu se připojíš k běžící session na PC

```bash
greenforge session new --project cba-backend     # Nová session
greenforge session new --project mhub            # Další session paralelně
greenforge session list                          # Seznam aktivních sessions
  ID    PROJECT       STATUS    STARTED       DEVICE
  s1    cba-backend   active    10:23         laptop-cli
  s2    mhub          idle      09:15         (detached)
  s3    -             active    11:02         iphone-web

greenforge session attach s2                     # Připojit se k session
greenforge session detach                        # Odpojit (session běží dál)
greenforge session close s1                      # Ukončit session
```

**Z mobilu/web UI:**
- Dashboard zobrazuje všechny aktivní sessions
- Klik → attach k existující session (plný context zachován)
- Nebo vytvořit novou session pro jiný projekt
- Session z CLI lze přepnout na web a zpět

**Session isolation:**
- Každá session má vlastní conversation history
- Každá session může mít jiný AI model
- Cert-based: session je vázaná na cert identity (kdo ji vytvořil)
- Timeout: idle sessions se po X hodinách automaticky ukončí

### 6. Secure Remote Access + Auto-Cert Lifecycle
- Tailscale/WireGuard pro síťový přístup
- SSH certifikáty s **konfigurovatelným lifetime** (8h / 1d / 7d / 30d)
- **Auto-renewal daemon:** obnoví cert automaticky když zbývá 20% lifetime
- **Mobile provisioning via QR kód:**
  ```
  greenforge auth device add --name "iPhone"
  → Vygeneruje device-specific cert (s omezenými permissions)
  → Zobrazí QR kód v terminálu
  → QR obsahuje: cert + CA pubkey + Tailscale endpoint + SSH config
  → Na mobilu: scan → SSH klient se automaticky konfiguruje
  ```
- **Device management:**
  ```
  greenforge auth devices              # seznam zařízení + cert status + expiry
  greenforge auth device revoke "iPhone"  # okamžitá revokace přes KRL
  greenforge auth device renew "iPhone"   # manuální obnova
  ```
- Každý dev má svou instanci → plná izolace
- Secrets zůstávají na stroji → nikdy se neposílají do AI API

---

## Architektura

```
                    ┌─────────────────────────────┐
                    │   KLIENTI (multi-channel)    │
                    │  CLI/TUI ─ PWA ─ Telegram ─  │
                    │  WhatsApp ─ Email ─ SMS      │
                    └──────────────┬───────────────┘
                                   │ SSH cert auth
                    ┌──────────────▼───────────────┐
                    │     GATEWAY (Go, gRPC/WS)    │
                    │  ┌─────────┐ ┌────────────┐  │
                    │  │Cert     │ │Session Mgr │  │
                    │  │Validator│ │(lane queue) │  │
                    │  └─────────┘ └────────────┘  │
                    │  ┌─────────┐ ┌────────────┐  │
                    │  │RBAC     │ │Audit Logger│  │
                    │  │Engine   │ │(hash chain)│  │
                    │  └─────────┘ └────────────┘  │
                    └───┬──────────┬───────────┬───┘
                        │          │           │
          ┌─────────────▼──┐ ┌─────▼────┐ ┌───▼──────────────┐
          │  SSH CA (Go)   │ │  AGENT   │ │ TOOL EXECUTION   │
          │                │ │ RUNTIME  │ │ ENGINE           │
          │ User CA        │ │ (Go)     │ │ (Docker sandbox) │
          │ Host CA        │ │          │ │                  │
          │ KRL/Revocation │ │ Context  │ │ Network isolace  │
          │ Cert Store     │ │ Assembler│ │ FS isolation     │
          └────────────────┘ │ Model    │ │ Resource limits  │
                             │ Router   │ │ Secret injection │
                             │ Agent    │ └──────┬───────────┘
                             │ Loop     │        │
                             │ Memory   │   ┌────▼─────────────────────────┐
                             └────┬─────┘   │  TOOL REGISTRY               │
                                  │         │  ┌────────────────────────┐   │
                        ┌─────────▼──────┐  │  │ JVM-Specific:         │   │
                        │ MODEL PROVIDER │  │  │  gradle/maven_build   │   │
                        │ LAYER          │  │  │  spring_analyzer      │   │
                        │ ┌──────────┐   │  │  │  kafka_mapper         │   │
                        │ │Claude    │   │  │  │  db_migrations        │   │
                        │ │(cloud)   │   │  │  │  jvm_profiler         │   │
                        │ ├──────────┤   │  │  ├────────────────────────┤   │
                        │ │GPT-4o   │   │  │  │ General:              │   │
                        │ │(cloud)   │   │  │  │  git, shell, file     │   │
                        │ ├──────────┤   │  │  │  code_review          │   │
                        │ │Ollama   │   │  │  │  docker, logs          │   │
                        │ │(local)   │   │  │  │  azure_devops         │   │
                        │ └──────────┘   │  │  │  gitlab_ci            │   │
                        └────────────────┘  │  └────────────────────────┘   │
                                            └──────────────────────────────┘
          ┌─────────────────────────────────────────────────────┐
          │  CODEBASE INDEX ENGINE (zero-LLM, local-only)      │
          │  ┌──────────┐ ┌──────────┐ ┌───────────────────┐   │
          │  │Tree-sitter│ │Build     │ │Spring/Kafka       │   │
          │  │AST Parser │ │Graph     │ │Annotation Parser  │   │
          │  │(Java/Kt)  │ │Analyzer  │ │(custom)           │   │
          │  └──────────┘ └──────────┘ └───────────────────┘   │
          │  ┌──────────────────────────────────────────────┐   │
          │  │ SQLite: FTS5 (text) + sqlite-vec (embeddings)│   │
          │  └──────────────────────────────────────────────┘   │
          │  Git-diff incremental updates │ Background daemon   │
          └─────────────────────────────────────────────────────┘

          ┌─────────────────────────────────────────────────────┐
          │  NOTIFICATION ENGINE                                │
          │  WhatsApp │ Telegram │ Email │ SMS │ Web Push │ CLI │
          │  (konfigurovatelné per-user, per-event)             │
          └─────────────────────────────────────────────────────┘
```

## Tech Stack

| Komponenta | Technologie | Důvod |
|-----------|------------|-------|
| **Core/Gateway/CA** | **Go 1.23+** | SSH crypto nativně, single binary, Docker SDK |
| CLI | cobra + bubbletea | Industry standard (kubectl, gh), rich TUI |
| SSH Certs | `golang.org/x/crypto/ssh` + `go.step.sm/crypto` | Teleport/step-ca reference |
| Sandbox | Docker Engine API (Go) | Per-tool izolace |
| Codebase Index | tree-sitter (Go bindings) + SQLite FTS5 + sqlite-vec | Lokální, rychlé, zero-LLM |
| AI modely | Go HTTP streaming | Multi-provider (Claude, GPT-4o, Ollama) |
| Config | TOML + YAML | Config + tool manifests |
| DB | SQLite (pure Go `modernc.org/sqlite`) | Zero deps, local-first |
| API | gRPC + REST (grpc-gateway) | Type-safe, streaming |
| Notifikace | Go + provider SDKs | WhatsApp/Telegram/Email/SMS pluggable |

## Security Model

### Secrets Protection (hlavní concern)
```
Problem: Agent má přístup k DB credentials, API keys, Azure tokens.
         Jak zajistit že je neprozradí?

Řešení (4 vrstvy):

1. SECRET ISOLATION
   ├── Secrets v OS keychain (Windows Credential Manager)
   ├── Agent core NIKDY nevidí secret values
   ├── Secrets injektovány POUZE do Docker sandboxu jako env vars
   └── Po tool execution kontejner zničen → secrets zmizí

2. AI MODEL FIREWALL
   ├── Před odesláním kontextu do LLM: secret scrubbing
   │   (regex + known patterns: JDBC URLs, API keys, tokens)
   ├── Tool výsledky sanitizovány před feedbackem do LLM
   ├── Konfigurovatelné per-projekt: cloud AI / only local
   └── Audit log: co přesně bylo posláno do kterého modelu

3. SSH CERT SCOPED ACCESS
   ├── Certifikát definuje KTERÉ secrets smí user/role vidět
   ├── Extensions: devagent-secrets@greenforge.dev: "db-dev,azure-dev"
   ├── Admin vidí vše, developer jen dev secrets, viewer nic
   └── Per-projekt granularita

4. AUDIT TRAIL
   ├── Každý secret access logován (kdo, kdy, který secret, jaký tool)
   ├── Hash chain → tamper-evident
   └── Alerting: neobvyklý secret access → notifikace admin
```

### Auto-fix Policy (per-repo + per-branch)
```yaml
# configs/autofix-policy.yaml
policies:
  - repo: "cba-backend"
    rules:
      - branch: "master"
        on_failure: notify_only
        notify: [whatsapp, email]
      - branch: "develop"
        on_failure: fix_and_pr
        pr_assignee: auto           # Autor posledního commitu
        require_review: true
      - branch: "feature/*"
        on_failure: fix_and_merge   # Agresivní na feature branches
        require_tests_pass: true
        max_auto_fixes: 3           # Max 3, pak jen notify
      - branch: "release/*"
        on_failure: notify_only
        escalate_after: "30m"       # Eskalace pokud nikdo nereaguje

  - repo: "*"                       # Default pro všechny
    rules:
      - branch: "*"
        on_failure: notify_only     # Safe default
```

### SSH Certificate Lifecycle Config
```toml
# V greenforge.toml
[ca]
cert_lifetime = "8h"              # Konfigurovatelné: 8h / 1d / 7d / 30d
auto_renew_threshold = "20%"      # Obnoví cert když zbývá 20% lifetime
algo = "ed25519"

[ca.device_certs]
default_lifetime = "30d"          # Device (mobil) certs mají delší lifetime
max_devices_per_user = 5
permissions_mode = "restricted"   # Device certs mají omezená oprávnění
allowed_tools = ["git:read", "logs:read", "audit:read", "notify:send"]
```

### Hybrid AI Model Policy
```yaml
# configs/model-policy.yaml
policies:
  - project_pattern: "*/GC/*"      # Firemní projekty
    allowed_providers: [ollama]      # Pouze lokální model
    reason: "Company code cannot leave network"

  - project_pattern: "*/personal/*" # Osobní projekty
    allowed_providers: [anthropic, openai, ollama]
    reason: "Personal code OK for cloud AI"

  - project_pattern: "*"            # Default
    allowed_providers: [ollama]      # Safe default
```

## Codebase Index Engine (Detailní Návrh)

### Indexované entity
```
Per-file:
  - Třídy, interfaces, enums, sealed classes, data classes
  - Metody (name, params, return type, annotations)
  - Fields/properties
  - Import statements

Per-module (Gradle/Maven):
  - Module dependency graph
  - Plugin konfigurace
  - Build tasks

Spring-specific:
  - @RestController endpoints (method + path + params)
  - @Service, @Component, @Repository beany
  - @Configuration + @Bean definice
  - application.yml/properties values
  - @Profile bindings

Kafka-specific:
  - @KafkaListener topics + groups
  - KafkaTemplate producers (topic + message type)
  - Event flow: producer → topic → consumer

JPA/Hibernate:
  - @Entity → tabulka mapping
  - Relations (@ManyToOne, @OneToMany, ...)
  - @Query custom queries
  - Repository interfaces + derived query methods

Liquibase/Flyway:
  - Migration history (version, description, checksum)
  - Schema change timeline
```

### Inkrementální Update Strategie
```
1. TRIGGER: git post-merge hook, post-checkout hook, nebo manual
2. DIFF:   git diff --name-status HEAD@{1}..HEAD
3. FILTER: Pouze *.java, *.kt, *.gradle.kts, *.xml, *.yml, *.properties
4. PARSE:  Tree-sitter AST pro změněné soubory
5. UPDATE: Upsert do SQLite (DELETE old entries + INSERT new)
6. EMBED:  Batch vector embedding pro nové/změněné entity (lokální model)
7. COST:   ~0 LLM tokens, ~2-5 sec pro typický commit (10-20 souborů)

Full reindex: ~30-60 sec pro 500-file projekt, ~3-5 min pro 1700-file projekt
Incremental: ~2-5 sec (jen diff)
```

### Query Interface
```
greenforge query "kde se zpracovává VCF event?"
  → Codebase index: finds vcf-listener module, VcfEventListener.java
  → Shows: class, method, Kafka topic, downstream calls
  → Zero LLM calls for factual queries (pure index lookup)

greenforge query "jaké endpointy má UserController?"
  → Index: @RestController endpoints, methods, params
  → Instant response from SQLite FTS5

greenforge query "vysvětli architekturu mhub projektu"
  → Combines: module graph + Spring beans + Kafka flows
  → THEN sends structured context to LLM for natural language explanation
  → Efficient: only relevant index data sent, not whole codebase
```

## Denní UX Workflow

### Ráno: Digest (konfigurovatelný trigger)
```
Trigger: automaticky (cron) NEBO on-demand:
  CLI:  greenforge digest
  Chat: /digest
  UI:   [📊 Morning Digest] button na dashboardu

📱 WhatsApp/Telegram/Email (konfigurovatelné):

🟢 GreenForge Morning Digest - cba-backend
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📊 Pipeline: ✅ master green, ⚠️ feature/vum-update RED
   └ Test failure: VumClientTest.shouldReturnUserProfile - NPE at line 42
   └ Agent suggestion: Missing null check on userProfile.getAddress()

🔀 PRs: 2 active
   └ #187 "Add GDPR data cleanup job" (Petr) - waiting your review
   └ #192 "Update Spring Boot to 3.4.13" (bot) - approved, ready to merge

📋 Work Items: 1 new assigned
   └ TASK-4521 "Implement VCF event retry logic"

📝 Yesterday: 5 commits by 3 authors
   └ Petr: 2 commits (GDPR cleanup)
   └ You: 2 commits (VUM client refactor)
   └ CI bot: 1 commit (dependency update)

Reply: "fix pipeline" | "review PR 187" | "more details"
```

### V práci: CLI Session
```bash
$ greenforge run --project /c/GC/cba-backend

🟢 GreenForge v0.1.0 │ Project: cba-backend │ Model: ollama/codestral
   Index: 533 files │ 62 services │ 21 modules │ Last update: 2 min ago
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

> kde se zpracovává VCF event?

📦 Module: cba-service (main)
📄 File: src/main/java/com/skoda/cba/kafka/VcfEventListener.java

  @KafkaListener(topics = "vcf.events", groupId = "cba-vcf-consumer")
  public void handleVcfEvent(VcfEvent event) {
      vcfService.processEvent(event);  // → VcfService.java:45
  }

Flow: vcf.events topic → VcfEventListener → VcfService → VcfRepository (DB)
Related: VcfClient (external), VcfEventDto, VcfEntity

> přidej retry logiku na VcfEventListener

[Agent: Analyzing VcfEventListener + Spring Kafka retry patterns...]
[Tool: git_diff → checking current state]
[Tool: spring_analyzer → checking existing retry config]

Navrhuju přidat @RetryableTopic:

  @RetryableTopic(
      attempts = "3",
      backoff = @Backoff(delay = 1000, multiplier = 2),
      dltTopicSuffix = ".dlt"
  )
  @KafkaListener(topics = "vcf.events", groupId = "cba-vcf-consumer")
  public void handleVcfEvent(VcfEvent event) { ... }

+ DLT handler pro failed messages.
Chceš abych to implementoval? [Y/n]
```

### Na cestě: Mobilní Quick Fix
```
📱 Push notifikace:
  🔴 Pipeline cba-backend FAILED
  VumClientTest.shouldReturnUserProfile - NullPointerException

  [View Details] [Fix It] [Ignore]

Klik "Fix It":

🤖 Analyzing failure...
   Root cause: userProfile.getAddress() returns null for test user
   Fix: Add null check + update test data

   Creating branch: fix/vum-null-check
   Committing fix...
   Creating PR #193...
   Running tests... ✅ All green

   PR #193 assigned to you for review.
   [Approve & Merge] [Review Later]
```

## Tool Manifesty (JVM-Specific)

### `tools/gradle/TOOL.yaml` - Gradle/Maven Build
```yaml
apiVersion: greenforge.dev/v1
kind: Tool
metadata:
  name: build
  description: "Gradle/Maven build operations for JVM projects"
  category: build
  tags: [java, kotlin, gradle, maven, jvm]
spec:
  functions:
    - name: build_project
      description: "Build project (gradle build / mvn package)"
      parameters:
        type: object
        properties:
          path: { type: string }
          tasks: { type: array, items: { type: string }, default: ["build"] }
          args: { type: array, items: { type: string } }
        required: [path]
    - name: run_tests
      description: "Run tests with optional filter"
      parameters:
        type: object
        properties:
          path: { type: string }
          filter: { type: string, description: "Test class/method filter" }
          module: { type: string, description: "Specific submodule" }
        required: [path]
    - name: list_dependencies
      description: "Show dependency tree, detect conflicts"
      parameters:
        type: object
        properties:
          path: { type: string }
          configuration: { type: string, default: "runtimeClasspath" }
        required: [path]
    - name: run_app
      description: "Run application (bootRun / application:run)"
      parameters:
        type: object
        properties:
          path: { type: string }
          profile: { type: string, description: "Spring profile" }
        required: [path]
  sandbox:
    image: greenforge-tool-build:latest
    network:
      mode: restricted
      allowedHosts: ["repo.maven.apache.org:443", "plugins.gradle.org:443",
                     "dl.google.com:443", "repo1.maven.org:443"]
    filesystem:
      mounts:
        - { source: "${WORKSPACE}", target: /workspace }
        - { source: "${HOME}/.gradle", target: /home/agent/.gradle }
        - { source: "${HOME}/.m2", target: /home/agent/.m2, readOnly: true }
    resources: { cpuLimit: "4.0", memoryLimit: "4096m", timeoutSeconds: 600 }
  permissions: ["build:execute", "build:read", "network:outbound:https"]
```

### `tools/spring_analyzer/TOOL.yaml` - Spring Context Analysis
```yaml
apiVersion: greenforge.dev/v1
kind: Tool
metadata:
  name: spring_analyzer
  description: "Analyze Spring Boot context: beans, endpoints, configs, profiles"
  category: analysis
  tags: [spring, spring-boot, beans, endpoints, configuration]
spec:
  functions:
    - name: list_endpoints
      description: "List all REST/MVC endpoints with methods, paths, params"
      parameters:
        type: object
        properties:
          path: { type: string }
          filter: { type: string, description: "Filter by path pattern" }
        required: [path]
    - name: list_beans
      description: "List Spring beans (services, repos, components, configs)"
      parameters:
        type: object
        properties:
          path: { type: string }
          type: { enum: [service, repository, component, controller, configuration, all] }
        required: [path]
    - name: analyze_config
      description: "Analyze application.yml/properties - show all config values per profile"
      parameters:
        type: object
        properties:
          path: { type: string }
          profile: { type: string, description: "Specific profile or 'all'" }
          key: { type: string, description: "Specific config key to trace" }
        required: [path]
    - name: dependency_injection_graph
      description: "Show bean dependency/injection graph for a specific bean"
      parameters:
        type: object
        properties:
          path: { type: string }
          bean_name: { type: string }
        required: [path, bean_name]
  sandbox:
    image: greenforge-tool-spring:latest
    network: { mode: none }
    filesystem:
      mounts: [{ source: "${WORKSPACE}", target: /workspace, readOnly: true }]
    resources: { cpuLimit: "1.0", memoryLimit: "1024m", timeoutSeconds: 60 }
  permissions: ["analysis:spring"]
```

### `tools/kafka_mapper/TOOL.yaml` - Kafka Flow Analysis
```yaml
apiVersion: greenforge.dev/v1
kind: Tool
metadata:
  name: kafka_mapper
  description: "Map Kafka event flows: topics, producers, consumers, message types"
  category: analysis
  tags: [kafka, events, streaming, topics]
spec:
  functions:
    - name: map_topics
      description: "List all Kafka topics with their producers and consumers"
      parameters:
        type: object
        properties:
          path: { type: string }
        required: [path]
    - name: trace_event
      description: "Trace event flow: who produces → topic → who consumes → what happens"
      parameters:
        type: object
        properties:
          path: { type: string }
          topic: { type: string, description: "Kafka topic name" }
        required: [path]
    - name: list_listeners
      description: "List all @KafkaListener methods with their topics and groups"
      parameters:
        type: object
        properties:
          path: { type: string }
        required: [path]
  sandbox:
    image: greenforge-tool-kafka:latest
    network: { mode: none }
    filesystem:
      mounts: [{ source: "${WORKSPACE}", target: /workspace, readOnly: true }]
    resources: { cpuLimit: "0.5", memoryLimit: "512m", timeoutSeconds: 30 }
  permissions: ["analysis:kafka"]
```

### Další Tools (kompletní seznam)

| Tool | Funkce | Phase |
|------|--------|-------|
| **git** | status, diff, commit, log, branch, blame | 1 |
| **shell** | sandboxed command execution | 1 |
| **file** | read, write, search (ripgrep), tree | 1 |
| **code_review** | review diff, review file, Kotlin/Java idiom checks | 1 |
| **build** | gradle/maven build, test, deps, run | 2 |
| **spring_analyzer** | endpoints, beans, config, DI graph | 2 |
| **kafka_mapper** | topic map, event trace, listeners | 2 |
| **database** | query (PG/MySQL/H2), schema, migrations (Liquibase/Flyway) | 2 |
| **azure_devops** | pipelines, PRs, work items (Azure DevOps REST API) | 2 |
| **gitlab_ci** | pipelines, merge requests (GitLab API) | 2 |
| **logs** | search, tail, analyze, Spring Boot log parser | 2 |
| **docker** | build, run, compose, logs | 3 |
| **k8s** | pods, logs, describe, helm status | 3 |
| **notifications** | send via WhatsApp/Telegram/Email/SMS | 3 |

## Setup Wizard (`greenforge init`)

Interaktivní TUI wizard (bubbletea) který provede nového uživatele celým setupem:

```
$ greenforge init

  ╔══════════════════════════════════════════════════════════╗
  ║          🔧 GreenForge Setup Wizard (1/7)               ║
  ╠══════════════════════════════════════════════════════════╣

  Step 1/7: ZÁKLADNÍ KONFIGURACE
  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Vaše jméno: [Jan Veselý]
  Email: [jan.vesely@greencode.com]
  Workspace root (kde jsou vaše projekty):
    > [ /c/GC ]
    Nalezeno 4 Git projektů: mhub, cba-backend, pde-backend, devops
    Přidat další cestu? [/c/PROJECTS] → +1 projekt: UMBERbot

  [Next →]

  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Step 2/7: SSH CERTIFIKÁTOVÁ AUTORITA
  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  GreenForge potřebuje vytvořit Certificate Authority pro
  bezpečnou autentizaci. Toto je jednorázový krok.

  CA passphrase (pro šifrování CA klíčů):
    > [••••••••••••]
    Confirm: [••••••••••••]

  Cert lifetime:
    ○ 8 hodin (doporučeno pro denní práci)
    ● 1 den
    ○ 7 dní
    ○ 30 dní

  ✅ CA vytvořena: ~/.greenforge/ca/
  ✅ Admin certifikát vygenerován (platnost: 1 den)

  [Next →]

  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Step 3/7: AI MODEL
  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Jaký AI model chcete používat?

  ● Ollama (lokální, žádná data neopustí váš stroj)
    └ Detekováno: Ollama běží na localhost:11434
    └ Modely: codestral, llama3.1:70b
  ○ Claude (Anthropic API) - vyžaduje API klíč
  ○ GPT-4o (OpenAI API) - vyžaduje API klíč
  ○ Konfigurovat později

  Vybraný model: ollama/codestral

  Chcete nastavit AI model policy per-projekt?
  (Firemní projekty = local only, osobní = cloud OK)
  ● Ano, nastavit nyní
  ○ Ne, použít stejný model pro vše

  Firemní projekty (cesty začínající):
    > [/c/GC] → ollama only
  Osobní projekty (ostatní): → všechny modely povoleny

  [Next →]

  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Step 4/7: DOCKER SANDBOX
  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  GreenForge spouští nástroje v izolovaných Docker kontejnerech.

  ✅ Docker Engine detekován (Docker Desktop 4.38.0)
  ✅ Docker daemon běží

  Chcete stáhnout base tool images nyní?
  (git, shell, file, code_review ~ 800 MB celkem)
  ● Ano, stáhnout nyní
  ○ Stáhnout on-demand (při prvním použití)

  Stahuji images... [████████████░░░░] 67% greenforge-tool-git

  [Next →]

  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Step 5/7: NOTIFIKACE
  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Jak chcete dostávat upozornění? (lze změnit později)

  ☑ CLI toast (vždy zapnuto)
  ☑ Email → jan.vesely@greencode.com
  ☐ WhatsApp → [číslo]
  ☑ Telegram → [bot token: ••••] [chat ID: ••••]
  ☐ SMS

  Které události chcete sledovat?
  ☑ Pipeline failures
  ☑ PR assigned to me
  ☐ All commits

  Morning digest:
  ○ Automaticky každé ráno (čas: [07:30])
  ● Jen na vyžádání (/digest, CLI, UI button)
  ○ Obojí

  [Next →]

  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Step 6/7: CI/CD INTEGRACE
  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Jaké CI/CD platformy používáte?

  ☑ Azure DevOps
    └ Organization: [greencode]
    └ PAT token: [••••••••] (uložen do OS keychain)
  ☑ GitLab CI
    └ URL: [https://gitlab.greencode.com]
    └ Token: [••••••••] (uložen do OS keychain)
  ☐ GitHub Actions

  Auto-fix policy (výchozí pro všechny repo):
    ● Pouze upozornění (doporučeno pro začátek)
    ○ Fix + PR k review
    ○ Fix + auto-merge
  Chcete konfigurovat per-repo pravidla nyní?
  ○ Ano  ● Ne, udělám později (greenforge config autofix)

  [Next →]

  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Step 7/7: CODEBASE INDEX
  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  GreenForge indexuje vaše projekty pro okamžité vyhledávání.

  Nalezené projekty:
  ☑ mhub         (1,748 Java files) ~3 min
  ☑ cba-backend  (533 Java files)   ~45 sec
  ☑ pde-backend  (421 Java files)   ~40 sec
  ☑ devops       (YAML/Helm)        ~10 sec
  ☑ UMBERbot     (Python + TS)      ~30 sec

  Indexovat nyní?
  ● Ano, indexovat všechny vybrané projekty na pozadí
  ○ Ne, indexovat on-demand

  Indexuji na pozadí... můžete začít pracovat.

  [Finish →]

  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  ✅ SETUP DOKONČEN!
  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  Konfigurace uložena: ~/.greenforge/greenforge.toml
  CA certifikáty:      ~/.greenforge/ca/
  Váš certifikát:      ~/.greenforge/certs/current (platnost: 1 den)

  Rychlý start:
    greenforge run                    # Spustit interaktivní session
    greenforge run --project cba      # Session pro konkrétní projekt
    greenforge query "list endpoints" # Dotaz na codebase index
    greenforge auth device add        # Přidat mobilní zařízení
    greenforge digest                 # Zobrazit dnešní digest

  Dokumentace: greenforge docs
  Nápověda:    greenforge help
```

**Wizard features:**
- Detekuje existující nástroje (Docker, Ollama, Git projekty)
- Odhaduje čas indexování per-projekt
- Ukládá secrets do OS keychain (nikdy do config souborů)
- Bezpečné výchozí hodnoty (local AI, notify only, etc.)
- Každý krok lze přeskočit a dokonfigurovat později
- Na konci zobrazí quick start příkazy

**Post-wizard příkazy pro dokonfiguraci:**
```bash
greenforge config edit              # Otevřít config v editoru
greenforge config autofix           # Konfigurovat auto-fix policy
greenforge config notify            # Změnit notification preferences
greenforge config models            # Změnit AI model policy
greenforge config projects add .    # Přidat projekt do workspace
```

## Light Web UI (PWA)

Webový dashboard embedded v Go binary (`go:embed`) - přístupný přes browser na `localhost:18789` nebo přes Tailscale z mobilu.

### Hlavní sekce:

```
┌──────────────────────────────────────────────────────────────────┐
│ 🔧 GreenForge                           [Jan Veselý] [⚙ Settings]│
├──────────────────────────────────────────────────────────────────┤
│                                                                  │
│  📊 Dashboard                                                    │
│  ├── Pipeline status (všechny projekty)                         │
│  ├── Aktivní sessions                                           │
│  ├── Poslední akce (audit log preview)                          │
│  └── Index status (per-projekt freshness)                       │
│                                                                  │
│  💬 Chat                                                         │
│  └── Webový chat interface (alternativa k CLI)                  │
│      Streaming odpovědi, tool execution vizualizace             │
│                                                                  │
│  📋 Audit Log                                                    │
│  └── Filtrovatelný log (per-user, per-tool, per-project, čas)   │
│                                                                  │
│  📱 Devices                                                      │
│  └── Seznam zařízení, cert status, QR provisioning, revoke      │
│                                                                  │
│  ⚙ Settings (HLAVNÍ CONFIG UI)                                  │
│  └── Viz detailně níže                                          │
└──────────────────────────────────────────────────────────────────┘
```

### Settings UI (kompletní config bez YAML)

```
⚙ Settings
├── 🔑 General
│   ├── Workspace paths          [/c/GC, /c/PROJECTS]  [+ Add]
│   ├── Log level                [Info ▼]
│   └── Language                 [Czech ▼]
│
├── 🤖 AI Models
│   ├── Default model            [ollama/codestral ▼]
│   ├── Per-project policy:
│   │   ┌────────────────┬──────────────────┬─────────┐
│   │   │ Path pattern   │ Allowed models   │ Actions │
│   │   ├────────────────┼──────────────────┼─────────┤
│   │   │ /c/GC/*        │ ollama only      │ ✏️ 🗑️  │
│   │   │ /c/PROJECTS/*  │ all              │ ✏️ 🗑️  │
│   │   └────────────────┴──────────────────┴─────────┘
│   │   [+ Add rule]
│   └── Provider configs:
│       ├── Ollama endpoint      [localhost:11434]  [Test ✅]
│       ├── Anthropic API key    [••••••] (keychain) [Test]
│       └── OpenAI API key       [not set]          [Set]
│
├── 🔐 Security
│   ├── Cert lifetime            [1 day ▼]
│   ├── Auto-renew threshold     [20% ▼]
│   ├── CA passphrase            [Change...]
│   ├── Device cert lifetime     [30 days ▼]
│   └── Max devices per user     [5]
│
├── 🔧 Auto-fix Policy
│   ├── Default: [notify_only ▼]
│   ├── Per-repo rules:
│   │   ┌──────────────┬──────────────┬────────────────┬──────────┐
│   │   │ Repository   │ Branch       │ On failure     │ Actions  │
│   │   ├──────────────┼──────────────┼────────────────┼──────────┤
│   │   │ cba-backend  │ master       │ notify_only    │ ✏️ 🗑️   │
│   │   │ cba-backend  │ develop      │ fix_and_pr     │ ✏️ 🗑️   │
│   │   │ cba-backend  │ feature/*    │ fix_and_merge  │ ✏️ 🗑️   │
│   │   │ mhub         │ *            │ notify_only    │ ✏️ 🗑️   │
│   │   └──────────────┴──────────────┴────────────────┴──────────┘
│   │   [+ Add rule]
│   └── Advanced:
│       ├── Max auto-fixes/branch  [3]
│       └── Escalate after         [30 min]
│
├── 🔔 Notifications
│   ├── Channels:
│   │   ☑ Email          jan.vesely@greencode.com    [Test 📧]
│   │   ☑ Telegram       @greenforge_bot             [Test 📱]
│   │   ☐ WhatsApp       [Configure...]
│   │   ☐ SMS            [Configure...]
│   ├── Events:
│   │   ☑ Pipeline failures
│   │   ☑ PR assigned to me
│   │   ☐ All commits
│   │   ☐ Auto-fix completed
│   ├── Morning Digest:
│   │   Trigger: ○ Automatic (cron)  čas: [07:30]
│   │            ● On-demand only (/digest, CLI, UI button)
│   │            ○ Obojí (auto + on-demand)
│   └── Quiet hours:       [22:00] - [07:00]
│
├── 🗂️ Projects
│   ├── Per-project settings:
│   │   ┌──────────────┬─────────┬──────────┬───────────┬──────────┐
│   │   │ Project      │ Build   │ CI/CD    │ Index     │ Status   │
│   │   ├──────────────┼─────────┼──────────┼───────────┼──────────┤
│   │   │ cba-backend  │ Gradle  │ AzDO+GL  │ ✅ Fresh  │ ✏️      │
│   │   │ mhub         │ Maven   │ GitLab   │ ⚠️ 2h old│ 🔄      │
│   │   │ pde-backend  │ Gradle  │ AzDO+GL  │ ✅ Fresh  │ ✏️      │
│   │   └──────────────┴─────────┴──────────┴───────────┴──────────┘
│   └── [+ Add project] [Reindex all]
│
├── 🔗 CI/CD Integrations
│   ├── Azure DevOps:
│   │   ├── Organization    [greencode]
│   │   ├── PAT token       [••••] (keychain)  [Refresh] [Test ✅]
│   │   └── Watched pipelines: [Select...]
│   └── GitLab:
│       ├── URL             [https://gitlab.greencode.com]
│       ├── Token           [••••] (keychain)  [Test ✅]
│       └── Watched projects: [Select...]
│
├── 🛡️ RBAC (Roles & Permissions)
│   └── Roles:
│       ┌──────────┬─────────────────────────────────┬──────────┐
│       │ Role     │ Permissions                     │ Actions  │
│       ├──────────┼─────────────────────────────────┼──────────┤
│       │ admin    │ * (all)                         │ ✏️       │
│       │ developer│ vcs:*, build:*, shell, db:read  │ ✏️       │
│       │ viewer   │ vcs:read, logs:read, cicd:read  │ ✏️       │
│       └──────────┴─────────────────────────────────┴──────────┘
│       [+ Add role]
│
└── 🧰 Tools
    └── Installed tools:
        ┌──────────────────┬─────────┬────────────┬──────────┐
        │ Tool             │ Version │ Sandbox    │ Status   │
        ├──────────────────┼─────────┼────────────┼──────────┤
        │ git              │ 1.0.0   │ restricted │ ✅ Ready │
        │ build (gradle)   │ 1.0.0   │ restricted │ ✅ Ready │
        │ spring_analyzer  │ 1.0.0   │ none       │ ✅ Ready │
        │ kafka_mapper     │ 1.0.0   │ none       │ ✅ Ready │
        │ database         │ 1.0.0   │ restricted │ ⚠️ No DB│
        └──────────────────┴─────────┴────────────┴──────────┘
        [+ Install tool]
```

**Tech stack pro web UI:**
- **SvelteKit** (embedded v Go binary přes `go:embed`)
- Komunikace s Gateway přes WebSocket (real-time updates)
- Responsive design → funguje jako PWA na mobilu
- Změny v Settings → instant update `greenforge.toml` + YAML configs
- Žádná manuální editace config souborů potřeba

**Přístup:**
- Lokálně: `http://localhost:18789`
- Remote: `https://[tailscale-ip]:18789` (TLS via host cert)
- Mobil: PWA install z browseru

## README Struktura

README.md bude obsahovat:

```markdown
# 🔧 GreenForge

Secure AI developer agent pro JVM týmy. Rozumí vašemu Spring Boot projektu,
hlídá pipeline, a pomáhá z terminálu i z mobilu.

## Highlights
- 🧠 **JVM Intelligence** - Zná vaše moduly, Spring beany, Kafka topiky, DB schéma
- 🔐 **SSH Certifikáty** - Short-lived certs, zero static passwords, audit trail
- 🐳 **Sandboxed Tools** - Každý nástroj běží v izolovaném Docker kontejneru
- 📱 **Remote Access** - CLI + mobil (QR provisioning) + WhatsApp/Telegram notifikace
- 🤖 **Proaktivní** - Morning digest, pipeline watcher, auto-fix

## Quick Start
  $ curl -sSL https://greenforge.dev/install.sh | sh
  $ greenforge init    # Interaktivní wizard
  $ greenforge run     # Spustit AI agenta

## Table of Contents
1. [Installation](#installation)
2. [Setup Wizard](#setup-wizard)
3. [Daily Usage](#daily-usage)
   - CLI Commands
   - Codebase Queries
   - Mobile Access
4. [Architecture](#architecture)
   - SSH Certificate Authority
   - Gateway & Sessions
   - Tool Sandbox Engine
   - Codebase Index Engine
5. [Configuration](#configuration)
   - greenforge.toml Reference
   - AI Model Policy
   - Auto-fix Policy
   - RBAC Roles
   - Notification Channels
6. [Built-in Tools](#tools)
   - General: git, shell, file, code_review
   - JVM: build, spring_analyzer, kafka_mapper, database
   - CI/CD: azure_devops, gitlab_ci
   - Ops: logs, docker, k8s
7. [Writing Custom Tools](#custom-tools)
   - TOOL.yaml Manifest Reference
   - Tool SDK (Go)
   - Dockerfile Best Practices
8. [Security Model](#security)
   - Certificate Lifecycle
   - Secret Management
   - AI Model Firewall
   - Audit Logging
9. [Remote Access](#remote)
   - Tailscale Setup
   - Mobile QR Provisioning
   - Device Management
10. [Skills (SKILL.md)](#skills)
11. [Troubleshooting](#troubleshooting)
12. [Contributing](#contributing)
```

Každá sekce bude mít:
- **Co to dělá** (1-2 věty)
- **Quick example** (copy-paste příkaz)
- **Detailní reference** (tabulka parametrů / YAML ukázka)
- **Troubleshooting** tips pro tu sekci

Kromě README budou v `docs/`:
```
docs/
├── getting-started.md          # Rozšířený tutorial (15 min)
├── architecture.md             # Detailní architektura s diagramy
├── auth-flow.md                # SSH certifikáty detailně
├── tool-development.md         # Jak napsat vlastní tool
├── security-model.md           # Security whitepaper
├── configuration-reference.md  # Kompletní config reference
├── api-reference.md            # gRPC/REST API dokumentace
├── faq.md                      # Často kladené otázky
└── changelog.md                # Historie verzí
```

## Projektová Struktura

```
greenforge/
├── cmd/greenforge/main.go              # CLI entry point
├── internal/
│   ├── ca/                             # SSH Certificate Authority
│   │   ├── authority.go                # Core: sign, verify, revoke
│   │   ├── store.go                    # BoltDB cert store + KRL
│   │   └── provisioner.go             # Auth methods (password, cert renewal)
│   ├── gateway/
│   │   ├── server.go                   # WS/gRPC server
│   │   ├── session.go                  # Session manager (lane queues)
│   │   └── middleware.go              # Cert validation, rate limiting
│   ├── rbac/engine.go                  # RBAC policy evaluation
│   ├── agent/
│   │   ├── runtime.go                  # Agent loop (plan-execute)
│   │   ├── context.go                  # Prompt assembly (uses codebase index)
│   │   └── memory.go                  # Session history + knowledge base
│   ├── model/
│   │   ├── router.go                   # Model selection + policy enforcement
│   │   ├── firewall.go                # Secret scrubbing before LLM calls
│   │   └── providers/{anthropic,openai,ollama}.go
│   ├── sandbox/
│   │   ├── docker.go                   # Container lifecycle
│   │   ├── network.go                 # Network policies
│   │   ├── secrets.go                 # Secret injection (keychain → env var)
│   │   └── resource.go               # CPU/mem/time limits
│   ├── tools/
│   │   ├── registry.go                # Discovery + validation
│   │   ├── executor.go                # Execution orchestration
│   │   └── schema.go                  # JSON Schema validation
│   ├── index/                          # ★ Codebase Index Engine
│   │   ├── engine.go                   # Main indexer orchestration
│   │   ├── parser_java.go             # Tree-sitter Java AST parsing
│   │   ├── parser_kotlin.go           # Tree-sitter Kotlin AST parsing
│   │   ├── parser_build.go            # Gradle/Maven build file parsing
│   │   ├── parser_spring.go           # Spring annotation extraction
│   │   ├── parser_kafka.go            # Kafka annotation extraction
│   │   ├── parser_jpa.go              # JPA/Hibernate entity extraction
│   │   ├── store.go                    # SQLite FTS5 + vector storage
│   │   ├── incremental.go             # Git-diff based incremental updates
│   │   └── daemon.go                  # Background file watcher
│   ├── notify/                         # ★ Notification Engine
│   │   ├── engine.go                   # Dispatcher
│   │   ├── whatsapp.go                # WhatsApp provider
│   │   ├── telegram.go                # Telegram Bot API
│   │   ├── email.go                   # SMTP
│   │   └── sms.go                     # Twilio/custom
│   ├── digest/                         # ★ Morning Digest
│   │   ├── collector.go               # Collect data from all sources
│   │   ├── formatter.go               # Format digest message
│   │   └── scheduler.go              # Cron-like scheduling
│   ├── autofix/                        # ★ Auto-fix Engine
│   │   ├── watcher.go                 # Pipeline status watcher
│   │   ├── analyzer.go                # Failure analysis
│   │   └── fixer.go                   # Fix generation + PR creation
│   ├── audit/{logger,store}.go
│   └── config/config.go
├── pkg/
│   ├── toolsdk/sdk.go                 # Public Tool SDK
│   └── certsdk/client.go              # CA client library
├── tools/                              # Built-in tool implementations
│   ├── git/, shell/, file/, code_review/
│   ├── build/, spring_analyzer/, kafka_mapper/
│   ├── database/, azure_devops/, gitlab_ci/
│   ├── logs/, docker/, k8s/, notifications/
├── skills/                             # SKILL.md files
│   ├── spring-boot-debug/SKILL.md
│   ├── kafka-event-trace/SKILL.md
│   ├── jvm-code-review/SKILL.md
│   └── migration-helper/SKILL.md
├── api/proto/{gateway,auth,audit}.proto
├── configs/
│   ├── greenforge.toml                # Main config
│   ├── rbac.yaml                      # RBAC policies
│   ├── models.yaml                    # Model providers
│   └── model-policy.yaml             # Per-project AI model policy
├── go.mod, Makefile, README.md
```

## Implementační Fáze

### Phase 1: Core + Auth + Basic Tools (Týdny 1-4)
**Cíl:** `greenforge init` → SSH cert auth → CLI session → git/shell/file/code_review v Docker sandboxu

| Týden | Deliverable |
|-------|------------|
| 1 | Go scaffold, cobra CLI (`greenforge init/auth/run/query/audit`), SSH CA (Ed25519 sign/verify/revoke) |
| 2 | Gateway WS server, cert middleware, session manager, audit logger, TOML config, secrets manager (Windows Credential Manager) |
| 3 | Agent runtime (Claude streaming + secret firewall), tool call interception, Docker sandbox engine |
| 4 | Tools: `git`, `shell`, `file`, `code_review` + `greenforge init` wizard |

**Exit:** `greenforge init` → auth → session → git operations v sandboxu → audit log ✓

### Phase 2: JVM Intelligence + CI/CD (Týdny 5-10)
**Cíl:** Codebase index, JVM-specific tools, Azure DevOps/GitLab integration

| Týden | Deliverable |
|-------|------------|
| 5-6 | **Codebase Index Engine**: tree-sitter Java/Kotlin, build parser, SQLite FTS5, incremental git-diff updates |
| 7 | **Spring/Kafka parsers**: endpoint map, bean registry, topic flow, JPA entities |
| 8 | Tools: `build` (Gradle+Maven), `spring_analyzer`, `kafka_mapper` |
| 9 | Tools: `database` (PG/MySQL/H2 + Liquibase/Flyway), `logs` |
| 10 | Tools: `azure_devops`, `gitlab_ci` + multi-model support (OpenAI, Ollama) + RBAC |

**Exit:** `greenforge query "kde se zpracovává VCF event?"` → instant answer from index ✓

### Phase 3: Automation + Remote + Notifications (Týdny 11-16)
**Cíl:** Morning digest, auto-fix, pipeline watcher, multi-channel notifikace, remote access

| Týden | Deliverable |
|-------|------------|
| 11-12 | **Notification Engine**: WhatsApp, Telegram, Email, SMS providers |
| 13 | **Morning Digest**: data collector, formatter, scheduler |
| 14 | **Pipeline Watcher**: Azure DevOps/GitLab polling, failure detection |
| 15 | **Auto-fix Engine**: failure analysis, fix generation, PR creation |
| 16 | **Remote**: Tailscale integration docs, PWA web interface |

**Exit:** Ráno přijde digest na WhatsApp, po failnutém pipeline přijde notifikace + auto-fix PR ✓

### Phase 4: Knowledge Base + Polish (17+)
- Persistent knowledge base (konvence, rozhodnutí, preference)
- Docker/K8s tools
- Onboarding mode pro nové členy týmu
- Team-sharable skills a configs
- VS Code extension

## Verifikace

1. `greenforge init` → CA + certs + config vytvořeny
2. `greenforge auth login` → podepsaný SSH certifikát (8h validity)
3. `greenforge run --project /c/GC/cba-backend` → interaktivní session
4. `greenforge query "list all kafka topics"` → instant z indexu
5. "fix the failing test" → sandbox build + test + git commit
6. Pipeline failure → WhatsApp notifikace → "fix it" → auto-fix PR
7. `greenforge audit list` → všechny akce s cert identity + hash chain
8. `go test ./...` pro unit testy, Docker-in-Docker pro integration
