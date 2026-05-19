#!/usr/bin/env python3
"""
Aggregate audit results from per-logstore audits.

Reads all _audit/*/audit_result.json files and generates:
- _audit/audit_data.json - Structured aggregated data
- _audit/audit_summary.md - Human-readable summary report

Usage:
    aggregate_audit.py <project_dir>

stdout: JSON summary of aggregation
stderr: progress messages
"""

import argparse
import json
import os
import sys
from collections import Counter
from datetime import datetime
from typing import Any


def log(msg: str):
    print(msg, file=sys.stderr, flush=True)


def find_audit_results(project_dir: str) -> list[tuple[str, str]]:
    """
    Find all audit_result.json files in _audit/<logstore>/.
    
    Returns:
        List of (logstore_name, file_path) tuples
    """
    audit_dir = os.path.join(project_dir, "_audit")
    if not os.path.exists(audit_dir):
        return []
    
    results = []
    for item in os.listdir(audit_dir):
        item_path = os.path.join(audit_dir, item)
        # Only check directories (skip files like audit_plan.json)
        if not os.path.isdir(item_path):
            continue
        result_path = os.path.join(item_path, "audit_result.json")
        if os.path.exists(result_path):
            results.append((item, result_path))
    
    return results


def load_json(path: str) -> dict | None:
    """Load JSON file, return None if not exists or invalid."""
    if not os.path.exists(path):
        return None
    try:
        with open(path, "r", encoding="utf-8") as f:
            return json.load(f)
    except (json.JSONDecodeError, IOError):
        return None


def _get_issues_from_result(result: dict) -> list:
    """Extract issues list from result, expanding from checks if old format."""
    issues = result.get("issues", [])
    if not issues and "checks" in result:
        for check in result.get("checks", []):
            check_name = check.get("check", "unknown")
            for issue in check.get("issues", []):
                issue = dict(issue)
                issue["check"] = check_name
                issues.append(issue)
    return issues


def validate_result(result: dict, logstore: str) -> list[str]:
    """
    Validate audit_result.json numeric consistency.

    Returns list of warning messages.
    """
    warnings = []
    issues = _get_issues_from_result(result)
    summary = result.get("summary", {})
    
    # Check total_issues
    expected_total = len(issues)
    actual_total = summary.get("total_issues", 0)
    if actual_total != expected_total:
        warnings.append(
            f"{logstore}: total_issues mismatch (summary={actual_total}, actual={expected_total})"
        )
    
    # Check by_severity sum
    by_severity = summary.get("by_severity", {})
    severity_sum = sum(by_severity.values())
    if severity_sum != expected_total:
        warnings.append(
            f"{logstore}: severity sum mismatch (sum={severity_sum}, actual={expected_total})"
        )
    
    return warnings


def aggregate_audits(project_dir: str) -> dict[str, Any]:
    """
    Aggregate all per-logstore audit results.
    
    Returns:
        Aggregated audit data dictionary
    """
    audit_results = find_audit_results(project_dir)
    
    if not audit_results:
        log("WARNING: No audit results found")
        return {
            "project": os.path.basename(project_dir),
            "timestamp": datetime.now().isoformat(),
            "logstores_audited": 0,
            "total_issues": 0,
            "issues_by_check": {},
            "issues_by_severity": {},
            "typical_issues_by_check": {},
            "per_logstore_summary": [],
            "all_issues": [],
        }
    
    log(f"Found {len(audit_results)} audit result files")
    
    VALID_SEVERITIES = {"ERROR", "WARN"}

    # Aggregate data
    all_issues = []
    per_logstore_summary = []
    issues_by_check = Counter()
    issues_by_severity = Counter()
    first_issue_by_check = {}  # One representative issue per check

    for logstore_name, result_path in audit_results:
        result = load_json(result_path)
        if result is None:
            log(f"WARNING: Failed to load {result_path}")
            continue

        # Validate numeric consistency
        validation_warnings = validate_result(result, logstore_name)
        for warning in validation_warnings:
            log(f"WARNING: {warning}")

        logstore_issues = []
        logstore_by_severity = Counter()

        # New format: issues array at top level
        issues = result.get("issues", [])

        # Fallback: old format with checks array (for backward compatibility)
        if not issues and "checks" in result:
            for check in result.get("checks", []):
                check_name = check.get("check", "unknown")
                for issue in check.get("issues", []):
                    issue["check"] = check_name
                    issues.append(issue)

        for issue in issues:
            severity = issue.get("severity", "WARN")
            if severity not in VALID_SEVERITIES:
                continue

            check_name = issue.get("check", "unknown")
            detail = issue.get("detail", issue.get("type", issue.get("issue", "")))

            issues_by_check[check_name] += 1
            issues_by_severity[severity] += 1
            logstore_by_severity[severity] += 1

            logstore_issues.append({
                "logstore": logstore_name,
                "check": check_name,
                "severity": severity,
                "detail": detail,
                "details": issue,
            })

            if check_name not in first_issue_by_check:
                first_issue_by_check[check_name] = detail

        all_issues.extend(logstore_issues)

        per_logstore_summary.append({
            "name": logstore_name,
            "total_issues": len(logstore_issues),
            "by_severity": dict(logstore_by_severity),
        })

    per_logstore_summary.sort(key=lambda x: x["total_issues"], reverse=True)

    return {
        "project": os.path.basename(project_dir),
        "timestamp": datetime.now().isoformat(),
        "logstores_audited": len(audit_results),
        "total_issues": len(all_issues),
        "issues_by_check": dict(issues_by_check),
        "issues_by_severity": dict(issues_by_severity),
        "typical_issues_by_check": first_issue_by_check,
        "per_logstore_summary": per_logstore_summary,
        "all_issues": all_issues,
    }


