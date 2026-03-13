#!/usr/bin/env python3
"""
Fetch SLS index configs and dashboard configs from a project using aliyun CLI.

Usage:
    python fetch_sls_data.py <project> <output_dir> [--logstores=<name1,name2>] [--all-dashboard-langs]

Output directory structure (per logstore):
    <output_dir>/
    ├── <logstore_full_name>/
    │   ├── index.json
    │   ├── dashboards/
    │   │   ├── dashboard_a.json
    │   │   └── dashboard_b.json
    │   ├── alerts/
    │   │   ├── alert_a.json
    │   │   └── alert_b.json
    │   ├── scheduled_sqls/     # when ScheduledSQL references this logstore
    │   │   └── *.json
    │   └── saved_searches/     # when SavedSearch references this logstore
    │       └── *.json

Output: summary JSON to stdout with keys: project, logstores_processed, dashboards_fetched, alerts_fetched, scheduled_sqls_fetched, saved_searches_fetched, warnings, errors
Progress info is printed to stderr.
"""

import argparse
import concurrent.futures
import json
import os
import re
import subprocess
import sys
from collections import defaultdict
from pathlib import Path


def log(msg: str):
    """Print progress info to stderr."""
    print(msg, file=sys.stderr, flush=True)


def _load_internal_logstores(config_path: Path | None = None) -> set[str]:
    """Load internal logstore names to exclude when mode=all."""
    path = config_path or (Path(__file__).resolve().parent.parent / "rules" / "internal_logstores.txt")
    if not path.exists():
        return set()
    result = set()
    for line in path.read_text(encoding="utf-8").splitlines():
        s = line.strip()
        if s and not s.startswith("#"):
            result.add(s)
    return result


def run_cli(args: list[str]) -> dict:
    """Run aliyun CLI command and return parsed JSON output."""
    cmd = ["aliyun", "sls"] + args
    result = subprocess.run(cmd, capture_output=True, text=True)
    if result.returncode != 0:
        # New CLI outputs errors to stdout with "ERROR:" prefix, or to stderr
        error_msg = result.stdout.strip() or result.stderr.strip()
        raise RuntimeError(f"CLI error: {error_msg}")
    if not result.stdout.strip():
        raise RuntimeError("CLI returned empty output")
    return json.loads(result.stdout)


def list_logstores(project: str) -> list[str]:
    """List all logstores in the project (with pagination)."""
    PAGE_SIZE = 500
    all_logstores = []
    offset = 0
    while True:
        result = run_cli(["ListLogStores", f"--project={project}",
                          f"--size={PAGE_SIZE}", f"--offset={offset}"])
        items = result.get("logstores", [])
        all_logstores.extend(items)
        total = result.get("total", len(items))
        if offset + len(items) >= total or len(items) == 0:
            break
        offset += len(items)
    return all_logstores


def list_dashboards(project: str) -> dict:
    """List all dashboards in the project (with pagination). Returns combined response."""
    PAGE_SIZE = 500
    all_items = []
    offset = 0
    total = None
    while True:
        result = run_cli(["ListDashboard", f"--project={project}",
                          f"--size={PAGE_SIZE}", f"--offset={offset}"])
        items = result.get("dashboardItems", [])
        all_items.extend(items)
        total = result.get("total", len(items))
        if offset + len(items) >= total or len(items) == 0:
            break
        offset += len(items)
    return {"dashboardItems": all_items, "total": total or len(all_items)}


def list_alerts(project: str) -> list[dict]:
    """List all alerts in the project (with pagination). Returns full alert configs."""
    PAGE_SIZE = 200
    all_alerts = []
    offset = 0
    while True:
        result = run_cli(["ListAlerts", f"--project={project}",
                          f"--size={PAGE_SIZE}", f"--offset={offset}"])
        items = result.get("results", [])
        all_alerts.extend(items)
        total = result.get("total", len(items))
        if offset + len(items) >= total or len(items) == 0:
            break
        offset += len(items)
    return all_alerts


def get_index_config(project: str, logstore: str) -> dict:
    """Get index config for a logstore."""
    return run_cli(["GetIndex", f"--project={project}", f"--logstore={logstore}"])


