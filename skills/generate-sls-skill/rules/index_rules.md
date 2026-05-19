# Project SKILL 入口规则

## 输入

从 `<project_dir>/selected_logstores.json` 读取：
- `output_root`：输出根目录
- `project_alias`：project 目录名
- `logstores[].output_path`：每个 logstore 的 reference 文档路径，格式为 `<output_root>/<project_alias>/references/<logstore_alias>.md`

## 输出

只生成或更新一个入口文件：

```text
<output_root>/<project_alias>/SKILL.md
```

不生成 `<output_root>/SKILL.md`，也不在 logstore 级目录生成 `SKILL.md`。

## 新建模板

当 `<output_root>/<project_alias>/SKILL.md` 不存在时，按以下结构新建：

```markdown
---
name: {project 可读名称}
description: {基于 logstore reference 概括该 project 的日志分析范围}
---

# {project 可读名称}

## 使用说明

- 根据问题所属系统或日志类型，优先查看下方 reference 文档。
- 查询语法复杂时，优先复用 reference 文档中的查询示例。

## Logstore References

| Logstore | 说明 |
|----------|------|
| [{logstore 可读名}](references/{logstore_alias}.md) | {description} |
```

其中：
- `name`：根据 project_alias 和子级内容推断简洁中文可读名。
- `description`：概括该 project 下所有 logstore 的功能范围。
- 表格行：从每个 logstore reference 文档的标题和首段描述提取；缺失时使用 logstore_alias。

## 更新规则

1. 读取所有 `logstores[].output_path`，确认路径均位于同一个 `<output_root>/<project_alias>/references/` 下。
2. 对每个 reference 文档生成一行相对链接：`references/<logstore_alias>.md`。
3. 若 `SKILL.md` 已存在：
   - 保留现有 frontmatter 和非 reference 章节内容。
   - 更新 `## Logstore References` 表格：新增缺失项，更新描述变化项，删除不在 manifest 中的旧项。
4. 若 `SKILL.md` 不存在：使用新建模板生成。
5. 不修改 `references/*.md` 内容；这些文件由 Step 11 生成。
