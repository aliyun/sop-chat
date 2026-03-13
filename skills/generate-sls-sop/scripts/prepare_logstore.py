#!/usr/bin/env python3
"""
Prepare a logstore input directory for SOP generation.

Combines all mechanical steps of the SKILL workflow (Step 2):
  - Parse index.json and resource directories → fields.json, queries.json
  - Generate datasource.md based on project/logstore naming strategy

Usage:
    python prepare_logstore.py <input_dir>

Input directory must contain:
    - index.json (optional)
    - At least one resource sub-directory:
      - dashboards/ (optional)
      - alerts/ (optional)
      - scheduled_sqls/ (optional)
      - saved_searches/ (optional)

Output:
  <input_dir>/parsed/:
    - fields.json          # Raw field data (retained)
    - queries.json         # Step 6 input: LLM selects representative queries
  <input_dir>/fragments/:
    - datasource.md        # Embed directly into overview.md (Step 10 assembly)
"""

import json
import os
import re
import sys
from collections import Counter
from pathlib import Path


# ---------------------------------------------------------------------------
# Parse fields, queries, common values
# ---------------------------------------------------------------------------

def parse_fields(index_path: str) -> list[dict]:
    """Parse index.json and extract field definitions, recursively expanding json_keys."""
    with open(index_path, "r", encoding="utf-8") as f:
        index_data = json.load(f)

    keys = index_data.get("keys", {})
    fields = []

    for field_name, field_def in keys.items():
        # Filter out internal __tag__: prefixed fields
        if field_name.startswith("__tag__:"):
            continue

        field_type = field_def.get("type", "text")
        alias = field_def.get("alias", "")

        # For json type fields, recursively expand json_keys
        if field_type == "json":
            json_keys = field_def.get("json_keys", {})
            if json_keys:
                for child_name, child_def in json_keys.items():
                    child_alias = child_def.get("alias", "")
                    child_type = child_def.get("type", "text")
                    fields.append({
                        "field": f"{field_name}.{child_name}",
                        "alias": child_alias,
                        "type": child_type,
                        "parent": field_name,
                    })
            else:
                fields.append({
                    "field": field_name,
                    "alias": alias,
                    "type": field_type,
                })
        else:
            fields.append({
                "field": field_name,
                "alias": alias,
                "type": field_type,
            })

    return fields


def _is_placeholder_query(query: str) -> bool:
    """Query 为空或为占位符（如 @）时视为无效，不纳入提取结果。"""
    q = (query or "").strip()
    return not q or q == "@"


def _extract_token_defaults(tokens: list) -> dict:
    """Extract token_defaults metadata from a tokens array.

    Returns a dict mapping token key -> value (default).
    Entries missing 'key' or 'value' are skipped; duplicate keys use last value.
    """
    defaults = {}
    for t in (tokens or []):
        key = t.get("key")
        value = t.get("value")
        if key is not None and value is not None:
            defaults[key] = value
    return defaults


def _select_query_with_defaults(search_obj: dict, parent_tokens: list | None = None) -> tuple[str, dict]:
    """Select the best query from a search/chartQuery object and extract token_defaults.

    Priority logic:
    - If tokenQuery contains template variables (${...}), use tokenQuery (preserves template syntax)
    - Otherwise, use query (more accurate when tokenQuery is stale due to dashboard copy-paste errors)

    token_defaults are extracted from search.tokens (or parent_tokens for chartQueries).

    Returns (selected_query, token_defaults).
    """
    token_query = (search_obj.get("tokenQuery") or "").strip()
    query = (search_obj.get("query") or "").strip()

    # tokens live at the search level; chartQueries inherit from parent
    tokens = search_obj.get("tokens") if "tokens" in search_obj else parent_tokens
    token_defaults = _extract_token_defaults(tokens)

    # Prefer tokenQuery only when it contains template variables (${...})
    # This avoids stale tokenQuery from dashboard copy-paste errors
    if not _is_placeholder_query(token_query) and '${' in token_query:
        return token_query, token_defaults
    # fallback to query — more reliable when tokenQuery has no templates
    if not _is_placeholder_query(query):
        return query, {}
    # final fallback to tokenQuery if query is also empty/placeholder
    return token_query, token_defaults


