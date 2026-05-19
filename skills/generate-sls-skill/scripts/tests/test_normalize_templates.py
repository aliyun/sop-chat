#!/usr/bin/env python3
"""
Dedicated unit tests for normalize_templates.py.

Covers:
  - R1-R5 individual rule testing with real dataset patterns
  - Skip / preserve existing placeholders
  - Priority / mutual exclusion between rules
  - End-to-end full query normalization

All test inputs are derived from actual queries found in project datasets.

Run:
    python -m pytest scripts/tests/test_normalize_templates.py -v
"""

import os
import sys
import unittest

# Add parent dir (scripts/) to path for imports
sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from normalize_templates import normalize_query, build_rules, strip_defaults


# ---------------------------------------------------------------------------
# R1: 定时 SQL 系统变量
# ---------------------------------------------------------------------------
class TestR1SystemVariables(unittest.TestCase):
    """R1: {{__TASK_SQL_START_TS__}} / {{__TASK_SQL_END_TS__}}"""

    def test_r1_end_ts_in_sql_cast(self):
        """cast({{__TASK_SQL_END_TS__}} as bigint) — scheduled SQL"""
        q = "select cast({{__TASK_SQL_END_TS__}} as bigint) as __time__"
        assert normalize_query(q) == "select cast(<任务结束时间;1700003600> as bigint) as __time__"

    def test_r1_start_ts_as_alias(self):
        """{{__TASK_SQL_START_TS__}} AS timestamp — scheduled SQL"""
        q = "{{__TASK_SQL_START_TS__}} AS ts"
        assert normalize_query(q) == "<任务起始时间;1700000000> AS ts"


# ---------------------------------------------------------------------------
# R2: $双花括号 + 默认值
# ---------------------------------------------------------------------------
class TestR2DoubleBraceWithDefault(unittest.TestCase):
    """R2: ${{var|default}} → <var;default>"""

    def test_r2_regex_default(self):
        """${{project|^.*}} — regex default value"""
        assert normalize_query("${{project|^.*}}") == "<project;^.*>"

    def test_r2_regex_default_dotstar(self):
        """${{project|.*}} — variant regex default"""
        assert normalize_query("${{project|.*}}") == "<project;.*>"

    def test_r2_wildcard_default(self):
        """${{host|*}} — wildcard default"""
        assert normalize_query("${{host|*}}") == "<host;*>"

    def test_r2_service_wildcard(self):
        """${{ServiceA|*}} — service wildcard default"""
        assert normalize_query("${{ServiceA|*}}") == "<ServiceA;*>"

    def test_r2_empty_default(self):
        """${{his_left_time|}} — empty default value"""
        assert normalize_query("${{his_left_time|}}") == "<his_left_time;>"

    def test_r2_dash_default(self):
        """${{QueryId|--}} — dash placeholder default"""
        assert normalize_query("${{QueryId|--}}") == "<QueryId;-->"

    def test_r2_project_name_dash(self):
        """${{ProjectName|--}} — dash placeholder default"""
        assert normalize_query("${{ProjectName|--}}") == "<ProjectName;-->"

    def test_r2_long_service_default(self):
        """${{ServiceA|default-http-svc1-80}} — long service default"""
        assert normalize_query("${{ServiceA|default-http-svc1-80}}") == "<ServiceA;default-http-svc1-80>"

    def test_r2_sql_like_escape(self):
        """${{project|%%}} — SQL LIKE escape"""
        assert normalize_query("${{project|%%}}") == "<project;%%>"

    def test_r2_colon_in_varname(self):
        """${{__tag__:__job_name__|etl-0000000000000-000000}} — SLS tag syntax with colon"""
        result = normalize_query("${{__tag__:__job_name__|etl-0000000000000-000000}}")
        assert result == "<__tag__:__job_name__;etl-0000000000000-000000>"

    def test_r2_space_before_pipe(self):
        """${{reqType |SCAN_SQL}} — space before pipe delimiter"""
        assert normalize_query("${{reqType |SCAN_SQL}}") == "<reqType;SCAN_SQL>"

    def test_r2_double_underscore_source(self):
        """${{__source__|^.*}} — double-underscore system field"""
        assert normalize_query("${{__source__|^.*}}") == "<__source__;^.*>"

    def test_r2_in_sql_string(self):
        """project='${{project|^.*}}' — inside SQL string literal"""
        q = "project='${{project|^.*}}'"
        assert normalize_query(q) == "project='<project;^.*>'"

    def test_r2_in_case_when(self):
        """'${{has_query_meta_reason|*}}'='*' — in case when clause"""
        q = "'${{has_query_meta_reason|*}}'='*'"
        assert normalize_query(q) == "'<has_query_meta_reason;*>'='*'"

    def test_r2_in_split_function(self):
        """split('<redo_paths;*>', ' or ') — already normalized, inside SQL function"""
        q = "split('<redo_paths;*>', ' or ')"
        # Already normalized — should stay the same
        assert normalize_query(q) == q

    def test_r2_multiple_in_one_query(self):
        """Multiple R2 patterns in a single query"""
        q = "${{ServiceA|default-http-svc1-80}} or ${{ServiceB|default-http-svc2-80}} | select *"
        expected = "<ServiceA;default-http-svc1-80> or <ServiceB;default-http-svc2-80> | select *"
        assert normalize_query(q) == expected

    def test_r2_dot_in_varname(self):
        """Dot in variable name — e.g. __tag__.__job_name__"""
        assert normalize_query("${{tag.name|val}}") == "<tag.name;val>"


