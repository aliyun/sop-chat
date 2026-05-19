#!/usr/bin/env python3
"""
Finalize audit: filter issues, assemble result, generate report.

Reads:
- _audit/<logstore>/audit_issues.json - LLM semantic issues
- _audit/<logstore>/audit_context.json - merged context (from prepare_audit.py)

Generates:
- _audit/<logstore>/audit_result.json - complete structured result
- _audit/<logstore>/audit_report.md - human-readable report

Filters: only ERROR and WARN issues are kept; OK/INFO are discarded.

Usage:
    finalize_audit.py <project_dir> <logstore>

stdout: JSON summary
stderr: progress messages
"""

import argparse
import json
import os
import sys
from collections import Counter, defaultdict
from typing import Any


VALID_SEVERITIES = {"ERROR", "WARN"}


def log(msg: str):
    print(msg, file=sys.stderr, flush=True)


def load_json(path: str) -> dict | list | None:
    """Load JSON file, return None if not exists or invalid."""
    if not os.path.exists(path):
        return None
    try:
        with open(path, "r", encoding="utf-8") as f:
            return json.load(f)
    except (json.JSONDecodeError, IOError) as e:
        log(f"WARNING: Failed to load {path}: {e}")
        return None


def filter_issues(issues: list[dict]) -> list[dict]:
    """Keep only ERROR and WARN issues."""
    return [i for i in issues if i.get("severity") in VALID_SEVERITIES]


def read_context_from_audit_context(audit_context: dict) -> dict[str, Any]:
    """Build context dict from audit_context.json for report compatibility."""
    return {
        "candidates_total": audit_context.get("candidates_count", 0),
        "selected_count": audit_context.get("selected_count", 0),
        "extra_count": audit_context.get("extra_count", 0),
        "categories": audit_context.get("categories", []),
    }


def compute_summary(issues: list[dict]) -> dict[str, Any]:
    """Compute summary statistics from issues list."""
    by_severity = Counter()
    by_check = Counter()
    for issue in issues:
        severity = issue.get("severity", "WARN")
        check = issue.get("check", "unknown")
        by_severity[severity] += 1
        by_check[check] += 1
    return {
        "total_issues": len(issues),
        "by_severity": dict(by_severity),
        "by_check": dict(by_check),
    }


def generate_report_md(logstore: str, context: dict, issues: list[dict], summary: dict) -> str:
    """Generate markdown report from audit result."""
    lines = [
        f"# 审计报告：{logstore}",
        "",
        "## 上下文",
        "",
        "| 指标 | 值 |",
        "|------|-----|",
        f"| 候选查询数 | {context.get('candidates_total', 0)} |",
        f"| 入选查询数 | {context.get('selected_count', 0)} |",
        f"| Extra 查询数 | {context.get('extra_count', 0)} |",
        f"| 分类数 | {len(context.get('categories', []))} |",
        "",
    ]
    categories = context.get("categories", [])
    if categories:
        lines.append("**分类**：" + "、".join(categories))
        lines.append("")

    lines.extend([
        "## 问题统计",
        "",
        "| 指标 | 值 |",
        "|------|-----|",
        f"| 问题总数 | {summary.get('total_issues', 0)} |",
        f"| ERROR | {summary.get('by_severity', {}).get('ERROR', 0)} |",
        f"| WARN | {summary.get('by_severity', {}).get('WARN', 0)} |",
        "",
    ])

    by_check = summary.get("by_check", {})
    if by_check:
        lines.extend([
            "### 按检查项分布",
            "",
            "| 检查项 | 问题数 |",
            "|--------|--------|",
        ])
        for check_name, count in sorted(by_check.items(), key=lambda x: -x[1]):
            lines.append(f"| {check_name} | {count} |")
        lines.append("")

    lines.extend(["## 问题详情", ""])

    if not issues:
        lines.append("无问题发现。")
        lines.append("")
    else:
        issues_by_check = defaultdict(list)
        for issue in issues:
            check = issue.get("check", "unknown")
            issues_by_check[check].append(issue)

        for check_name, check_issues in sorted(issues_by_check.items()):
            lines.append(f"### {check_name} ({len(check_issues)})")
            lines.append("")
            lines.append("| ID | 严重度 | 详情 |")
            lines.append("|-----|--------|------|")
            for issue in check_issues:
                issue_id = issue.get("query_id", issue.get("id", "-"))
                severity = issue.get("severity", "WARN")
                detail = (issue.get("detail", "-") or "").replace("|", "\\|")
                lines.append(f"| {issue_id} | {severity} | {detail} |")
            lines.append("")

    return "\n".join(lines)


def finalize_audit(project_dir: str, logstore: str) -> dict[str, Any]:
    """
    Finalize audit: filter issues, read context, compute summary, build result.
    """
    audit_dir = os.path.join(project_dir, "_audit", logstore)

    issues_data = load_json(os.path.join(audit_dir, "audit_issues.json"))
    issues = issues_data.get("issues", []) if issues_data else []
    original_count = len(issues)
    issues = filter_issues(issues)
    filtered_count = original_count - len(issues)
    if filtered_count:
        log(f"Filtered {filtered_count} non-ERROR/WARN issues")
    log(f"Loaded {len(issues)} issues from audit_issues.json")

    audit_context = load_json(os.path.join(audit_dir, "audit_context.json"))
    if audit_context:
        context = read_context_from_audit_context(audit_context)
    else:
        log("WARNING: audit_context.json not found, using empty context")
        context = {"candidates_total": 0, "selected_count": 0, "extra_count": 0, "categories": []}

    summary = compute_summary(issues)
    result = {
        "logstore": logstore,
        "context": context,
        "issues": issues,
        "summary": summary,
    }
    return result


def save_audit_result(project_dir: str, logstore: str, result: dict):
    """Save audit_result.json."""
    audit_dir = os.path.join(project_dir, "_audit", logstore)
    os.makedirs(audit_dir, exist_ok=True)
    path = os.path.join(audit_dir, "audit_result.json")
    with open(path, "w", encoding="utf-8") as f:
        json.dump(result, f, ensure_ascii=False, indent=2)
        f.write("\n")
    log(f"Saved {path}")


def save_report(project_dir: str, logstore: str, md_content: str):
    """Save audit_report.md."""
    audit_dir = os.path.join(project_dir, "_audit", logstore)
    os.makedirs(audit_dir, exist_ok=True)
    path = os.path.join(audit_dir, "audit_report.md")
    with open(path, "w", encoding="utf-8") as f:
        f.write(md_content)
    log(f"Saved {path}")


def main():
    parser = argparse.ArgumentParser(
        description="Finalize audit: filter issues, assemble result, generate report"
    )
    parser.add_argument("project_dir", help="Project input directory")
    parser.add_argument("logstore", help="Logstore name")

    args = parser.parse_args()

    result = finalize_audit(args.project_dir, args.logstore)
    save_audit_result(args.project_dir, args.logstore, result)

    md_content = generate_report_md(
        result["logstore"],
        result["context"],
        result["issues"],
        result["summary"],
    )
    save_report(args.project_dir, args.logstore, md_content)

    output = {
        "logstore": result["logstore"],
        "context": result["context"],
        "summary": result["summary"],
    }
    print(json.dumps(output, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