def parse_dashboards(dashboards_dir: str) -> list[dict]:
    """Extract all non-empty queries from dashboard JSON files.

    Prioritises tokenQuery over query to preserve template variables.
    Extracts token_defaults metadata from the tokens array for downstream
    normalization (方案 B).
    """
    queries = []
    dashboard_path = Path(dashboards_dir)

    for json_file in sorted(dashboard_path.glob("*.json")):
        with open(json_file, "r", encoding="utf-8") as f:
            try:
                dashboard = json.load(f)
            except json.JSONDecodeError:
                continue

        dashboard_display_name = dashboard.get("displayName", "")

        charts = dashboard.get("charts", [])
        for chart in charts:
            search = chart.get("search", {})
            display = chart.get("display", {})
            display_name = (
                display.get("basicOptions", {}).get("displayName", "")
                or display.get("displayName", "")
            )
            chart_queries = search.get("chartQueries", [])

            # Extract top-level query (tokenQuery preferred)
            top_query, top_defaults = _select_query_with_defaults(search)
            parent_tokens = search.get("tokens")

            if not _is_placeholder_query(top_query):
                queries.append({
                    "source": json_file.name,
                    "source_type": "dashboard",
                    "dashboard_name": dashboard_display_name,
                    "display_name": display_name,
                    "query": top_query,
                    "token_defaults": top_defaults,
                    "logstore": search.get("logstore", ""),
                })
            elif chart_queries:
                for cq in chart_queries:
                    cq_query, cq_defaults = _select_query_with_defaults(cq, parent_tokens)
                    if _is_placeholder_query(cq_query):
                        continue
                    queries.append({
                        "source": json_file.name,
                        "source_type": "dashboard",
                        "dashboard_name": dashboard_display_name,
                        "display_name": display_name or cq.get("displayName", ""),
                        "query": cq_query,
                        "token_defaults": cq_defaults,
                        "logstore": cq.get("logstore", "") or search.get("logstore", ""),
                    })

    return queries


def parse_alerts(alerts_dir: str) -> list[dict]:
    """Extract queries from alert JSON files."""
    queries = []
    alerts_path = Path(alerts_dir)

    if not alerts_path.exists():
        return queries

    for json_file in sorted(alerts_path.glob("*.json")):
        with open(json_file, "r", encoding="utf-8") as f:
            try:
                alert = json.load(f)
            except json.JSONDecodeError:
                continue

        alert_display_name = alert.get("displayName", "")
        configuration = alert.get("configuration", {})
        query_list = configuration.get("queryList", [])

        for query_item in query_list:
            q = (query_item.get("query") or "").strip()
            if _is_placeholder_query(q):
                continue

            logstore = (query_item.get("store") or query_item.get("logStore") or "").strip()

            queries.append({
                "source": json_file.name,
                "source_type": "alert",
                "dashboard_name": alert_display_name,
                "display_name": alert_display_name,
                "query": q,
                "logstore": logstore,
            })

    return queries


def parse_scheduled_sqls(scheduled_sqls_dir: str) -> list[dict]:
    """Extract queries from scheduled SQL JSON files."""
    queries = []
    ss_path = Path(scheduled_sqls_dir)

    if not ss_path.exists():
        return queries

    for json_file in sorted(ss_path.glob("*.json")):
        with open(json_file, "r", encoding="utf-8") as f:
            try:
                sql_config = json.load(f)
            except json.JSONDecodeError:
                continue

        display_name = sql_config.get("displayName", "") or sql_config.get("name", "")
        configuration = sql_config.get("configuration", {})
        script = (configuration.get("script") or "").strip()

        if _is_placeholder_query(script):
            continue

        source_logstore = (configuration.get("sourceLogstore") or "").strip()

        queries.append({
            "source": json_file.name,
            "source_type": "scheduled_sql",
            "dashboard_name": display_name,
            "display_name": display_name,
            "query": script,
            "logstore": source_logstore,
        })

    return queries


