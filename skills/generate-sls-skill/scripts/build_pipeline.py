#!/usr/bin/env python3
"""
Build query_pipeline.json from query_selection.json + source query files.

This script resolves ID references in query_selection.json to full query
objects from queries.json and reference_queries.json, producing the
self-contained query_pipeline.json used by subsequent pipeline steps.

Usage:
    python build_pipeline.py <input_dir>

Input files (read from <input_dir>/parsed/):
    - query_selection.json (required) — ID references
    - queries.json (required) — source queries with id q0, q1, ...
    - reference_queries.json (optional) — reference queries with id r0, r1, ...

Output files (written to <input_dir>/parsed/):
    - query_pipeline.json — full query objects in selected/extra arrays

Exit codes:
    0 — success
    1 — missing required files or invalid data
"""

import json
import os
import sys


def log(msg: str):
    print(msg, file=sys.stderr, flush=True)


def load_json(path: str):
    with open(path, "r", encoding="utf-8") as f:
        return json.load(f)


def build_id_index(queries: list, label: str) -> dict:
    """Build {id: query_object} lookup from a query list."""
    index = {}
    for q in queries:
        qid = q.get("id")
        if qid is None:
            log(f"ERROR: entry in {label} missing 'id' field: {q.get('source', '?')}")
            sys.exit(1)
        if qid in index:
            log(f"WARNING: duplicate id '{qid}' in {label}, keeping first")
            continue
        index[qid] = q
    return index


def resolve_ids(id_list: list, id_index: dict, list_name: str) -> list:
    """Resolve a list of IDs to full query objects."""
    resolved = []
    for qid in id_list:
        if qid not in id_index:
            log(f"ERROR: id '{qid}' in {list_name} not found in queries.json or reference_queries.json")
            sys.exit(1)
        resolved.append(id_index[qid])
    return resolved


def main():
    if len(sys.argv) != 2:
        print(f"Usage: {sys.argv[0]} <input_dir>", file=sys.stderr)
        sys.exit(1)

    input_dir = sys.argv[1]
    parsed_dir = os.path.join(input_dir, "parsed")

    # --- Load required files ---
    selection_path = os.path.join(parsed_dir, "query_selection.json")
    queries_path = os.path.join(parsed_dir, "queries.json")

    if not os.path.exists(selection_path):
        log(f"ERROR: {selection_path} not found")
        sys.exit(1)
    if not os.path.exists(queries_path):
        log(f"ERROR: {queries_path} not found")
        sys.exit(1)

    selection = load_json(selection_path)
    queries = load_json(queries_path)

    # --- Load optional reference queries ---
    ref_path = os.path.join(parsed_dir, "reference_queries.json")
    refs = load_json(ref_path) if os.path.exists(ref_path) else []

    # --- Build unified ID index ---
    id_index = build_id_index(queries, "queries.json")
    ref_index = build_id_index(refs, "reference_queries.json")

    # Check for ID collisions between sources
    collisions = set(id_index.keys()) & set(ref_index.keys())
    if collisions:
        log(f"ERROR: ID collision between queries.json and reference_queries.json: {collisions}")
        sys.exit(1)

    id_index.update(ref_index)

    # --- Resolve selections ---
    selected_ids = selection.get("selected", [])
    extra_ids = selection.get("extra", [])

    selected = resolve_ids(selected_ids, id_index, "selected")
    extra = resolve_ids(extra_ids, id_index, "extra")

    # --- Build pipeline ---
    pipeline = {
        "stats": {
            "input": len(queries) + len(refs),
            "selected": len(selected),
            "extra": len(extra),
        },
        "selected": selected,
        "extra": extra,
    }

    # --- Write output ---
    pipeline_path = os.path.join(parsed_dir, "query_pipeline.json")
    with open(pipeline_path, "w", encoding="utf-8") as f:
        json.dump(pipeline, f, ensure_ascii=False, indent=2)

    log(f"Built query_pipeline.json: selected={len(selected)}, extra={len(extra)}")


if __name__ == "__main__":
    main()
