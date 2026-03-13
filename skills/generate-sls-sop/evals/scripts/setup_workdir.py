#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Setup isolated workdirs for generate-sls-sop eval runs.

IMPORTANT: generate-sls-sop requires execution within workdir.
- Executor must run in: <eval-name>/<config>/run-X/workdir/
- Input data available at: workdir/.input/<project>/

Creates with_skill/workdir and without_skill/workdir, each containing
only a copy of the fixture data under .input/.

The with_skill workdir also gets SKILL.md, scripts/ and rules/ to support SKILL execution.
The without_skill workdir contains ONLY the fixture data for true baseline.

Directory structure:
    iteration-1/
    └── eval-bill-analysis/
        ├── with_skill/
        │   ├── run-1/              ← first run
        │   │   ├── workdir/        ← executor runs here
        │   │   │   ├── .input/bill-analysis-xxx/
        │   │   │   ├── SKILL.md
        │   │   │   ├── scripts/
        │   │   │   └── rules/
        │   │   ├── grading.json
        │   │   └── timing.json
        │   ├── run-2/              ← second run (variance analysis)
        │   └── run-3/              ← third run
        └── without_skill/
            ├── run-1/workdir/
            ├── run-2/workdir/
            └── run-3/workdir/

Run numbering:
- Automatically increments: run-1, run-2, run-3, ...
- Supports variance analysis (multiple runs for statistical significance)
- Compatible with skill-creator's aggregate_benchmark.py

Path convention:
- Fixture source: evals/fixtures/<project-name>/
- evals.json files: [".input/<project-name>"]
- Workdir destination: workdir/.input/<project-name>/
- Convention: fixture basename = .input subdirectory name

Usage:
    # First run (creates run-1)
    setup_workdir.py <iteration_dir> <eval_name> <fixture_path>
    
    # Second run (creates run-2)
    setup_workdir.py <iteration_dir> <eval_name> <fixture_path>
    
    # Variance analysis (run 3 times for statistics)
    for i in {1..3}; do
        setup_workdir.py iteration-1 eval-bill fixtures/bill-xxx
        # Execute test...
    done

Example:
    # Single run
    setup_workdir.py generate-sls-sop-workspace/iteration-1 eval-bill-analysis evals/fixtures/bill-analysis-xxx
    
    # Multiple runs for variance analysis
    setup_workdir.py generate-sls-sop-workspace/iteration-1 eval-bill-analysis evals/fixtures/bill-analysis-xxx
    setup_workdir.py generate-sls-sop-workspace/iteration-1 eval-bill-analysis evals/fixtures/bill-analysis-xxx
    setup_workdir.py generate-sls-sop-workspace/iteration-1 eval-bill-analysis evals/fixtures/bill-analysis-xxx
"""

import argparse
import shutil
import sys
from pathlib import Path


def get_next_run_number(iteration_dir: Path, eval_name: str) -> int:
    """
    自动查找下一个可用的 run 编号。
    
    扫描 with_skill 和 without_skill 下的 run-* 目录，
    返回两者中最大编号 + 1。如果没有任何 run，返回 1。
    
    Args:
        iteration_dir: 迭代目录路径
        eval_name: 评估目录名
        
    Returns:
        下一个可用的 run 编号
    """
    max_run = 0
    for config in ("with_skill", "without_skill"):
        config_dir = iteration_dir / eval_name / config
        if config_dir.exists():
            for run_dir in config_dir.glob("run-*"):
                try:
                    num = int(run_dir.name.split("-")[1])
                    max_run = max(max_run, num)
                except (IndexError, ValueError):
                    # 跳过格式不正确的目录名
                    continue
    return max_run + 1


def main():
    parser = argparse.ArgumentParser(description="Setup isolated workdirs for eval runs")
    parser.add_argument("iteration_dir", type=Path, help="Iteration directory (e.g. generate-sls-sop-workspace/iteration-2)")
    parser.add_argument("eval_name", type=str, help="Eval directory name (e.g. eval-bill-analysis)")
    parser.add_argument("fixture_path", type=Path, help="Path to fixture dir (relative or absolute)")
    args = parser.parse_args()

    iteration_dir = args.iteration_dir.resolve()
    fixture = args.fixture_path if args.fixture_path.is_absolute() else (Path.cwd() / args.fixture_path).resolve()

    if not fixture.is_dir():
        print(f"Error: fixture not found: {fixture}", file=sys.stderr)
        sys.exit(1)

    fixture_name = fixture.name
    proj_root = Path(__file__).resolve().parent.parent.parent  # 项目根目录

    # 自动确定 run 编号
    run_number = get_next_run_number(iteration_dir, args.eval_name)

    for config in ("with_skill", "without_skill"):
        run_dir = iteration_dir / args.eval_name / config / f"run-{run_number}"
        workdir = run_dir / "workdir"
        input_dir = workdir / ".input"
        dest = input_dir / fixture_name

        # 检查目录是否已存在（不应该发生，因为我们自动递增）
        if run_dir.exists():
            print(f"Error: {run_dir} already exists (unexpected!)", file=sys.stderr)
            print(f"This should not happen with auto-increment.", file=sys.stderr)
            print(f"Please remove it manually: rm -rf {run_dir}", file=sys.stderr)
            sys.exit(1)

        # 复制 fixture 数据
        dest.parent.mkdir(parents=True, exist_ok=True)
        shutil.copytree(fixture, dest)
        print(f"  {config}: copied {fixture_name} -> {dest}")

        # 仅 with_skill 需要 SKILL.md、scripts 和 rules
        if config == "with_skill":
            # 复制 SKILL.md
            skill_src = proj_root / "SKILL.md"
            if skill_src.exists():
                shutil.copy2(skill_src, workdir / "SKILL.md")
                print(f"  {config}: copied SKILL.md")
            # 复制 scripts 和 rules
            for asset in ("scripts", "rules"):
                src = proj_root / asset
                dst = workdir / asset
                if dst.exists():
                    shutil.rmtree(dst)
                shutil.copytree(src, dst)
                print(f"  {config}: copied {asset}/")

    print(f"✓ Setup complete: {iteration_dir / args.eval_name} (run-{run_number})")


if __name__ == "__main__":
    main()