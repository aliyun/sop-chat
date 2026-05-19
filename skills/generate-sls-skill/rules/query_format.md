# 清理 + 标注

为每条 query 生成 `title`、`category`、`cleaned_query` 注解字段。

## 输入

- `<logstore_dir>/parsed/query_pipeline.json` — `selected` + `extra` 全部 query
- 清理依据：**`pre_cleaned_query`**
- 标注依据：`display_name`、`dashboard_name`、`source_type`、query 语义

## 清理规则

LLM **必须逐条审视** `pre_cleaned_query` 判断是否需要清理；**`source_type` 为 `reference` 时**无需清理。

**无需清理时：** `"cleaned_query": "PRE_CLEANED"`（必须显式写出，不可省略）。含义：`pre_cleaned_query` 即最终版本，LLM 确认无需进一步清理。`source_type: reference` 同样使用。

**需要清理时** — 输出完整的修改后 query，仅适用于以下场景：

1. **硬编码值替换**
   - **替换**：project/logstore 名 → `<目标project>`、`<目标logstore>`
   - **替换**：UID、资源 ID、集群 ID、IP 等 → 语义占位符（如
  `<目标UID>`、`<IP>`）
   - **不替换**：系统固定值（API 名、错误码、方法名等）

2. **敏感信息脱敏**
   - AK/SK、Token、密码等凭据 → `<AccessKeyId>`、`<AccessKeySecret>`、`<Token>` 等占位符

## 标注规则

为每条 query 生成 `category` 和 `title` 标注字段：

- **`category`**：中文功能分类名（如"流量与大盘"、"告警与异常"），可参考 `dashboard_name` 但需归纳提炼
  - 每个 category 至少包含 2 条 query（selected + extra 合并统计），无法归入时优先合并到最相近类别
  - 功能相近的类别应合并
- **`title`**：语义化中文标题，可直接复用高质量的 `display_name`
  - 禁止使用：默认名（"新建图表"、"Markdown文本"）、空白、纯编号、单词泛化名（"filter"、"name"、"count"）、技术 ID

> **约束**：
> - selected / extra 各自内部标题必须唯一
> - `extra` 与 `selected` 适用完全相同的清理和标题规则，不得因数量多而降低质量
> - `source_type` 仅用于分类参考，不要求单独分组或标注来源
> - query 数量较多（>30）时可分批写入（先 selected 再 extra）

## 输出

写入 `<logstore_dir>/parsed/query_annotations.json`（2 空格缩进），仅包含注解字段，复用源 ID：

```json
[
  {
    "id": "q3",
    "title": "按告警类型统计最近异常次数",
    "category": "告警与异常",
    "cleaned_query": "PRE_CLEANED"
  },
  {
    "id": "r0",
    "title": "按 IP 查采集异常详情",
    "category": "告警与异常",
    "cleaned_query": "ip: <目标IP> and project_name: <目标project> | select alarm_type, alarm_message, config limit 50"
  },
  {
    "id": "q12",
    "title": "按版本统计实例数趋势",
    "category": "聚合与统计",
    "cleaned_query": "PRE_CLEANED"
  }
]
```

> `id` 复用 `query_pipeline.json` 源 ID。顺序：先 selected 再 extra（均按 pipeline 顺序）。长度必须等于 selected + extra 总条数。不要修改 `query_pipeline.json`，注解写入独立文件。

## 校验

`python3 scripts/validate_step.py <logstore_dir> annotations`

> 校验失败时修复所有 ERROR 后重新运行，最多重试 3 次。
> 仍未通过则执行：
>
> **记录步骤失败**：
> ```
> python3 scripts/update_status.py <project_dir> --mark-failed <logstore> --step 9 --errors-file /tmp/validate_errors_<logstore>.json
> ```
> ⚠️ 记录失败后**立即停止**当前 logstore，处理下一个。

**记录步骤成功**：`python3 scripts/update_status.py <project_dir> --mark-step <logstore> --step 9`
