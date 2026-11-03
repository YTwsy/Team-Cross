# 分身会话与任务面板

## 范围与判断

2026-11-03，按用户对当前空间“记忆与任务”的批注改进实际界面，让 S1/S2 与 W1/W2 的关系、内容、状态及创建时机直接可见。实施分支 `codex/shadow-activity`，基线 `c56946a`；没有修改主工作区。

- 入口改为“会话与任务”，打开独立详情弹窗，保留关闭、Esc 与焦点返回。
- 当前 S、已交接的历史段、独立 Work 分开展示。S 的忙碌只读现有 `busy`（Core 仅按当前主 thread 的活动 Run 计算），不借用 W 的状态。
- S/W 内容用分类条展示；真实上下文用量另外使用原生 `usageValid/inputTokens/contextWindow`。不推算各类别 token 占比。
- Work 按创建时间和 ID 稳定排序，排队任务保留编号位置，但只有已有 `threadId` 才展示 W 编号。旧 coordination 不标 W。展开可看完成条件、原生 ID 与结果；任务版本、错误、记忆及本机操作继续保留。
- 文件保留的原文、任务结果、记忆与交接笔记单独说明。列表尚未加载完整详情时显示加载，离线时注明最后同步记录。
- 使用现有共享投影字段；没有增加后端字段、原生会话、任务状态、模型调用或调度流程。

## 普通关注来源错误仍在

本次只解释该问题，没有改变观察协议。重新读取当前测试空间的 owner `attention.json`，讨论和材料的两个 focus 仍为 `active` 且保存错误：`观察结果无效，未推进处理位置: 证据未在本批或此前有效认识中提供`。

普通关注原文已送入观察；模型返回 evidence 时自行增加 `requestId` 或 `turnId/itemIds`，与 Core 提供的整个准确引用不一致，保存被拒绝。处理水位未因此推进；后来的直接 @ 回复成功和 S 中“都已处理”的自然语言不能证明该错误消失。具体来源与修复方向见 [11 月 2 日实测](2026-11-02-shadow-activity-live-demo.md)。后续宜由 Core 提供短引用 ID、模型选择、Core 还原 SourceRef，保留来源授权与版本校验。

## 验证

macOS arm64、Node 24.18.0、Go 1.27.1。以下运行在基线加本次未提交代码上，构建后的安装来源另记：

- `go test ./cmd/... ./internal/...` 与对应 `go vet` 通过。工作树现有 `bin/go-mod` 缓存影响 `./...` 的递归范围；以上覆盖全部产品 Go 包，没有删除用户缓存。
- `go test -race ./internal/collab ./internal/shadow ./internal/mcp` 全部通过。
- Web check 1403 条中英消息通过；完整 Web 29 文件、260 项通过，最后的离线说明／加载保护后复跑分身 9 项通过。
- 两套嵌入资源构建成功；最终资源额外通过 `internal/mcpassets` 和 `internal/webassets` 检查。
- 合成状态浏览器 fixture：真实 Core、实际 production 资源，S2 空闲、W1 执行、W2 等待、W3 完成、排队但未建会话；历史 S1、任务条件、原生 ID 和结果展开准确。Esc 关闭恢复“会话与任务”焦点，聊天输入可用。
- 实际浏览器默认约 566 × 712 窗口，中／英文与浅／深色检查无横向溢出。没有改变用户正式 Core 的语言或主题。
- 本次未调用真实模型；上述合成状态不证明新的调度能力或两台 Mac 行为。

复现入口见 [验证契约](../sources/validation/test-gates.md#本机数字分身)。截图在 `output/acceptance/shadow-session-panel-20261103/screenshots/`（仅本地保存、Git 忽略），包含浅色总览、任务详情、结果展开和英文深色。测试日志在 `.local-backups/shadow-session-panel-20261103/`（仅本地保存）。测试目录已写入 `finish` 并关闭测试页面。

## 安装

待从干净提交构建 `0.2.6-dev.shadow-session-panel.20261103`，完成 `verify-release` 后替换本机现有 App，保留前版 App 与全部空间数据，再在原测试空间核对 S1 和无 Work 的实际状态。
