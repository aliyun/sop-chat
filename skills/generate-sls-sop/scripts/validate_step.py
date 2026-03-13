#!/usr/bin/env python3
"""
Validate intermediate outputs of the SOP generation pipeline.

Provides three subcommands — one per checkpoint:

  fields       After Step 5: validate field_annotations.json
  pipeline     After Step 6: validate query_pipeline.json counts
  annotations  After Step 9: validate query_annotations.json

All checks are objective (zero false-positive) and ERROR-level only.

Exit codes:
  0  All checks passed
  1  One or more errors found

When exit code is 1, errors are also written as a JSON array to
  /tmp/validate_errors_<logstore_name>.json
for consumption by update_status.py --mark-failed --errors-file.

Usage:
    validate_step.py <logstore_dir> fields
    validate_step.py <logstore_dir> pipeline
    validate_step.py <logstore_dir> annotations
"""

import argparse
import json
import os
import re
import sys
from collections import Counter


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def log_error(check: str, location: str, detail: str) -> str:
    """Format and print a single error line. Returns the formatted string."""
    msg = f"[ERROR] {check} | {location} | {detail}"
    print(msg, file=sys.stderr, flush=True)
    return msg


def write_errors_json(logstore_dir: str, errors: list[str]):
    """Write error list to /tmp/validate_errors_<logstore>.json."""
    logstore_name = os.path.basename(os.path.normpath(logstore_dir))
    path = f"/tmp/validate_errors_{logstore_name}.json"
    with open(path, "w", encoding="utf-8") as f:
        json.dump(errors, f, ensure_ascii=False, indent=2)
        f.write("\n")
    print(f"Errors written to {path}", file=sys.stderr, flush=True)


def load_json(path: str):
    with open(path, "r", encoding="utf-8") as f:
        return json.load(f)



# ---------------------------------------------------------------------------
# fields subcommand — after Step 5
# ---------------------------------------------------------------------------

def validate_fields(logstore_dir: str) -> list[str]:
    """Validate field_annotations.json against fields.json."""
    errors: list[str] = []
    parsed_dir = os.path.join(logstore_dir, "parsed")

    fields_path = os.path.join(parsed_dir, "fields.json")
    annotations_path = os.path.join(parsed_dir, "field_annotations.json")

    if not os.path.exists(fields_path):
        errors.append(log_error("file_missing", fields_path, "fields.json not found"))
        return errors
    if not os.path.exists(annotations_path):
        errors.append(log_error("file_missing", annotations_path, "field_annotations.json not found"))
        return errors

    fields_json = load_json(fields_path)
    annotations = load_json(annotations_path)

    # Build lookup maps
    field_type_map: dict[str, str] = {f["field"]: f.get("type", "text") for f in fields_json}
    field_names: set[str] = {f["field"] for f in fields_json}
    ann_map: dict[str, str] = {}  # field -> desc
    for entry in annotations:
        fname = entry.get("field", "")
        desc = entry.get("desc", "")
        ann_map[fname] = desc

    # Check 1: every fields.json entry has a corresponding annotation desc
    for f in fields_json:
        fname = f["field"]
        if fname not in ann_map:
            errors.append(log_error(
                "missing_annotation",
                f"field_annotations.json",
                f"field \"{fname}\" has no annotation entry"
            ))
        elif not ann_map[fname].strip():
            errors.append(log_error(
                "empty_desc",
                f"field_annotations.json#{fname}",
                f"desc is empty for field \"{fname}\""
            ))

    # Check 2: desc must not equal the field's type value (dynamic, from fields.json)
    for entry in annotations:
        fname = entry.get("field", "")
        desc = entry.get("desc", "").strip()
        ftype = field_type_map.get(fname, "")
        if desc and ftype and desc == ftype:
            errors.append(log_error(
                "desc_is_type",
                f"field_annotations.json#{fname}",
                f"desc \"{desc}\" is the raw type name — must be a semantic description"
            ))

    # Check 3: desc must not equal the field name (exact match, prevent lazy copy)
    for entry in annotations:
        fname = entry.get("field", "")
        desc = entry.get("desc", "").strip()
        if desc and desc == fname:
            errors.append(log_error(
                "desc_equals_field_name",
                f"field_annotations.json#{fname}",
                f"desc \"{desc}\" is identical to the field name"
            ))

    # Check 4: duplicate description detection (>30% same text AND count > 1 → ERROR)
    descriptions = [entry.get("desc", "").strip() for entry in annotations
                    if entry.get("desc", "").strip()]
    if descriptions:
        desc_counts = Counter(descriptions)
        total = len(descriptions)
        for desc_text, count in desc_counts.most_common():
            if count <= 1:
                break
            ratio = count / total
            if ratio > 0.3:
                errors.append(log_error(
                    "duplicate_description",
                    "field_annotations.json",
                    f"{count}/{total} ({ratio:.0%}) fields share the same description \"{desc_text[:60]}\""
                ))
            else:
                break

    return errors