def get_dashboard(project: str, dashboard_name: str) -> dict:
    """Get full dashboard config."""
    return run_cli(["GetDashboard", f"--project={project}", f"--dashboardName={dashboard_name}"])


def list_scheduled_sqls(project: str) -> list[dict]:
    """List all scheduled SQLs in the project (with pagination)."""
    PAGE_SIZE = 200
    all_items = []
    offset = 0
    while True:
        result = run_cli(["ListScheduledSQLs", f"--project={project}",
                          f"--size={PAGE_SIZE}", f"--offset={offset}"])
        items = result.get("results", [])
        all_items.extend(items)
        total = result.get("total", len(items))
        if offset + len(items) >= total or len(items) == 0:
            break
        offset += len(items)
    return all_items


def list_saved_searches(project: str) -> list[dict]:
    """List all saved searches in the project (with pagination). Returns name items only."""
    PAGE_SIZE = 500
    all_items = []
    offset = 0
    while True:
        result = run_cli(["ListSavedSearch", f"--project={project}",
                          f"--size={PAGE_SIZE}", f"--offset={offset}"])
        items = result.get("savedsearchItems", [])
        all_items.extend(items)
        total = result.get("total", len(items))
        if offset + len(items) >= total or len(items) == 0:
            break
        offset += len(items)
    return all_items


def get_saved_search(project: str, name: str) -> dict:
    """Get full saved search config."""
    return run_cli(["GetSavedSearch", f"--project={project}", f"--savedsearchName={name}"])


def get_scheduled_sql_logstore(sql_config: dict) -> str:
    """Extract source logstore from ScheduledSQL configuration."""
    return (sql_config.get("configuration", {}).get("sourceLogstore") or "").strip()


def is_valid_logstore_name(name: str) -> bool:
    """Check if a logstore name is valid (not a special/placeholder value)."""
    if not name or len(name) <= 1:
        return False
    # Filter out special characters and placeholder values
    if name in ("@", "*", ".", "-", "_"):
        return False
    return True


def get_dashboard_logstores(dashboard_config: dict) -> set[str]:
    """Return all logstores referenced by a dashboard's charts.
    
    Collects logstore names from both search.logstore and chartQueries[].logstore
    to handle both simple and complex dashboard structures.
    """
    logstores = set()
    for chart in dashboard_config.get("charts", []):
        search = chart.get("search", {})
        ls = search.get("logstore", "").strip()
        
        if ls and is_valid_logstore_name(ls):
            logstores.add(ls)
        
        # 回退：当 search.logstore 无效或为占位符时，从 chartQueries 获取
        if not ls or not is_valid_logstore_name(ls):
            for cq in search.get("chartQueries", []):
                cq_ls = (cq.get("logstore") or "").strip()
                if cq_ls and is_valid_logstore_name(cq_ls):
                    logstores.add(cq_ls)
    
    return logstores


def filter_dashboard_by_logstore(dashboard_config: dict, target_logstore: str) -> dict:
    """Return a copy of dashboard_config with only charts referencing target_logstore.
    
    Filters out charts that don't belong to the target logstore to avoid
    downstream contamination in prepare_logstore.py.
    """
    filtered_charts = []
    for chart in dashboard_config.get("charts", []):
        search = chart.get("search", {})
        ls = search.get("logstore", "").strip()
        
        if ls and is_valid_logstore_name(ls):
            if ls == target_logstore:
                filtered_charts.append(chart)
        else:
            # 回退检查 chartQueries：只要有任何一个 chartQuery 属于目标 logstore 就保留该 chart
            for cq in search.get("chartQueries", []):
                cq_ls = (cq.get("logstore") or "").strip()
                if cq_ls == target_logstore:
                    filtered_charts.append(chart)
                    break
    
    result = dict(dashboard_config)
    result["charts"] = filtered_charts
    return result


