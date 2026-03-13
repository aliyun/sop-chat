#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Programmatic grading for generate-sls-sop eval outputs.

Checks expectations that can be verified from files alone (no transcript needed).
Outputs partial grading.json format to stdout for merger with LLM grader results.

Usage:
    grade_eval.py <outputs_dir> <eval_id> [--logstores audit,k8s-event,nginx-ingress]
    
    # Standard structure (with run-1/)
    grade_eval.py iteration-1/eval-bill-analysis/with_skill/run-1/outputs 1
    grade_eval.py iteration-1/eval-k8s/with_skill/run-1/outputs 2 --logstores audit,k8s-event,nginx-ingress

Output (stdout): JSON with expectations array and summary.
Expectations not covered by this script must be graded by the LLM grader.

Expected file structure:
    iteration-1/
    └── eval-X/
        ├── with_skill/
        │   └── run-1/
        │       ├── outputs/           ← pass this as outputs_dir
        │       ├── grading.json       ← save output here
        │       └── timing.json
        └── without_skill/
            └── run-1/
                ├── outputs/
                └── grading.json
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


def get_logstore_dirs(
    outputs_dir: Path,
    logstores_arg: str | None,
    doc_filename: str = "overview.md",
) -> list[Path]:
    """返回 logstore 目录列表，按 3 层结构: outputs/{project}/{logstore}/
    
    支持 overview.md 和 SKILL.md（内容结构相同，仅文件名不同）。
    - outputs/{project}/{doc_filename} (project 索引)
    - outputs/{project}/{logstore}/{doc_filename} (logstore 内容)
    
    强制 2 层嵌套：outputs/{project}/{logstore}/
    """
    logstore_dirs = []

    for project_dir in outputs_dir.iterdir():
        if not project_dir.is_dir():
            continue

        for item in project_dir.iterdir():
            if item.is_dir() and (item / doc_filename).exists():
                logstore_dirs.append(item)

    if logstores_arg:
        names = {s.strip() for s in logstores_arg.split(",") if s.strip()}
        logstore_dirs = [d for d in logstore_dirs if d.name in names]

    return sorted(logstore_dirs)


def check_overview_md_table(content: str) -> tuple[bool, str]:
    """Check for Markdown table format | col | col | col |"""
    table_lines = re.findall(r"^\|.+\|.+\|", content, re.MULTILINE)
    if len(table_lines) >= 2:  # header + separator or data
        return True, f"包含 {len(table_lines)} 行 Markdown 表格"
    return False, "未找到 Markdown 表格格式（| 列 | 列 | 列 |）"


def check_categorized_queries(content: str) -> tuple[bool, str]:
    """Check for ### category headers."""
    headers = re.findall(r"^###\s+.+", content, re.MULTILINE)
    if len(headers) >= 1:
        return True, f"包含 {len(headers)} 个分类标题 (###)"
    return False, "未找到 ### 分类标题"


def check_query_has_title_sql_category(content: str) -> tuple[bool, str]:
    """Check that queries have bold title, code block, and are under ### category."""
    # Must have ### headers
    if not re.search(r"^###\s+", content, re.MULTILINE):
        return False, "未找到 ### 分类标题"
    # Must have **title** pattern
    if not re.search(r"\*\*[^*]+\*\*", content):
        return False, "未找到粗体查询标题 (**标题**)"
    # Must have ```query or ```sql or ``` code block
    if not re.search(r"```(?:query|sql|)\s*\n", content):
        return False, "未找到查询代码块 (```query 或 ```sql)"
    return True, "包含 ### 分类、粗体标题和查询代码块"


def check_query_count(content: str, expected_count: int) -> tuple[bool, str]:
    """Check exactly expected_count **query_title** exist in ## 查询示例 section."""
    match = re.search(r'## 查询示例\s*\n(.*?)(?=\n## |\Z)', content, re.DOTALL)
    if not match:
        return False, "缺少 ## 查询示例 章节"
    section = match.group(1)
    queries = re.findall(r'^\*\*[^*]+\*\*$', section, re.MULTILINE)
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


def check_sop_md_exists(outputs_dir: Path) -> tuple[bool, str]:
    """检查 SOP.md 是否存在"""
    sop = outputs_dir / "SOP.md"
    if not sop.exists():
        return False, "SOP.md 不存在"
    return True, f"SOP.md 存在 ({sop.stat().st_size} bytes)"