def parse_saved_searches(saved_searches_dir: str) -> list[dict]:
    """Extract queries from saved search JSON files."""
    queries = []
    ss_path = Path(saved_searches_dir)

    if not ss_path.exists():
        return queries

    for json_file in sorted(ss_path.glob("*.json")):
        with open(json_file, "r", encoding="utf-8") as f:
            try:
                ss_config = json.load(f)
            except json.JSONDecodeError:
                continue

        display_name = ss_config.get("displayName", "") or ss_config.get("savedsearchName", "")
        search_query = (ss_config.get("searchQuery") or "").strip()

        if _is_placeholder_query(search_query):
            continue

        logstore = (ss_config.get("logstore") or "").strip()

        queries.append({
            "source": json_file.name,
            "source_type": "saved_search",
            "dashboard_name": display_name,
            "display_name": display_name,
            "query": search_query,
            "logstore": logstore,
        })

    return queries


# ---------------------------------------------------------------------------
# Dedup queries
# ---------------------------------------------------------------------------

# source_type dedup priority (lower = higher priority).
# Canonical definition: rules/query_select.md — keep in sync.
_SOURCE_PRIORITY = {"alert": 0, "dashboard": 1, "saved_search": 2, "scheduled_sql": 3}


def _normalize_query(q: str) -> str:
    """Normalize query for dedup: collapse whitespace, normalize ``|`` surroundings, strip."""
    q = q.strip()
    q = re.sub(r'\s+', ' ', q)
    q = re.sub(r'\s*\|\s*', ' | ', q)
    return q


def _dedup_queries(queries: list[dict]) -> list[dict]:
    """Deduplicate by normalized query text, keeping highest source_type priority.

    When two entries have identical normalized text, the one with lower
    ``_SOURCE_PRIORITY`` value wins.  Original order is preserved for the
    kept entries.
    """
    seen: dict[str, tuple[int, int]] = {}  # normalized_text -> (priority, index)
    for i, q in enumerate(queries):
        key = _normalize_query(q["query"])
        pri = _SOURCE_PRIORITY.get(q.get("source_type", ""), 99)
        if key not in seen or pri < seen[key][0]:
            seen[key] = (pri, i)
    keep_indices = sorted(v[1] for v in seen.values())
    return [queries[i] for i in keep_indices]


# ---------------------------------------------------------------------------
# Generate datasource.md
# ---------------------------------------------------------------------------

# Regex to detect 20+ hex suffix at end of name: e.g. "k8s-log-c8f3002d205724aa..."
_HEX_SUFFIX_RE = re.compile(r'^(.+)-([0-9a-f]{20,})$', re.IGNORECASE)


def _strip_hex_suffix(name: str) -> tuple[str, bool]:
    """If name has a 20+ hex suffix, return (prefix, True). Otherwise (name, False)."""
    m = _HEX_SUFFIX_RE.match(name)
    if m:
        return m.group(1), True
    return name, False