# ---------------------------------------------------------------------------
# pipeline subcommand — after Step 6
# ---------------------------------------------------------------------------

def validate_pipeline(logstore_dir: str) -> list[str]:
    errors: list[str] = []
    parsed_dir = os.path.join(logstore_dir, "parsed")
    pipeline_path = os.path.join(parsed_dir, "query_pipeline.json")

    if not os.path.exists(pipeline_path):
        errors.append(log_error("file_missing", pipeline_path, "query_pipeline.json not found"))
        return errors

    pipeline = load_json(pipeline_path)

    selected = pipeline.get("selected", [])
    extra = pipeline.get("extra", [])

    if len(selected) > 20:
        errors.append(log_error(
            "selected_limit",
            "query_pipeline.json",
            f"selected count ({len(selected)}) exceeds limit of 20"
        ))

    if len(extra) > 40:
        errors.append(log_error(
            "extra_limit",
            "query_pipeline.json",
            f"extra count ({len(extra)}) exceeds limit of 40"
        ))

    # Check 3: ID deduplication
    sel_ids = [e.get("id") for e in selected if e.get("id")]
    ext_ids = [e.get("id") for e in extra if e.get("id")]
    all_ids = sel_ids + ext_ids
    dup_ids = [eid for eid, cnt in Counter(all_ids).items() if cnt > 1]
    for eid in dup_ids:
        errors.append(log_error(
            "duplicate_id", "query_pipeline.json",
            f"duplicate id '{eid}' in selected+extra"
        ))

    # Check 4: selected not maximized when candidates > 20
    queries_path = os.path.join(parsed_dir, "queries.json")
    ref_queries_path = os.path.join(parsed_dir, "reference_queries.json")

    total_candidates = 0
    if os.path.exists(queries_path):
        queries = load_json(queries_path)
        total_candidates += len(queries)
    if os.path.exists(ref_queries_path):
        ref_queries = load_json(ref_queries_path)
        total_candidates += len(ref_queries)

    if total_candidates > 20 and len(selected) < 20:
        errors.append(log_error(
            "selected_not_maximized",
            "query_pipeline.json",
            f"selected count ({len(selected)}) < 20 when {total_candidates} candidates available"
        ))

    return errors


# ---------------------------------------------------------------------------
# annotations subcommand — after Step 9
# ---------------------------------------------------------------------------

from placeholder_re import RE_SEMICOLON_DEFAULT

AK_LEAKAGE_RE = re.compile(r"LTAI[A-Za-z0-9]{12,}")
REQUIRED_FIELDS = {"id", "title", "category", "cleaned_query"}
SENTINEL_PRE_CLEANED = "PRE_CLEANED"


def _resolve_cleaned_query(annotation: dict, pipeline_map: dict) -> str:
    """Resolve the effective cleaned_query for validation.

    If cleaned_query is the PRE_CLEANED sentinel, look up pre_cleaned_query
    (or normalized_query as fallback) from the pipeline entry.
    """
    cq = annotation.get("cleaned_query", "")
    if cq == SENTINEL_PRE_CLEANED:
        eid = annotation.get("id", "")
        pentry = pipeline_map.get(eid, {})
        return pentry.get("pre_cleaned_query",
                          pentry.get("normalized_query", ""))
    return cq