def get_alert_logstore(alert: dict) -> str:
    """从告警的 queryList 中提取主要 logstore，兼容新旧版本 API。
    
    新版用 store，旧版用 logStore。
    Returns the most frequently referenced logstore name, or empty string if none found.
    """
    logstore_counts: dict[str, int] = defaultdict(int)
    for q in alert.get("configuration", {}).get("queryList", []):
        # 新版用 store，旧版用 logStore（store 为空时）
        ls = (q.get("store") or q.get("logStore") or "").strip()
        if ls and is_valid_logstore_name(ls):
            logstore_counts[ls] += 1
    if not logstore_counts:
        return ""
    return max(logstore_counts, key=logstore_counts.get)


def filter_lang_variants(dashboard_items: list[dict]) -> list[dict]:
    """按 dashboardName 基础名分组，每组只保留一个中文版本。
    
    规则：
    1. 去掉末尾的语言/地区后缀（_cn、_en、-en、-jp、_ja 等）得到基础名
    2. 同组优先级：_cn 后缀 > 无语言后缀（displayName 为中文）> 其它
    3. 无法识别语言分组的 dashboard（如 dashboard-17xxx 随机命名）全部保留
    """
    def get_base_name(name: str) -> str:
        """去掉末尾语言后缀，返回基础名。只匹配末尾，避免误伤中间含 cn/en 等的名字。"""
        base = re.sub(r'[-_](cn|en|jp|ja|zh)$', '', name, flags=re.IGNORECASE)
        return base if base else name
    
    def is_chinese(text: str) -> bool:
        """检查文本是否主要为中文。"""
        if not text:
            return False
        chinese_chars = sum(1 for c in text if '\u4e00' <= c <= '\u9fff')
        return chinese_chars > len(text) * 0.3
    
    # 按基础名分组
    groups: dict[str, list[dict]] = defaultdict(list)
    for item in dashboard_items:
        name = item["dashboardName"]
        base = get_base_name(name)
        groups[base].append(item)
    
    # 每组选一个
    selected = []
    for base, items in groups.items():
        if len(items) == 1:
            selected.append(items[0])
            continue
        
        # 优先级：_cn 后缀 > 无语言后缀（displayName 中文）> 其它
        cn_suffix = [x for x in items if re.search(r'[-_]cn$', x["dashboardName"], re.IGNORECASE)]
        if cn_suffix:
            selected.append(cn_suffix[0])
            continue
        
        no_suffix = [
            x for x in items 
            if not re.search(r'[-_](cn|en|jp|ja|zh)$', x["dashboardName"], re.IGNORECASE)
        ]
        if no_suffix:
            # 进一步检查 displayName 是否为中文
            chinese_ones = [x for x in no_suffix if is_chinese(x.get("displayName", ""))]
            if chinese_ones:
                selected.append(chinese_ones[0])
            else:
                selected.append(no_suffix[0])
            continue
        
        # 兜底：取第一个
        selected.append(items[0])
    
    return selected


