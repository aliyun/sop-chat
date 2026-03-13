# 索引更新规则

## 输入

从 `selected_logstores.json` 读取：
- `output_format`：决定索引文件名（`SOP` / `SKILL`，默认 `SOP`）
- `output_root`：根目录（原 `sop_root`）
- `logstores[].output_path`：各 logstore 输出路径

## 新建索引的默认模板

> 以下模板仅用于目录下**不存在**任何索引文件时的新建场景。如需调整索引最佳实践格式，只需修改此处模板。

### SOP 格式

**根目录索引**（`{根目录}/SOP.md`）：

```markdown
# SLS SOP

| 类目 | 说明 |
|------|------|
| [{子目录可读名}]({project目录名}/overview.md) | {description} |
```

**子目录索引**（`{dir}/overview.md`）：

```markdown
---
name: {目录可读名称}
description: {基于子级内容概括}
---

| 子项 | 说明 |
|------|------|
| [{子项可读名}]({project目录名}/{logstore目录名}/overview.md) | {description} |
```

### SKILL 格式

> SKILL 格式不生成根索引。

**子目录索引**（`{dir}/SKILL.md`）：

```markdown
---
name: {目录可读名称}
description: {基于子级内容概括}
---

| 子项 | 说明 |
|------|------|
| [{子项可读名}]({project目录名}/{logstore目录名}/SKILL.md) | {description} |
```

其中：
- `name`：根据目录简化名和子级内容，推断简洁的中文可读名称
- `description`：概括该目录下所有子级的功能范围
- 表格行：从每个子级的索引文件 frontmatter 提取 `name` 和 `description`；若子级是叶子 logstore，从其 `overview.md` 提取

> **路径规范**：所有索引中的链接路径必须是相对于 SOP 根目录的完整路径（如 `k8s-log/audit/overview.md`），而非相对于当前文件的路径（如 `audit/overview.md`）。这确保路径在不同上下文中可被正确解析。

## 逐级遍历算法

### 收集起点

从所有已确认 logstore 的 `skill_options.json` 中读取 `output_path`（仅 `output_path` 非 null 的），计算需要更新索引的目录集合：

1. 对每个 `output_path`，取其父目录的父级（即 project 级目录）
2. 对所有 project 级目录去重
3. 从每个 project 级目录开始，向上收集直到 SOP 根目录的所有中间目录
4. 对所有中间目录去重 → 得到最终的"需更新目录集合"

> 示例：3 个 logstore 的 output_path 都在 `sop-docs/my-project/` 下，则 project 级只有一个目录，向上只需遍历 `sop-docs/my-project/` 和 `sop-docs/` 两级。

### 对每一级目录执行

从最深层开始，逐级向上直到 SOP 根目录：

1. **探索**：扫描当前目录，寻找已有的索引文件
   - 不要假设固定文件名；根据文件内容判断——包含子目录链接/引用表格的 `.md` 或 `.yaml` 文件即为索引
   - 常见形式包括但不限于 `SOP.md`、`README.md`、`overview.md`、`overview.yaml`
2. **若已存在索引** → 按其现有格式更新：
   - 检查当前子级是否已收录
   - 未收录 → 在合适位置追加条目
   - 已收录但描述不一致 → 更新描述
   - 保持原有文件格式（markdown / yaml / 表格风格等）不变
3. **若不存在索引** → 根据 `output_format` 选择模板：
   - `SOP` 格式：
     - 根目录 → `SOP.md`
     - 子目录 → `overview.md`
   - `SKILL` 格式：
     - 根目录 → **跳过**（不生成根索引）
     - 子目录 → `SKILL.md`
4. **向上一级**，重复步骤 1-3，直到处理完 SOP 根目录

> 每个目录最多更新一次索引。如果同一目录下有多个新增/变更子级，在一次操作中全部处理。