def validate_annotations(logstore_dir: str) -> list[str]:
    errors: list[str] = []
    parsed_dir = os.path.join(logstore_dir, "parsed")

    annotations_path = os.path.join(parsed_dir, "query_annotations.json")
    pipeline_path = os.path.join(parsed_dir, "query_pipeline.json")

    if not os.path.exists(annotations_path):
        errors.append(log_error("file_missing", annotations_path, "query_annotations.json not found"))
        return errors
    if not os.path.exists(pipeline_path):
        errors.append(log_error("file_missing", pipeline_path, "query_pipeline.json not found"))
        return errors

    annotations = load_json(annotations_path)
    pipeline = load_json(pipeline_path)

    pipeline_selected = pipeline.get("selected", [])
    pipeline_extra = pipeline.get("extra", [])
    all_pipeline_entries = pipeline_selected + pipeline_extra
    pipeline_ids = {e["id"] for e in all_pipeline_entries}
    pipeline_map = {e["id"]: e for e in all_pipeline_entries}
    expected_count = len(all_pipeline_entries)

    # Check 1: JSON schema — each entry must have the 4 required fields
    for idx, entry in enumerate(annotations):
        missing = REQUIRED_FIELDS - set(entry.keys())
        if missing:
            entry_id = entry.get("id", f"index:{idx}")
            errors.append(log_error(
                "schema",
                f"query_annotations.json#{entry_id}",
                f"missing fields: {sorted(missing)}"
            ))

    # Check 2: count consistency
    if len(annotations) != expected_count:
        errors.append(log_error(
            "count_mismatch",
            "query_annotations.json",
            f"annotations count ({len(annotations)}) != pipeline selected+extra ({expected_count})"
        ))

    # Check 3: ID cross-reference
    for entry in annotations:
        eid = entry.get("id")
        if eid and eid not in pipeline_ids:
            errors.append(log_error(
                "id_not_in_pipeline",
                f"query_annotations.json#{eid}",
                f"id '{eid}' not found in query_pipeline.json"
            ))

    # Check 4: unstripped ;default in effective cleaned_query
    for entry in annotations:
        eid = entry.get("id", "?")
        effective_cq = _resolve_cleaned_query(entry, pipeline_map)
        matches = RE_SEMICOLON_DEFAULT.findall(effective_cq)
        for m in matches:
            errors.append(log_error(
                "unstripped_default",
                f"query_annotations.json#{eid}",
                f"unstripped ;default placeholder in cleaned_query: \"{m}\""
            ))

    # Check 5: duplicate titles
    titles_seen: dict[str, str] = {}  # title -> first id
    for entry in annotations:
        title = entry.get("title", "")
        eid = entry.get("id", "?")
        if title in titles_seen:
            errors.append(log_error(
                "duplicate_title",
                f"query_annotations.json#{eid}",
                f"title \"{title}\" duplicates entry {titles_seen[title]}"
            ))
        else:
            titles_seen[title] = eid

    # Check 6: AK leakage in effective cleaned_query
    for entry in annotations:
        eid = entry.get("id", "?")
        effective_cq = _resolve_cleaned_query(entry, pipeline_map)
        ak_matches = AK_LEAKAGE_RE.findall(effective_cq)
        for ak in ak_matches:
            errors.append(log_error(
                "ak_leakage",
                f"query_annotations.json#{eid}",
                f"possible AccessKeyId in cleaned_query: {ak[:8]}..."
            ))

    # Check 7: empty effective cleaned_query
    for entry in annotations:
        eid = entry.get("id", "?")
        effective_cq = _resolve_cleaned_query(entry, pipeline_map).strip()
        if not effective_cq:
            errors.append(log_error(
                "empty_cleaned_query",
                f"query_annotations.json#{eid}",
                "cleaned_query is empty"
            ))

    # Check 8: redundant override — cleaned_query != PRE_CLEANED but identical to pre_cleaned_query
    for entry in annotations:
        cq = entry.get("cleaned_query", "")
        eid = entry.get("id", "?")
        if cq != SENTINEL_PRE_CLEANED and cq:
            pentry = pipeline_map.get(eid, {})
            pre_cq = pentry.get("pre_cleaned_query", "")
            if pre_cq and cq == pre_cq:
                msg = f"[WARNING] redundant_override | query_annotations.json#{eid} | cleaned_query is identical to pre_cleaned_query — consider using PRE_CLEANED"
                print(msg, file=sys.stderr, flush=True)

    return errors


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

SUBCOMMANDS = {
    "fields": validate_fields,
    "pipeline": validate_pipeline,
    "annotations": validate_annotations,
}


def main():
    parser = argparse.ArgumentParser(
        description="Validate SOP generation intermediate outputs"
    )
    parser.add_argument("logstore_dir", help="Path to the logstore directory")
    parser.add_argument("subcommand", choices=SUBCOMMANDS.keys(),
                        help="Validation subcommand")

    args = parser.parse_args()

    if not os.path.isdir(args.logstore_dir):
        print(f"ERROR: directory not found: {args.logstore_dir}", file=sys.stderr)
        sys.exit(1)

    validate_fn = SUBCOMMANDS[args.subcommand]
    errors = validate_fn(args.logstore_dir)

    if errors:
        print(f"\n{len(errors)} error(s) found.", file=sys.stderr, flush=True)
        write_errors_json(args.logstore_dir, errors)
        sys.exit(1)
    else:
        logstore_name = os.path.basename(os.path.normpath(args.logstore_dir))
        print(f"[PASS] {args.subcommand} — {logstore_name}", file=sys.stderr, flush=True)
        sys.exit(0)


if __name__ == "__main__":
    main()
