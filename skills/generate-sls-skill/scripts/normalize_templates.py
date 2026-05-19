#!/usr/bin/env python3
"""
Normalize all template syntaxes in query_pipeline.json to unified <var;default> / <var> format.

Step 7 of the SKILL knowledge generation pipeline — pure script, no LLM.

Rules (applied in order, priority by position):
  R1: {{__TASK_SQL_START_TS__}} / {{__TASK_SQL_END_TS__}} → <任务起始时间;1700000000> / <任务结束时间;1700003600>
  R2: ${{var|default}} → <var;default>
  R3: ${{var}} → <var>
  R4: ${var} → <var;default> (if token_defaults has key) or <var>
  R5: {{ var }} → <var>

Pre-existing <...> placeholders are naturally skipped — no rule's input pattern
matches angle-bracket syntax.

Usage:
    python normalize_templates.py <logstore_dir>

Input:  <logstore_dir>/parsed/query_pipeline.json (must have 'query' and optional 'token_defaults')
Output: <logstore_dir>/parsed/query_pipeline.json (updated: adds 'normalized_query' and 'pre_cleaned_query' to each entry)
"""

import json
import os
import re
import sys
from dataclasses import dataclass
from typing import Callable

from placeholder_re import RE_WITH_DEFAULT


def log(msg: str):
    print(msg, file=sys.stderr, flush=True)


@dataclass
class NormRule:
    name: str
    pattern: str
    replacement: Callable


def build_rules(token_defaults: dict | None = None) -> list[NormRule]:
    """Build the ordered list of normalization rules.

    token_defaults is used by R4 to inject default values for ${var} patterns.
    """
    defaults = token_defaults or {}
    return [
        # R1: 定时SQL系统变量 — 必须先于 R5，否则 {{__TASK_*__}} 被 R5 误捕
        NormRule("R1_start", r'\{\{__TASK_SQL_START_TS__\}\}',
                 lambda m: '<任务起始时间;1700000000>'),
        NormRule("R1_end", r'\{\{__TASK_SQL_END_TS__\}\}',
                 lambda m: '<任务结束时间;1700003600>'),
        # R2: $双花括号+默认值（变量名可含 : 如 __tag__:__job_name__，| 两侧可有空格）
        NormRule("R2", r'\$\{\{([\w:.]+)\s*\|\s*([^}]*)\}\}',
                 lambda m: f'<{m.group(1)};{m.group(2).strip()}>'),
        # R3: $双花括号无默认值（变量名可含 :）
        NormRule("R3", r'\$\{\{([\w:.]+)\}\}',
                 lambda m: f'<{m.group(1)}>'),
        # R4: $单花括号 — 利用 token_defaults 注入默认值
        NormRule("R4", r'\$\{([\w:.]+)\}',
                 lambda m: (f'<{m.group(1)};{defaults[m.group(1)]}>'
                            if m.group(1) in defaults
                            else f'<{m.group(1)}>')),
        # R5: 空格双花括号
        NormRule("R5", r'\{\{\s+(\w+)\s+\}\}',
                 lambda m: f'<{m.group(1)}>'),
    ]


def strip_defaults(normalized: str) -> str:
    """Strip ;default from <var;default> placeholders, keeping <var>.

    This produces a pre_cleaned_query that removes the internal ;default
    values (only needed for Step 8 executable_query derivation) while
    preserving the original variable names unchanged.
    """
    return RE_WITH_DEFAULT.sub(r'<\1>', normalized)


def normalize_query(query: str, token_defaults: dict | None = None) -> str:
    """Apply all normalization rules to a query string.

    Returns the normalized query with all template syntaxes converted to
    <var;default> or <var> format.
    """
    result = query
    for rule in build_rules(token_defaults):
        result = re.sub(rule.pattern, rule.replacement, result)
    return result


def main():
    if len(sys.argv) < 2:
        print(f"Usage: {sys.argv[0]} <logstore_dir>", file=sys.stderr)
        sys.exit(1)

    logstore_dir = sys.argv[1]
    parsed_dir = os.path.join(logstore_dir, "parsed")
    pipeline_path = os.path.join(parsed_dir, "query_pipeline.json")

    if not os.path.exists(pipeline_path):
        log(f"ERROR: {pipeline_path} not found")
        sys.exit(1)

    with open(pipeline_path, "r", encoding="utf-8") as f:
        pipeline = json.load(f)

    stats = {"total": 0, "rules_hit": {}}

    for list_name in ("selected", "extra"):
        for entry in pipeline.get(list_name, []):
            query = entry.get("query", "")
            td = entry.get("token_defaults", {})
            normalized = normalize_query(query, td if td else None)
            entry["normalized_query"] = normalized
            entry["pre_cleaned_query"] = strip_defaults(normalized)
            stats["total"] += 1

            # Count which rules matched (for diagnostics)
            if normalized != query:
                for rule in build_rules(td if td else None):
                    if re.search(rule.pattern, query):
                        stats["rules_hit"][rule.name] = stats["rules_hit"].get(rule.name, 0) + 1

    with open(pipeline_path, "w", encoding="utf-8") as f:
        json.dump(pipeline, f, ensure_ascii=False, indent=2)

    log(f"Normalized {stats['total']} queries. Rules hit: {stats['rules_hit']}")


if __name__ == "__main__":
    main()
