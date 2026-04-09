# SOP Chat

[中文](README.md) | English

A client application for Alibaba Cloud SLS and CMS AI Chat Assistants. It provides a standalone Web UI and supports DingTalk, Feishu (Lark), and WeCom bot integrations, enabling you to use SOP intelligent conversation capabilities without any development.

## Features

- **Standalone Web UI** — Ready-to-use chat interface with Markdown rendering and multi-session management
- **Multi-platform Bot Integration** — Connect SOP Agent to DingTalk, Feishu, and WeCom; multiple bot instances can run simultaneously
  - **DingTalk** — Based on DingTalk Stream SDK, no public IP required; supports group @mention and direct messages
  - **Feishu (Lark)** — Based on Feishu WebSocket long connection, no public IP required; supports group and direct messages
  - **WeCom** — Callback-based, supports application message receiving
- **Scheduled Tasks (Cron)** — Configure multiple cron jobs to periodically query digital employees and push responses to DingTalk, Feishu, or WeCom Webhooks
- **OpenAI-Compatible API** — Exposes `/openai/v1/chat/completions` endpoint for integration with Cherry Studio, ChatBox, and other OpenAI-compatible clients
- **Visual Configuration** — Built-in `/config` page for browser-based configuration, no manual file editing required
- **User Authentication** — JWT-based local user management with role-based permissions
- **Streaming Chat** — SSE real-time streaming output with visible tool invocation process

## Quick Start (Recommended)

### 1. Download Binary

Download the binary for your platform from the [Releases](../../releases) page:

| Platform | Filename |
|----------|----------|
| Linux x86_64 | `sop-chat-server-linux-amd64` |
| macOS Intel | `sop-chat-server-darwin-amd64` |
| macOS Apple Silicon | `sop-chat-server-darwin-arm64` |

### 2. Start the Server

```bash
# Grant execute permission (macOS / Linux)
chmod +x sop-chat-server

# Foreground mode (default, logs to terminal, Ctrl+C to exit)
./sop-chat-server

# Daemon mode (logs written to logs/sop-chat-server.log)
./sop-chat-server --daemon
```

After startup, the terminal will display the configuration UI URL. Complete the initial setup through the config page.

#### Daemon Commands

```bash
# Show admin UI URL
./sop-chat-server adminurl

# Stop the daemon
./sop-chat-server stop
```

> **Note:** Foreground mode also writes `logs/sop-chat-server.pid` and `logs/sop-chat-server.url`, so `adminurl` / `stop` subcommands work in both modes.

---

## Alibaba Cloud Prerequisites

Before using the application, complete the following authorization setup on Alibaba Cloud.

### Required IAM Policy (for the AccessKey account):

```json
{
  "Version": "1",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "cms:CreateChat",
        "cms:GetDigitalEmployee",
        "cms:ListDigitalEmployees",
        "cms:GetThread",
        "cms:GetThreadData",
        "cms:ListThreads",
        "cms:CreateDigitalEmployee",
        "cms:UpdateDigitalEmployee",
        "cms:DeleteDigitalEmployee",
        "cms:CreateThread",
        "cms:UpdateThread",
        "cms:DeleteThread"
      ],
      "Resource": [
        "acs:cms:*:*:digitalemployee/*",
        "acs:cms:*:*:digitalemployee/*/thread/*"
      ]
    },
    {
      "Effect": "Allow",
      "Action": "ram:PassRole",
      "Resource": "*",
      "Condition": {
        "StringEquals": {
          "acs:Service": "cloudmonitor.aliyuncs.com"
        }
      }
    }
  ]
}
```

> **Tip:** You can restrict the `ram:PassRole` Resource to the specific RAM role ARN.

---

## Building from Source

### Requirements

- Go 1.23+
- Node.js 18+

### One-Step Build (Recommended)

```bash
# Build binary for current platform (frontend embedded)
make build

# Cross-platform build (Linux + macOS)
make build-all
```

**Output:**
- Single platform: `backend/sop-chat-server`, `backend/sop-chat-cli`
- Cross-platform: `dist/linux/sop-chat-server`, `dist/darwin/sop-chat-server`, `dist/darwin/sop-chat-server-arm64`

### Other Build Commands

```bash
make build-frontend  # Build frontend only
make build-backend   # Build backend only (requires frontend built first)
make build-cli       # Build CLI tool only
make clean           # Clean all build artifacts
make clean-dist      # Clean cross-platform build artifacts only
```

### Manual Build

```bash
# Backend
cd backend
go build -o sop-chat-server cmd/sop-chat-server/main.go

# Frontend
cd frontend
npm install
npm run build
```

### Development Mode

```bash
# Backend (hot reload)
cd backend
go run cmd/sop-chat-server/main.go

# Frontend (Vite dev server, http://localhost:5173)
cd frontend
npm install
npm run dev
```

---

## Architecture

