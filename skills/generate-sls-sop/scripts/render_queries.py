#!/usr/bin/env python3
"""
Render query_pipeline.json into markdown fragments and a report.

This script is a deterministic renderer — it reads query_pipeline.json
and merges query_annotations.json (if present), then produces:
  - fragments/queries_selected.md (when selected or extra is non-empty)
  - fragments/queries_extra.md (when extra is non-empty)
  - fragments/common_values.md (from queries.json + reference_queries.json + fields.json)
  - parsed/query_report.md (always)

Usage:
    python render_queries.py <input_dir>

Input files (read from <input_dir>):
    - parsed/query_pipeline.json (required)
    - parsed/query_annotations.json (optional, merged by id into pipeline entries)
    - parsed/prepare_summary.json (required, for waterfall numbers in report)
    - parsed/query_validation.json (optional, for failure report)
    - parsed/query_validation_LLM.json (optional, LLM-processed validation entries)
    - parsed/reference_queries.json (optional, for reference doc section)
    - parsed/queries.json (optional, for common values extraction)
    - parsed/fields.json (optional, for common values extraction)

Output files (written to <input_dir>):
    - fragments/queries_selected.md (only if selected or extra non-empty)
    - fragments/queries_extra.md (only if extra non-empty)
    - fragments/common_values.md (only if common values non-empty)
    - parsed/query_report.md

stderr: summary of what was generated
stdout: nothing
"""

import json
import os
import re
import sys
from collections import OrderedDict, defaultdict
from pathlib import Path

from placeholder_re import RE_PLACEHOLDER


def log(msg: str):
    """Print progress/summary info to stderr."""
    print(msg, file=sys.stderr, flush=True)


def load_json(path: str) -> dict | list | None:
    """Load a JSON file, return None if not found."""
    if not os.path.exists(path):
        return None
    with open(path, "r", encoding="utf-8") as f:
        return json.load(f)


# ---------------------------------------------------------------------------
# Common values extraction (copied from prepare_logstore.py for independence)
# ---------------------------------------------------------------------------

# Regex to detect 20+ consecutive hex characters (used for env-specific IDs)
_HEX_ID_RE = re.compile(r'[0-9a-f]{20,}', re.IGNORECASE)

# Fields whose values are free-text (sentence fragments from regex splitting).
_FREE_TEXT_FIELD_RE = re.compile(r'(?:msg|message)$|^(?:msg|message|error)$', re.IGNORECASE)

# Bare SQL keywords that leak from query field:value extraction
_SQL_KEYWORDS = {"select", "insert", "update", "delete", "from", "where", "join", "group", "order"}

# Placeholder / sentinel values
_PLACEHOLDER_VALUES = {"--", "-", "?", "null", "none", "_", "n/a", "na", "undefined"}

# Trivial boolean-like values (for post-pass single-value field filtering)
_TRIVIAL_VALUES = {"true", "false", "ok", "yes", "no"}

# Credential-related field names (exact match, case-insensitive)
_CREDENTIAL_FIELDS = {
    "accesskeyid", "accesskey", "accesskeysecret",
    "secretkey", "secretaccesskey",
    "password", "passwd", "pwd",
    "token", "securitytoken", "accesstoken", "authtoken",
    "secret",
    "credential", "credentials",
    "akid", "ak",
}

# Credential value patterns (e.g. Alibaba Cloud AccessKeyId)
_CREDENTIAL_VALUE_RE = re.compile(r'^LTAI[A-Za-z0-9]{12,}$')


