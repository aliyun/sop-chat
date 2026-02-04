# SOP Chat

An intelligent Q&A system powered by Alibaba Cloud SOP Agent, featuring a modern web interface and RESTful API for seamless human-AI interaction.

## Features

- **Digital Employee Management** - List and query AI employees
- **Real-time Chat** - Streaming chat with SSE support
- **Authentication** - JWT-based auth with local user management
- **Thread Management** - Create and manage conversation threads
- **Modern UI** - React-based responsive interface with Markdown rendering
- **CLI Tools** - Command-line interface for testing and automation

## Architecture

```
┌─────────────────┐
│   SOP Chat      │  (Web UI)
│   Frontend      │
└────────┬────────┘
         │ HTTP/SSE
         ▼
┌─────────────────┐
│   SOP Chat      │  (Go Backend)
│   API Server    │
└────────┬────────┘
         │ API Calls
         ▼
┌─────────────────────────────────────────────────────────────┐
│                        SOP Agent                            │
│                     (Alibaba Cloud)                         │
│                                                             │
│  ┌──────────────────────────────────────────────────────┐  │
│  │              ReAct Loop (Agent Runtime)              │  │
│  │                                                      │  │
│  │    ┌────────┐         ┌──────────────────────┐     │  │
│  │    │        │────────▶│  SOP Knowledge Base  │     │  │
│  │    │  AI    │         └──────────────────────┘     │  │
│  │    │ Agent  │                                       │  │
│  │    │ (Role) │         ┌──────────────────────┐     │  │
│  │    │        │────────▶│ SLS & OpenAPI Tools  │     │  │
│  │    └────────┘         └──────────────────────┘     │  │
│  │                                                      │  │
│  │   (Reasoning + Action + Observation)                │  │
│  └──────────────────────────────────────────────────────┘  │
│                                                             │
│  Authentication: RAM Role                                   │
└─────────────────────────────────────────────────────────────┘
```

**Key Components:**
- **SOP Chat Frontend**: React-based web interface
- **SOP Chat Server**: Go backend with authentication and session management
- **SOP Agent**: AI agent with role-based capabilities (Alibaba Cloud CMS)
- **ReAct Loop**: Agent reasoning framework for tool usage
- **Knowledge Sources**: SOP documentation and SLS/OpenAPI integrations

## Quick Start

### Prerequisites

- Go 1.22+
- Node.js 18+
- Alibaba Cloud account with CMS service access

### Alibaba Cloud Setup

**1. Create RAM Role for Digital Employee**

Create a RAM role at https://ram.console.aliyun.com/roles and attach the following policy:

```json
{
  "Version": "1",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "oss:GetObject",
        "oss:GetObjectAcl",
        "oss:ListObjects",
        "oss:ListObjectVersions"
      ],
      "Resource": [
        "acs:oss:*:*:<Your-OSS-Bucket>",
        "acs:oss:*:*:<Your-OSS-Bucket>/*"
      ]
    },
    {
      "Effect": "Allow",
      "Action": [
        "log:Get*",
        "log:List*"
      ],
      "Resource": ["*"]
    }
  ]
}
```

**2. Configure Account RAM Policy**

Attach the following policy to your Alibaba Cloud account :

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

> **Note**: You can restrict the `ram:PassRole` Resource to the specific RAM role ARN created in step 1.

### Backend Setup

**1. Configure `config.yaml`**

Create a `config.yaml` file in the backend directory:

```yaml
global:
  accessKeyId: "$ACCESS_KEY_ID"           # From environment variable
  accessKeySecret: "$ACCESS_KEY_SECRET"   # From environment variable
  endpoint: "cms.cn-hangzhou.aliyuncs.com"
  port: 8080
  timeZone: "Asia/Shanghai"
  language: "zh"

auth:
  method: local                            # Options: local, disabled
  jwtSecretKey: your-secret-key-change-me  # IMPORTANT: Change in production
  jwtExpiresIn: 24h
  
  local:
    user:
      - name: admin
        # Password is MD5 hash. Generate with: echo -n 'your-password' | md5
        password: "21232f297a57a5a743894a0e4a801fc3"
```

> **Note**: The AccessKey account must have the RAM policies configured in the Alibaba Cloud Setup section.

**2. Run Development Server**

```bash
cd backend
go mod download
go run cmd/sop-chat-api/main.go
```

Server runs on `http://localhost:8080`

### Frontend Setup

```bash
cd frontend
npm install
npm run dev
```

Frontend runs on `http://localhost:5173`

## Build & Deploy

### Single Binary Build (Recommended)

Build a standalone binary with embedded frontend:

```bash
# Using Makefile
make build

# Or using build script
./build.sh

# For multi-platform builds (Linux and macOS)
./build-all.sh
```

**Output:**
- Single platform: `backend/sop-chat-server`
- Multi-platform: `dist/linux/sop-chat-server`, `dist/darwin/sop-chat-server`, `dist/darwin/sop-chat-server-arm64`

**Run the binary:**

```bash
cd backend
./sop-chat-server -c config.yaml -p 8080
```

**Command-line options:**
- `-c, -config`: Path to config file (default: `config.yaml`)
- `-p, -port`: Server port (default: `8080`)
- `-h, -help`: Show help message

### Separate Builds

If you need to build frontend and backend separately:

```bash
# Backend
cd backend
go build -o sop-chat-server cmd/sop-chat-api/main.go

# Frontend
cd frontend
npm run build
```

## CLI Tools

```bash
cd backend

# List all digital employees
go run cmd/sop-chat-cli/main.go employee list

# Interactive chat with an employee
go run cmd/sop-chat-cli/main.go interactive --employee <employee-name>

# Send a single message
go run cmd/sop-chat-cli/main.go chat --employee <employee-name> --message "Hello"
```

## API Documentation

### Authentication Endpoints

```bash
# Login
POST /api/auth/login
Content-Type: application/json
{"username": "user", "password": "pass"}

# Get current user info
GET /api/auth/me
Authorization: Bearer <token>
```

### Employee Management

```bash
# List all digital employees
GET /api/employees

# Get employee details
GET /api/employees/:name
Authorization: Bearer <token>
```

### Chat

```bash
# Streaming chat (Server-Sent Events)
POST /api/chat/stream
Authorization: Bearer <token>
Content-Type: application/json

{
  "employeeName": "employee-name",
  "threadId": "optional-thread-id",
  "message": "Hello"
}
```

**SSE Response Format:**
```
data: {"type":"meta","threadId":"xxx"}
data: {"type":"content","content":"Hello"}
data: {"type":"tool_call","tool_name":"search","arguments":{...}}
data: {"type":"tool_result","tool_name":"search","result":{...}}
data: {"type":"done"}
```

> **Note**: All endpoints except `/api/health` and `/api/auth/login` require authentication via `Authorization: Bearer <token>` header.

## Project Structure

```
sop-chat/
├── backend/
│   ├── cmd/
│   │   ├── sop-chat-api/    # Server binary
│   │   └── sop-chat-cli/    # CLI tools
│   ├── internal/            # Internal packages
│   └── pkg/sopchat/         # Core SDK
└── frontend/
    └── src/
        ├── components/      # React components
        └── services/        # API client
```

## License

This project is licensed under the Apache-2.0 License - see the LICENSE file for details.