def check_sop_md_has_project_index(outputs_dir: Path) -> tuple[bool, str]:
    """检查 SOP.md 是否包含 project 索引表格"""
    sop = outputs_dir / "SOP.md"
    if not sop.exists():
        return False, "SOP.md 不存在"
    content = sop.read_text(encoding="utf-8")
    # 检查是否有表格格式 | ... | ... |
    table_rows = re.findall(r"^\|.+\|.+\|", content, re.MULTILINE)
    if len(table_rows) < 2:  # 至少需要 header + 1 行数据
        return False, "SOP.md 不包含索引表格"
    return True, f"SOP.md 包含 {len(table_rows) - 1} 行索引"


def check_project_doc_exists(outputs_dir: Path, doc_filename: str) -> tuple[bool, str]:
    """检查 project/{doc_filename} 是否存在（支持 overview.md 或 SKILL.md）"""
    for item in outputs_dir.iterdir():
        if item.is_dir():
            doc_path = item / doc_filename
            if doc_path.exists():
                return True, f"找到 {item.name}/{doc_filename}"
    return False, f"未找到 project/{doc_filename}"


def check_project_overview_exists(outputs_dir: Path) -> tuple[bool, str]:
    """检查 project/overview.md 是否存在"""
    return check_project_doc_exists(outputs_dir, "overview.md")


def check_fields_section_with_table(content: str) -> tuple[bool, str]:
    """检查 ## 字段参考 章节是否存在且包含至少 5 行表格"""
    if "## 字段参考" not in content:
        return False, "缺少 ## 字段参考 章节"
    # 提取 ## 字段参考 到下一个 ## 之间的内容
    match = re.search(r"## 字段参考\s*\n(.*?)(?=\n## |\Z)", content, re.DOTALL)
    if not match:
        return False, "## 字段参考 章节为空"
    section_content = match.group(1)
    # 统计表格行数（| ... | ... |）
    rows = re.findall(r"^\|.+\|.+\|", section_content, re.MULTILINE)
    data_rows = len(rows) - 1 if rows else 0  # 减去表头
    if data_rows < 5:
        return False, f"字段参考表格只有 {data_rows} 行（需要至少 5 行）"
    return True, f"包含 {data_rows} 行字段参考"


def _content_check(ld: list[Path], text: str, check_fn, doc_filename: str = "overview.md") -> list[tuple[str, bool, str]]:
    """Run content check on each logstore's doc; pass only if all pass.
    SKILL.md 和 overview.md 内容结构相同，共用此函数。"""
    if not ld:
        return [(text, False, "无 logstore 目录")]
    failures = []
    for d in ld:
        doc_path = d / doc_filename
        if not doc_path.exists():
            failures.append(f"{d.name}: 无 {doc_filename}")
            continue
        passed, evidence = check_fn(doc_path.read_text(encoding="utf-8"))
        if not passed:
            failures.append(f"{d.name}: {evidence}")
    ok = len(failures) == 0
    return [(text, ok, "全部通过" if ok else "; ".join(failures))]


def _make_logstore_count_checker(expected: int, doc_filename: str = "overview.md"):
    """创建一个检查特定数量 logstore 文档的函数（支持 overview.md 或 SKILL.md）"""
    text = f"project 有 {expected} 个 logstore 的 {doc_filename}"

    def checker(o: Path, logstores_arg: str | None, _) -> list[tuple[str, bool, str]]:
        actual_ld = get_logstore_dirs(o, logstores_arg, doc_filename)
        actual = len(actual_ld)
        passed = actual == expected
        evidence = f"期望 {expected} 个，实际 {actual} 个"
        return [(text, passed, evidence)]

    return checker


