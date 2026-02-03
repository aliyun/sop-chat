# SOP Chat CLI Tool

一个用于管理数字员工的命令行工具。

> 🚀 **新用户？** 查看 [QUICK_START.md](./QUICK_START.md) 快速上手，5分钟开始使用！  
> 📚 **命令速查**：查看 [COMMANDS.md](./COMMANDS.md) 获取所有命令的速查表和常用示例。  
> 🔍 **调试帮助**：遇到问题？查看 [DEBUG_GUIDE.md](./DEBUG_GUIDE.md) 获取详细的调试指南。

## 功能特性

- ✅ 列出所有数字员工
- ✅ 创建新的数字员工
- ✅ 列出会话线程
- ✅ 创建新的会话线程
- ✅ 与数字员工聊天（支持SSE实时流式输出）
- ✅ 支持多种配置选项

## 环境变量配置

在使用CLI工具之前，需要配置以下环境变量（可以通过 `.env` 文件配置）：

```bash
# 必需配置
TEST_ACCESS_KEY_ID=your-access-key-id
TEST_ACCESS_KEY_SECRET=your-access-key-secret

# 可选配置
PRE_CMS_ENDPOINT=cms.cn-shanghai.aliyuncs.com  # CMS API endpoint，默认：cms.cn-shanghai.aliyuncs.com
ROLE_ARN=acs:ram::123456789012:role/your-role   # 数字员工的 Role ARN

# 数字员工配置（用于测试）
DIGITAL_EMPLOYEE_NAME=sop-test02               # 默认数字员工名称
```

## 编译

```bash
go build -o sop-chat .
```

## 快速开始

运行示例脚本快速体验所有功能：

```bash
./example.sh
```

示例脚本会依次演示：
1. 列出所有数字员工
2. 列出会话线程
3. 创建新的会话线程
4. 与数字员工进行聊天对话

## 使用说明

### 完整使用流程

#### 方式一：快速开始（推荐）

使用 `--create-new-thread` 参数，无需手动创建线程：

```bash
# 1. 列出所有数字员工
./sop-chat employee list

# 2. 直接开始聊天（自动创建新线程）
./sop-chat chat \
  --employee "sop-test02" \
  --message "什么是SLS" \
  --create-new-thread

# 3. 查看创建的线程列表
./sop-chat thread list --employee "sop-test02"

# 4. 使用返回的 thread-id 继续对话
./sop-chat chat \
  --employee "sop-test02" \
  --thread-id "thread-xxx" \
  --message "如何查询日志"
```

#### 方式二：传统方式

先创建线程，再聊天：

```bash
# 1. 列出所有数字员工，找到你要使用的员工
./sop-chat employee list

# 2. 为指定员工创建会话线程
./sop-chat thread create --employee "sop-test02" --title "我的问题"

# 3. 使用返回的 thread ID 进行聊天
./sop-chat chat \
  --employee "sop-test02" \
  --thread-id "thread-xxx" \
  --message "什么是SLS"

# 4. 继续在同一个线程中聊天
./sop-chat chat \
  --employee "sop-test02" \
  --thread-id "thread-xxx" \
  --message "如何查询日志"

# 5. 查看该员工的所有会话线程
./sop-chat thread list --employee "sop-test02"
```

### 1. 列出所有数字员工

```bash
./sop-chat employee list
```

输出示例：
```
Listing digital employees...

✓ Status Code: 200
✓ Request ID: 12345678-1234-1234-1234-123456789012
✓ Total: 2

Found 2 digital employee(s):
==========================================

[1] Name: sop-test01
    Display Name: SOP测试员工01
    Description: 这是一个测试数字员工
    Employee Type: standard
    Create Time: 2025-01-15T08:30:00Z
    Update Time: 2025-01-15T10:20:00Z

[2] Name: sop-test02
    Display Name: SOP测试员工02
    Description: 另一个测试数字员工
    Employee Type: standard
    Create Time: 2025-01-16T09:00:00Z
    Update Time: 2025-01-16T09:00:00Z
==========================================
```

### 2. 创建数字员工