def extract_common_values(queries: list[dict], fields: list[dict]) -> dict[str, list[str]]:
    """Extract common field values from the search part of queries (before |).

    Only processes the search part (before first |). Does NOT process SQL part
    (regex is unreliable for SQL, too much noise).

    Applies 7 noise-filtering rules to produce clean common values.
    """
    values = defaultdict(set)

    # Build set of json-type parent fields to filter out
    json_parent_fields = {f["field"] for f in fields if f.get("type") == "json" and "parent" not in f}

    # Pattern: field:value (standalone matching)
    standalone_pattern = re.compile(
        r'([\w.]+)'                        # field name
        r'\s*:\s*'                          # colon
        r'"?([^"\s,|()]+)"?',              # value
        re.IGNORECASE,
    )

    for q in queries:
        query = q["query"]
        # Extract search part (before first |)
        parts = query.split("|", 1)
        search_part = parts[0].strip()

        if not search_part:
            continue

        for match in standalone_pattern.finditer(search_part):
            field = match.group(1)
            value = match.group(2).strip('"').strip("'")

            # --- Original filters ---
            if value == "*" or "*" in value:
                continue
            if "\\" in value:
                continue
            if value.lower() in ("", "and", "or", "not"):
                continue
            if field.lower() in ("and", "or", "not"):
                continue
            if re.match(r'^\d+$', value):
                continue

            # --- Filter 1: Template / token variables ---
            if "${{" in value or "${" in value or "{{" in value:
                continue

            # --- Filter 1b: Angle-bracket placeholders from reference docs ---
            if value.startswith("<"):
                continue

            # --- Filter 2: Placeholder values ---
            if value.lower() in _PLACEHOLDER_VALUES:
                continue

            # --- Filter 3: __tag__ metadata leakage ---
            if field == "__tag__" or field.startswith("__tag__"):
                continue

            # --- Filter 4: Free-text message fields ---
            if _FREE_TEXT_FIELD_RE.search(field):
                continue

            # --- Filter 5: Environment-specific hex IDs ---
            if _HEX_ID_RE.search(value):
                continue

            # --- Filter 5b: Credential field names ---
            if field.lower() in _CREDENTIAL_FIELDS:
                continue

            # --- Filter 5c: Credential value patterns (e.g. Alibaba Cloud AK) ---
            if _CREDENTIAL_VALUE_RE.match(value):
                continue

            # --- Filter 6: Bare SQL keywords / debug commands ---
            if value.lower() in _SQL_KEYWORDS:
                continue
            if value.startswith(".") and len(value) > 1:
                continue

            values[field].add(value)

    # Filter out json-type parent fields
    for jf in json_parent_fields:
        values.pop(jf, None)

    # --- Filter 7 (post-pass): Remove trivial single-value fields ---
    result = {}
    for field, vals in sorted(values.items()):
        if not vals:
            continue
        sorted_vals = sorted(vals)
        # Single value that is a trivial boolean -> skip entire field
        if len(sorted_vals) == 1 and sorted_vals[0].lower() in _TRIVIAL_VALUES:
            continue
        result[field] = sorted_vals

    return result


def generate_common_values_md(common_values: dict[str, list[str]]) -> str | None:
    """Format common_values dict as markdown. Returns None if empty."""
    if not common_values:
        return None

    lines = ["## 常见值速查", ""]
    for field, vals in sorted(common_values.items()):
        lines.append(f"**{field}**：")
        lines.append("")
        lines.append("```")
        lines.append(", ".join(vals))
        lines.append("```")
        lines.append("")

    return "\n".join(lines).rstrip()


# ---------------------------------------------------------------------------
# Markdown rendering
# ---------------------------------------------------------------------------

def render_queries_md(entries: list[dict], header: str) -> str:
    """
    Render a list of query entries into markdown, grouped by category.

    Each entry must have: title, category, cleaned_query.
    """
    # Group by category, preserving original order
    groups: OrderedDict[str, list[dict]] = OrderedDict()
    for entry in entries:
        cat = entry.get("category", "其他")
        if cat not in groups:
            groups[cat] = []
        groups[cat].append(entry)

    lines = [header, ""]
    for category, items in groups.items():
        lines.append(f"### {category}")
        lines.append("")
        for item in items:
            title = item.get("title", "未命名查询")
            query_text = item.get("cleaned_query", item.get("query", ""))
            has_placeholder = bool(RE_PLACEHOLDER.search(query_text))
            base = "tpl_query" if has_placeholder else "query"
            suffix = " reserved" if item.get("source_type") == "reference" else ""
            fence = f"```{base}{suffix}"
            lines.append(f"**{title}**")
            lines.append("")
            lines.append(fence)
            lines.append(query_text)
            lines.append("```")
            lines.append("")

    return "\n".join(lines)


def render_selected_md(entries: list[dict]) -> str:
    """Render queries_selected.md."""
    return render_queries_md(entries, "## 查询示例")


def render_extra_md(entries: list[dict]) -> str:
    """Render queries_extra.md."""
    count = len(entries)
    header = (
        f"# 补充查询\n\n"
        f"> 本文件包含 {count} 条补充查询。精选查询参见 [overview.md](overview.md)。"
    )
    return render_queries_md(entries, header)


# ---------------------------------------------------------------------------
# Report rendering
# ---------------------------------------------------------------------------

def _format_dist(dist: dict) -> str:
    """Format a source_type distribution dict into a readable string.

    Example: {"dashboard": 52, "alert": 22} -> "dashboard 52 + alert 22"
    Sorted by count descending — no hardcoded type list.
    """
    parts = [f"{k} {v}" for k, v in sorted(dist.items(), key=lambda x: x[1], reverse=True) if v > 0]
    return " + ".join(parts)