def _make_project_doc_table_checker(min_rows: int, doc_filename: str):
    """创建一个检查 project/{doc_filename} 索引表格行数的函数"""
    text = f"project/{doc_filename} 包含 logstore 索引表格，至少 {min_rows} 行数据"

    def checker(o: Path, _la, _ei) -> list[tuple[str, bool, str]]:
        for item in o.iterdir():
            if item.is_dir():
                doc_path = item / doc_filename
                if doc_path.exists():
                    content = doc_path.read_text(encoding="utf-8")
                    rows = re.findall(r"^\|.+\|.+\|", content, re.MULTILINE)
                    data_rows = len(rows) - 1  # 减去表头
                    passed = data_rows >= min_rows
                    evidence = f"找到 {data_rows} 行数据（需要至少 {min_rows} 行）"
                    return [(text, passed, evidence)]
        return [(text, False, f"未找到 project/{doc_filename}")]

    return checker


def _make_project_doc_exists_checker(doc_filename: str, text: str | None = None):
    """创建一个检查 project/{doc_filename} 是否存在的函数"""
    display_text = text or f"project/{doc_filename} 已生成"

    def checker(o: Path, _la, _ei) -> list[tuple[str, bool, str]]:
        passed, evidence = check_project_doc_exists(o, doc_filename)
        return [(display_text, passed, evidence)]

    return checker


def _content_check_for_doc(o: Path, la: str | None, text: str, check_fn, doc_filename: str):
    """Helper: run _content_check with ld from get_logstore_dirs(o, la, doc_filename)."""
    ld = get_logstore_dirs(o, la, doc_filename)
    return _content_check(ld, text, check_fn, doc_filename)