#### 基本用法

```bash
./sop-chat employee create \
  --name "sop-test03" \
  --display-name "SOP测试员工03"
```

#### 完整配置

```bash
./sop-chat employee create \
  --name "sop-test03" \
  --display-name "SOP测试员工03" \
  --description "这是一个SOP数字员工" \
  --default-rule "default_greeting_rule" \
  --sop-type "oss" \
  --sop-base-path "arms-qa/master" \
  --sop-region "cn-hangzhou" \
  --sop-bucket "sls-sop-dev"
```

#### 可用参数

- `--name` **(必需)**: 数字员工的唯一标识名称
- `--display-name` **(必需)**: 数字员工的显示名称
- `--description`: 数字员工的描述（默认："数字员工"）
- `--default-rule`: 默认规则（默认："default_greeting_rule"）
- `--sop-type`: SOP 类型（默认："oss"）
- `--sop-base-path`: SOP 文档路径前缀
- `--sop-region`: SOP 区域（默认："cn-hangzhou"）
- `--sop-bucket`: OSS Bucket 名称

输出示例：
```
Creating digital employee...
  Name: sop-test03
  Display Name: SOP测试员工03
  Description: 这是一个SOP数字员工
  Role ARN: acs:ram::123456789012:role/test-digital-employee-role

✓ Status Code: 201
✓ Digital Employee Name: sop-test03
✓ Request ID: 12345678-1234-1234-1234-123456789012

✓ Digital employee created successfully!
```

### 3. 管理会话线程

#### 3.1 列出会话线程

查看指定数字员工的所有会话线程。

```bash
./sop-chat thread list --employee "sop-test02"
```

**可用参数：**
- `--employee`, `-e` **(必需)**: 数字员工名称
- `--max-results`: 最大返回结果数（默认：20）

**输出示例：**
```
Listing threads for employee: sop-test02

✓ Status Code: 200
✓ Request ID: 12345678-1234-1234-1234-123456789012
✓ Total: 5
✓ MaxResults: 20

Found 5 thread(s):
==========================================

[1] ThreadId: thread-t8wmew-x0b4kpbj8oh9
    Title: SLS问题咨询
    Status: active
    Create Time: 2025-01-15T10:30:00Z
    Update Time: 2025-01-16T08:20:00Z
    Version: 3
    Variables:
      Project: my-project
      Workspace: my-workspace
    Attributes:
      source: web
      user: user123

[2] ThreadId: thread-abc123-def456ghi789
    Title: 告警排查
    Status: active
    Create Time: 2025-01-16T09:00:00Z
    Update Time: 2025-01-16T09:15:00Z
    Version: 1
    Variables:
      Project: test-project
      Workspace: test-workspace
    Attributes:
      source: cli
      user: admin
==========================================
```

#### 3.2 创建会话线程

在与数字员工聊天之前，需要先创建一个会话线程。

**基本用法：**
```bash
./sop-chat thread create --employee "sop-test02"
```

**完整配置：**
```bash
./sop-chat thread create \
  --employee "sop-test02" \
  --title "SLS问题咨询" \
  --project "my-project" \
  --workspace "my-workspace" \
  --attr-source "cli" \
  --attr-user "admin"
```

**可用参数：**
- `--employee`, `-e` **(必需)**: 数字员工名称
- `--title`: 会话线程标题（可选）
- `--project`, `-p`: 项目名称（默认："test-project"）
- `--workspace`, `-w`: 工作空间名称（默认："test-workspace"）
- `--attr-source`: 来源属性（默认："cli"）
- `--attr-user`: 用户属性（可选）

**输出示例：**
```
Creating thread...
  Employee: sop-test02
  Title: SLS问题咨询
  Project: my-project
  Workspace: my-workspace

✓ Status Code: 201
✓ Thread ID: thread-t8wmew-x0b4kpbj8oh9
✓ Request ID: 12345678-1234-1234-1234-123456789012

✅ Thread created successfully!

💡 You can now use this thread ID for chat:
   ./sop-chat chat --employee "sop-test02" --thread-id "thread-t8wmew-x0b4kpbj8oh9" --message "your message"
```

