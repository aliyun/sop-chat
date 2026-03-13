# 输出路径命名规则

## 输入

`<project_dir>/project_summary.json`（Step 2 输出），包含：
- `logstores[]`：每个有效 logstore 的 `name`、`deduped_queries_count`、`deduped_source_dist`、`has_reference`、`fields_count`
- `errors[]`：Step 2 处理失败的 logstore

## 名称简化

从原始名称中去除环境特定信息，保留语义核心作为输出目录名。

**核心原则**：移除地域、环境、实例/集群 ID 等标识，保留描述服务/功能的部分。

移除模式（LLM 启发式参考，非脚本执行）：
| 模式 | 参考正则 | 示例 |
|------|----------|------|
| 十六进制串 | `[0-9a-f]{8,}` | `a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6` |
| 数字 UID | `\d{10,}` | `1234567890123456` |
| 地域标识 | `(cn|us|ap|eu|me)-[a-z]+-?\d*` | `cn-hangzhou`, `us-west-1` |
| 环境标识 | `(pre|prod|test|dev|staging)` | `app-prod`, `service-test` |

保留模式（不应移除）：
- 产品/服务前缀（如 `xxx_`、`yyy-`）：这些标识服务归属，非环境信息
- 功能描述词（如 `_logs`、`_metrics`、`-worker`）：描述 logstore 用途
- 短数字后缀（如 `-1`、`-2`）：版本或分片标识，非 UID

> **判断标准**：移除后名称是否仍能准确描述该 logstore 的功能？若不能，则不应移除。

> **注意**：正则仅供参考，LLM 需结合语义判断，避免误删（见示例表格）。

清理规则：
- 移除后产生的连续分隔符（`--`、`__`）合并为单个
- 移除首尾分隔符
- **保持原有分隔符风格**：若原名使用下划线（`_`），简化名也用下划线；若原名使用中划线（`-`），简化名也用中划线；混用时保持各部分原样

> 不匹配上述模式的内容默认保留，无需枚举。

示例：

| 原始名称 | 简化结果 | 说明 |
|----------|----------|------|
| `service-a1b2c3d4e5f6a7b8c9d0` | `service` | 移除 hex 串 |
| `worker-cn-hangzhou-metrics` | `worker-metrics` | 移除地域，保持中划线 |
| `backend-1234567890123456-cn-shanghai` | `backend` | 移除 UID + 地域 |
| `api-gateway-prod` | `api-gateway` | 移除环境标识，保持中划线 |
| `etl_worker_1234567890123456` | `etl_worker` | 移除 UID，保持下划线 |
| `xxx_service_logs` | `xxx_service_logs` | `xxx_` 是产品前缀，保留 |
| `task-runner-1` | `task-runner-1` | `-1` 是分片标识，保留 |
| `contest-service` | `contest-service` | `test` 是单词一部分，保留 |
| `user-service-test` | `user-service` | `test` 是独立环境标识，移除 |

## 去重

批量处理同一 project 下的多个 logstore 时，如果多个 logstore 简化后同名（如 `audit-c5de...`、`audit-c76a...` 都简化为 `audit`）：

**去重策略**：选 `deduped_queries_count` 最多的一个作为代表，其余标记为"去重跳过"。

## 确认输出路径

从 `<project_dir>/skill_options.json` 读取 `output_format`（默认 `SOP`），确定输出路径模板：
- `SOP`: `{根目录}/{project目录名}/{logstore目录名}/overview.md`
- `SKILL`: `{根目录}/{project目录名}/{logstore目录名}/SKILL.md`

匹配规则：完全一致优先，语义相似次之。

**根目录**：
1. 扫描当前工作目录下的所有一级子目录
2. `SOP` 格式：优先选择包含 SOP 特征文件的目录（如 `SOP.md`、`overview.md`），常见名称 `sop-docs`、`sls-docs`、`sls-sop`
3. `SKILL` 格式：优先选择包含 SKILL 特征文件的目录（如 `SKILL.md`），常见名称 `skills`
4. 在批量确认时展示，用户可修改

**Project 目录名**：
1. 扫描 `{根目录}/` 下所有一级子目录
2. 用简化后的 project 名匹配已有目录
3. 匹配到 → 使用已有目录名；无匹配 → 使用简化名

**Logstore 目录名**：
1. 扫描 `{根目录}/{project目录名}/` 下所有一级子目录
2. 用简化后的 logstore 名匹配已有目录
3. 匹配到 → 使用已有目录名；无匹配 → 使用简化名

示例：
| 层级 | 简化名 | 已有目录 | 最终目录名 |
|------|--------|----------|------------|
| 根目录 | - | `sls-sop/`（含 SOP.md）/ `skills/` | `sls-sop` / `skills` |
| project | `k8s-log` | `k8s` | `k8s` |
| logstore | `audit` | `my-audit` | `my-audit` |

## 批量确认

一次性展示所有 logstore，按状态分区（先跳过/失败，后确认）：

```markdown
**输出格式**: `{output_format}`
**根目录**: `{根目录}/`
**Project**: `{原始名}` → `{project目录名}`
**Total**: T = `logstores_count`（来自 project_summary.json）

### 去重跳过 (K 个)
| 原始名 | 保留 | 原因 |
|--------|------|------|
| audit-aaa | audit-bbb (12 queries) | 简化名冲突 |

### 失败 (E 个)
| 原始名 | 错误 |
|--------|------|
| logstore-y | Error: ... |

### 确认处理 (N = T - K - E 个)
| 原始名 | queries | 来源 | 简化名 | 输出路径 |
|--------|---------|------|--------|----------|
| logstore-abc123 | 138 [R] | dashboard:112 alert:17 | logstore | sop-docs/.../overview.md |
```

标记：`[R]` = 有参考文档

> **断点**：等待用户确认后再继续。用户可：
> - 修改根目录、任意简化名或输出路径
> - 将任意 logstore 改为跳过（不处理）

## 持久化

用户确认后，调用脚本一次性持久化（LLM 只需提供 selections JSON）：

```bash
python3 scripts/save_selections.py <project_dir> <<'SELECTIONS'
{
  "output_root": "{根目录}",
  "project_alias": "{project目录名}",
  "output_format": "{output_format}",
  "selections": {
    "{logstore_name}": "{output_path}",
    ...
  }
}
SELECTIONS
```

脚本输出：
1. `<project_dir>/selected_logstores.json`（补全 logstore_dir，供 Phase B / Step 12 读取）
2. 每个确认 logstore 的 `<logstore_dir>/skill_options.json` 追加 `output_path`（供 Step 11 读取）
