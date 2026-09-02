# Session 审阅与接力

涉及 Session 导入、精准批注、分享内容范围、Round 继续、本机 Fork 或离线包时，先读
[稳定契约](../../sources/decisions/session-review-and-continuation.md)。

## 最容易混淆的边界

- Import 是不可变历史捕获，零 Run、零 prompt。Session-first Thread 默认只读，
  不自动把来源 cwd 变成 Git baseline。
- Share 是冻结的内容投影，默认只有 view/annotate。live managed 输出需单独授权，
  不表示原生 Follow；详情、列表、下载和 SSE 必须同样受限。
- Continue 创建新 Session，必须由执行主机 Owner 确认；只读取所选 Round。
- 最新 Round 的 worktree 已变化时拒绝顺序继续；历史 Round 或显式 Fork 创建新 Thread。
- 离线包携带 baseline 可达 Git 历史，不只 diff。导入自己持有的副本不执行 Agent；
  接收者需要自己的凭据，双方不自动同步。
- M3 原生 Follow/Open、M4 同会话接管仍不可用。API 存在不等于能力已验收。

## 修改入口

- 数据模型：`internal/domain/review.go`、`internal/storage/migrations.go`
- M1：`internal/server/review.go`、`share_projection.go`
- M2/M4 Fork：`internal/server/continuation.go`、`bundles.go`、`internal/transfer/`
- UI：`SessionStart`、`SessionReview`、`SharePanel`、`ContinuationPanel`

## 验收必须覆盖

只读导入不调用执行 RPC；隐藏内容不能绕过路由；旧 scope 不扩大；来源快照引用不丢失；
历史 Round 不回退当前 worktree；失败不破坏旧 Run；交接包路径/哈希/对象闭包安全；
源仓库消失后副本仍能导出 patch。真实两机与原生能力另见
[验证门槛](validation-gates.md)及
[原生能力门槛](../../sources/validation/native-capability-gates.md)。
