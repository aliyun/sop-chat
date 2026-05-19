#!/usr/bin/env python3
"""
Validate SLS query syntax by calling aliyun sls GetLogsV2 with line=0.

This script is a deterministic executor — it reads pre-processed query files,
calls GetLogsV2 for each, and writes pass/fail results back into the input
files.  All semantic work (placeholder substitution, cross-logstore detection,
etc.) is handled upstream before invoking this script.

Usage:
    python validate_queries.py \
      --project <project> \
      --logstore <logstore> \
      <input_dir> \
      [--from <unix_sec>] [--to <unix_sec>]

Input (read from <input_dir>/parsed/):
    - query_validation.json      (required)
    - query_validation_LLM.json  (optional, LLM-processed residual queries)

    Each entry has at least:
      {"id": "q0", "title": "...", "executable_query": "..."}

Output:
    The same input files, with `pass` (bool) and `error` (string) appended
    to each entry.  No separate output file is generated.

stderr: summary line like "Validated 9 queries: 8 passed, 1 failed"
stdout: nothing

Note: requires `aliyun` CLI configured and accessible.
      Must run in an environment with network access (disable sandbox if restricted).
"""

import argparse
import json
import os
import subprocess
import sys
import time


def log(msg: str):
    """Print progress/summary info to stderr."""
    print(msg, file=sys.stderr, flush=True)


def load_json(path: str):
    with open(path, "r", encoding="utf-8") as f:
        return json.load(f)


def save_json(data, path: str):
    with open(path, "w", encoding="utf-8") as f:
        json.dump(data, f, ensure_ascii=False, indent=2)


def validate_query(project: str, logstore: str, query: str,
                   from_ts: int, to_ts: int, timeout: int = 30) -> tuple[bool, str]:
    """
    Validate a single query via aliyun sls GetLogsV2.

    Returns (pass, error_message).
    """
    body = json.dumps({
        "query": query,
        "from": from_ts,
        "to": to_ts,
        "line": 0,
    })
    cmd = [
        "aliyun", "sls", "GetLogsV2",
        f"--project={project}",
        f"--logstore={logstore}",
        f"--body={body}",
    ]
    try:
        result = subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)
    except subprocess.TimeoutExpired:
        return False, f"Timeout after {timeout}s"

    if result.returncode == 0:
        return True, ""

    error = (result.stderr or result.stdout or "").strip()
    if len(error) > 300:
        error = error[:300] + "..."
    return False, error


def validate_file(path: str, project: str, logstore: str,
                  from_ts: int, to_ts: int) -> tuple[int, int]:
    """Validate all queries in a single file and write results back.

    Returns (passed_count, failed_count).
    """
    queries = load_json(path)
    if not queries:
        return 0, 0

    passed = 0
    failed = 0

    for entry in queries:
        qid = entry.get("id", "")
        title = entry.get("title", "")
        executable_query = entry.get("executable_query", "")

        if not executable_query:
            entry["pass"] = False
            entry["error"] = "Empty executable_query"
            failed += 1
            log(f"  [{qid}] FAIL (empty query): {title}")
            continue

        ok, error = validate_query(project, logstore, executable_query, from_ts, to_ts)
        entry["pass"] = ok
        entry["error"] = error

        if ok:
            passed += 1
            log(f"  [{qid}] PASS: {title}")
        else:
            failed += 1
            log(f"  [{qid}] FAIL: {title}")
            log(f"         {error[:120]}")

    save_json(queries, path)
    return passed, failed


def main():
    parser = argparse.ArgumentParser(
        description="Validate SLS query syntax via GetLogsV2 (line=0)")
    parser.add_argument("input_dir", help="Logstore input directory")
    parser.add_argument("--project", required=True, help="SLS project name")
    parser.add_argument("--logstore", required=True, help="SLS logstore name")
    parser.add_argument("--from", dest="from_ts", type=int, default=None,
                        help="Query start time (unix seconds)")
    parser.add_argument("--to", dest="to_ts", type=int, default=None,
                        help="Query end time (unix seconds)")
    args = parser.parse_args()

    now = int(time.time())
    from_ts = args.from_ts if args.from_ts is not None else now - 300
    to_ts = args.to_ts if args.to_ts is not None else now

    parsed_dir = os.path.join(args.input_dir, "parsed")
    main_path = os.path.join(parsed_dir, "query_validation.json")
    llm_path = os.path.join(parsed_dir, "query_validation_LLM.json")

    if not os.path.exists(main_path):
        log(f"ERROR: {main_path} not found")
        sys.exit(1)

    total_passed = 0
    total_failed = 0

    log(f"Validating queries against {args.project}/{args.logstore} ...")

    p, f = validate_file(main_path, args.project, args.logstore, from_ts, to_ts)
    total_passed += p
    total_failed += f

    if os.path.exists(llm_path):
        p, f = validate_file(llm_path, args.project, args.logstore, from_ts, to_ts)
        total_passed += p
        total_failed += f

    total = total_passed + total_failed
    log(f"Validated {total} queries: {total_passed} passed, {total_failed} failed")


if __name__ == "__main__":
    main()
