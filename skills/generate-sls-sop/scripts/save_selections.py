#!/usr/bin/env python3
"""
Persist Step 3 selection results from LLM decisions.

Reads a lightweight JSON from stdin (logstore_name → output_path mapping)
and writes two kinds of output:
  1. A project-level manifest: <project_dir>/selected_logstores.json
  2. Per-logstore updates: each confirmed logstore's skill_options.json
     gets an "output_path" field appended (or updated if already present).

This script is idempotent — re-running with the same input overwrites
selected_logstores.json and updates (not duplicates) output_path in
each skill_options.json.

Usage:
    python save_selections.py <project_dir> <<'SELECTIONS'
    {
      "output_root": "<output_directory>",
      "project_alias": "<project_identifier>",
      "output_format": "SOP",
      "selections": {
        "<logstore_name_1>": "<output_directory>/<project_identifier>/<logstore_name_1>/overview.md",
        "<logstore_name_2>": "<output_directory>/<project_identifier>/<logstore_name_2>/overview.md"
      }
    }
    SELECTIONS

Input (stdin JSON):
    output_root     (string) — output root directory
    project_alias   (string) — simplified project name for output paths
    output_format   (string, optional) — "SOP" or "SKILL" (default: "SOP")
    selections      (dict)   — {logstore_name: output_path} mapping

Output files:
    <project_dir>/selected_logstores.json
    <project_dir>/<logstore_name>/skill_options.json  (for each selection)

stdout: JSON summary {"count": N, "selected_logstores_path": "...", "logstores": [...]}
stderr: progress messages
"""

import argparse
import json
import os
import sys


def log(msg: str):
    """Print progress/summary info to stderr."""
    print(msg, file=sys.stderr, flush=True)


def load_json(path: str) -> dict | None:
    """Load a JSON file, return None if not found."""
    if not os.path.exists(path):
        return None
    with open(path, "r", encoding="utf-8") as f:
        return json.load(f)


def save_json(path: str, data: dict):
    """Write a dict as formatted JSON."""
    os.makedirs(os.path.dirname(path) or ".", exist_ok=True)
    with open(path, "w", encoding="utf-8") as f:
        json.dump(data, f, ensure_ascii=False, indent=2)
        f.write("\n")


def main():
    parser = argparse.ArgumentParser(
        description="Persist Step 3 selection results from LLM decisions."
    )
    parser.add_argument("project_dir", help="Project input directory")
    args = parser.parse_args()

    project_dir = args.project_dir.rstrip("/")

    # --- Read selections from stdin ---
    try:
        input_data = json.load(sys.stdin)
    except json.JSONDecodeError as e:
        log(f"ERROR: Invalid JSON on stdin: {e}")
        sys.exit(1)

    output_root = input_data.get("output_root")
    project_alias = input_data.get("project_alias")
    output_format = input_data.get("output_format", "SOP")
    selections = input_data.get("selections")

    if not output_root or not project_alias or not isinstance(selections, dict):
        log("ERROR: stdin JSON must contain 'output_root' (str), "
            "'project_alias' (str), and 'selections' (dict)")
        sys.exit(1)

    if not selections:
        log("WARNING: selections is empty, no logstores to persist")

    # --- Build selected_logstores.json ---
    logstores_list = []
    for name, output_path in selections.items():
        logstore_dir = f"{project_dir}/{name}/"
        logstores_list.append({
            "name": name,
            "logstore_dir": logstore_dir,
            "output_path": output_path,
        })

    manifest = {
        "output_root": output_root,
        "project_alias": project_alias,
        "output_format": output_format,
        "logstores": logstores_list,
        "_phase_b_reminder": "单任务内仅处理一个 logstore，禁止交替执行。",
    }

    manifest_path = os.path.join(project_dir, "selected_logstores.json")
    save_json(manifest_path, manifest)
    log(f"Wrote {manifest_path} ({len(logstores_list)} logstores)")

    # --- Update each logstore's skill_options.json ---
    for entry in logstores_list:
        opts_path = os.path.join(entry["logstore_dir"], "skill_options.json")
        existing = load_json(opts_path)
        if existing is None:
            existing = {}
            log(f"  Creating {opts_path}")
        else:
            log(f"  Updating {opts_path}")

        existing["output_path"] = entry["output_path"]
        save_json(opts_path, existing)

    # --- Summary to stdout ---
    summary = {
        "count": len(logstores_list),
        "selected_logstores_path": manifest_path,
        "logstores": [e["name"] for e in logstores_list],
    }
    print(json.dumps(summary, ensure_ascii=False))


if __name__ == "__main__":
    main()
