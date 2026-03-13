"""Shared placeholder regex patterns for the SOP generation pipeline.

All scripts that match <var> or <var;default> placeholders import from here.
Variable-name character class: [\\w:.]+  (word chars, colon, dot)

Used by:
  - normalize_templates.py  (Step 7: strip_defaults via RE_WITH_DEFAULT)
  - prepare_validation.py   (Step 8: derive_executable via RE_PLACEHOLDER)
  - validate_step.py        (Step 9 validation: detect residual ;default via RE_SEMICOLON_DEFAULT)
"""
import re

VAR_NAME = r'[\w:.]+'

# <var> or <var;default> — captures (var_name, default_or_None)
RE_PLACEHOLDER = re.compile(rf'<({VAR_NAME})(?:;([^<>]{{0,80}}))?>') 

# Only <var;default> — captures (var_name) — used by strip_defaults()
RE_WITH_DEFAULT = re.compile(rf'<({VAR_NAME});[^<>]{{0,80}}>')

# Only detects <var;default> presence (no capture groups) — used by validation
RE_SEMICOLON_DEFAULT = re.compile(rf'<{VAR_NAME};[^<>]{{1,80}}>')
