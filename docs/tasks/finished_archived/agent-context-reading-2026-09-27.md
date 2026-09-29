# Agent 上下文按需读取验证（2026-09-27）

> 历史验收记录：仅适用于本文注明的版本与环境，不作为当前版本已通过的证明；未覆盖项按原记录保留。当前检查要求见 [验证门槛](../../agent-wiki/sources/validation/test-gates.md)。

范围：`codex/feature-context-reading`，基于 `b9ebd73`。环境为本机 macOS arm64、Go 1.27.1、pnpm 11.19.0；使用独立临时仓库、模拟原生运行时和真实 Core HTTP/MCP 调用，不读取或发布用户真实材料，不启动真实模型。此次未修改 Web 源码或原生客户端接入协议，因此不作新 UI、真实模型、两台 Mac 或 Tailcat 验收声明。

## 固定回归场景

| 场景 | 结果与证据 |
| --- | --- |
| 问题、轮内补充、过程发言、工具输出、最终答复同轮 | `answers` 只返回问题、补充与明确的 `final_answer`；`conversation` 加入过程发言；隐藏推理不导出；phase 进入固定清单 |
| outline / items / 默认无工具输出 | 目录和记录索引不调用正文读取器；默认视图不读取工具正文，索引保留命令标识和原始 item ID |
| 中文、表情、引号、反斜杠、换行及控制字符组成的长工具输出 | 22 页，每页封装预算不超过 8192 字节；UTF-16 坐标连续、未切开代理对，拼接后与原文相同，按条读取不进入下一条 |
| 8 轮目录、没有 final 标记、运行中轮次与空历史 | 最近窗口按时间正序，向更早窗口翻页不丢轮次；不猜最终答复；保留运行状态；空历史也保留轻量视图与预算 |
| 5766 字符引用、另外 8 条无关批注 | 精确读取仅含指定批注；状态查询无 quote；回复只含本次 saved 回执；相同 requestId 得到相同回执；缺失 ID 不退回全部批注 |
| 长讨论的 40 条回复 | 回复分页不丢失、不重复；nextRead 明确省略续页 quote，目录没有引用全文或回复正文 |
| JSON 内容自身未超限，但 MCP 二次转义后超限 | 返回完整、明确的 isError 响应，不让外层截断成功 JSON |

合成引用场景的预算计数：单条读取 18272 字节，状态 1283 字节，回复回执 812 字节。口径为 JSON 内容二次转义后的 UTF-8 字节数，加 512 字节封装余量；这是固定 fixture 的载荷检查，不是 token 或模型质量基准，也不是旧真实会话的前后对照。

对应代码：[读取投影测试](../../../internal/collab/agent_read_test.go)、[批注与回执测试](../../../internal/collab/agent_annotations_test.go)、[MCP 封装检查](../../../internal/mcp/read_test.go)。现有授权、撤回、共享运行时四工具范围、回复幂等和原生历史分页检查同时运行。

## 工程检查

- `go test ./...` 通过。
- `go vet ./...` 通过。
- `go test -race ./internal/collab ./internal/mcp ./internal/sharing ./internal/nativecodex ./internal/nativeclaude ./internal/service ./internal/cliinstall` 通过；空历史收尾修改后额外重跑受影响的协作包。
- `pnpm --filter @teamcross/web check` 通过，750 条界面翻译检查通过。
- `pnpm --filter @teamcross/web test` 通过，10 个文件、126 项测试。
- `pnpm --filter @teamcross/web build` 通过，嵌入式资源无差异；Vite 保留现有大于 500 kB 的 chunk 提示。
- `go build -o bin/teamcross ./cmd/teamcross` 通过。
- 构建后的 CLI 完成真实 STDIO `initialize/tools/list` 冒烟检查，41 个工具正常枚举，材料/草稿视图与精确批注参数已暴露；此检查没有启动 Core。
- 修改文档的相对链接、代码路径、过时描述检查及 `git diff --check` 通过。

构建缓存位于本任务的 `/private/tmp` 目录。模块和工具链使用本机缓存；测试中的服务和模拟进程由 fixture 清理，没有启动用户的原生客户端或更换正在运行的 Core。

## 能力边界

新导出材料保留来源 phase，旧材料缺失的阶段不做反推。活跃历史仍从 Provider 读取完整源页后投影；已发布材料的轻量视图可避免加载工具 blob。此次验证支持减少 Agent 接收的过程内容、无关讨论和重复引用，不支持宣称已经降低 Provider 原始读取成本。

完整字段与调用例外见 [Agent 读取协议](../../agent-wiki/sources/protocol.md#agent-按需读取视图)，后续版本需按 [验证门槛](../../agent-wiki/sources/validation/test-gates.md) 重新检查。