def main():
    parser = argparse.ArgumentParser(description="Fetch SLS data for SOP generation")
    parser.add_argument("project", help="SLS project name")
    parser.add_argument("output_dir", help="Output root directory")
    parser.add_argument("--logstores", help="Only process these logstores (comma-separated)")
    parser.add_argument("--all-dashboard-langs", action="store_true", help="Keep all language variants of dashboards (default: dedup, keep Chinese version per group)")
    parser.add_argument("--concurrency", type=int, default=16, help="Max concurrent API calls for detail fetching (default: 16)")
    args = parser.parse_args()

    project = args.project
    output_dir = args.output_dir
    concurrency = args.concurrency
    logstore_filter = set(args.logstores.split(",")) if args.logstores else None

    summary = {
        "project": project,
        "logstores_processed": {},  # { logstore_name: {"dashboards": N, "alerts": M, ...} }
        "dashboards_fetched": 0,  # Total dashboard-to-logstore assignments
        "dashboards_total": 0,
        "dashboards_after_lang_filter": 0,
        "alerts_fetched": 0,
        "scheduled_sqls_fetched": 0,
        "saved_searches_fetched": 0,
        "warnings": [],
        "errors": [],
    }

    # Fetch summary stats for data_summary.md generation
    fetch_stats = {
        "total_logstores": 0,
        "internal_skipped": 0,
        "no_resource_skipped": 0,
        "no_index_skipped": 0,
        "fetched_logstores": 0,
    }

    # === Layer 1: Parallel discovery (5 List API calls in parallel) ===
    log(f"[1/4] Discovering project resources: {project}")
    with concurrent.futures.ThreadPoolExecutor(max_workers=5) as executor:
        fut_logstores = executor.submit(list_logstores, project)
        fut_dashboards = executor.submit(list_dashboards, project)
        fut_alerts = executor.submit(list_alerts, project)
        fut_scheduled_sqls = executor.submit(list_scheduled_sqls, project)
        fut_saved_searches = executor.submit(list_saved_searches, project)

    # Collect results - logstores and dashboards are required, others degrade gracefully
    try:
        all_logstores = fut_logstores.result()
        fetch_stats["total_logstores"] = len(all_logstores)
        log(f"  Found {len(all_logstores)} logstores")
    except Exception as e:
        summary["errors"].append(f"Failed to list logstores: {e}")
        print(json.dumps(summary, ensure_ascii=False, indent=2))
        sys.exit(1)

    try:
        dashboard_response = fut_dashboards.result()
        dashboard_items = dashboard_response.get("dashboardItems", [])
        summary["dashboards_total"] = len(dashboard_items)
        log(f"  Found {len(dashboard_items)} dashboards")
    except Exception as e:
        summary["errors"].append(f"Failed to list dashboards: {e}")
        print(json.dumps(summary, ensure_ascii=False, indent=2))
        sys.exit(1)

    try:
        all_alerts = fut_alerts.result()
        log(f"  Found {len(all_alerts)} alerts")
    except Exception as e:
        summary["warnings"].append(f"Failed to list alerts: {e}")
        all_alerts = []

    try:
        all_scheduled_sqls = fut_scheduled_sqls.result()
        log(f"  Found {len(all_scheduled_sqls)} scheduled SQLs")
    except Exception as e:
        summary["warnings"].append(f"Failed to list scheduled SQLs: {e}")
        all_scheduled_sqls = []

    try:
        saved_search_items = fut_saved_searches.result()
        log(f"  Found {len(saved_search_items)} saved searches")
    except Exception as e:
        summary["warnings"].append(f"Failed to list saved searches: {e}")
        saved_search_items = []

    os.makedirs(output_dir, exist_ok=True)

    # Filter dashboard language variants (CPU only, no API calls)
    if not args.all_dashboard_langs:
        filtered_items = filter_lang_variants(dashboard_items)
        log(f"  Applied language filter: {len(filtered_items)} dashboards (from {summary['dashboards_total']})")
    else:
        filtered_items = dashboard_items

    summary["dashboards_after_lang_filter"] = len(filtered_items)

    # === Layer 2: Parallel detail fetching (dashboards + saved searches) ===
    valid_logstores = set(all_logstores)
    logstore_dashboards: dict[str, list[dict]] = defaultdict(list)
    logstore_saved_searches: dict[str, list[dict]] = defaultdict(list)

    ss_names = [item.get("savedsearchName", "") for item in saved_search_items
                if item.get("savedsearchName")]

    total_detail_fetches = len(filtered_items) + len(ss_names)
    log(f"[2/4] Fetching resource details ({total_detail_fetches} items, concurrency={concurrency})...")

    completed = 0
    with concurrent.futures.ThreadPoolExecutor(max_workers=concurrency) as executor:
        # Submit all dashboard detail fetches
        dashboard_futures = {
            executor.submit(get_dashboard, project, item["dashboardName"]): item
            for item in filtered_items
        }
        # Submit all saved search detail fetches
        ss_futures = {
            executor.submit(get_saved_search, project, name): name
            for name in ss_names
        }

        # Process dashboard results as they complete
        for future in concurrent.futures.as_completed(dashboard_futures):
            completed += 1
            item = dashboard_futures[future]
            name = item["dashboardName"]
            log(f"  ({completed}/{total_detail_fetches}) Fetched dashboard: {name}")
            try:
                config = future.result()
                dashboard_logstores = get_dashboard_logstores(config)

                if not dashboard_logstores:
                    summary["warnings"].append(f"Dashboard '{name}' has no logstore reference, skipped")
                    continue

                for ls in dashboard_logstores:
                    if logstore_filter and ls not in logstore_filter:
                        continue
                    if ls not in valid_logstores:
                        summary["warnings"].append(
                            f"Dashboard '{name}' references logstore '{ls}' which is not in the project"
                        )
                    filtered_config = filter_dashboard_by_logstore(config, ls)
                    logstore_dashboards[ls].append(filtered_config)
                    summary["dashboards_fetched"] += 1

            except Exception as e:
                summary["errors"].append(f"Failed to fetch dashboard '{name}': {e}")

        # Process saved search results as they complete
        for future in concurrent.futures.as_completed(ss_futures):
            completed += 1
            ss_name = ss_futures[future]
            log(f"  ({completed}/{total_detail_fetches}) Fetched saved search: {ss_name}")
            try:
                ss_config = future.result()
                ss_logstore = (ss_config.get("logstore") or "").strip()
                if not ss_logstore:
                    summary["warnings"].append(f"SavedSearch '{ss_name}' has no logstore, skipped")
                    continue
                if logstore_filter and ss_logstore not in logstore_filter:
                    continue
                logstore_saved_searches[ss_logstore].append(ss_config)
                summary["saved_searches_fetched"] += 1
            except Exception as e:
                summary["warnings"].append(f"Failed to fetch saved search '{ss_name}': {e}")

    log(f"  Grouped {summary['dashboards_fetched']} dashboards into {len(logstore_dashboards)} logstores")
    log(f"  Grouped {summary['saved_searches_fetched']} saved searches into {len(logstore_saved_searches)} logstores")

    # Group alerts by logstore (CPU only, no API calls)
    logstore_alerts: dict[str, list[dict]] = defaultdict(list)
    for alert in all_alerts:
        alert_name = alert.get("name", "unknown")
        primary_ls = get_alert_logstore(alert)
        if not primary_ls:
            continue
        if logstore_filter and primary_ls not in logstore_filter:
            continue
        if primary_ls not in valid_logstores:
            summary["warnings"].append(
                f"Alert '{alert_name}' references logstore '{primary_ls}' which is not in the project"
            )
        logstore_alerts[primary_ls].append(alert)
        summary["alerts_fetched"] += 1
    log(f"  Grouped {summary['alerts_fetched']} alerts into {len(logstore_alerts)} logstores")

    # Group ScheduledSQLs by sourceLogstore (CPU only, no API calls)
    logstore_scheduled_sqls: dict[str, list[dict]] = defaultdict(list)
    for sql_config in all_scheduled_sqls:
        source_ls = get_scheduled_sql_logstore(sql_config)
        if not source_ls:
            continue
        if logstore_filter and source_ls not in logstore_filter:
            continue
        logstore_scheduled_sqls[source_ls].append(sql_config)
        summary["scheduled_sqls_fetched"] += 1
    log(f"  Grouped {summary['scheduled_sqls_fetched']} scheduled SQLs into {len(logstore_scheduled_sqls)} logstores")

    # === Layer 3: Parallel index fetch ===
    all_target_logstores = (set(logstore_dashboards.keys()) | set(logstore_alerts.keys()) |
                            set(logstore_scheduled_sqls.keys()) | set(logstore_saved_searches.keys()))
    # Filter to only logstores that actually exist (dashboards/alerts may reference non-existent logstores)
    all_target_logstores &= set(all_logstores)

    # Track internal logstores that exist in this project
    internal_in_project: set[str] = set()

    if logstore_filter:
        for ls in logstore_filter:
            if ls in valid_logstores:
                all_target_logstores.add(ls)
    else:
        # 用户选「全部」：排除 internal logstores
        internal = _load_internal_logstores()
        # 统计所有存在的 internal logstores（不管有无资源）
        internal_in_project = internal & set(all_logstores)
        fetch_stats["internal_skipped"] = len(internal_in_project)
        all_target_logstores -= internal
        if internal_in_project:
            log(f"  Excluded {len(internal_in_project)} internal logstores: {', '.join(sorted(internal_in_project))}")

    # Calculate no_resource_skipped: logstores that exist but have no resources
    # These are logstores in all_logstores but not in all_target_logstores (after internal filter)
    # and not in internal logstores
    if not logstore_filter:
        no_resource = set(all_logstores) - all_target_logstores - internal_in_project
        fetch_stats["no_resource_skipped"] = len(no_resource)
    else:
        fetch_stats["no_resource_skipped"] = 0

    if logstore_filter:
        fetch_stats["total_logstores"] = len(all_target_logstores)

    log(f"[3/4] Fetching index configs ({len(all_target_logstores)} logstores, concurrency={concurrency})...")

    index_configs: dict[str, dict] = {}
    completed = 0
    with concurrent.futures.ThreadPoolExecutor(max_workers=concurrency) as executor:
        index_futures = {
            executor.submit(get_index_config, project, ls): ls
            for ls in all_target_logstores
        }
        for future in concurrent.futures.as_completed(index_futures):
            completed += 1
            ls = index_futures[future]
            log(f"  ({completed}/{len(all_target_logstores)}) Fetched index: {ls}")
            try:
                index_configs[ls] = future.result()
            except Exception as e:
                summary["warnings"].append(f"No index config for logstore '{ls}': {e}")

    # === Phase 4: Save structured output (sequential file I/O) ===
    # Only process logstores with valid index config
    logstores_with_index = sorted(ls for ls in all_target_logstores if ls in index_configs)
    fetch_stats["no_index_skipped"] = len(all_target_logstores) - len(logstores_with_index)
    fetch_stats["fetched_logstores"] = len(logstores_with_index)

    if fetch_stats["no_index_skipped"] > 0:
        log(f"  Skipped {fetch_stats['no_index_skipped']} logstores without index config")

    log(f"[4/4] Saving output for {len(logstores_with_index)} logstores...")

    for ls in logstores_with_index:
        ls_dir = os.path.join(output_dir, ls)
        os.makedirs(ls_dir, exist_ok=True)

        # Save index config
        index_path = os.path.join(ls_dir, "index.json")
        with open(index_path, "w", encoding="utf-8") as f:
            json.dump(index_configs[ls], f, ensure_ascii=False, indent=2)

        # Save associated dashboards (only create dir when there's data)
        db_count = 0
        dashboard_list = logstore_dashboards.get(ls, [])
        if dashboard_list:
            dashboards_dir = os.path.join(ls_dir, "dashboards")
            os.makedirs(dashboards_dir, exist_ok=True)
            for dashboard_config in dashboard_list:
                db_name = dashboard_config.get("dashboardName", "unknown")
                db_path = os.path.join(dashboards_dir, f"{db_name}.json")
                with open(db_path, "w", encoding="utf-8") as f:
                    json.dump(dashboard_config, f, ensure_ascii=False, indent=2)
                db_count += 1

        # Save associated alerts (only create dir when there's data)
        alert_count = 0
        alert_list = logstore_alerts.get(ls, [])
        if alert_list:
            alerts_dir = os.path.join(ls_dir, "alerts")
            os.makedirs(alerts_dir, exist_ok=True)
            for alert_config in alert_list:
                alert_name = alert_config.get("name", "unknown")
                alert_path = os.path.join(alerts_dir, f"{alert_name}.json")
                with open(alert_path, "w", encoding="utf-8") as f:
                    json.dump(alert_config, f, ensure_ascii=False, indent=2)
                alert_count += 1

        # Save associated scheduled SQLs (only create dir when there's data)
        scheduled_sqls_count = 0
        scheduled_sqls_list = logstore_scheduled_sqls.get(ls, [])
        if scheduled_sqls_list:
            ss_dir = os.path.join(ls_dir, "scheduled_sqls")
            os.makedirs(ss_dir, exist_ok=True)
            for sql_config in scheduled_sqls_list:
                sql_name = sql_config.get("name", "unknown")
                sql_path = os.path.join(ss_dir, f"{sql_name}.json")
                with open(sql_path, "w", encoding="utf-8") as f:
                    json.dump(sql_config, f, ensure_ascii=False, indent=2)
                scheduled_sqls_count += 1

        # Save associated saved searches (only create dir when there's data)
        saved_searches_count = 0
        saved_searches_list = logstore_saved_searches.get(ls, [])
        if saved_searches_list:
            saved_dir = os.path.join(ls_dir, "saved_searches")
            os.makedirs(saved_dir, exist_ok=True)
            for ss_config in saved_searches_list:
                ss_name = ss_config.get("savedsearchName", "unknown")
                saved_path = os.path.join(saved_dir, f"{ss_name}.json")
                with open(saved_path, "w", encoding="utf-8") as f:
                    json.dump(ss_config, f, ensure_ascii=False, indent=2)
                saved_searches_count += 1

        summary["logstores_processed"][ls] = {
            "dashboards": db_count,
            "alerts": alert_count,
            "scheduled_sqls": scheduled_sqls_count,
            "saved_searches": saved_searches_count,
        }
        log(f"  Saved {db_count} dashboards, {alert_count} alerts, {scheduled_sqls_count} scheduled_sqls, {saved_searches_count} saved_searches for: {ls}")

    # Write fetch_summary.json for prepare_project.py to read
    fetch_summary_path = os.path.join(output_dir, "fetch_summary.json")
    with open(fetch_summary_path, "w", encoding="utf-8") as f:
        json.dump(fetch_stats, f, ensure_ascii=False, indent=2)
    log(f"  Saved fetch_summary.json: {fetch_stats}")

    # Validate statistical partition: all logstores must be accounted for
    _validate_fetch_stats_partition(fetch_stats)

    # Generate data_summary.md and output to stdout
    _write_data_summary_md(output_dir, fetch_stats)

    log("Done!")
    print(json.dumps(summary, ensure_ascii=False, indent=2))


