---
title: Session 审阅、继续与离线 Fork
kind: decision
status: accepted
---

# Session 审阅、继续与离线 Fork

## 实现边界

本契约定义 M1 审阅闭环、M2 新 Session 继续和 M4 离线 Fork 的本机实现。真实两台 Mac、
真实 Provider Turn、M3 原生 Follow/Open 与 M4 同会话接管分别验收，不能由下面的代码
或 Mock 测试推定完成。原生控制遵循
[能力门槛](../validation/native-capability-gates.md)，当前保持关闭。

## M1：只读导入与审阅

- `sessions.snapshot` 只读取历史；预览不写数据库。导入保存不可变 SessionSnapshot，
  追加不可变 Round 和 `session.imported`，不调用任何 `runs.*`，不影响既有 Run。
- Session-first 创建只读 Thread，不自动访问来源 cwd、不创建 worktree，也不要求
  原目录仍可访问。要执行必须先有用户明确捕获的 Git baseline；现有 Git Thread
  可以追加导入 Session。导入时继承的代码 checkpoint 不是原生 Session 当时的代码。
- SessionRef 保留 Provider 身份、来源界面和版本；未知来源不猜成 Desktop。消息、
  工具、未知类型与截断都可读呈现；快照 ID 与 entry ID 构成稳定锚点。
- 批注目标为一个 SessionSnapshot/entry、Evidence 或 Round；代码路径/行号与 Round
  绑定。后续导入不会重写旧目标。Markdown 反馈包含来源引用与摘录，仅由 Owner 复制
  回原生 UI，不自动发送 Agent。
- SessionSnapshot 数据存入 CAS，数据库记录不可更新/删除；追加快照、Round、revision
  与事件在同一 SQLite 事务提交。旧库通过 `PRAGMA user_version` 迁移而不重写 Round。
  Session-first 的 Thread 与初始快照也原子创建；失败可以留下未引用 CAS，不留下部分
  Thread。未来 schema 版本在运行旧建表语句前被拒绝。

## 分享是独立授权

`ShareScope` 是 allowlist：一个 snapshot、其 entry IDs、Evidence IDs、封存代码开关、
managed 实时事件开关。空列表表示不分享，不表示全部。

创建 Share 时冻结内容投影。服务端列表、详情、patch、Evidence 下载、批注和 SSE 都受
同一投影约束；没有授权的原始 transcript 不能从 Evidence 绕过。分享中的代码来自
最新不可变 Round，不来自后来变化的 worktree；不包含未捕获文件的路径元数据。

默认 capability 只有 `view`、`annotate`。控制需 Owner 明确启用，并同时确认封存代码
与实时 managed Agent 输出；后者可能包含工具结果和文件路径，不能当成只分享旧 snapshot。
不启用实时输出时，事件仅以脱敏 `thread.updated` 推动 revision/cursor 更新。
后续导入不扩大已分享快照。变更范围/权限必须撤销后重新分享，活动 Share 的重复创建
返回冲突。撤销/到期也终止 SSE 的后续交付。

旧无锚点评论不隐式公开；分享后该 Share 参与者的新评论可见。整个 snapshot 的批注仅
在整个 snapshot 已获分享时可见，防止部分消息选择被宽泛批注绕过。

## M2：从 Round 创建新 Session

执行主机 Owner 显式选择 Round、Provider、工具网络权限和首条指令；UI 显示执行主机、
隔离目录以及“新 Session”，不使用 Resume。此操作不是远端 Controller 权限的一部分。

最新 Round 只有在整个 worktree 与封存状态一致时才能在原 Thread 顺序继续。比较包含
ignored/untracked 文件；不一致拒绝执行并提示显式 Fork。历史 Round 自动 Fork 新
Thread，不回退原 Thread 的唯一 worktree。

上下文只取所选 Round 的 CAS 引用；每个后续 Round 继承已有 SessionSnapshot 引用。
新 Run 初始化失败不破坏旧 Run/Round。执行前消费 expectedRevision，并在 Provider
准备后再次核对 worktree；不确定结果不能用旧 revision 自动重发创建/输入。
新 Run 激活后失败不自动复活旧 Run，需 Owner 查看状态再决定下一步。

## M4：可离线的 Fork，不是原生 Session 搬家

`.tcx.json` v1 保存来源 Thread/Round、baseline Git 可达对象、sealed patch、已捕获
untracked、显式选中的 Evidence/SessionSnapshot。baseline 包含可达历史，不只是当前
diff，Owner 必须确认历史和上下文可导出。材料默认不可信，完整性哈希不是作者认证。

接收者验证后建立自己的 bare Git 对象库与 detached worktree，以一个事务保存新 Thread、
Round 0、快照、Evidence 与来源事件。snapshot ID 在本机重新分配并保存原始映射；
SQLite event seq 只作本机 cursor，不充当跨机器身份。保留来源 Git baseline，
最终 patch 仍相对该 baseline。

导入和本机 Fork 不执行 Agent。发送方离线或来源仓库删除后，副本仍可独立工作；
接收者使用自己的 Provider 凭据，明确创建新 Session。凭据、Share secret、旧租约、
进程和本机配置不进入包。原始 checkout 不被修改，双方不自动合并历史。

v1 采用 fail-closed 限制：

- JSON 最大 100 MiB，解码 payload/物化树预算 64 MiB，单对象 16 MiB；
- Git/CAS 对象数与落盘文件数各最多 10,000；untracked 仍为单文件 5 MiB、总量 20 MiB；
- 校验 Git 对象哈希与 baseline 可达闭包、CAS 哈希、patch 路径及二进制膨胀预算；
- 拒绝危险路径、大小写/Unicode 规范化冲突、submodule/gitlink、
  含父目录跳转的 symlink、穿越 symlink 父级写入与 copy patch；
- 物化前不加载接收主机 Git 全局/系统配置、hooks、fsmonitor、textconv 或内容 filters。
  Git capture/export 同样不能执行仓库选择的 filter 程序。

这些限制适用于本机 Fork 和离线包，不是完整环境复刻。已交付副本不会随着 Share 撤销
消失。M4 同会话接管仍要求真实原生对话 ID、旧 Writer 失效、持久 fencing、隔离目录及
交还原生 UI 的独立证据；Fork 新 Session 永远不能冒充这个结果。

## 事实来源

- `internal/domain/review.go`
- `internal/storage/migrations.go`、`session_capture.go`、`forks.go`
- `internal/server/review.go`、`share_projection.go`、`continuation.go`、`bundles.go`
- `internal/transfer/`、`internal/gitstate/`
- `packages/web/src/components/SessionReview.tsx`、`ContinuationPanel.tsx`
- [验证门槛](../validation/test-gates.md)
