#!/usr/bin/env python3
"""
Persist Step 1 user options into skill_options.json files.

Scans <project_dir> for valid logstore directories and writes
skill_options.json for each, containing:
  - validate_queries (bool): global flag from --validate-queries
  - reference_source (str, optional): path to reference document

Also writes project-level skill_options.json with:
  - output_format (str): "SOP" or "SKILL" (default: "SOP")

Reference matching:
  --reference-dir scans a directory for files whose stem (filename without
  extension) matches a logstore name. Only regular files are considered;
  directories and hidden files are skipped.
  --reference provides explicit logstore=file bindings (higher priority).

This script is idempotent — re-running merges new fields into existing
skill_options.json without removing other fields (e.g. output_path).

Usage:
    python save_options.py <project_dir> [--output-format SOP|SKILL] \
        [--validate-queries] [--reference-dir <path>] \
        [--reference <logstore>=<file> ...]

Output: JSON summary to stdout
Progress info is printed to stderr.
"""

import argparse
import json
import os
import sys
from pathlib import Path


def log(msg: str):
    """Print progress info to stderr."""
    print(msg, file=sys.stderr, flush=True)


def is_valid_logstore(ls_dir: str) -> bool:
    """A logstore directory must contain index.json (created by fetch_sls_data.py)."""
    return os.path.isfile(os.path.join(ls_dir, "index.json"))


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


def build_reference_map(
    logstore_names: set[str],
    reference_dir: str | None,
    explicit_refs: list[str] | None,
) -> dict[str, str]:
    """Build a mapping of logstore_name -> reference file path.

    Priority: explicit --reference bindings > --reference-dir auto-match.
    """
    ref_map: dict[str, str] = {}

    # Auto-match from --reference-dir
    if reference_dir and os.path.isdir(reference_dir):
        for entry in sorted(os.listdir(reference_dir)):
            full_path = os.path.join(reference_dir, entry)

            # Skip directories and hidden files
            if not os.path.isfile(full_path):
                continue
            if entry.startswith("."):
                continue

            stem = Path(entry).stem
            if stem in logstore_names:
                if stem in ref_map:
                    log(f"  WARNING: multiple files match logstore '{stem}', "
                        f"keeping '{ref_map[stem]}', skipping '{full_path}'")
                else:
                    ref_map[stem] = full_path

    # Explicit --reference bindings (override auto-match)
    if explicit_refs:
        for binding in explicit_refs:
            if "=" not in binding:
                log(f"  WARNING: invalid --reference format '{binding}', "
                    f"expected '<logstore>=<file>', skipping")
                continue
            name, filepath = binding.split("=", 1)
            name = name.strip()
            filepath = filepath.strip()
            if name not in logstore_names:
                log(f"  WARNING: --reference logstore '{name}' not found "
                    f"in project, skipping")
                continue
            ref_map[name] = filepath

    return ref_map


def main():
    parser = argparse.ArgumentParser(
        description="Persist Step 1 user options into skill_options.json files."
    )
    parser.add_argument("project_dir", help="Project input directory")
    parser.add_argument(
        "--output-format",
        choices=["SOP", "SKILL"],
        default="SOP",
        help="Output format: SOP (default) or SKILL",
    )
    parser.add_argument(
        "--validate-queries",
        action="store_true",
        help="Set validate_queries=true for all logstores",
    )
    parser.add_argument(
        "--reference-dir",
        help="Directory to auto-match reference files by filename stem",
    )
    parser.add_argument(
        "--reference",
        action="append",
        help="Explicit binding: <logstore>=<file> (can specify multiple times)",
    )
    args = parser.parse_args()

    project_dir = args.project_dir.rstrip("/")

    # [0/3] Write project-level skill_options.json with output_format
    project_opts_path = os.path.join(project_dir, "skill_options.json")
    project_opts = load_json(project_opts_path) or {}
    project_opts["output_format"] = args.output_format
    save_json(project_opts_path, project_opts)
    log(f"[0/3] Wrote output_format='{args.output_format}' to {project_opts_path}")

    # [1/3] Scan for valid logstores
    log(f"[1/4] Scanning logstores in {project_dir}")

    all_subdirs = sorted(
        d
        for d in os.listdir(project_dir)
        if os.path.isdir(os.path.join(project_dir, d)) and not d.startswith(".")
    )

    logstore_names: list[str] = []
    for d in all_subdirs:
        ls_path = os.path.join(project_dir, d)
        if is_valid_logstore(ls_path):
            logstore_names.append(d)

    log(f"  Found {len(logstore_names)} valid logstores")

    if not logstore_names:
        log("WARNING: no valid logstores found, nothing to write")
        print(json.dumps({"count": 0, "logstores": []}))
        return

    # [2/4] Build reference mapping
    log("[2/4] Building reference mapping")
    ref_map = build_reference_map(
        set(logstore_names), args.reference_dir, args.reference
    )
    if ref_map:
        log(f"  Matched {len(ref_map)} reference(s): "
            + ", ".join(f"{k}={v}" for k, v in sorted(ref_map.items())))
    else:
        log("  No reference documents matched")

    # [3/4] Write skill_options.json for each logstore
    log(f"[3/4] Writing skill_options.json for {len(logstore_names)} logstores")

    results = []
    for name in logstore_names:
        opts_path = os.path.join(project_dir, name, "skill_options.json")
        existing = load_json(opts_path) or {}

        # Set validate_queries
        existing["validate_queries"] = args.validate_queries

        # Set or remove reference_source
        if name in ref_map:
            existing["reference_source"] = ref_map[name]
        else:
            existing.pop("reference_source", None)

        save_json(opts_path, existing)

        entry = {"name": name, "validate_queries": args.validate_queries}
        if name in ref_map:
            entry["reference_source"] = ref_map[name]
        results.append(entry)

        action = "updated" if os.path.exists(opts_path) else "created"
        log(f"  {action}: {opts_path}")

    # Summary to stdout
    summary = {
        "output_format": args.output_format,
        "count": len(results),
        "logstores": results,
    }
    print(json.dumps(summary, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
