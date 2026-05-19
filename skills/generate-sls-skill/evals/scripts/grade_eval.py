#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Programmatic grading for generate-sls-skill eval outputs.

Checks expectations that can be verified from files alone (no transcript needed).

Usage:
    grade_eval.py <outputs_dir> <eval_id> [--logstores audit,k8s-event,nginx-ingress]
"""

import argparse
import json
import re
import sys
from pathlib import Path


def load_evals(evals_path: Path, eval_id: int) -> list[str]:
    """Load expectations for eval_id from evals.json."""
    with open(evals_path, encoding="utf-8") as f:
        data = json.load(f)
    for e in data["evals"]:
        if e["id"] == eval_id:
            return e.get("expectations", [])
    return []


def get_project_dirs(outputs_dir: Path) -> list[Path]:
    """Return project dirs that contain a SKILL.md entry file."""
    return sorted(
        p for p in outputs_dir.iterdir()
        if p.is_dir() and (p / "SKILL.md").exists()
    )


def get_reference_docs(outputs_dir: Path, logstores_arg: str | None) -> list[Path]:
    """Return project/references/*.md docs, excluding *-queries-extra.md."""
    docs: list[Path] = []
    for project_dir in get_project_dirs(outputs_dir):
        refs_dir = project_dir / "references"
        if not refs_dir.is_dir():
            continue
        docs.extend(
            p for p in refs_dir.glob("*.md")
            if not p.name.endswith("-queries-extra.md")
        )

    if logstores_arg:
        names = {s.strip() for s in logstores_arg.split(",") if s.strip()}
        docs = [p for p in docs if p.stem in names]

    return sorted(docs)


def check_markdown_table(content: str, min_rows: int) -> tuple[bool, str]:
    """Check for a Markdown table with at least min_rows data rows."""
    rows = re.findall(r"^\|.+\|.+\|", content, re.MULTILINE)
    data_rows = max(len(rows) - 1, 0) if rows else 0
    if data_rows >= min_rows:
        return True, f"包含 {data_rows} 行数据"
    return False, f"只有 {data_rows} 行数据，期望至少 {min_rows} 行"


def check_query_count(content: str, expected_count: int) -> tuple[bool, str]:
    """Check exactly expected_count query titles in ## 查询示例."""
    match = re.search(r"## 查询示例\s*\n(.*?)(?=\n## |\Z)", content, re.DOTALL)
    if not match:
        return False, "缺少 ## 查询示例 章节"
    section = match.group(1)
    queries = re.findall(r"^\*\*[^*]+\*\*$", section, re.MULTILINE)
    if len(queries) == expected_count:
        return True, f"包含 {len(queries)} 个查询示例"
    return False, f"包含 {len(queries)} 个查询示例，期望恰好 {expected_count} 个"


def check_yaml_frontmatter(content: str) -> tuple[bool, str]:
    """Check for YAML frontmatter with name and description."""
    if not content.strip().startswith("---"):
        return False, "未找到 YAML frontmatter (---)"
    match = re.match(r"^---\n(.*?)\n---", content, re.DOTALL)
    if not match:
        return False, "frontmatter 格式无效"
    fm = match.group(1)
    if "name:" not in fm and "name " not in fm:
        return False, "frontmatter 缺少 name 字段"
    if "description:" not in fm and "description " not in fm:
        return False, "frontmatter 缺少 description 字段"
    return True, "包含 YAML frontmatter (name, description)"


def check_fields_section_with_table(content: str) -> tuple[bool, str]:
    """Check ## 字段参考 exists and contains at least 5 data rows."""
    if "## 字段参考" not in content:
        return False, "缺少 ## 字段参考 章节"
    match = re.search(r"## 字段参考\s*\n(.*?)(?=\n## |\Z)", content, re.DOTALL)
    if not match:
        return False, "## 字段参考 章节为空"
    return check_markdown_table(match.group(1), 5)


def check_project_skill_exists(outputs_dir: Path) -> tuple[bool, str]:
    projects = get_project_dirs(outputs_dir)
    if projects:
        return True, f"找到 {projects[0].name}/SKILL.md"
    return False, "未找到 project/SKILL.md"


def make_project_skill_table_checker(min_rows: int):
    text = f"project/SKILL.md 包含 logstore reference 表格，至少 {min_rows} 行数据"

    def checker(o: Path, _la, _ei) -> list[tuple[str, bool, str]]:
        projects = get_project_dirs(o)
        if not projects:
            return [(text, False, "未找到 project/SKILL.md")]
        content = (projects[0] / "SKILL.md").read_text(encoding="utf-8")
        return [(text, *check_markdown_table(content, min_rows))]

    return checker


def make_reference_count_checker(expected: int):
    text = f"project 有 {expected} 个 logstore reference 文档"

    def checker(o: Path, la: str | None, _ei) -> list[tuple[str, bool, str]]:
        actual = len(get_reference_docs(o, la))
        return [(text, actual == expected, f"期望 {expected} 个，实际 {actual} 个")]

    return checker


def content_check_for_references(
    outputs_dir: Path,
    logstores_arg: str | None,
    text: str,
    check_fn,
) -> list[tuple[str, bool, str]]:
    docs = get_reference_docs(outputs_dir, logstores_arg)
    if not docs:
        return [(text, False, "无 logstore reference 文档")]
    failures = []
    for doc in docs:
        passed, evidence = check_fn(doc.read_text(encoding="utf-8"))
        if not passed:
            failures.append(f"{doc.name}: {evidence}")
    ok = len(failures) == 0
    return [(text, ok, "全部通过" if ok else "; ".join(failures))]


def check_no_root_skill(outputs_dir: Path) -> tuple[bool, str]:
    root_skill = outputs_dir / "SKILL.md"
    if root_skill.exists():
        return False, "错误：存在根目录 SKILL.md"
    return True, "根目录不存在 SKILL.md"


def check_no_legacy_outputs(outputs_dir: Path) -> tuple[bool, str]:
    bad_files: list[str] = []
    if (outputs_dir / "SOP.md").exists():
        bad_files.append("SOP.md")
    for path in outputs_dir.rglob("overview.md"):
        bad_files.append(str(path.relative_to(outputs_dir)))
    for skill_path in outputs_dir.rglob("SKILL.md"):
        if skill_path.parent == outputs_dir:
            bad_files.append(str(skill_path.relative_to(outputs_dir)))
            continue
        if skill_path.parent.parent != outputs_dir:
            bad_files.append(str(skill_path.relative_to(outputs_dir)))

    if bad_files:
        return False, "发现旧结构文件: " + ", ".join(sorted(bad_files))
    return True, "未发现旧结构文件"


def check_project_skill_frontmatter(outputs_dir: Path) -> tuple[bool, str]:
    projects = get_project_dirs(outputs_dir)
    if not projects:
        return False, "未找到 project/SKILL.md"
    return check_yaml_frontmatter((projects[0] / "SKILL.md").read_text(encoding="utf-8"))


PROGRAMMATIC_CHECKS = {
    "project/SKILL.md 已生成": lambda o, _la, _: [
        ("project/SKILL.md 已生成", *check_project_skill_exists(o))
    ],
    "project/SKILL.md 包含 logstore reference 表格，至少 1 行数据": make_project_skill_table_checker(1),
    "project/SKILL.md 包含 logstore reference 表格，至少 3 行数据": make_project_skill_table_checker(3),
    "project 有 1 个 logstore reference 文档": make_reference_count_checker(1),
    "project 有 3 个 logstore reference 文档": make_reference_count_checker(3),
    "不存在根目录 SKILL.md": lambda o, _la, _: [
        ("不存在根目录 SKILL.md", *check_no_root_skill(o))
    ],
    "不存在 SOP.md 或 overview.md 或 logstore 级 SKILL.md": lambda o, _la, _: [
        ("不存在 SOP.md 或 overview.md 或 logstore 级 SKILL.md", *check_no_legacy_outputs(o))
    ],
    "每个 logstore reference 文档包含 ## 使用说明 章节": lambda o, la, _: content_check_for_references(
        o, la, "每个 logstore reference 文档包含 ## 使用说明 章节",
        lambda c: (True, "包含") if "## 使用说明" in c else (False, "缺少 ## 使用说明"),
    ),
    "每个 logstore reference 文档包含 ## 数据源 章节": lambda o, la, _: content_check_for_references(
        o, la, "每个 logstore reference 文档包含 ## 数据源 章节",
        lambda c: (True, "包含") if "## 数据源" in c else (False, "缺少 ## 数据源"),
    ),
    "每个 logstore reference 文档包含 ## 字段参考 章节，至少 5 行表格": lambda o, la, _: content_check_for_references(
        o, la, "每个 logstore reference 文档包含 ## 字段参考 章节，至少 5 行表格",
        check_fields_section_with_table,
    ),
    "每个 logstore reference 文档包含 ## 查询示例 章节": lambda o, la, _: content_check_for_references(
        o, la, "每个 logstore reference 文档包含 ## 查询示例 章节",
        lambda c: (True, "包含") if "## 查询示例" in c else (False, "缺少 ## 查询示例"),
    ),
    "project/SKILL.md 包含 YAML frontmatter，且有 name 和 description 字段": lambda o, _la, _: [
        ("project/SKILL.md 包含 YAML frontmatter，且有 name 和 description 字段", *check_project_skill_frontmatter(o))
    ],
    "查询示例恰好 20 个": lambda o, la, _ei: content_check_for_references(
        o, la, "查询示例恰好 20 个",
        lambda c: check_query_count(c, 20),
    ),
}


def run_checks(outputs_dir: Path, eval_id: int, logstores_arg: str | None) -> list[dict]:
    """Run all programmatic checks."""
    evals_path = Path(__file__).resolve().parent.parent / "evals.json"
    expectations = load_evals(evals_path, eval_id)

    results = []
    for exp in expectations:
        if exp not in PROGRAMMATIC_CHECKS:
            continue
        try:
            entries = PROGRAMMATIC_CHECKS[exp](outputs_dir, logstores_arg, eval_id)
            for t, p, e in entries:
                results.append({"text": t, "passed": p, "evidence": e})
        except Exception as err:
            results.append({"text": exp, "passed": False, "evidence": f"检查异常: {err}"})
    return results


def main():
    parser = argparse.ArgumentParser(description="Programmatic grading for generate-sls-skill evals")
    parser.add_argument("outputs_dir", type=Path, help="Path to outputs directory")
    parser.add_argument("eval_id", type=int, help="Eval ID")
    parser.add_argument("--logstores", type=str, default=None,
                        help="Comma-separated logstore aliases")
    args = parser.parse_args()

    if not args.outputs_dir.is_dir():
        print(json.dumps({"error": f"outputs_dir not found: {args.outputs_dir}"}, ensure_ascii=False), file=sys.stderr)
        sys.exit(1)

    results = run_checks(args.outputs_dir, args.eval_id, args.logstores)
    passed = sum(1 for r in results if r["passed"])
    total = len(results)

    out = {
        "expectations": results,
        "summary": {
            "passed": passed,
            "failed": total - passed,
            "total": total,
            "pass_rate": round(passed / total, 2) if total else 0,
        },
        "_note": "Partial result. LLM grader must fill transcript-based expectations.",
    }
    print(json.dumps(out, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