def _removed_by_type(raw_dist: dict, deduped_dist: dict) -> dict:
    """Compute per-type removal counts between two distributions."""
    result = {}
    for k in raw_dist:
        diff = raw_dist[k] - deduped_dist.get(k, 0)
        if diff > 0:
            result[k] = diff
    return result


def render_report(
    pipeline: dict,
    input_dir: str,
    validation_result: list | None,
    reference_queries: list | None,
) -> str:
    """Render query_report.md from pipeline data.

    The overview section uses a **waterfall** format — each row shows the
    cumulative query count after that pipeline stage.  All numbers are
    computed from script-generated data (prepare_summary.json,
    reference_queries.json, query_validation.json), never from LLM-declared
    ``stats``.  Seven invariant assertions verify arithmetic consistency.
    """
    logstore_name = os.path.basename(os.path.normpath(input_dir))
    parsed_dir = os.path.join(input_dir, "parsed")

    selected = pipeline.get("selected", [])
    extra = pipeline.get("extra", [])

    # Load prepare_summary.json (new output from prepare_logstore.py)
    prepare_summary = load_json(os.path.join(parsed_dir, "prepare_summary.json"))

    lines = [f"# Query 改写报告：{logstore_name}", ""]

    # ===================================================================
    # 概览 — waterfall pipeline table
    # ===================================================================
    lines.append("## 概览")
    lines.append("")
    lines.append("| 阶段 | 数量 | 说明 |")
    lines.append("|------|------|------|")

    if prepare_summary:
        # ---------- Row 1: 提取 ----------
        raw_count = prepare_summary["raw_queries_count"]
        raw_dist = prepare_summary["raw_source_dist"]
        assert raw_count == sum(raw_dist.values()), \
            f"Invariant 1 failed: raw_queries_count({raw_count}) != sum(raw_source_dist)({sum(raw_dist.values())})"
        lines.append(f"| 提取 | {raw_count} | {_format_dist(raw_dist)} |")

        # ---------- Row 2: 去重 (conditional) ----------
        dedup_removed = prepare_summary["dedup_removed"]
        deduped_count = prepare_summary["deduped_queries_count"]
        assert deduped_count == raw_count - dedup_removed, \
            f"Invariant 2 failed: deduped({deduped_count}) != raw({raw_count}) - removed({dedup_removed})"
        if dedup_removed > 0:
            deduped_dist = prepare_summary["deduped_source_dist"]
            removed_dist = _removed_by_type(raw_dist, deduped_dist)
            removed_detail = "、".join(f"{k} {v}" for k, v in removed_dist.items())
            lines.append(f"| 去重 | {deduped_count} | 移除 {dedup_removed} 条（{removed_detail}）重复 |")

        prev_count = deduped_count
    else:
        # prepare_summary.json is required for accurate waterfall numbers
        log("WARNING: prepare_summary.json not found, skipping extraction/dedup rows")
        prev_count = 0

    # ---------- Row 3: 参考合并 (conditional) ----------
    ref_count = len(reference_queries) if reference_queries else 0
    if ref_count > 0:
        candidate = prev_count + ref_count
        assert candidate == prev_count + ref_count, \
            f"Invariant 3 failed: candidate({candidate}) != prev({prev_count}) + ref({ref_count})"
        lines.append(f"| 参考合并 | {candidate} | +{ref_count} reference |")
    else:
        candidate = prev_count

    # ---------- Row 4: 精选 ----------
    actual_selected = len(selected)
    actual_extra = len(extra)

    # Load original selection to get pre-validation selected/extra counts.
    # query_selection.json is never modified after Step 6 (LLM selection).
    query_selection = load_json(os.path.join(parsed_dir, "query_selection.json"))

    if validation_result is not None:
        pre_total = len(validation_result)
        if query_selection:
            pre_selected = len(query_selection.get("selected", []))
            pre_extra = len(query_selection.get("extra", []))
        else:
            pre_selected = pre_total
            pre_extra = 0
        dropped = candidate - pre_total
        select_count = pre_total
        assert pre_selected + pre_extra == pre_total, \
            f"Invariant 5 failed: pre_selected({pre_selected}) + pre_extra({pre_extra}) != pre_total({pre_total})"
        select_desc = f"selected {pre_selected} + extra {pre_extra}（丢弃 {dropped}）"
    else:
        pipeline_total = actual_selected + actual_extra
        dropped = candidate - pipeline_total
        select_count = pipeline_total
        select_desc = f"selected {actual_selected} + extra {actual_extra}（丢弃 {dropped}）"

    assert select_count + dropped == candidate, \
        f"Invariant 4 failed: selected_total({select_count}) + dropped({dropped}) != candidate({candidate})"
    lines.append(f"| 精选 | {select_count} | {select_desc} |")

    # ---------- Row 5: 验证 (conditional) ----------
    if validation_result is not None:
        passed = sum(1 for r in validation_result if r.get("pass", True))
        failed = sum(1 for r in validation_result if not r.get("pass", True))
        final_count = actual_selected + actual_extra
        assert passed + failed == select_count, \
            f"Invariant 6 failed: passed({passed}) + failed({failed}) != pre_total({select_count})"
        assert final_count == passed, \
            f"Invariant 7 failed: final({final_count}) != passed({passed})"

        # Determine where failures came from using original selection IDs
        if failed > 0 and query_selection:
            original_selected_ids = set(query_selection.get("selected", []))
            failed_ids = {r["id"] for r in validation_result if not r.get("pass", True)}
            sel_failed = len(failed_ids & original_selected_ids)
            ext_failed = failed - sel_failed
            if ext_failed == 0:
                removal_note = "已从 selected 移除"
            elif sel_failed == 0:
                removal_note = "已从 extra 移除"
            else:
                removal_note = f"selected 移除 {sel_failed} + extra 移除 {ext_failed}"
        elif failed > 0:
            removal_note = "已移除"
        else:
            removal_note = ""

        if removal_note:
            lines.append(f"| 验证 | {final_count} | 通过 {passed} / 失败 {failed}（{removal_note}） |")
        else:
            lines.append(f"| 验证 | {final_count} | 通过 {passed} / 失败 {failed} |")

    lines.append("")

    # ===================================================================
    # 精选来源分布
    # ===================================================================
    lines.append("## 精选来源分布")
    lines.append("")
    lines.append("| source_type | 数量 |")
    lines.append("|-------------|------|")

    type_counts: OrderedDict[str, int] = OrderedDict()
    for entry in selected:
        st = entry.get("source_type", "unknown")
        type_counts[st] = type_counts.get(st, 0) + 1

    for st, count in type_counts.items():
        if count > 0:
            lines.append(f"| {st} | {count} |")

    lines.append("")

    # ===================================================================
    # 参考文档处理
    # ===================================================================
    if reference_queries is not None:
        lines.append("## 参考文档处理")
        lines.append("")
        source_file = "N/A"
        if reference_queries:
            source_file = reference_queries[0].get("source", "N/A")
        lines.append(f"- 参考文档：{source_file}")
        lines.append(f"- 提取查询：{len(reference_queries)} 条")
        lines.append("")

    # ===================================================================
    # 精选查询清单
    # ===================================================================
    lines.append("## 精选查询清单")
    lines.append("")
    lines.append("| # | 输出标题 | 来源类型 | 来源名称 | 原始标题 | 来源文件 |")
    lines.append("|---|---------|---------|---------|---------|---------|")

    for i, entry in enumerate(selected, 1):
        title = entry.get("title", entry.get("display_name", "?"))
        source_type = entry.get("source_type", "?")
        dashboard_name = entry.get("dashboard_name", "")
        display_name = entry.get("display_name", "")
        source = entry.get("source", "?")
        lines.append(f"| {i} | {title} | {source_type} | {dashboard_name} | {display_name} | {source} |")

    lines.append("")

    # ===================================================================
    # 验证失败清单
    # ===================================================================
    if validation_result:
        failures = [r for r in validation_result if not r.get("pass", True)]
        if failures:
            lines.append("## 验证失败清单")
            lines.append("")
            lines.append("| ID | 来源类型 | 来源名称 | 原始标题 | 来源文件 | 错误原因 |")
            lines.append("|---|---------|---------|---------|---------|---------|")

            for f in failures:
                qid = f.get("id", "?")
                source_type = f.get("source_type", "?")
                dashboard_name = f.get("dashboard_name", "")
                display_name = f.get("title", "")
                source = f.get("source", "?")
                error = f.get("error", "?")
                error = error.replace("\n", " ").strip()
                error = error.replace("|", "\\|")
                if len(error) > 200:
                    error = error[:200] + "..."
                lines.append(
                    f"| {qid} | {source_type} | {dashboard_name} | {display_name} | {source} | {error} |"
                )

            lines.append("")

    return "\n".join(lines)


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