### 4. 与数字员工聊天

`chat` 命令允许你与数字员工进行实时聊天对话，支持SSE（Server-Sent Events）流式输出。

#### 方式一：自动创建新线程并聊天（推荐）

```bash
./sop-chat chat \
  --employee "sop-test02" \
  --message "什么是SLS" \
  --create-new-thread
```

或指定线程标题：

```bash
./sop-chat chat \
  --employee "sop-test02" \
  --message "什么是SLS" \
  --create-new-thread \
  --thread-title "SLS学习"
```

#### 方式二：使用已有线程

```bash
./sop-chat chat \
  --employee "sop-test02" \
  --thread-id "thread-t8wmew-x0b4kpbj8oh9" \
  --message "什么是SLS"
```

#### 完整配置

```bash
./sop-chat chat \
  --employee "sop-test02" \
  --thread-id "thread-t8wmew-x0b4kpbj8oh9" \
  --message "帮我查询最近的告警信息" \
  --region "cn-hangzhou" \
  --workspace "my-workspace" \
  --project "my-project" \
  --skill "sop" \
  --time-range 30
```

#### 可用参数

**必需参数：**
- `--employee`, `-e` **(必需)**: 数字员工名称
- `--message` **(必需)**: 要发送的消息内容
- `--thread-id` 或 `--create-new-thread` **(二选一)**：
  - `--thread-id`: 使用已有的会话线程ID
  - `--create-new-thread`: 自动创建新的会话线程

**可选参数：**
- `--region`, `-r`: 区域（默认："cn-hangzhou"）
- `--workspace`, `-w`: 工作空间（默认："test-workspace"）
- `--project`, `-p`: 项目名称（默认："test-project"）
- `--skill`: 使用的技能（默认："sop"）
- `--time-range`: 时间范围（分钟），用于查询数据（默认：15）
- `--from-time`: 开始时间（Unix时间戳，0表示自动计算）
- `--to-time`: 结束时间（Unix时间戳，0表示当前时间）
- `--thread-title`: 新线程的标题（与 `--create-new-thread` 配合使用）
- `--show-system`: 显示工具执行提示
- `--debug`: 启用调试模式，显示原始消息结构

#### 输出示例

```
==========================================
🤖 Employee: sop-test02
💬 Thread: thread-t8wmew-x0b4kpbj8oh9
📝 Message: 什么是SLS
🌍 Region: cn-hangzhou
📦 Workspace: test-workspace
📁 Project: test-project
🎯 Skill: sop
==========================================

🔄 开始接收响应（实时流式输出）...

[👤 User]
什么是SLS

[🤖 Assistant]
让我查询一下这个bucket的操作统计...

  🔧 工具调用:
    [1] QuerySLSLogs (success)
        结果:
          ## Query Summary
          
          - **Project**: oss-log-1654218965343050-cn-hangzhou
          - **Logstore**: oss-log-store
          - **Time Range**: 2026-01-16 09:15:14 ~ 2026-01-16 09:30:14
          - **Progress**: Complete
          - **Processed Rows**: 1
          
          ## Query Results
          
          | operation | count |
          |---|---|
          | GetObject | 3 |

[🤖 Assistant]
根据查询结果，在最近15分钟内，sls-sop-dev 这个bucket有3次 GetObject 操作...

✅ SSE流结束

==========================================
✅ 聊天请求完成!
📝 RequestId: 12345678-1234-1234-1234-123456789012
📝 TraceId: 87654321-4321-4321-4321-210987654321
==========================================
```

**输出优化说明：**
- 同一角色的连续消息会合并显示，只显示一次角色标识
- 流式文本会实时输出，无冗余信息
- 角色切换时会自动换行并显示新的角色标识

**查看工具执行提示：**

如果想知道数字员工何时在后台执行工具，可以使用 `--show-system` 标志：

```bash
./sop-chat chat \
  --employee "sop-test02" \
  --thread-id "thread-xxx" \
  --message "查询bucket操作" \
  --show-system
```

