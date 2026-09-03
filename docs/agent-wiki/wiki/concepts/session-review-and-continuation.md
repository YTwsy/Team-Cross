# Session 审阅与接力

涉及 Session 导入、精准批注、分享内容范围、Round 继续、本机 Fork 或离线包时，先读
[稳定契约](../../sources/decisions/session-review-and-continuation.md)。

## 最容易混淆的边界

- Import 是不可变历史捕获，零 Run、零 prompt。Session-first Thread 默认只读，
  不自动把来源 cwd 变成 Git baseline。
- Share 是冻结的内容投影，默认只有 view/annotate。live managed 输出需单独授权，
  不表示原生 Follow；详情、列表、下载和 SSE 必须同样受限。
- Continue 创建新 Session，必须由执行主机 Owner 确认；只读取所选 Round。
- 无代码基线的审阅可显式选择当前 Git 基线，预览后创建独立后继 Thread，携带已保存
  快照和带原锚点的反馈 Evidence；创建本身零执行，不能改写旧 Round 或声称历史代码一致。
- 新代码批注必须有 sealed Round/path/old-or-new side/line；live diff 不可归锚。
  Continue 选择固定 Round，执行依据变化后须重新确认，不能后台切换成最新 Round。
- 最新 Round 的 worktree 已变化时拒绝顺序继续；历史 Round 或显式 Fork 创建新 Thread。
- 离线包携带 baseline 可达 Git 历史，不只 diff。导入自己持有的副本不执行 Agent；
  接收者需要自己的凭据，双方不自动同步。
- Codex Follow 仅对通过当前 reader 有界历史合同探测的精确 Session 开放；它读取
  已保存输出，不是事件订阅。断线重验、停止 fence 和不可变检查点不能授权原生输入，
  也不能把新内容送进已冻结 Share。Open、Resume 和同会话接管仍不可用。
- 原生实时分享须单独确认 `nativeLive` 的当前预览和未来类别，服务端将 Follow
  ID/epoch/source 与不可变公开窗口绑定。旧批注通过精确 snapshot 接口读取原文，
  不按当前窗口或相同 entry ID 猜授权。128 窗口/64 MiB 后停止追加公开窗口。
- `sessions/open` 仅 Owner 显式确认，fresh Open 能力检查和 managed Writer 排除后请求
  固定 Codex Desktop 深链；202 不等于原生验收成功，也不能用它交还 managed Session。

## 修改入口

- 数据模型：`internal/domain/review.go`、`internal/storage/migrations.go`
- M1：`internal/server/review.go`、`share_projection.go`
- 精确代码审阅/后继工作：`sealed_code.go`、`review_successor.go`
- M2/M4 Fork：`internal/server/continuation.go`、`bundles.go`、`internal/transfer/`
- M3 reader：`internal/server/follow.go`、`internal/storage/follows.go`、Bridge `sessions.poll`
- UI：`SessionStart`、`SessionReview`、`SharePanel`、`ContinuationPanel`

## 验收必须覆盖

只读导入不调用执行 RPC；隐藏内容不能绕过路由；旧 scope 不扩大；来源快照引用不丢失；
历史 Round 不回退当前 worktree；失败不破坏旧 Run；交接包路径/哈希/对象闭包安全；
源仓库消失后副本仍能导出 patch。真实两机与原生能力另见
[验证门槛](validation-gates.md)及
[原生能力门槛](../../sources/validation/native-capability-gates.md)。

Follow 还须覆盖：断线不吃掉游标、stable-ID upsert 不改旧快照、长工具输出裁剪可前进、
cursor/snapshot/Round 原子提交、重启与更换读取器后重验能力、停止后迟到数据不能写入。
Share 还须覆盖并发创建、预热中撤销、迟到发布拒绝和进程关闭清理；已授权静态/live
窗口并集不能随后回缩。只读后继工作须覆盖原生历史不可用、预览后代码变更和原子导入失败。
