# 查询精选规则

## 输入

- `<logstore_dir>/parsed/queries.json` — 候选池（必需）。每条 query 的 `id` 格式为 `q0, q1, ...`（由 `prepare_logstore.py` 生成）。
- `<logstore_dir>/parsed/reference_queries.json` — 参考查询（可选，Step 4 输出）。每条 query 的 `id` 格式为 `r0, r1, ...`（由 Step 4 生成）。

如果 `reference_queries.json` 存在，将其内容合并到候选池。

## 精选策略（LLM）

> ⚠️ **精选是语义理解任务，禁止退化为机械脚本式操作。** 必须由 LLM 逐条理解 query 的业务含义后，综合判断代表性再做取舍。

**通用约束**：语义重复的 query 不得同时入选。判断标准见下方「语义重复判断标准」。

- **候选 ≤20 条**：逐条检查语义重复，去重后将剩余全部放入 `selected`，`extra` 为空数组
- **候选 >20 条**：LLM 优选一批有代表性的 query（**总上限 60 条：selected 20 + extra 最多 40**），优选过程中自然排除语义重复，无需单独去重步骤。同时兼顾：
  - source_type 优先级：reference ≈ alert > dashboard > saved_search > scheduled_sql
    （理由：reference 经人工验证；alert 定义"什么算异常"，是排障第一入口）
  - SQL 结构多样性（避免同一 SQL 模板占满配额）
  - search 条件覆盖度（不同过滤条件 = 不同排障场景）
  当三者冲突时，倾向高优类型，同时尽量让每种 source_type 都有代表。
  其中最具代表性的 20 条放入 `selected`（候选 21-60 条时优先填满 20 条），余下的放入 `extra`。两个数组内均按代表性从高到低排列。

> 代码定义：scripts/prepare_logstore.py `_SOURCE_PRIORITY` — 调整优先级时两处同步修改。

> **SLS 查询结构**：`search_condition | SQL_statement`。`|` 前为搜索条件（过滤日志），`|` 后为 SQL 分析语句。下文的"search"和"SQL"分别指这两部分。

### 语义重复判断标准

以下情况视为语义重复，**仅保留一条**（优先保留高优 source_type）：

1. **冗余通配符**：search 条件仅在不影响语义的通配符上有差异
   - `not field:value` 等价于 `field:* not field:value`
   - `* and field:value` 等价于 `field:value`
2. **逻辑等价**：search 条件和 SQL 主体逻辑相同，仅输出字段或别名不同
3. **输出参数差异**：仅 `limit`、`offset` 等输出控制参数不同
4. **细节差异**：仅正则表达式细节等不影响核心功能的差异

以下情况**不视为重复**：

1. **不同过滤值**：search 条件包含不同的过滤值（如不同错误类型、不同角色）
2. **SQL 实质差异**：SQL 逻辑有实质性差异（不同聚合函数、不同 join）
3. **不同排障场景**：覆盖不同的排障场景

## 输出（LLM）

写入 `<logstore_dir>/parsed/query_selection.json`（2 空格缩进），**仅包含 ID 引用**：

```json
{
  "selected": ["r0", "q46", "q48", "q49", "q50"],
  "extra": ["q1", "q2", "q3", "q4"]
}
```

> ID 前缀天然区分来源：`q*` = queries.json，`r*` = reference_queries.json。

> ⚠️ **必须写入磁盘后再进入下一步。**

## 构建 pipeline（脚本）

写入 `query_selection.json` 后，立即运行脚本构建完整 pipeline：

```bash
python3 scripts/build_pipeline.py <logstore_dir>
```

脚本按 ID 从 queries.json + reference_queries.json 查找完整条目，构建 `<logstore_dir>/parsed/query_pipeline.json`。此文件是后续步骤（验证/格式化/渲染）的数据源。

## 校验

`python3 scripts/validate_step.py <logstore_dir> pipeline`

> 校验失败时修复所有 ERROR 后重新运行，最多重试 3 次。
> 仍未通过则执行：
>
> **记录步骤失败**：
> ```
> python3 scripts/update_status.py <project_dir> --mark-failed <logstore> --step 6 --errors-file /tmp/validate_errors_<logstore>.json
> ```
> ⚠️ 记录失败后**立即停止**当前 logstore，处理下一个。

**记录步骤成功**：`python3 scripts/update_status.py <project_dir> --mark-step <logstore> --step 6`