def generate_datasource_md(project_name: str, logstore_name: str) -> str:
    """Generate datasource.md content based on naming strategy.

    Datasource Strategy (previously in reference.md)
    =================================================

    Inference rule: project and logstore names are taken from the input
    directory path.  Names with a 20+ hex suffix (e.g.
    ``myproject-a1b2c3d4e5f6a7b8c9d0...``) are recognised as dynamic
    patterns — the suffix is replaced with ``{id}`` to form a pattern
    name (e.g. ``myproject-{id}``).

    Strategy: K8s logs
    ------------------
    Match condition: project directory name contains ``k8s-`` prefix.

    ``{project_prefix}`` is extracted from the project directory name
    (e.g. ``k8s-log-c8f3002d...`` → ``k8s-log``);
    ``{project_pattern}`` is the dynamic pattern (e.g. ``k8s-log-{id}``).

    Output includes project/logstore confirmation rules with priority:
    context → cluster-ID concatenation → tool query (ListSLSProjects).

    Fallback strategy
    -----------------
    Match condition: none of the above strategies matched.

    Only output the logstore name — no project.  In the fallback
    scenario, the project name comes from the input directory (e.g.
    ``ali-cn-chengdu-sls-admin``, ``log-service-xxx-cn-shanghai``) and
    is merely a sample value.  Writing it into the SOP would be
    misleading.  The project should be determined by usage context.

    Extension: to add a new datasource type, insert a new strategy
    branch before the fallback.
    """
    _, logstore_has_hex = _strip_hex_suffix(logstore_name)
    logstore_display = f"{_strip_hex_suffix(logstore_name)[0]}-{{id}}" if logstore_has_hex else logstore_name

    # K8s strategy: project name contains 'k8s-' prefix
    if "k8s-" in project_name:
        project_prefix, _ = _strip_hex_suffix(project_name)
        project_pattern = f"{project_prefix}-{{id}}"

        lines = [
            "## 数据源",
            "",
            "- project: 按以下优先级确定",
            f"- logstore: {logstore_display}",
            "",
            "### project 确认规则",
            "",
            "1. **上下文指定**：如果上下文中已有 project，直接使用",
            f"2. **集群 ID 拼接**：如果提供了 K8s 集群 ID（如 `c1a2b3c4d...`），拼接为 `{project_pattern}`",
            "3. **工具查询**：",
            f"   - 调用 `ListSLSProjects`（如环境中不存在则先加载）列出以 `{project_prefix}-` 开头的项目",
            "   - 展示结果",
            "   - ⚠️ **等待用户选择后再继续执行**",
            "",
            "### logstore 确认规则",
            "",
            "1. **上下文指定**：如果上下文中已明确 logstore，直接使用",
            f"2. **默认值**：使用 `{logstore_display}`，如含 `{{id}}` 占位符则用集群 ID 拼接",
        ]
        return "\n".join(lines)

    # Fallback strategy: only output logstore, no project
    lines = [
        "## 数据源",
        "",
        f"- logstore: `{logstore_display}`",
    ]
    return "\n".join(lines)


# ---------------------------------------------------------------------------
# Core logic (importable)
# ---------------------------------------------------------------------------

