# 审计规则

---

## 审计范围

**审计目标**：评估 SKILL 在给定源数据下的决策质量，而非源数据本身的丰富度。

---

## 审计准备（脚本）

> 此步骤为纯脚本执行，无需 LLM。

```bash
python3 scripts/prepare_audit.py <project_dir> [--mode full|sample|targeted] [--logstores <ls1,ls2,...>]
```

**审计范围决策**：
- logstore 数量 ≤ 10 → 全量审计
- logstore 数量 > 10 → 抽样 20%（最少 10，最多 30），按高价值评分排序
- 用户显式指定 → 按指定列表审计

> 评分公式见 [prepare_audit.py](scripts/prepare_audit.py) `calculate_score()` 函数。

**输出**：
- `_audit/audit_plan.json`（含 `skipped` 字段，记录 overview.md 缺失等被跳过的 logstore）
- `_audit/<logstore>/audit_context.json`（每个待审计 logstore 的合并上下文）

---

## Per-logstore 查询审计

> **执行方式**（按平台能力选择）：
> - **并行**（平台支持多任务/子代理时）：为每个 logstore 启动独立审计任务
> - **串行**：逐个 logstore 执行
>
> **禁止**跨 logstore 交替执行，以免上下文污染。

对每个待审计 logstore 执行 **单轮** 查询审计，读 `_audit/<logstore>/audit_context.json` 一个文件，对每条查询一次性评估选择合理性 + 标题 + 分类 + cleaned_query。

**开始审核**：`python3 scripts/update_status.py <project_dir> --mark-audit-in-progress <logstore>`

### 输入

`_audit/<logstore>/audit_context.json`，包含：
- `auditable_queries`：入选查询的合并视图（pipeline + annotations），`pre_cleaned_query` 与 `cleaned_query` 并排
- `candidates_not_selected`：未入选候选的摘要（id、display_name、source_type）
- `pipeline_summary`：管道摘要（原始候选数、验证通过/失败数、有效候选数）
- `validation_failures`：验证失败的查询（id + 错误原因）
- 顶层元数据：`candidates_count`、`selected_count`、`extra_count`、`categories`

### 检查项（check 枚举）

**check 字段必须为以下枚举值之一**：

| check | 说明 |
|-------|------|
| `selection_duplicates` | 入选查询间的语义重复 |
| `title_accuracy` | 标题是否准确描述查询功能 |
| `category_reasonableness` | 分类是否合适 |
| `cleaned_query_correctness` | 清理是否正确（无遗漏、无过度清理） |

### 判断标准

**severity 约束**：仅输出 ERROR 和 WARN。不输出正面确认（OK）或纯观察（INFO）。无问题时该 check 不出现在 issues 中。

**cleaned_query 审计基准**：以 `pre_cleaned_query` 为基准（非原始 `query`）。`PRE_CLEANED` 表示 `pre_cleaned_query` 即最终版本，LLM 确认无需进一步清理。

**selection_duplicates 判断标准**：严格遵循 `query_select.md` 中「语义重复判断标准」。判断必须基于 query 内容（search 条件 + SQL 语句）的语义分析，元数据字段（display_name、dashboard_name 等）不构成重复判断依据。仅以下情况报 issue：(1) search 条件和 SQL 主体逻辑相同（包括语法等价，如 `not X` 与 `* not X`），仅输出字段/别名/limit 不同；(2) 仅正则等细节差异。search 条件相同但 SQL 逻辑有实质性差异（不同聚合函数、不同子查询结构）不视为重复。

**title_accuracy 客观标准**：仅当以下情况报 WARN：(1) 描述功能与查询实际功能不符；(2) 使用误导性术语；(3) 与其他查询标题难以区分。风格偏好不构成 issue。审计范围仅限 `title` 字段（SKILL 决策），不评价 `display_name`（源数据）。

**cleaned_query_correctness 范围**：仅检查 (1) 应清理但未清理的遗漏（硬编码值、敏感信息）；(2) 不应清理但被替换的过度清理（系统固定值如 API 名、错误码、方法名）。不评价是否应添加额外注释或说明。

### 输出格式

LLM 只输出发现的 issues（仅语义判断，不计算任何数字）：

```json
{
  "issues": [
    {
      "check": "title_accuracy",
      "query_id": "q3",
      "severity": "WARN",
      "detail": "标题未反映查询的核心功能"
    }
  ]
}
```

`issues` 中每项包含：
- `check`：检查项名称，**必须**为 `selection_duplicates`、`title_accuracy`、`category_reasonableness`、`cleaned_query_correctness` 之一
- `query_id` 或 `id`：问题定位
- `severity`：**必须**为 `ERROR` 或 `WARN`
- `detail`：问题描述

**文件**：`_audit/<logstore>/audit_issues.json`

### 后处理（脚本）

```bash
python3 scripts/finalize_audit.py <project_dir> <logstore>
```

脚本自动：
- 读取 `audit_issues.json`，过滤非 ERROR/WARN 条目
- 从 `audit_context.json` 读取上下文
- 计算 summary（total_issues、by_severity、by_check）
- 生成 `audit_result.json` 和 `audit_report.md`

**记录审核完成**：`python3 scripts/update_status.py <project_dir> --mark-audited <logstore>`

---

## 汇总报告（脚本）

```bash
python3 scripts/aggregate_audit.py <project_dir>
```

脚本读取所有 `_audit/*/audit_result.json`，聚合生成：
- `_audit/audit_data.json` - 结构化聚合数据
- `_audit/audit_summary.md` - 统计报告（问题数量、分布、典型问题示例）

---

## 审计完成

1. 运行 `python3 scripts/update_status.py <project_dir> --mark-audit-completed`
2. 展示 `_audit/audit_summary.md` 摘要给用户
3. 询问用户：
   a) "审计完成，是否需要根据审计结果修复具体问题？"
   b) "是否需要针对某类问题深入分析并生成 SKILL 改进建议？"
4. 若用户选择 b)：
   - 用户指定关注的 check 类型（如 "title_accuracy 问题比较多"）
   - LLM 读取 audit_data.json 中该 check 的全部 issues + 对应 rule 文件
   - 生成针对性的 `_audit/skill_improvements.md`（仅覆盖用户关注的问题）
5. 若用户无需进一步分析 → 审计流程结束