```
┌──────────────────────────────────────────────────────────────┐
│        Users / DingTalk / Feishu / WeCom                     │
└───┬──────────────┬──────────────┬──────────────┬─────────────┘
    │ HTTP/SSE      │ DingTalk     │ Feishu       │ WeCom
    │               │ Stream SDK   │ WebSocket    │ Callback
    ▼               ▼              ▼              ▼
┌──────────────────────────────────────────────────────────────┐
│                      SOP Chat Server                         │
│                                                              │
│  ┌────────────┐ ┌────────────┐ ┌────────────┐ ┌──────────┐  │
│  │  Web UI    │ │  DingTalk  │ │   Feishu   │ │  WeCom   │  │
│  │ (Embedded) │ │   Bot      │ │    Bot     │ │   Bot    │  │
│  └────────────┘ └────────────┘ └────────────┘ └──────────┘  │
│                                                              │
│  ┌──────────────────────────────────────────────────────┐   │
│  │    Auth / Config / API Router / OpenAI-Compatible    │   │
│  └──────────────────────────────────────────────────────┘   │
└──────────────────────────┬───────────────────────────────────┘
                           │ API Calls
                           ▼
┌─────────────────────────────────────────────────────────────┐
│                        SOP Agent                            │
│                  (Alibaba Cloud Monitor)                     │
│                                                             │
│  ┌──────────────────────────────────────────────────────┐  │
│  │              ReAct Loop (Agent Runtime)               │  │
│  │                                                      │  │
│  │    ┌────────┐         ┌──────────────────────┐     │  │
│  │    │        │────────▶│   SOP Knowledge Base  │     │  │
│  │    │   AI   │         └──────────────────────┘     │  │
│  │    │ Agent  │                                       │  │
│  │    │ (Role) │         ┌──────────────────────┐     │  │
│  │    │        │────────▶│  SLS & OpenAPI Tools  │     │  │
│  │    └────────┘         └──────────────────────┘     │  │
│  │                                                      │  │
│  │         (Reason → Act → Observe)                     │  │
│  └──────────────────────────────────────────────────────┘  │
│                                                             │
│  Auth: RAM Role                                             │
└─────────────────────────────────────────────────────────────┘
```

**Core Components:**

| Component | Description |
|-----------|-------------|
| SOP Chat Frontend | React frontend, embedded into the binary after build |
| SOP Chat Server | Go backend handling auth, session management, and multi-platform bot integration |
| Config Page | Built-in `/config` page for file-free configuration |
| SOP Agent | Alibaba Cloud Monitor digital employee, based on ReAct framework |
| DingTalk Bot | Via DingTalk Stream SDK, no public IP required, supports serial session isolation |
| Feishu Bot | Via Feishu WebSocket long connection, no public IP required, supports group and direct chat |
| WeCom Bot | Via callback Webhook, supports application messages, requires public accessibility |
| Scheduler | Cron-based scheduled queries to digital employees, results pushed via Webhook |
| OpenAI-Compatible API | Standard `/openai/v1/chat/completions` endpoint for third-party tool integration |

---

## Project Structure

```
sop-chat/
├── backend/
│   ├── cmd/
│   │   ├── sop-chat-server/   # Server entry point
│   │   └── sop-chat-cli/      # CLI debug tool
│   ├── internal/
│   │   ├── api/               # HTTP routes & handlers
│   │   ├── auth/              # Authentication
│   │   ├── config/            # Configuration
│   │   ├── dingtalk/          # DingTalk bot
│   │   ├── feishu/            # Feishu bot
│   │   ├── scheduler/         # Cron scheduler
│   │   └── wecom/             # WeCom bot
│   └── pkg/sopchat/           # SOP Agent SDK wrapper
└── frontend/
    └── src/
        ├── components/        # React components
        └── services/          # API client
```

---

## Scheduled Tasks (Cron)

Scheduled tasks automatically query designated digital employees on a schedule and push responses to DingTalk, Feishu, or WeCom group bot Webhooks.

Manage tasks in the **"Scheduled Tasks"** tab of the config UI, or edit `config.yaml` manually:

```yaml
scheduledTasks:
  - name: daily-standup          # Unique task identifier
    enabled: true
    cron: "0 9 * * 1-5"          # Weekdays at 09:00, standard 5-field cron
    prompt: "What alerts need attention today?"
    employeeName: apsara-ops     # Digital employee name
    webhook:
      type: dingtalk             # Platform: dingtalk | feishu | wecom
      url: "https://oapi.dingtalk.com/robot/send?access_token=xxx"
      msgType: text              # text | markdown (DingTalk/Feishu); text | markdown | post (Feishu)
```

**Cron expression reference (5 fields):**

| Example | Meaning |
|---------|---------|
| `*/5 * * * *` | Every 5 minutes |
| `*/15 * * * *` | Every 15 minutes |
| `0 9 * * *` | Daily at 09:00 |
| `0 9 * * 1-5` | Weekdays at 09:00 |
| `0 9,18 * * 1-5` | Weekdays at 09:00 and 18:00 |

**Supported Webhook types:**

| Type | Description |
|------|-------------|
| `dingtalk` | DingTalk custom bot, supports `text` / `markdown` |
| `feishu` | Feishu custom bot, supports `text` / `post` / `markdown` |
| `wecom` | WeCom group bot, supports `text` / `markdown` |

> You can **trigger a test run immediately** in the config UI to verify your setup without waiting for the schedule.

---

## License

This project is licensed under the Apache-2.0 License. See the [LICENSE](LICENSE) file for details.
