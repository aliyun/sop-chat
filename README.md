# SOP Chat

阿里云SLS智能问答助手（SOP Agent) 的客户端应用，提供独立的 Web UI 界面，并支持钉钉机器人接入，让你无需开发即可快速使用 SOP 智能对话能力。

## 主要功能

- **独立 Web UI** — 开箱即用的聊天界面，支持 Markdown 渲染、多会话管理
- **钉钉机器人对接** — 将 SOP Agent 能力接入钉钉群，支持群内 @机器人 对话
- **可视化配置管理** — 内置 `/config` 页面，启动后直接在浏览器中完成所有配置，无需手动编辑文件
- **用户认证** — 基于 JWT 的本地用户管理
- **流式对话** — SSE 实时流式输出，工具调用过程可见

## 快速开始（推荐）

### 1. 下载二进制

从 [Releases](../../releases) 页面下载对应平台的二进制文件：

| 平台 | 文件名 |
|------|--------|
| Linux x86_64 | `sop-chat-server-linux-amd64` |
| macOS Intel | `sop-chat-server-darwin-amd64` |
| macOS Apple Silicon | `sop-chat-server-darwin-arm64` |

### 2. 启动服务

```bash
# 赋予执行权限（macOS / Linux）
chmod +x sop-chat-server

# 启动（首次运行会自动生成默认 config.yaml）
./sop-chat-server
```

启动后在终端会输出后台配置的链接地址

保存后重启服务即可生效。

> **命令行参数：**
> - `-c / -config`：指定配置文件路径（默认 `config.yaml`）
> - `-p / -port`：指定监听端口（默认 `8080`）
> - `-h / -help`：显示帮助

---

## 阿里云前置配置

在使用前，需要在阿里云侧完成以下授权配置。

### 1. AK账号需要的权限策略（AccessKey 所属账号）：

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

> **提示**：可将 `ram:PassRole` 的 Resource 限制为第一步创建的 RAM 角色 ARN。

---

## 手动构建

### 环境要求

- Go 1.23+
- Node.js 18+

### 一键构建（推荐）

```bash
# 构建当前平台的单一二进制（前端已嵌入）
make build

# 或使用构建脚本
./build.sh

# 多平台构建（Linux + macOS）
./build-all.sh
```

**产物：**
- 单平台：`backend/sop-chat-server`
- 多平台：`dist/linux/sop-chat-server`、`dist/darwin/sop-chat-server`、`dist/darwin/sop-chat-server-arm64`

### 分开构建

```bash
# 后端
cd backend
go build -o sop-chat-server cmd/sop-chat-server/main.go

# 前端
cd frontend
npm install
npm run build
```

### 开发模式

```bash
# 后端（热重载）
cd backend
go run cmd/sop-chat-server/main.go

# 前端（Vite 开发服务器，http://localhost:5173）
cd frontend
npm install
npm run dev
```

---

## 架构说明

```
┌─────────────────────────────────────────┐
│           用户 / 钉钉群                  │
└────────┬────────────────┬───────────────┘
         │ HTTP/SSE        │ DingTalk Stream SDK
         ▼                 ▼
┌─────────────────────────────────────────┐
│           SOP Chat Server               │
│                                         │
│  ┌──────────────┐  ┌──────────────────┐ │
│  │   Web UI     │  │  钉钉机器人       │ │
│  │  (内嵌前端)  │  │  (Stream 模式)   │ │
│  └──────────────┘  └──────────────────┘ │
│                                         │
│  ┌──────────────────────────────────┐   │
│  │  认证 / 配置管理 / API 路由       │   │
│  └──────────────────────────────────┘   │
└────────────────────┬────────────────────┘
                     │ API Calls
                     ▼
┌─────────────────────────────────────────────────────────────┐
│                        SOP Agent                            │
│                     (阿里云云监控)                           │
│                                                             │
│  ┌──────────────────────────────────────────────────────┐  │
│  │              ReAct Loop（Agent 运行时）               │  │
│  │                                                      │  │
│  │    ┌────────┐         ┌──────────────────────┐     │  │
│  │    │        │────────▶│   SOP 知识库          │     │  │
│  │    │  AI    │         └──────────────────────┘     │  │
│  │    │ Agent  │                                       │  │
│  │    │ (角色) │         ┌──────────────────────┐     │  │
│  │    │        │────────▶│  SLS & OpenAPI 工具   │     │  │
│  │    └────────┘         └──────────────────────┘     │  │
│  │                                                      │  │
│  │         （推理 → 行动 → 观察）                       │  │
│  └──────────────────────────────────────────────────────┘  │
│                                                             │
│  认证方式：RAM 角色                                          │
└─────────────────────────────────────────────────────────────┘
```

**核心组件：**

| 组件 | 说明 |
|------|------|
| SOP Chat Frontend | React 前端，构建后嵌入二进制 |
| SOP Chat Server | Go 后端，负责认证、会话管理、钉钉对接 |
| 配置管理页面 | 内置 `/config` 页面，支持无文件化配置 |
| SOP Agent | 阿里云云监控数字员工，基于 ReAct 框架 |
| 钉钉机器人 | 通过 DingTalk Stream SDK 接入，支持串行会话隔离 |

---

## 项目结构

```
sop-chat/
├── backend/
│   ├── cmd/
│   │   ├── sop-chat-server/   # 服务端入口
│   │   └── sop-chat-cli/      # CLI 调试工具
│   ├── internal/
│   │   ├── api/               # HTTP 路由与处理器
│   │   ├── auth/              # 认证逻辑
│   │   ├── config/            # 配置结构
│   │   └── dingtalk/          # 钉钉机器人
│   └── pkg/sopchat/           # SOP Agent SDK 封装
└── frontend/
    └── src/
        ├── components/        # React 组件
        └── services/          # API 客户端
```

---

## License

本项目基于 Apache-2.0 协议开源，详见 LICENSE 文件。
