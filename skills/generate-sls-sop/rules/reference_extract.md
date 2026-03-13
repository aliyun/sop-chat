# 参考文档提取规则

读取 `skill_options.json` 中 `reference_source` 指定的参考文档（值为文件路径字符串），从中提取 SLS 查询语句，写入 `<logstore_dir>/parsed/reference_queries.json`。

## 提取规则

- **已知格式（SOP yaml）**：`query_sqls` 中的内容**一律提取，不做内容判断**（字段名即语义，无需二次审判）。`notes` 辅助推断标题和分类，其他字段（`fields`、`scenarios`、`faq` 等）不提取
- **通用格式**：LLM 自行判断，识别文档中的 SLS 查询语句
- **禁止**将非查询内容"翻译"或"改写"为 SLS 查询

## 输出格式

写入 `parsed/reference_queries.json`（与 `queries.json` 相同 schema，2 空格缩进）：

```json
[
  {
    "id": "r0",
    "source": "sop-docs/project/logstore.yaml",
    "source_type": "reference",
    "dashboard_name": "从 notes/上下文推断的功能名",
    "display_name": "从 notes/上下文推断的查询标题",
    "query": "SLS 查询语句",
    "logstore": "目标 logstore 名"
  }
]
```

- `id` 为 logstore 级全局唯一标识，格式 `r0, r1, r2...`，按数组顺序递增
- 空数组 `[]` 表示文档中无可提取的查询
- 被跳过的内容记录在终端输出中（供调试），不写入 JSON 文件
- Step 9 的改写报告（query_report.md）中会汇总参考文档处理情况

**记录步骤成功**：`python3 scripts/update_status.py <project_dir> --mark-step <logstore> --step 4`
