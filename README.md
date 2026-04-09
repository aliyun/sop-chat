# SOP Chat

中文 | [English](README_EN.md)

阿里云SLS和CMS智能问答助手的客户端应用，提供独立的 Web UI 界面，并支持钉钉、飞书、企业微信机器人接入，让你无需开发即可快速使用 SOP 智能对话能力。

## 主要功能
- **独立 Web UI** — 开箱即用的聊天界面，支持 Markdown 渲染、多会话管理
- **多平台机器人对接** — 支持钉钉、飞书、企业微信三大 IM 平台接入 SOP Agent，同一服务可同时运行多个机器人实例
  - **钉钉** — 基于 DingTalk Stream SDK，无需公网 IP，支持群内 @机器人 及单聊
  - **飞书** — 基于飞书 WebSocket 长连接，无需公网 IP，支持群聊与单聊
  - **企业微信** — 基于回调模式，支持应用消息接收
- **定时任务（Cron）** — 支持配置多个定时任务，定期向指定数字员工发起提问，并将回答自动推送至钉钉、飞书或企业微信 Webhook
- **OpenAI 兼容接口** — 暴露 `/openai/v1/chat/completions` 接口，方便使用 Cherry Studio、ChatBox 等兼容 OpenAI 协议的聊天客户端直接接入
- **可视化配置管理** — 内置 `/config` 页面，启动后直接在浏览器中完成所有配置，无需手动编辑文件
- **用户认证** — 基于 JWT 的本地用户管理，支持角色权限
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

# 前台运行（默认，日志直接输出到终端，Ctrl+C 退出）
./sop-chat-server

# 后台守护进程模式运行（日志写入 logs/sop-chat-server.log）
./sop-chat-server --daemon
```

启动后终端会输出配置管理 UI 的访问地址，根据配置页面完成初始配置。

#### 守护进程常用命令

```bash
# 查看管理 UI 地址
./sop-chat-server adminurl

# 停止后台进程
./sop-chat-server stop
```

> **说明：** 前台模式同样会写入 `logs/sop-chat-server.pid` 和 `logs/sop-chat-server.url`，
> 因此 `adminurl` / `stop` 子命令在前台和后台两种模式下均可用。

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
# 构建当前平台的二进制（前端已嵌入）
make build

# 多平台构建（Linux + macOS）
make build-all
```

**产物：**
- 单平台：`backend/sop-chat-server`、`backend/sop-chat-cli`
- 多平台：`dist/linux/sop-chat-server`、`dist/darwin/sop-chat-server`、`dist/darwin/sop-chat-server-arm64`

### 其他构建命令

```bash
make build-frontend  # 仅构建前端
make build-backend   # 仅构建后端（需先构建前端）
make build-cli       # 仅构建 CLI 工具
make clean           # 清理所有构建产物
make clean-dist      # 仅清理多平台构建产物
```

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
┌──────────────────────────────────────────────────────────────┐
│          用户 / 钉钉群 / 飞书群 / 企业微信                    │
└───┬──────────────┬──────────────┬──────────────┬─────────────┘
    │ HTTP/SSE      │ DingTalk     │ Feishu       │ WeCom
    │               │ Stream SDK   │ WebSocket    │ Callback
    ▼               ▼              ▼              ▼
┌──────────────────────────────────────────────────────────────┐
│                      SOP Chat Server                         │
│                                                              │
│  ┌────────────┐ ┌────────────┐ ┌────────────┐ ┌──────────┐  │
│  │  Web UI    │ │  钉钉机器人 │ │  飞书机器人 │ │企业微信  │  │
│  │ (内嵌前端) │ │(Stream模式)│ │(WebSocket) │ │机器人    │  │
│  └────────────┘ └────────────┘ └────────────┘ └──────────┘  │
│                                                              │
│  ┌──────────────────────────────────────────────────────┐   │
│  │       认证 / 配置管理 / API 路由 / OpenAI 兼容接口    │   │
│  └──────────────────────────────────────────────────────┘   │
└──────────────────────────┬───────────────────────────────────┘
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
| SOP Chat Server | Go 后端，负责认证、会话管理、多平台机器人对接 |
| 配置管理页面 | 内置 `/config` 页面，支持无文件化配置 |
| SOP Agent | 阿里云云监控数字员工，基于 ReAct 框架 |
| 钉钉机器人 | 通过 DingTalk Stream SDK 接入，无需公网 IP，支持串行会话隔离 |
| 飞书机器人 | 通过飞书 WebSocket 长连接接入，无需公网 IP，支持群聊与单聊 |
| 企业微信机器人 | 通过回调 Webhook 接入，支持应用消息，需公网可达 |
| 定时任务调度器 | 基于 cron 表达式定时提问数字员工，结果推送至 Webhook |
| OpenAI 兼容接口 | 暴露标准 `/openai/v1/chat/completions`，可对接第三方工具 |

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
│   │   ├── dingtalk/          # 钉钉机器人
│   │   ├── feishu/            # 飞书机器人
│   │   ├── scheduler/         # 定时任务调度器（cron）
│   │   └── wecom/             # 企业微信机器人
│   └── pkg/sopchat/           # SOP Agent SDK 封装
└── frontend/
    └── src/
        ├── components/        # React 组件
        └── services/          # API 客户端
```

---

## 定时任务（Cron）

定时任务允许你按计划自动向指定数字员工发起提问，并将回答推送至钉钉、飞书或企业微信群机器人 Webhook。

在配置管理 UI 的 **「定时任务」** 标签页中可直接管理，也可手动编辑 `config.yaml`：

```yaml
scheduledTasks:
  - name: daily-standup          # 任务唯一标识
    enabled: true
    cron: "0 9 * * 1-5"          # 工作日 09:00，标准 5 字段 cron 表达式
    prompt: "今天有哪些需要关注的告警？"
    employeeName: apsara-ops     # 数字员工名称
    webhook:
      type: dingtalk             # 平台：dingtalk | feishu | wecom
      url: "https://oapi.dingtalk.com/robot/send?access_token=xxx"
      msgType: text              # text | markdown（钉钉/飞书）；text | markdown | post（飞书）
```

**cron 表达式说明（5 字段，秒级可选）：**

| 示例 | 含义 |
|------|------|
| `*/5 * * * *` | 每 5 分钟 |
| `*/15 * * * *` | 每 15 分钟 |
| `0 9 * * *` | 每天 09:00 |
| `0 9 * * 1-5` | 工作日 09:00 |
| `0 9,18 * * 1-5` | 工作日 09:00 和 18:00 |

**支持的 Webhook 类型：**

| type | 说明 |
|------|------|
| `dingtalk` | 钉钉自定义机器人，支持 `text` / `markdown` 消息类型 |
| `feishu` | 飞书自定义机器人，支持 `text` / `post` / `markdown` 消息类型 |
| `wecom` | 企业微信群机器人，支持 `text` / `markdown` 消息类型 |

> 在配置管理 UI 中可对任务进行**立即触发测试**，无需等待定时到来即可验证配置是否正确。

---

## License

本项目基于 Apache-2.0 协议开源，详见 LICENSE 文件。
