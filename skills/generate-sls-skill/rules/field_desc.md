# 生成字段说明

## 输入

- `<logstore_dir>/parsed/fields.json` — 字段列表（由 prepare_logstore.py 生成），每条含 field、alias、type

## 任务

读取 fields.json，为每个字段生成简短中文描述，写入 `<logstore_dir>/parsed/field_annotations.json`：

```json
[
  { "field": "Errno", "desc": "错误码" },
  { "field": "__FILE__", "desc": "源文件路径" },
  { "field": "cpuNano", "desc": "CPU 耗时（纳秒）" }
]
```

每个 fields.json 条目必须有对应输出，禁止遗漏。

## 规则

- 根据字段名和别名的语义生成描述
- 常见字段直接映射（如 `level` → "日志级别"、`requestId` → "请求 ID"）
- 驼峰/下划线命名拆分后翻译（如 `ErrorCode` → "错误码"、`ProcessRows` → "处理行数"）
- 嵌套字段根据父子关系推断（如 `metric_json.cpu_usage_limit` → "CPU 使用上限"）
- 禁止将字段名原样作为描述（如 `APIVersion` → "APIVersion" 是错误的，应为"API 版本"）
- 禁止纯类型描述：`数值`、`浮点数值`、`字符串`、`布尔值`、`JSON 对象` 等均不可接受
- 若字段数量较多（>50），可分批写入

## 校验

`python3 scripts/validate_step.py <logstore_dir> fields`

> 校验失败时修复所有 ERROR 后重新运行，最多重试 3 次。
> 仍未通过则执行：
>
> **记录步骤失败**：
> ```
> python3 scripts/update_status.py <project_dir> --mark-failed <logstore> --step 5 --errors-file /tmp/validate_errors_<logstore>.json
> ```
> ⚠️ 记录失败后**立即停止**当前 logstore，处理下一个。

## 渲染

校验通过后，运行渲染脚本生成最终表格：

```bash
python3 scripts/render_fields.py <logstore_dir>
```

输出：`<logstore_dir>/fragments/fields_table.md`

**记录步骤成功**：`python3 scripts/update_status.py <project_dir> --mark-step <logstore> --step 5`