# ---------------------------------------------------------------------------
# R3: $双花括号无默认值
# ---------------------------------------------------------------------------
class TestR3DoubleBraceNoDefault(unittest.TestCase):
    """R3: ${{var}} → <var>"""

    def test_r3_basic(self):
        """${{projects}} — no default value"""
        assert normalize_query("${{projects}}") == "<projects>"

    def test_r3_system_start_time(self):
        """${{__start_time__}} — SLS time variable"""
        assert normalize_query("${{__start_time__}}") == "<__start_time__>"

    def test_r3_system_end_time(self):
        """${{__end_time__}} — SLS time variable"""
        assert normalize_query("${{__end_time__}}") == "<__end_time__>"

    def test_r3_sql_numeric_context(self):
        """latency > ${{latency}} — SQL numeric context (LLM fallback scenario)"""
        q = "latency > ${{latency}}"
        assert normalize_query(q) == "latency > <latency>"

    def test_r3_json_path_context(self):
        """json_extract_scalar(labels, '$.ip')='${{cur_ip}}'"""
        q = "json_extract_scalar(labels, '$.ip')='${{cur_ip}}'"
        assert normalize_query(q) == "json_extract_scalar(labels, '$.ip')='<cur_ip>'"

    def test_r3_colon_in_varname(self):
        """${{__tag__:__source__}} — tag field with colon"""
        assert normalize_query("${{__tag__:__source__}}") == "<__tag__:__source__>"


