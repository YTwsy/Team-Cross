# 接收会话释放与接回验证

日期：2026-10-05（Asia/Shanghai）。基线：Meta `910e07b395c1a00a7623a3701fd19c04a9661913`；被测代码为 `codex/receiver-handoff` 的本次实现工作树。环境：单台 macOS、Codex CLI `0.160.0`、真实模型 `gpt-5.6-luna`、Node `24.18.0`、pnpm `11.19.0`。用户原来的会话、Core 与工作目录未参与测试。

## 改动与来源

沿用 `codex/collaboration-flow` 调查工作树已经验证的审批解决同步、跨 thread 隔离和准确轮次停止，在最新 Meta 的恢复死锁/客户端发现逻辑之上集成。新增持久化暂停、等待当前轮次/审批/已接受 RPC、确认专属进程退出后释放、同原生 ID/目录/配对接回，以及个人客户端占用时保留暂停的失败处理。

只有释放完毕才显示并接受 Desktop 继续入口。空原生会话尚无可读取历史时拒绝释放。普通刷新、Core 重启和恢复操作均不发业务输入；停止当前轮次不等于释放会话。空间结束后仍可进行本机暂停/停止，恢复必须具备有效空间访问。

## 工程与原生检查

- `go test ./...`、`go vet ./...`：通过。
- `go test -race ./internal/collab ./internal/mcp ./internal/nativecodex`：通过；末尾本机清理权限及进程退出检查追加接收生命周期 race 回归。
- Web check/test/build：通过，164 项测试，两套嵌入资源更新。既有 Vite 大 chunk 提示保留。
- `scripts/verify-receiver-lifecycle.py`：隔离真实原生往返通过。首次 native 审批由第二 WebSocket 客户端回应，不经 Core respond 替身；中断取得真实 `interrupted`。释放后第二个独立 app-server 恢复同一 thread 并续写；Team Cross 在它持锁时恢复失败。第二进程退出后，接回相同身份和历史，模型报告仅出现在续写轮次中的随机标记，证明并非只检查列表元数据。

原生结构化结果：

```json
{
  "scope": "single Mac, isolated Core/home/workspace, real native WebSocket client",
  "model": "gpt-5.6-luna",
  "nativeVersion": "codex-cli 0.160.0",
  "emptyHistoryOpenRejected": true,
  "emptyHistoryReleaseRejected": true,
  "nativeApprovalsResolved": 2,
  "availableAfterNativeCompletion": true,
  "exactTurnInterrupted": true,
  "duplicateStopNotReplayed": true,
  "desktopOpenArgumentsCaptured": true,
  "occupiedResumeRejected": true,
  "sameThreadAndPairingRestored": true,
  "personalHistoryRetained": true,
  "resumeDidNotReplayInput": true,
  "pauseWaitedForNativeApproval": true,
  "modelUsedPersonalContinuation": true,
  "releasedAfterApprovedTurn": true,
  "desktopSidebarRefresh": "not tested; Desktop UI was not automated",
  "passed": true
}
```

## 界面与边界

生产构建在真实 Chrome 中完成“已暂停接收 → 恢复接收 → 再次释放”；核对原有目标恢复可用、同一接收卡片保留、释放前不出现 Desktop 继续入口。复核中文浅色 1440 与深色 768 宽度，截图位于本次工作树 `output/playwright/receiver-handoff/`；未另做英文浏览器截图，新增英文文案由翻译扫描和交互测试覆盖。

Desktop 系统打开命令由测试替身捕获，核对精确 thread 深链接；没有自动控制个人 Desktop 主窗口。第二 app-server 的真实写入锁/续写/接回证明原生交接，不能代替个人 Desktop 界面完整往返、侧边栏自动刷新或 `agents` 的 `x` 快捷键验收。未新增个人 Desktop 主动推送能力，也不是两台 Mac 验证。

复现入口：[原生脚本](../../../../scripts/verify-receiver-lifecycle.py)、[审批/停止测试](../../../../internal/collab/receiver_lifecycle_test.go)、[交接测试](../../../../internal/collab/receiver_handoff_test.go)。完整本机临时证据位于 `/private/tmp/teamcross-receiver-handoff-20261005-a/evidence/`；上述关键结果在本记录中长期保留。本次不发布安装包或替换用户已安装 App。
