#!/usr/bin/env python3
"""
Render fields.json + field_annotations.json into fields_table.md.

Reads the raw field list and LLM-generated descriptions, then produces
a Markdown field-reference table identical in format to the old
prepare_logstore.py skeleton — but with real descriptions instead of
type placeholders.

Usage:
    python render_fields.py <logstore_dir>

Input files (read from <logstore_dir>):
    - parsed/fields.json (required)
    - parsed/field_annotations.json (required)

Output:
    - fragments/fields_table.md
"""

import json
import os
import sys
from pathlib import Path


def load_json(path: str):
    with open(path, "r", encoding="utf-8") as f:
        return json.load(f)


def render_fields_table(fields: list[dict], annotations: list[dict]) -> str:
    """Merge fields + annotations and render the Markdown table.

    ``annotations`` is a list of ``{"field": ..., "desc": ...}`` dicts
    produced by the LLM in Step 5.  Descriptions are matched by field
    name; any field without a matching annotation falls back to its
    type as ``(type)``.
    """
    desc_map: dict[str, str] = {a["field"]: a["desc"] for a in annotations}

    has_nested = any("." in f["field"] for f in fields)

    lines: list[str] = [
        "## 字段参考",
        "",
        "> **查询格式**：`search | sql`（search 为过滤条件，sql 为分析语句）",
    ]

    if has_nested:
        lines += [
            ">",
            "> **嵌套字段引用规则**（含 `.` 的字段）：",
            "> - search 部分（`|` 前）：禁止使用别名（别名会导致查询失败）",
            ">   - 正确：`nested.field.name : value`",
            ">   - 错误：`alias : value`（查询会返回空结果或报错）",
            "> - sql 部分（`|` 后）：可用双引号包裹或别名（如 `\"nested.field.name\"` 或 `alias`）",
        ]

    lines += [
        "",
        "| 字段 | 别名 | 说明 |",
        "|------|------|------|",
    ]

    for f in sorted(fields, key=lambda x: x["field"]):
        field_name = f["field"]
        alias = f.get("alias", "") or "-"
        if not alias:
            alias = "-"
        field_type = f.get("type", "text")

        display_name = f'"{field_name}"' if "." in field_name else field_name
        desc = desc_map.get(field_name, f"({field_type})")

        lines.append(f"| {display_name} | {alias} | {desc} |")

    return "\n".join(lines)


def main():
    if len(sys.argv) != 2:
        print(f"Usage: {sys.argv[0]} <logstore_dir>", file=sys.stderr)
        sys.exit(1)

    logstore_dir = sys.argv[1]
    parsed_dir = os.path.join(logstore_dir, "parsed")
    fragments_dir = os.path.join(logstore_dir, "fragments")

    fields_path = os.path.join(parsed_dir, "fields.json")
    annotations_path = os.path.join(parsed_dir, "field_annotations.json")

    if not os.path.isfile(fields_path):
        print(f"Error: {fields_path} not found", file=sys.stderr)
        sys.exit(1)
    if not os.path.isfile(annotations_path):
        print(f"Error: {annotations_path} not found", file=sys.stderr)
        sys.exit(1)

    fields = load_json(fields_path)
    annotations = load_json(annotations_path)

    os.makedirs(fragments_dir, exist_ok=True)

    md = render_fields_table(fields, annotations)
    out_path = os.path.join(fragments_dir, "fields_table.md")
    with open(out_path, "w", encoding="utf-8") as f:
        f.write(md)

    print(f"[render_fields] wrote {out_path} ({len(fields)} fields)",
          file=sys.stderr, flush=True)


if __name__ == "__main__":
    main()