# Mapping: expectation text -> check_fn(outputs_dir, logstores_arg, eval_id) -> list of (text, passed, evidence)
PROGRAMMATIC_CHECKS = {
    # === SOP.md 顶层索引（eval 2）===
    "SOP.md 已生成": lambda o, _la, _: [("SOP.md 已生成", *check_sop_md_exists(o))],
    "SOP.md 包含 project 索引表格，至少 1 行数据": lambda o, _la, _: [
        ("SOP.md 包含 project 索引表格，至少 1 行数据", *check_sop_md_has_project_index(o))
    ],

    # === project/overview.md 索引（eval 2）===
    "project/overview.md 已生成": lambda o, _la, _: [
        ("project/overview.md 已生成", *check_project_overview_exists(o))
    ],
    "project/overview.md 包含 logstore 索引表格，至少 1 行数据": _make_project_doc_table_checker(1, "overview.md"),
    "project/overview.md 包含 logstore 索引表格，至少 3 行数据": _make_project_doc_table_checker(3, "overview.md"),

    # === project/SKILL.md 索引（eval 1）===
    "project/SKILL.md 已生成（project 级索引）": _make_project_doc_exists_checker(
        "SKILL.md", "project/SKILL.md 已生成（project 级索引）"
    ),
    "project/SKILL.md 包含 logstore 索引表格，至少 1 行数据": _make_project_doc_table_checker(1, "SKILL.md"),

    # === logstore 数量检查 ===
    "project 有 1 个 logstore 的 overview.md": _make_logstore_count_checker(1, "overview.md"),
    "project 有 3 个 logstore 的 overview.md": _make_logstore_count_checker(3, "overview.md"),
    "project 有 1 个 logstore 的 SKILL.md": _make_logstore_count_checker(1, "SKILL.md"),

    # === 不存在根目录 SKILL.md（eval 1 特有）===
    "不存在根目录 SKILL.md（SKILL 格式不生成根索引）": lambda o, _la, _: [
        (
            "不存在根目录 SKILL.md（SKILL 格式不生成根索引）",
            not (o / "SKILL.md").exists(),
            "根目录不存在 SKILL.md" if not (o / "SKILL.md").exists() else "错误：存在根目录 SKILL.md",
        )
    ],

    # === logstore/SKILL.md 已生成（eval 1）===
    "logstore/SKILL.md 已生成（替代 overview.md）": lambda o, la, _: [
        (
            "logstore/SKILL.md 已生成（替代 overview.md）",
            len(get_logstore_dirs(o, la, "SKILL.md")) >= 1,
            f"找到 {len(get_logstore_dirs(o, la, 'SKILL.md'))} 个 logstore 的 SKILL.md",
        )
    ],

    # === logstore 内容质量检查（overview.md，eval 2）===
    "每个 logstore/overview.md 包含 ## 使用说明 章节": lambda o, la, _: _content_check_for_doc(
        o, la, "每个 logstore/overview.md 包含 ## 使用说明 章节",
        lambda c: (True, "包含") if "## 使用说明" in c else (False, "缺少 ## 使用说明"),
        "overview.md",
    ),
    "每个 logstore/overview.md 包含 ## 数据源 章节": lambda o, la, _: _content_check_for_doc(
        o, la, "每个 logstore/overview.md 包含 ## 数据源 章节",
        lambda c: (True, "包含") if "## 数据源" in c else (False, "缺少 ## 数据源"),
        "overview.md",
    ),
    "每个 logstore/overview.md 包含 ## 字段参考 章节，至少 5 行表格": lambda o, la, _: _content_check_for_doc(
        o, la, "每个 logstore/overview.md 包含 ## 字段参考 章节，至少 5 行表格",
        check_fields_section_with_table,
        "overview.md",
    ),
    "每个 logstore/overview.md 包含 ## 查询示例 章节": lambda o, la, _: _content_check_for_doc(
        o, la, "每个 logstore/overview.md 包含 ## 查询示例 章节",
        lambda c: (True, "包含") if "## 查询示例" in c else (False, "缺少 ## 查询示例"),
        "overview.md",
    ),
    "每个 logstore/overview.md 包含 YAML frontmatter，且有 name 和 description 字段": lambda o, la, _: _content_check_for_doc(
        o, la, "每个 logstore/overview.md 包含 YAML frontmatter，且有 name 和 description 字段",
        check_yaml_frontmatter,
        "overview.md",
    ),

    # === logstore 内容质量检查（SKILL.md，eval 1）===
    "每个 logstore/SKILL.md 包含 ## 使用说明 章节": lambda o, la, _: _content_check_for_doc(
        o, la, "每个 logstore/SKILL.md 包含 ## 使用说明 章节",
        lambda c: (True, "包含") if "## 使用说明" in c else (False, "缺少 ## 使用说明"),
        "SKILL.md",
    ),
    "每个 logstore/SKILL.md 包含 ## 数据源 章节": lambda o, la, _: _content_check_for_doc(
        o, la, "每个 logstore/SKILL.md 包含 ## 数据源 章节",
        lambda c: (True, "包含") if "## 数据源" in c else (False, "缺少 ## 数据源"),
        "SKILL.md",
    ),
    "每个 logstore/SKILL.md 包含 ## 字段参考 章节，至少 5 行表格": lambda o, la, _: _content_check_for_doc(
        o, la, "每个 logstore/SKILL.md 包含 ## 字段参考 章节，至少 5 行表格",
        check_fields_section_with_table,
        "SKILL.md",
    ),
    "每个 logstore/SKILL.md 包含 ## 查询示例 章节": lambda o, la, _: _content_check_for_doc(
        o, la, "每个 logstore/SKILL.md 包含 ## 查询示例 章节",
        lambda c: (True, "包含") if "## 查询示例" in c else (False, "缺少 ## 查询示例"),
        "SKILL.md",
    ),
    "每个 logstore/SKILL.md 包含 YAML frontmatter，且有 name 和 description 字段": lambda o, la, _: _content_check_for_doc(
        o, la, "每个 logstore/SKILL.md 包含 YAML frontmatter，且有 name 和 description 字段",
        check_yaml_frontmatter,
        "SKILL.md",
    ),

    # === 查询示例数量（eval 1 用 SKILL.md，eval 2 用 overview.md）===
    "查询示例恰好 20 个": lambda o, la, ei: _content_check_for_doc(
        o, la, "查询示例恰好 20 个",
        lambda c: check_query_count(c, 20),
        "SKILL.md" if ei == 1 else "overview.md",
    ),
}


def run_checks(outputs_dir: Path, eval_id: int, logstores_arg: str | None) -> list[dict]:
    """Run all programmatic checks. Returns list of {text, passed, evidence}."""
    evals_path = Path(__file__).resolve().parent.parent.parent / "evals" / "evals.json"
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
    parser = argparse.ArgumentParser(description="Programmatic grading for generate-sls-sop evals")
    parser.add_argument("outputs_dir", type=Path, help="Path to outputs directory")
    parser.add_argument("eval_id", type=int, help="Eval ID (1 or 2)")
    parser.add_argument("--logstores", type=str, default=None,
                        help="Comma-separated logstore names for eval 2 (e.g. audit,k8s-event,nginx-ingress)")
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
