# 故障排查

执行出错或需排查时使用。主流程与步骤以 SKILL.md 为准。

---

| 现象 | 可能原因 | 建议动作 |
|------|----------|----------|
| 执行 `aliyun` 报错或找不到命令 | CLI 未安装或未加入 PATH | 执行 `aliyun version` 验证；安装参考：https://github.com/aliyun/aliyun-cli |
| `aliyun` 命令报 TLS 证书校验失败（`x509: OSStatus -26276` 等） | 沙箱限制了系统证书链访问 | 须在非沙箱环境中执行 fetch / validate_queries |
| 使用 project 名拉取时超时或报错 | 网络、权限或 project 不存在 | 查看 `/tmp/sls_fetch_err_<project>.log` 中的错误信息；可延长等待时间或改为按 logstore 分批执行 |
| `fetch_sls_data.py` 运行后 summary 为空或报错 | 无权限、logstore 名不匹配、dashboard 无有效数据 | 确认 project/logstore 存在且当前账号有读权限；检查 summary JSON 中的 `warnings` 字段 |
| `prepare_logstore.py` 报错或输出 JSON 异常 | 输入目录缺少 `index.json` 或资源子目录，或文件格式异常 | 确认目录下存在 `index.json`（由 `fetch_sls_data.py` 生成）；检查 `/tmp/sls_sop_err_<dirname>.log` 中的堆栈与路径 |
| 生成的 overview 中查询报错（如列不存在） | 嵌套字段在 search 中用了别名 | 在「字段参考」中强调：search 部分禁止使用别名，必须用 `nested.field.name : value` 形式 |
| `queries_extra.md` 未生成 | `query_pipeline.json` 中 `stats.extra > 0` 但 Step 9 渲染脚本未生成 | 检查 `query_pipeline.json` 的 stats 和 extra 数组；重新执行 Step 9 |
| `query_report.md` 数字与实际不符 | 报告数字应从 JSON 文件取值而非 context 记忆 | 重新执行 Step 9 渲染脚本 |
| 验证中断后 overview.md 缺失 | Step 7 验证失败不影响后续步骤 | 跳过验证，直接用已有 query_pipeline.json 继续 Step 8 → Step 9 → Step 10 组装 |
| Phase B 并行执行时出现 400/429 或 "rate limit" 错误 | 同时启动过多 task agents，超出模型 RPM/TPM 限制 | 1) 限制**并发度**：同时运行的 agent 不超过 8 个；2) 若仍限流，逐步缩小（如 8→4→2），最后才考虑串行 |

详细错误以各步骤中指定的临时日志文件为准（`/tmp/sls_fetch_err_*.log`、`/tmp/sls_sop_err_*.log`）。
