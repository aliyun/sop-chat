#!/usr/bin/env python3
"""
Prepare queries for validation by deriving executable_query from normalized_query.

Step 8a of the SOP generation pipeline — pure script.

Derives executable_query by:
  - <var;default> → default  (use the preserved default value)
  - <var>         → xxx      (safe fallback; avoids quote-doubling when placeholder is inside quotes)

After derivation, detects residual angle-bracket placeholders that the strict
regex could not handle.  Entries with residuals are routed to a separate file
for LLM processing.

Usage:
    python prepare_validation.py <input_dir>

Input:  <input_dir>/parsed/query_pipeline.json (must have 'normalized_query' from Step 7)
Output:
    <input_dir>/parsed/query_validation.json          (always)
    <input_dir>/parsed/query_validation_LLM.json      (only when residual placeholders exist)
"""

import json
import os
import re
import sys


def log(msg: str):
    print(msg, file=sys.stderr, flush=True)


def load_json(path: str):
    with open(path, "r", encoding="utf-8") as f:
        return json.load(f)


def save_json(data, path: str):
    with open(path, "w", encoding="utf-8") as f:
        json.dump(data, f, ensure_ascii=False, indent=2)


from placeholder_re import RE_PLACEHOLDER

RE_RESIDUAL = re.compile(r'<[^<>]+>')


def derive_executable(normalized_query: str) -> str:
    """Replace normalized placeholders with executable values.

    <var;default> → default (the preserved default value)
    <var>         → xxx     (safe fallback; avoids quote-doubling when placeholder is inside quotes)
    """
    def replacer(m):
        default_val = m.group(2)
        if default_val is not None:
            return default_val
        return "xxx"
    return RE_PLACEHOLDER.sub(replacer, normalized_query)


def main():
    if len(sys.argv) < 2:
        print(f"Usage: {sys.argv[0]} <input_dir>", file=sys.stderr)
        sys.exit(1)

    input_dir = sys.argv[1]
    parsed_dir = os.path.join(input_dir, "parsed")
    pipeline_path = os.path.join(parsed_dir, "query_pipeline.json")

    if not os.path.exists(pipeline_path):
        log(f"ERROR: {pipeline_path} not found")
        sys.exit(1)

    pipeline = load_json(pipeline_path)

    script_entries = []
    llm_entries = []

    for list_name in ("selected", "extra"):
        for entry in pipeline.get(list_name, []):
            normalized = entry.get("normalized_query", "")
            if not normalized:
                normalized = entry.get("query", "")

            executable = derive_executable(normalized)

            record = {
                "id": entry.get("id", ""),
                "title": entry.get("display_name", ""),
                "source_type": entry.get("source_type", ""),
                "dashboard_name": entry.get("dashboard_name", ""),
                "source": entry.get("source", ""),
                "executable_query": executable,
            }

            if RE_RESIDUAL.search(executable):
                llm_entries.append(record)
            else:
                script_entries.append(record)

    validate_path = os.path.join(parsed_dir, "query_validation.json")
    save_json(script_entries, validate_path)

    if llm_entries:
        llm_path = os.path.join(parsed_dir, "query_validation_LLM.json")
        save_json(llm_entries, llm_path)
        llm_ids = [e["id"] for e in llm_entries]
        log(f"WARNING: {len(llm_entries)} queries have residual placeholders: {', '.join(llm_ids)}")

    log(f"Prepared {len(script_entries)} queries for validation"
        + (f", {len(llm_entries)} routed to LLM" if llm_entries else ""))


if __name__ == "__main__":
    main()