def generate_summary_md(audit_data: dict, project_dir: str) -> str:
    """Generate markdown summary report."""
    project_name = audit_data.get("project", "unknown")
    total = audit_data.get("total_issues", 0)
    by_severity = audit_data.get("issues_by_severity", {})
    by_check = audit_data.get("issues_by_check", {})
    typical_issues = audit_data.get("typical_issues_by_check", {})
    per_logstore = audit_data.get("per_logstore_summary", [])

    lines = [
        f"# 审计报告：{project_name}",
        "",
        "## 概览",
        "",
        "| 指标 | 值 |",
        "|------|-----|",
        f"| 审计 logstore 数 | {audit_data.get('logstores_audited', 0)} |",
        f"| 发现问题总数 | {total} |",
        f"| ERROR 级别 | {by_severity.get('ERROR', 0)} |",
        f"| WARN 级别 | {by_severity.get('WARN', 0)} |",
        "",
        "## 问题分布（按检查项）",
        "",
        "| 检查项 | 问题数 |",
        "|--------|--------|",
    ]

    for check_name, count in sorted(by_check.items(), key=lambda x: -x[1]):
        lines.append(f"| {check_name} | {count} |")

    lines.extend([
        "",
        "## 典型问题示例",
        "",
    ])

    if typical_issues:
        for check_name in sorted(by_check.keys(), key=lambda c: -by_check.get(c, 0)):
            detail = typical_issues.get(check_name, "")
            if detail:
                detail_escaped = detail.replace("`", "'")[:200]
                if len(detail) > 200:
                    detail_escaped += "..."
                lines.append(f"- **{check_name}**：{detail_escaped}")
    else:
        lines.append("无问题发现")
    
    lines.extend([
        "",
        "## 各 Logstore 问题数",
        "",
        "| Logstore | 问题数 | ERROR | WARN |",
        "|----------|--------|-------|------|",
    ])
    
    for ls in per_logstore[:20]:  # Show top 20
        name = ls["name"]
        count = ls["total_issues"]
        error = ls.get("by_severity", {}).get("ERROR", 0)
        warn = ls.get("by_severity", {}).get("WARN", 0)
        lines.append(f"| {name} | {count} | {error} | {warn} |")
    
    if len(per_logstore) > 20:
        lines.append(f"| ... | ... | ... | ... |")
        lines.append(f"| （共 {len(per_logstore)} 个 logstore） | | | |")
    
    lines.extend([
        "",
        "## 详细问题列表",
        "",
        "见 `_audit/<logstore>/audit_report.md`",
        "",
    ])
    
    return "\n".join(lines)


def save_outputs(project_dir: str, audit_data: dict, summary_md: str):
    """Save aggregated data and summary report."""
    audit_dir = os.path.join(project_dir, "_audit")
    os.makedirs(audit_dir, exist_ok=True)
    
    # Save audit_data.json
    data_path = os.path.join(audit_dir, "audit_data.json")
    with open(data_path, "w", encoding="utf-8") as f:
        json.dump(audit_data, f, ensure_ascii=False, indent=2)
        f.write("\n")
    log(f"Saved {data_path}")
    
    # Save audit_summary.md
    summary_path = os.path.join(audit_dir, "audit_summary.md")
    with open(summary_path, "w", encoding="utf-8") as f:
        f.write(summary_md)
    log(f"Saved {summary_path}")


def main():
    parser = argparse.ArgumentParser(
        description="Aggregate audit results from per-logstore audits"
    )
    parser.add_argument("project_dir", help="Project input directory")
    
    args = parser.parse_args()
    
    # Aggregate audits
    audit_data = aggregate_audits(args.project_dir)
    
    # Generate summary markdown
    summary_md = generate_summary_md(audit_data, args.project_dir)
    
    # Save outputs
    save_outputs(args.project_dir, audit_data, summary_md)
    
    # Output summary JSON to stdout (without all_issues for brevity)
    output = {k: v for k, v in audit_data.items() if k != "all_issues"}
    print(json.dumps(output, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