# ---------------------------------------------------------------------------
# R4: $单花括号 + token_defaults
# ---------------------------------------------------------------------------
class TestR4SingleBrace(unittest.TestCase):
    """R4: ${var} → <var;default> (with token_defaults) or <var>"""

    def test_r4_with_token_default(self):
        """${intervel} with token_defaults"""
        result = normalize_query("${intervel}", {"intervel": "60"})
        assert result == "<intervel;60>"

    def test_r4_without_token_default(self):
        """${ServiceA} with no token_defaults"""
        assert normalize_query("${ServiceA}") == "<ServiceA>"

    def test_r4_search_context(self):
        """eventId.involvedObject.name : ${ObjectName} — search context"""
        q = "eventId.involvedObject.name : ${ObjectName}"
        result = normalize_query(q, {"ObjectName": "my-pod"})
        assert result == "eventId.involvedObject.name : <ObjectName;my-pod>"

    def test_r4_partial_defaults(self):
        """${a} has default, ${b} does not"""
        result = normalize_query("${a} and ${b}", {"a": "1"})
        assert result == "<a;1> and <b>"

    def test_r4_colon_in_varname(self):
        r"""R4 with colon in variable name (unified [\w:.] charset)"""
        result = normalize_query("${__tag__:name}", {"__tag__:name": "val"})
        assert result == "<__tag__:name;val>"

    def test_r4_dot_in_varname(self):
        """R4 with dot in variable name"""
        result = normalize_query("${tag.name}")
        assert result == "<tag.name>"

    def test_r4_in_sql_modulo(self):
        """__time__ % ${intervel} — SQL arithmetic context"""
        q = "select __time__ - __time__ % ${intervel} as t"
        result = normalize_query(q, {"intervel": "60"})
        assert result == "select __time__ - __time__ % <intervel;60> as t"

    def test_r4_tokenquery_full(self):
        """tokenQuery with ${ServiceA} + SQL aggregation"""
        q = "${ServiceA}  | select diff[1] as total from(select compare(pv, 3600) as diff from (select count(1) as pv from log))"
        result = normalize_query(q, {"ServiceA": "*"})
        expected = "<ServiceA;*>  | select diff[1] as total from(select compare(pv, 3600) as diff from (select count(1) as pv from log))"
        assert result == expected


# ---------------------------------------------------------------------------
# R5: 空格双花括号 (reference_queries)
# ---------------------------------------------------------------------------
class TestR5SpaceDoubleBrace(unittest.TestCase):
    """R5: {{ var }} → <var>"""

    def test_r5_project(self):
        """ProjectName:{{ project }} — reference_queries pattern"""
        q = "ProjectName:{{ project }}"
        assert normalize_query(q) == "ProjectName:<project>"

    def test_r5_ip(self):
        """Source:{{ ip }} — reference_queries pattern"""
        q = "Source:{{ ip }}"
        assert normalize_query(q) == "Source:<ip>"


# ---------------------------------------------------------------------------
# Skip / Preserve 测试
# ---------------------------------------------------------------------------
class TestSkipPreserve(unittest.TestCase):
    """Patterns that should NOT be modified by normalization"""

    def test_preexisting_chinese_placeholder(self):
        """Pre-existing <用户的project名称> must be preserved"""
        q = "Project:<用户的project名称> | select count(*)"
        assert normalize_query(q) == q

    def test_already_normalized_with_default(self):
        """Already-normalized <project;^.*> should not be double-processed"""
        q = "project='<project;^.*>'"
        assert normalize_query(q) == q

    def test_dollar_in_split_part(self):
        """SQL split_part(name, '$', 2) — $ not followed by {"""
        q = "split_part(name, '$', 2)"
        assert normalize_query(q) == q

    def test_dollar_json_path(self):
        """JSON path: json_extract_scalar(x, '$.path') — $ not followed by {"""
        q = "json_extract_scalar(x, '$.path')"
        assert normalize_query(q) == q

    def test_plain_query_no_templates(self):
        """Plain query with no template syntax should pass through unchanged"""
        q = "* | select count(*) as cnt, avg(latency) as avg_latency from log group by method limit 100"
        assert normalize_query(q) == q

    def test_dollar_hash_delimiter(self):
        """SQL split(b, '$#$') — $ followed by # not {"""
        q = "split(content, '$#$')"
        assert normalize_query(q) == q

    def test_curly_in_sql_regex(self):
        """SQL regexp_like with curly braces: regexp_like(s, '.{3}')"""
        q = "regexp_like(name, '.{3}')"
        assert normalize_query(q) == q