def prepare(input_dir: str) -> dict:
    """Prepare a single logstore directory. Returns summary dict.

    Raises:
        FileNotFoundError: if input_dir doesn't exist
        ValueError: if no resource sub-directory found
    """
    if not os.path.isdir(input_dir):
        raise FileNotFoundError(f"Input directory not found: {input_dir}")

    index_path = os.path.join(input_dir, "index.json")
    dashboards_dir = os.path.join(input_dir, "dashboards")
    alerts_dir = os.path.join(input_dir, "alerts")
    scheduled_sqls_dir = os.path.join(input_dir, "scheduled_sqls")
    saved_searches_dir = os.path.join(input_dir, "saved_searches")
    parsed_dir = os.path.join(input_dir, "parsed")
    fragments_dir = os.path.join(input_dir, "fragments")

    os.makedirs(parsed_dir, exist_ok=True)
    os.makedirs(fragments_dir, exist_ok=True)

    # --- Infer project and logstore names from path ---
    input_path = Path(input_dir).resolve()
    logstore_name = input_path.name
    project_name = input_path.parent.name

    # --- Parse fields, queries, common values ---

    if os.path.isfile(index_path):
        fields = parse_fields(index_path)
    else:
        print(f"Warning: {index_path} not found, fields will be empty", file=sys.stderr)
        fields = []

    dashboard_queries = parse_dashboards(dashboards_dir) if os.path.isdir(dashboards_dir) else []
    alert_queries = parse_alerts(alerts_dir)
    scheduled_sql_queries = parse_scheduled_sqls(scheduled_sqls_dir)
    saved_search_queries = parse_saved_searches(saved_searches_dir)

    raw_queries = dashboard_queries + alert_queries + scheduled_sql_queries + saved_search_queries

    # Filter out queries that target other logstores (cross-logstore contamination from alerts/dashboards)
    raw_count_before_filter = len(raw_queries)
    raw_queries = [
        q for q in raw_queries
        if (q.get("logstore") or "") == "" or q.get("logstore") == logstore_name
    ]
    queries_filtered_by_logstore = raw_count_before_filter - len(raw_queries)

    # Dedup queries by normalized text, keeping highest source_type priority
    deduped_queries = _dedup_queries(raw_queries)

    queries_output = []
    for i, q in enumerate(deduped_queries):
        entry = {
            "id": f"q{i}",
            "source": q["source"],
            "source_type": q["source_type"],
            "dashboard_name": q["dashboard_name"],
            "display_name": q["display_name"],
            "query": q["query"],
            "logstore": q["logstore"],
        }
        # Include token_defaults only when non-empty (dashboard queries with tokens)
        td = q.get("token_defaults", {})
        if td:
            entry["token_defaults"] = td
        queries_output.append(entry)

    # --- Write parsed JSON data ---

    fields_path = os.path.join(parsed_dir, "fields.json")
    with open(fields_path, "w", encoding="utf-8") as f:
        json.dump(fields, f, ensure_ascii=False, indent=2)

    queries_path = os.path.join(parsed_dir, "queries.json")
    with open(queries_path, "w", encoding="utf-8") as f:
        json.dump(queries_output, f, ensure_ascii=False, indent=2)

    # --- Write prepare_summary.json (dedup stats for render_queries.py) ---

    prepare_summary = {
        "raw_queries_count": len(raw_queries),
        "deduped_queries_count": len(deduped_queries),
        "dedup_removed": len(raw_queries) - len(deduped_queries),
        "queries_filtered_by_logstore": queries_filtered_by_logstore,
        "raw_source_dist": dict(Counter(q["source_type"] for q in raw_queries)),
        "deduped_source_dist": dict(Counter(q["source_type"] for q in deduped_queries)),
    }
    prepare_summary_path = os.path.join(parsed_dir, "prepare_summary.json")
    with open(prepare_summary_path, "w", encoding="utf-8") as f:
        json.dump(prepare_summary, f, ensure_ascii=False, indent=2)

    # --- Write assembly fragments to fragments/ ---

    datasource_md = generate_datasource_md(project_name, logstore_name)
    datasource_path = os.path.join(fragments_dir, "datasource.md")
    with open(datasource_path, "w", encoding="utf-8") as f:
        f.write(datasource_md)

    # --- Build summary ---

    summary = {
        "parsed_dir": parsed_dir,
        "fragments_dir": fragments_dir,
        "project_name": project_name,
        "logstore_name": logstore_name,
        "fields_count": len(fields),
        "queries_count": len(queries_output),
        "queries_raw_count": len(raw_queries),
        "queries_deduped": len(raw_queries) - len(deduped_queries),
        "queries_filtered_by_logstore": queries_filtered_by_logstore,
        "dashboard_queries": len(dashboard_queries),
        "alert_queries": len(alert_queries),
        "scheduled_sql_queries": len(scheduled_sql_queries),
        "saved_search_queries": len(saved_search_queries),
        "datasource_strategy": "k8s" if "k8s-" in project_name else "fallback",
    }
    return summary


# ---------------------------------------------------------------------------
# CLI entry point
# ---------------------------------------------------------------------------

def main():
    if len(sys.argv) != 2:
        print(f"Usage: {sys.argv[0]} <input_dir>", file=sys.stderr)
        sys.exit(1)
    try:
        summary = prepare(sys.argv[1])
        print(json.dumps(summary, ensure_ascii=False, indent=2))
    except (ValueError, FileNotFoundError) as e:
        print(f"Error: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
