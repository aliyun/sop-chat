# 验证查询

对 `query_pipeline.json` 中 selected + extra 的查询进行语法验证，分三阶段执行：

### a) 派生 executable_query

运行脚本，从 `normalized_query`（Step 7 输出）派生可执行查询：

```bash
python3 scripts/prepare_validation.py <logstore_dir>
```

派生规则（单一正则，无需 search/SQL 区分）：

- `<var;default>` → 取 `default` 值（类型天然正确）
- `<var>` → `'xxx'`（安全兜底）

输出：
- `parsed/query_validation.json`（始终生成）
- `parsed/query_validation_LLM.json`（仅当存在正则无法处理的残留占位符时生成）

**LLM 处理残留占位符**：脚本执行后，**无条件检查** `parsed/query_validation_LLM.json` 是否存在：

- **不存在** → 跳过，进入 Step 8b
- **存在** → LLM **必须**逐条处理该文件中的条目：
  1. 审视每条 `executable_query`，将其中的占位符替换为语法合法的值
  2. 直接编辑文件中对应条目的 `executable_query`

`query_validation_LLM.json` 不删除，作为 LLM 处理记录留存。

**LLM 兜底**：若后续验证因类型不匹配失败（如 SQL 数值上下文中 `'xxx'` 导致的类型错误），由 LLM 修复 `executable_query` 中的对应占位符为语法合法的值，然后重新验证。

### b) 调用验证脚本

```bash
python3 scripts/validate_queries.py \
  --project <project> --logstore <logstore> \
  <logstore_dir>
```

- 执行命令时须在**非沙箱环境**中运行
- 脚本自动发现 `parsed/query_validation.json` + `parsed/query_validation_LLM.json`（如存在）
- 逐条调用 `aliyun sls GetLogsV2`（`line=0`，仅检查语法）
- 验证结果（`pass`/`error`）写回原文件

### c) 脚本后处理

运行后处理脚本自动完成验证结果的应用：

```bash
python3 scripts/apply_validation.py <logstore_dir>
```

脚本读取 `parsed/query_validation.json` + `parsed/query_validation_LLM.json`（如存在），自动执行：
- 从 `query_pipeline.json` 的 `selected` 和 `extra` 中移除 `pass=false` 的条目
- `selected` 中失败的 query，从 `extra` 头部依次递补（保持 LLM 的代表性排序，标记 `"backfilled": true`）
- 更新 `stats.selected`、`stats.extra` 为实际数量
- 在 `query_pipeline.json` 顶层添加 `validation` 对象：

```json
{
  "validation": {
    "total": 48,
    "pass": 46,
    "fail": 2,
    "backfilled": 1
  }
}
```

- **验证失败的 query 不得保留在 selected 或 extra 数组中**

> ⚠️ `stats` 从实际数组长度重新计算：`stats.selected = len(selected)`, `stats.extra = len(extra)`, `stats.input` 保持不变。

**记录步骤成功**：`python3 scripts/update_status.py <project_dir> --mark-step <logstore> --step 8`