def _validate_fetch_stats_partition(fetch_stats: dict) -> None:
    """Validate that fetch_stats partition sums correctly. Raises AssertionError if invalid."""
    total = fetch_stats.get("total_logstores", 0)
    internal = fetch_stats.get("internal_skipped", 0)
    no_resource = fetch_stats.get("no_resource_skipped", 0)
    no_index = fetch_stats.get("no_index_skipped", 0)
    fetched = fetch_stats.get("fetched_logstores", 0)
    assert total == internal + no_resource + no_index + fetched, "Statistical partition mismatch"


def _write_data_summary_md(output_dir: str, fetch_stats: dict):
    """Generate data_summary.md with filtering statistics and output to stdout."""
    total = fetch_stats.get("total_logstores", 0)
    internal_skip = fetch_stats.get("internal_skipped", 0)
    no_resource_skip = fetch_stats.get("no_resource_skipped", 0)
    no_index_skip = fetch_stats.get("no_index_skipped", 0)
    fetched = fetch_stats.get("fetched_logstores", 0)

    # Calculate intermediate values
    after_internal = total - internal_skip
    after_no_resource = after_internal - no_resource_skip
    after_no_index = after_no_resource - no_index_skip

    lines = [
        "## 数据摘要",
        "",
        "| 阶段 | 过滤数 | 剩余 | 说明 |",
        "|------|--------|------|------|",
        f"| SLS 全部 | - | {total} | ListLogStores |",
    ]

    if internal_skip > 0:
        lines.append(f"| internal 跳过 | -{internal_skip} | {after_internal} | 命中 internal_logstores.txt |")

    if no_resource_skip > 0:
        lines.append(f"| 无关联资源跳过 | -{no_resource_skip} | {after_no_resource} | 无 dashboard/alert 等 |")

    if no_index_skip > 0:
        lines.append(f"| 无索引配置跳过 | -{no_index_skip} | {after_no_index} | GetIndex 失败 |")

    lines.append("")

    content = "\n".join(lines)

    # Write to file
    md_path = os.path.join(output_dir, "data_summary.md")
    with open(md_path, "w", encoding="utf-8") as f:
        f.write(content)

    # Output to stdout for LLM visibility
    print(content)


if __name__ == "__main__":
    main()