def main():
    if len(sys.argv) < 2:
        print("Usage: render_queries.py <input_dir>", file=sys.stderr)
        sys.exit(1)

    input_dir = sys.argv[1]
    parsed_dir = os.path.join(input_dir, "parsed")
    fragments_dir = os.path.join(input_dir, "fragments")

    # Ensure output directories exist
    os.makedirs(parsed_dir, exist_ok=True)
    os.makedirs(fragments_dir, exist_ok=True)

    # Load pipeline JSON (required)
    pipeline_path = os.path.join(parsed_dir, "query_pipeline.json")
    pipeline = load_json(pipeline_path)
    if pipeline is None:
        log(f"ERROR: {pipeline_path} not found")
        sys.exit(1)

    selected = pipeline.get("selected", [])
    extra = pipeline.get("extra", [])

    # --- Merge query_annotations.json if present ---
    SENTINEL = "PRE_CLEANED"
    annotations = load_json(os.path.join(parsed_dir, "query_annotations.json"))
    if annotations is not None:
        ann_map = {a["id"]: a for a in annotations if "id" in a}
        merged = 0
        for entry in selected + extra:
            eid = entry.get("id", "")
            if eid in ann_map:
                ann = ann_map[eid]
                for field in ("title", "category", "cleaned_query"):
                    if field in ann:
                        entry[field] = ann[field]
                merged += 1
        log(f"Merged {merged}/{len(selected) + len(extra)} annotations from query_annotations.json")
        if merged < len(selected) + len(extra):
            missing = [e.get("id", "?") for e in selected + extra
                       if e.get("id", "") not in ann_map]
            log(f"WARNING: {len(missing)} entries missing annotations: {missing[:5]}...")

    # --- Expand PRE_CLEANED sentinel to pre_cleaned_query ---
    no_change_count = 0
    for entry in selected + extra:
        if entry.get("cleaned_query") == SENTINEL:
            entry["cleaned_query"] = entry.get(
                "pre_cleaned_query",
                entry.get("normalized_query", ""))
            no_change_count += 1
    if no_change_count:
        log(f"Expanded {no_change_count} PRE_CLEANED entries to pre_cleaned_query")

    # Validate that Step 9 fields exist (either inline or from annotations)
    if selected and "title" not in selected[0]:
        log("WARNING: selected entries missing 'title' field — "
            "was Step 9 (清理+标注) executed?")

    # Load optional validation files
    main_val = load_json(os.path.join(parsed_dir, "query_validation.json"))
    llm_val = load_json(os.path.join(parsed_dir, "query_validation_LLM.json"))
    validation_result = (main_val or []) + (llm_val or []) or None
    reference_queries = load_json(
        os.path.join(parsed_dir, "reference_queries.json")
    )
    # --- Render queries_selected.md (conditional) ---
    selected_path = os.path.join(fragments_dir, "queries_selected.md")
    if selected or extra:
        selected_md = render_selected_md(selected)
        with open(selected_path, "w", encoding="utf-8") as f:
            f.write(selected_md)
        log(f"Generated {selected_path} ({len(selected)} queries)")
    else:
        if os.path.exists(selected_path):
            os.remove(selected_path)
            log(f"Removed stale {selected_path} (no queries)")

    # --- Render queries_extra.md (conditional) ---
    if extra:
        extra_md = render_extra_md(extra)
        extra_path = os.path.join(fragments_dir, "queries_extra.md")
        with open(extra_path, "w", encoding="utf-8") as f:
            f.write(extra_md)
        log(f"Generated {extra_path} ({len(extra)} queries)")
    else:
        # Remove stale extra file if it exists
        extra_path = os.path.join(fragments_dir, "queries_extra.md")
        if os.path.exists(extra_path):
            os.remove(extra_path)
            log(f"Removed stale {extra_path} (extra is empty)")

    # --- Render query_report.md ---
    report_md = render_report(
        pipeline, input_dir, validation_result,
        reference_queries,
    )
    report_path = os.path.join(parsed_dir, "query_report.md")
    with open(report_path, "w", encoding="utf-8") as f:
        f.write(report_md)
    log(f"Generated {report_path}")

    # --- Generate common_values.md ---
    queries_json = load_json(os.path.join(parsed_dir, "queries.json"))
    fields_json = load_json(os.path.join(parsed_dir, "fields.json"))

    all_queries = (queries_json or []) + (reference_queries or [])
    common_values = extract_common_values(all_queries, fields_json or [])
    cv_md = generate_common_values_md(common_values)

    cv_md_path = os.path.join(fragments_dir, "common_values.md")
    if cv_md:
        with open(cv_md_path, "w", encoding="utf-8") as f:
            f.write(cv_md)
        log(f"Generated {cv_md_path} ({len(common_values)} fields)")
    else:
        if os.path.exists(cv_md_path):
            os.remove(cv_md_path)
            log(f"Removed stale {cv_md_path} (common values empty)")

    log("Render complete.")


if __name__ == "__main__":
    main()