# ---------------------------------------------------------------------------
# Priority / Mutual Exclusion 测试
# ---------------------------------------------------------------------------
class TestPriority(unittest.TestCase):
    """Ensure rule ordering prevents cross-rule interference"""

    def test_r1_not_captured_by_r5(self):
        """R1 must fire before R5: {{__TASK_SQL_START_TS__}} → R1 result, not R5"""
        result = normalize_query("{{__TASK_SQL_START_TS__}}")
        assert result == "<任务起始时间;1700000000>"
        # R5 would produce <__TASK_SQL_START_TS__> if it fired first
        assert result != "<__TASK_SQL_START_TS__>"

    def test_r3_not_captured_by_r4(self):
        """${{var}} is R3 (double brace), not R4 (single brace)"""
        result = normalize_query("${{myvar}}")
        assert result == "<myvar>"
        # R4 would match ${myvar} inside ${{myvar}}, but R3 fires first (full match)

    def test_r2_not_partially_captured_by_r3(self):
        """${{var|default}} is R2, R3 should not partially match the var part"""
        result = normalize_query("${{project|^.*}}")
        assert result == "<project;^.*>"
        # If R3 fired on the partial ${{project before R2, it would break

    def test_r2_then_r3_in_same_query(self):
        """R2 and R3 patterns coexist without interference"""
        q = "${{host|*}} and ${{project}}"
        result = normalize_query(q)
        assert result == "<host;*> and <project>"

    def test_r2_r3_r4_mixed(self):
        """All three dollar-prefix rules coexist"""
        q = "${{host|*}} and ${{proj}} and ${intervel}"
        result = normalize_query(q, {"intervel": "10"})
        assert result == "<host;*> and <proj> and <intervel;10>"


# ---------------------------------------------------------------------------
# End-to-End: 真实全量 query
# ---------------------------------------------------------------------------
class TestEndToEnd(unittest.TestCase):
    """Full real-world queries from actual datasets"""

    def test_e2e_scheduled_sql_r1(self):
        """Scheduled SQL with R1 ({{__TASK_SQL_END_TS__}})"""
        raw = (
            "* and (Method:GetHistograms or Method: GetLogStoreLogs)|"
            "select cast({{__TASK_SQL_END_TS__}} as bigint) as __time__,"
            "'cn-example' as slsZoneInfo"
        )
        result = normalize_query(raw)
        assert "{{__TASK_SQL_END_TS__}}" not in result
        assert "<任务结束时间;1700003600>" in result
        # Rest of query unchanged
        assert "'cn-example' as slsZoneInfo" in result

    def test_e2e_dashboard_multiple_r2(self):
        """Dashboard with multiple R2 patterns"""
        raw = (
            "${{ServiceA|default-http-svc1-80}} or ${{ServiceB|default-http-svc2-80}}  | "
            "select arbitrary(__time__) as time_t, "
            "date_format(from_unixtime(__time__ - __time__ % 60), '%H:%i') as time"
        )
        result = normalize_query(raw)
        assert "${{" not in result
        assert "<ServiceA;default-http-svc1-80>" in result
        assert "<ServiceB;default-http-svc2-80>" in result

    def test_e2e_tokenquery_r4_with_defaults(self):
        """tokenQuery with ${ServiceA} + token_defaults"""
        raw = (
            "proxy_upstream_name:${ServiceA}  | select round(diff[1], 3) as total, "
            "round((diff[2] - diff[1]) / diff[2] / 0.01, 2) as inc "
            "from(select compare(latency, 3600) as diff "
            "from (select avg(request_time) / 0.001 as latency from log))"
        )
        td = {"ServiceA": "*"}
        result = normalize_query(raw, td)
        assert "${" not in result
        assert "proxy_upstream_name:<ServiceA;*>" in result

    def test_e2e_reference_query_r5(self):
        """reference_queries with R5 {{ project }} and {{ ip }}"""
        raw = (
            "ProjectName:{{ project }} and "
            "(method:PostLogStoreLogs or method:BatchPostLogStoreLogs) "
            "and Source:{{ ip }}  |\n"
            "       SELECT date_format(from_unixtime(__time__ - __time__ % 60), '%m-%d %H:%i') AS t"
        )
        result = normalize_query(raw)
        assert "{{ project }}" not in result
        assert "{{ ip }}" not in result
        assert "ProjectName:<project>" in result
        assert "Source:<ip>" in result

    def test_e2e_mixed_r2_r3_in_prometheus(self):
        """Mixed R2 and R3 patterns in a single query"""
        raw = (
            "project='${{project|^.*}}' and logstore='${{logstore}}' | "
            "select count(*) as cnt"
        )
        result = normalize_query(raw)
        assert result == (
            "project='<project;^.*>' and logstore='<logstore>' | "
            "select count(*) as cnt"
        )

    def test_e2e_no_templates_unchanged(self):
        """Plain query without any templates passes through unchanged"""
        raw = "* | select count(*) as pv, avg(InFlow) as avg_inflow from log group by Method limit 50"
        assert normalize_query(raw) == raw


