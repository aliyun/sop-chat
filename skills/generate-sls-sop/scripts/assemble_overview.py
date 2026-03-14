#!/usr/bin/env python3
"""
Assemble overview.md from pre-generated fragments and LLM-provided metadata.

This script is a deterministic assembler — it reads fragments produced by
prepare_logstore.py (Step 2), LLM-filled fields_table.md (Step 5), and
render_queries.py (Step 9), then concatenates them into the final overview.md.

The only LLM-generated inputs are --name and --description (passed via CLI).

Usage:
    python assemble_overview.py <input_dir> --name "..." --description "..."

Input files (read from <input_dir>):
    fragments/datasource.md          (required)
    fragments/fields_table.md        (required)
    fragments/queries_selected.md    (optional — when no queries, render skips it)
    fragments/queries_extra.md       (optional — triggers pointer line + copy)
    fragments/common_values.md       (optional — appended when present)
    parsed/query_pipeline.json       (required when queries_extra.md exists)
    skill_options.json               (required — provides output_path)

Output:
    <output_path>/overview.md        (always)
    <output_path>/queries_extra.md   (only when fragments/queries_extra.md exists)

stdout: JSON summary {"output_path": "...", "queries_extra_copied": true/false}
stderr: progress messages
"""

import argparse
import json
import os
import shutil
import sys


def log(msg: str):
    """Print progress/summary info to stderr."""
    print(msg, file=sys.stderr, flush=True)


def read_file(path: str) -> str | None:
    """Read a text file, return None if not found."""
    if not os.path.exists(path):
        return None
    with open(path, "r", encoding="utf-8") as f:
        return f.read()


def load_json(path: str) -> dict | list | None:
    """Load a JSON file, return None if not found."""
    if not os.path.exists(path):
        return None
    with open(path, "r", encoding="utf-8") as f:
        return json.load(f)


def build_frontmatter(name: str, description: str) -> str:
    """Build YAML frontmatter block."""
    return f"---\nname: {name}\ndescription: {description}\n---"


USAGE_INSTRUCTION_WITH_EXTRA = """## 使用说明

- 本 logstore 的查询语法较复杂，建议优先使用本文档的查询示例，找不到时再参考 queries_extra.md，尽量避免自行编写。
- 变量取值优先参考本文档的「常见值速查」部分。"""

USAGE_INSTRUCTION_WITHOUT_EXTRA = """## 使用说明

- 本 logstore 的查询语法较复杂，建议尽量使用本文档中的查询示例，避免自行编写。
- 变量取值优先参考本文档的「常见值速查」部分。"""


def insert_extra_pointer(selected_md: str, selected_count: int, extra_count: int) -> str:
    """Insert a pointer line after the ## heading in queries_selected.md.

    The pointer line format:
        > 本节收录 {N} 条精选查询。更多查询参见 [queries_extra.md](queries_extra.md)（{M} 条补充）。
    """
    heading = "## 查询示例"
    pointer = (
        f"> 本节收录 {selected_count} 条精选查询。"
        f"更多查询参见 [queries_extra.md](queries_extra.md)（{extra_count} 条补充）。"
    )

    # Insert pointer line right after the heading
    if heading in selected_md:
        selected_md = selected_md.replace(
            heading + "\n",
            heading + "\n\n" + pointer + "\n",
            1,
        )
    else:
        log(f"WARNING: '{heading}' not found in queries_selected.md, "
            "prepending pointer line")
        selected_md = pointer + "\n\n" + selected_md

    return selected_md


def main():
    parser = argparse.ArgumentParser(
        description="Assemble overview.md from fragments and LLM metadata."
    )
    parser.add_argument("input_dir", help="Logstore input directory")
    parser.add_argument("--name", required=True, help="Logstore display name")
    parser.add_argument("--description", required=True,
                        help="One-line description of the logstore")
    args = parser.parse_args()

    input_dir = args.input_dir
    fragments_dir = os.path.join(input_dir, "fragments")
    parsed_dir = os.path.join(input_dir, "parsed")

    # --- Load skill_options.json for output_path ---
    skill_options = load_json(os.path.join(input_dir, "skill_options.json"))
    if not skill_options or "output_path" not in skill_options:
        log("ERROR: skill_options.json missing or lacks 'output_path'")
        sys.exit(1)

    output_path = skill_options["output_path"]
    output_dir = os.path.dirname(output_path)

    # --- Read required fragments ---
    datasource_md = read_file(os.path.join(fragments_dir, "datasource.md"))
    if datasource_md is None:
        log("ERROR: fragments/datasource.md not found")
        sys.exit(1)

    fields_table_md = read_file(os.path.join(fragments_dir, "fields_table.md"))
    if fields_table_md is None:
        log("ERROR: fragments/fields_table.md not found")
        sys.exit(1)

    queries_selected_md = read_file(os.path.join(fragments_dir, "queries_selected.md"))
    # When no queries, render_queries.py skips queries_selected.md — allow missing

    # --- Handle queries_extra.md (conditional) ---
    queries_extra_path = os.path.join(fragments_dir, "queries_extra.md")
    queries_extra_copied = False

    if os.path.exists(queries_extra_path):
        # Read pipeline to get counts
        pipeline = load_json(os.path.join(parsed_dir, "query_pipeline.json"))
        if pipeline is None:
            log("ERROR: queries_extra.md exists but parsed/query_pipeline.json not found")
            sys.exit(1)

        selected_count = len(pipeline.get("selected", []))
        extra_count = len(pipeline.get("extra", []))

        # Insert pointer line into selected_md (render ensures it exists when extra exists)
        if queries_selected_md is None:
            log("ERROR: queries_extra.md exists but queries_selected.md missing (inconsistent)")
            sys.exit(1)
        queries_selected_md = insert_extra_pointer(
            queries_selected_md, selected_count, extra_count
        )

        # Copy queries_extra.md to output directory
        os.makedirs(output_dir, exist_ok=True)
        dest_extra = os.path.join(output_dir, "queries_extra.md")
        shutil.copy2(queries_extra_path, dest_extra)
        queries_extra_copied = True
        log(f"Copied queries_extra.md to {dest_extra}")

    # --- Read optional fragments ---
    common_values_md = read_file(os.path.join(fragments_dir, "common_values.md"))

    # --- Assemble overview.md ---
    usage_instruction = USAGE_INSTRUCTION_WITH_EXTRA if queries_extra_copied else USAGE_INSTRUCTION_WITHOUT_EXTRA
    sections = [
        build_frontmatter(args.name, args.description),
        usage_instruction,
        datasource_md.rstrip("\n"),
        fields_table_md.rstrip("\n"),
    ]
    if queries_selected_md is not None:
        sections.append(queries_selected_md.rstrip("\n"))
    if common_values_md is not None:
        sections.append(common_values_md.rstrip("\n"))

    content = "\n\n".join(sections) + "\n"

    # --- Write output ---
    os.makedirs(output_dir, exist_ok=True)
    with open(output_path, "w", encoding="utf-8") as f:
        f.write(content)
    log(f"Assembled {output_path}")

    # --- Summary to stdout ---
    summary = {
        "output_path": output_path,
        "queries_extra_copied": queries_extra_copied,
    }
    print(json.dumps(summary, ensure_ascii=False))


if __name__ == "__main__":
    main()
