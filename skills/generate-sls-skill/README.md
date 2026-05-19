# generate-sls-skill

将 SLS project 或本地数据目录转换为结构化的 SKILL 知识文档。

## 功能概览

- **SKILL 知识文档输出**：生成 `project/SKILL.md` 入口和 `project/references/*.md` logstore 参考文档
- **自动拉取数据**：从阿里云 SLS 拉取 index、dashboard、alert、saved_search、scheduled_sql
- **智能精选查询**：LLM 从候选池中精选有代表性的查询，自动去重、脱敏、分类、标注
- **参考文档融合**：首次生成或重跑时，可指定已有文档作为参考，其中的查询优先保留
- **断点续跑**：运行中断后自动恢复进度，从上次停止的 logstore 继续
- **数据更新重跑**：SLS 新增 dashboard/alert 等数据后，可重新拉取并生成
- **查询语法验证**（可选，默认关闭）：调用 SLS API 验证查询语法，自动移除失败项并递补
- **质量审计**（可选）：对生成结果进行语义审计，发现标题不准确、分类不合理、清理遗漏等问题

详见 [SKILL.md](SKILL.md)。

## 快速开始

以**存放 SKILL 知识文档的项目仓库**作为 workspace 打开，然后对 Agent 说：

- `帮我生成 <project-name> 的 SKILL 文档` -- 从 SLS 拉取
- `帮我从 .input/my-project/ 生成 SKILL` -- 从本地数据生成
- `继续上次的 SKILL 生成` -- 断点续跑
- `帮我生成 <project-name> 的 SKILL 文档，并验证查询语法` -- 开启查询验证
- `对已生成的 SKILL 做质量审计` -- 质量审计（建议新会话）

详细步骤见 [SKILL.md](SKILL.md)。

## 安装

使用 `npx skills add` 安装。默认安装到当前 workspace 的项目级目录；加 `-g` 才会安装到用户全局目录。

常用客户端示例：

| 客户端 | `-a/--agent` |
|--------|--------------|
| Codex | `codex` |
| Claude Code | `claude-code` |
| Qoder | `qoder` |

项目级安装（推荐，在目标 workspace 根目录执行）：

```bash
npx skills add https://github.com/aliyun/sop-chat --skill generate-sls-skill -a codex claude-code qoder -y
```

全局安装：

```bash
npx skills add https://github.com/aliyun/sop-chat --skill generate-sls-skill -a codex claude-code qoder -g -y
```

## 前置依赖

- **Python 3**
- **aliyun CLI >= v3.0.308**（SLS 模式）：当输入为 SLS project 名称时需安装并完成鉴权配置（`aliyun sls` 子命令在 [v3.0.308](https://github.com/aliyun/aliyun-cli/releases/tag/v3.0.308) 中引入）
  - 安装：https://github.com/aliyun/aliyun-cli
  - 版本验证：`aliyun version`，确认版本号 >= 3.0.308
  - 鉴权配置：运行 `aliyun configure`，填入 AccessKey ID/Secret 和 Region（参考 [配置文档](https://help.aliyun.com/zh/cli/configure-credentials)）
  - 鉴权验证：`aliyun configure list`，确认存在已配置 AccessKey 的 profile
- **非沙箱环境**：fetch 与 validate_queries 等涉及 aliyun CLI 的步骤须在非沙箱环境中执行（沙箱会限制系统证书链访问，导致 TLS 失败）

## 卸载

项目级卸载（在安装时的 workspace 根目录执行）：

```bash
npx skills remove generate-sls-skill -a codex claude-code qoder -y
```

全局卸载：

```bash
npx skills remove generate-sls-skill -a codex claude-code qoder -g -y
```