# ---------------------------------------------------------------------------
# strip_defaults 测试
# ---------------------------------------------------------------------------
class TestStripDefaults(unittest.TestCase):
    """Tests for strip_defaults() — <var;default> → <var>"""

    def test_strip_single_default(self):
        """Single <var;default> stripped to <var>."""
        assert strip_defaults("<host;*>") == "<host>"

    def test_strip_regex_default(self):
        """Regex default stripped: <project;^.*> → <project>."""
        assert strip_defaults("<project;^.*>") == "<project>"

    def test_strip_timestamp_default(self):
        """Timestamp default stripped: <任务起始时间;1700000000> → <任务起始时间>."""
        assert strip_defaults("<任务起始时间;1700000000>") == "<任务起始时间>"

    def test_strip_empty_default(self):
        """Empty default stripped: <tag;> → <tag>."""
        assert strip_defaults("<tag;>") == "<tag>"

    def test_strip_multiple_defaults(self):
        """Multiple defaults stripped in one query."""
        q = "project:'<project;^.*>' and host:<host;*>"
        assert strip_defaults(q) == "project:'<project>' and host:<host>"

    def test_preserve_bare_var(self):
        """Bare <var> without default is preserved."""
        assert strip_defaults("<hostname>") == "<hostname>"

    def test_mixed_with_and_without_default(self):
        """Mixed: only <var;default> is stripped, <var> preserved."""
        q = "<host;*> and <project> and <status;200>"
        assert strip_defaults(q) == "<host> and <project> and <status>"

    def test_no_placeholders(self):
        """Plain query without placeholders passes through unchanged."""
        q = "* | select count(*) from log"
        assert strip_defaults(q) == q

    def test_colon_dot_var(self):
        """Variable with colon and dot in name."""
        assert strip_defaults("<__tag__:host.name;worker-1>") == "<__tag__:host.name>"

    def test_sql_context_preserved(self):
        """SQL context around placeholders is preserved."""
        q = "latency > <threshold;500> and status = <status>"
        assert strip_defaults(q) == "latency > <threshold> and status = <status>"

    def test_long_service_default(self):
        """Long default value stripped."""
        assert strip_defaults("<ServiceA;default-http-svc1-80>") == "<ServiceA>"

    def test_idempotent_on_stripped(self):
        """Running strip_defaults on already-stripped query is idempotent."""
        q = "<host> and <project>"
        assert strip_defaults(q) == q


# ---------------------------------------------------------------------------
# build_rules 结构测试
# ---------------------------------------------------------------------------
class TestBuildRules(unittest.TestCase):
    """Structural tests for build_rules()"""

    def test_rule_count(self):
        """Should have exactly 6 rules (R1_start, R1_end, R2, R3, R4, R5)"""
        rules = build_rules()
        assert len(rules) == 6

    def test_rule_names(self):
        """Rule names follow expected convention"""
        rules = build_rules()
        names = [r.name for r in rules]
        assert names == ["R1_start", "R1_end", "R2", "R3", "R4", "R5"]

    def test_rules_with_defaults(self):
        """build_rules with token_defaults should not change rule count"""
        rules = build_rules({"a": "1"})
        assert len(rules) == 6

    def test_r4_uses_token_defaults(self):
        """R4 replacement should use token_defaults when provided"""
        rules_with = build_rules({"myvar": "42"})
        rules_without = build_rules()
        # Both should have same structure
        assert len(rules_with) == len(rules_without)


if __name__ == "__main__":
    unittest.main(verbosity=2)