输出会包含工具执行提示：
```
[🤖 Assistant]
正在处理您的请求...

[⚙️  正在执行工具调用...]

[🤖 Assistant]
根据查询结果，bucket有以下操作...
```

#### 特性说明

1. **实时流式输出**：响应内容会实时显示，无需等待完整响应
2. **多角色消息**：自动区分并标识不同角色的消息：
   - 👤 User：用户消息
   - 🤖 Assistant：数字员工回复
   - 🔧 工具调用：显示工具名称、函数和参数
3. **工具调用可见**：自动显示数字员工调用的工具信息
4. **详细信息**：显示工具调用的详细参数和执行结果
5. **请求追踪**：返回 RequestId 和 TraceId 用于问题排查

### 5. 查看帮助

```bash
# 查看所有命令
./sop-chat --help

# 查看 employee 命令帮助
./sop-chat employee --help

# 查看 create 子命令帮助
./sop-chat employee create --help

# 查看 list 子命令帮助
./sop-chat employee list --help

# 查看 thread 命令帮助
./sop-chat thread --help

# 查看 thread list 子命令帮助
./sop-chat thread list --help

# 查看 thread create 子命令帮助
./sop-chat thread create --help

# 查看 chat 命令帮助
./sop-chat chat --help
```

## 测试

项目包含完整的测试套件：

```bash
# 运行所有测试
go test -v

# 运行特定测试
go test -v -run TestCreateChatThread
go test -v -run TestListThread
go test -v -run TestChat
go test -v -run TestChatEmployee
```

## 项目结构

```
backend/
├── main.go           # CLI 工具主入口和命令定义
├── utils.go          # 公共工具函数（CMS 客户端设置等）
├── main_test.go      # 测试文件
├── example.sh        # 使用示例脚本
├── go.mod            # Go 依赖管理
├── go.sum            # 依赖校验文件
├── README.md         # 本文档（完整文档）
├── QUICK_START.md    # 快速开始指南（推荐新用户阅读）
├── COMMANDS.md       # 命令速查表
└── DEBUG_GUIDE.md    # 调试指南
```

## 依赖

- `github.com/spf13/cobra` - CLI 框架
- `github.com/alibabacloud-go/cms-20240330/v4` - 阿里云 CMS SDK
- `github.com/alibabacloud-go/tea` - 阿里云 Tea 框架
- `github.com/joho/godotenv` - 环境变量加载

## 故障排查

### 错误：TEST_ACCESS_KEY_ID environment variable is required

确保已设置必需的环境变量。可以：
1. 在当前目录创建 `.env` 文件并配置变量
2. 或者在 shell 中导出环境变量：
   ```bash
   export TEST_ACCESS_KEY_ID=your-key
   export TEST_ACCESS_KEY_SECRET=your-secret
   ```

### 错误：failed to create CMS client

检查：
1. Access Key ID 和 Secret 是否正确
2. Endpoint 配置是否正确
3. 网络连接是否正常

### 工具调用信息显示

**最新版本已支持显示工具调用！** 当数字员工调用工具时，CLI会自动显示工具的名称、函数和参数。

如果想查看更详细的工具调用信息，可以使用调试模式：

```bash
./sop-chat chat \
  --employee "sop-test02" \
  --thread-id "thread-xxx" \
  --message "查询bucket的操作" \
  --debug
```

**正常的调试输出：**
```
[DEBUG] Role: assistant, Type: 
[DEBUG] Contents: [map[type:spin_text value:正在处理...]]

[DEBUG] Role: system, Type: 
# ← 这里表示工具在后台执行

[DEBUG] Role: assistant, Type: 
[DEBUG] Contents: [map[type:text value:根据查询结果...]]
```

**说明：**
- `spin_text` 表示正在处理请求（可能在调用工具）
- `system` 空消息是工具执行的分隔点
- 之后的消息包含工具执行的结果

详细说明请查看 [DEBUG_GUIDE.md](./DEBUG_GUIDE.md)

## 许可

本项目使用的 CMS SDK 遵循其原始许可证。
