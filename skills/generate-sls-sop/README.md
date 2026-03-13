# generate-sls-sop

将 SLS project 或本地数据目录转换为结构化的 overview.md SOP 文档。

## 功能概览

- **自动拉取数据**：从阿里云 SLS 拉取 index、dashboard、alert、saved_search、scheduled_sql
- **智能精选查询**：LLM 从候选池中精选有代表性的查询，自动去重、脱敏、分类、标注
- **参考文档融合**：首次生成或重跑时，可指定已有 SOP 文档作为参考，其中的查询优先保留
- **断点续跑**：运行中断后自动恢复进度，从上次停止的 logstore 继续
- **数据更新重跑**：SLS 新增 dashboard/alert 等数据后，可重新拉取并生成
- **查询语法验证**（可选，默认关闭）：调用 SLS API 验证查询语法，自动移除失败项并递补
- **质量审计**（可选）：对生成结果进行语义审计，发现标题不准确、分类不合理、清理遗漏等问题

详见 [SKILL.md](SKILL.md)。

## 快速开始

以**存放 SOP 文档的项目仓库**作为 workspace 打开，然后对 Agent 说：

- `帮我生成 <project-name> 的 SOP 文档` -- 从 SLS 拉取
- `帮我从 .input/my-project/ 生成 SOP` -- 从本地数据
- `继续上次的 SOP 生成` -- 断点续跑
- `帮我生成 <project-name> 的 SOP 文档，并验证查询语法` -- 开启查询验证
- `对已生成的 SOP 做质量审计` -- 质量审计（建议新会话）

详细步骤见 [SKILL.md](SKILL.md)。

## 安装

使用 npx 一键安装到所有支持的 Agent 平台（Cursor、Claude Code、Cline 等）：

```bash
npx skills add https://github.com/aliyun/sop-chat --skill generate-sls-sop -g --all
```

或手动 clone 到平台对应的 skills 目录：

| 平台 | 项目级 | 用户级 |
|------|--------|--------|
| Claude Code | `.claude/skills/generate-sls-sop` | `~/.claude/skills/generate-sls-sop` |
| Cursor | `.cursor/skills/generate-sls-sop` | `~/.cursor/skills/generate-sls-sop` |
| Qoder | `.qoder/skills/generate-sls-sop` | `~/.qoder/skills/generate-sls-sop` |

```bash
git clone https://github.com/aliyun/sop-chat.git
cp -r sop-chat/skills/generate-sls-sop <上表中的目标路径>
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

如果使用 npx 安装，可以用对应的卸载命令：

```bash
npx skills remove generate-sls-sop -g
```

## License

MIT
