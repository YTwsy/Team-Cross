---
title: Session 审阅、继续与离线 Fork
kind: decision
status: accepted
---

# Session 审阅、继续与离线 Fork

## 实现边界

本契约定义 M1 审阅闭环、M2 新 Session 继续、能力门控的只读 Follow 和 M4 离线 Fork
的本机实现。真实两台 Mac、
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
- 批注目标为一个 SessionSnapshot/entry、Evidence 或 Round；新的代码批注必须同时绑定
  Round、path、old/new side 与正行号，并以同一封存 diff 的结构行验证。UI 的 live diff
  不能用于历史归锚；binary 或显示预算外的 patch 只提供下载，不伪造可批注行。
  旧无锚点记录保留并标记，不自动改绑。后续导入不会重写旧目标。Markdown 反馈包含
  Session 摘录、Evidence 名称/类型/hash 与限长文本、代码 baseline/侧/位置，仅由 Owner 复制
  回原生 UI，不自动发送 Agent。
- SessionSnapshot 数据存入 CAS，数据库记录不可更新/删除；追加快照、Round、revision
  与事件在同一 SQLite 事务提交。旧库通过 `PRAGMA user_version` 迁移而不重写 Round。
  Session-first 的 Thread 与初始快照也原子创建；失败可以留下未引用 CAS，不留下部分
  Thread。未来 schema 版本在运行旧建表语句前被拒绝。

## 分享是独立授权

`ShareScope` 是 allowlist：一个 snapshot、其 entry IDs、Evidence IDs、封存代码开关、
managed 实时事件开关，以及单独确认的 `nativeLive` 原生窗口规则。空列表表示不分享，
不表示全部；不设置 `nativeLive` 的 Share，其原生 Session 范围始终保持冻结。

创建 Share 时冻结内容投影。服务端列表、详情、patch、Evidence 下载、批注和 SSE 都受
同一投影约束；没有授权的原始 transcript 不能从 Evidence 绕过。分享中的代码来自
最新不可变 Round，不来自后来变化的 worktree；不包含未捕获文件的路径元数据。

默认 capability 只有 `view`、`annotate`。控制需 Owner 明确启用，并同时确认封存代码
与实时 managed Agent 输出；后者可能包含工具结果和文件路径，不能当成只分享旧 snapshot。
不启用实时输出时，事件仅以脱敏 `thread.updated` 推动 revision/cursor 更新。
后续导入不扩大已分享快照。变更范围/权限必须撤销后重新分享，活动 Share 的重复创建
返回冲突。撤销/到期也终止 SSE 的后续交付。

Share 在预热前即占用该 Thread 的创建槽。创建中再请求返回 `share_starting`；撤销可取消
尚未发布的创建，槽保留到失败清理结束。迟到运行时不能发布邀请；范围持久化失败会关闭
listener 并撤销记录。远端请求必须同时匹配当前发布的 Share 和持久权限，旧记录不能借用
新 Share 的 scope。应用关闭会取消并等待创建、撤销及 listener 清理，最后关闭数据库；
并发 Close 共用同一完成结果。

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

UI 固定所选 Round；起点、最新 Round、执行目录、Run、Provider、首条指令或权限变化会
使执行确认失效，不能切回旧选项恢复旧授权。仅 revision 更新不改变具体执行方案，但
提交仍携带最新 expectedRevision。

### 只读审阅到可执行后继 Thread

没有 Git baseline 的审阅 Thread 不能直接 Continue。Owner 可选择其不可变 Round，再明确
选择本机 Git 仓库与 untracked 文件，预览后创建独立后继 Thread。预览摘要覆盖准确代码
内容、来源 Round、已保存 Session、相关批注、工作目标、仓库与 revision；创建时重新
计算，不匹配返回冲突。预览不创建 Thread/Run；创建本身也不执行 Agent。

来源 Round 中的 SessionSnapshot 被复制并保留原 ID 映射；精确指向这些材料的批注封存
为只读反馈 Evidence，保留原锚点，不冒充新 Thread 的原生历史。无锚点批注不自动携带。
代码来自本次单独确认的当前 Git 状态，不声称是来源 Session 当时的状态，也不改写原
只读 Thread/Round。使用独立 Git 对象库和 worktree，来源仓库后来不可用也能继续。
失败不发布部分 Thread；创建前消费来源 revision，响应不确定时不得重复创建。
Owner 随后必须在新 Thread 另行确认新 Session 的 Provider、权限和首条指令。

## M3：只读 Follow 的本机实现与未通过的原生门槛

- Follow 是独立 reader，不是 Run。Owner 明确确认后，Core 重读指定 Session 的当前
  能力并核对 provider、原生 ID、identityKind、来源界面与版本；不信任旧快照自带的授权。
  当前原生能力仍禁用；合成 Provider 测试只证明下面的本机状态机。
- Codex `sessions.poll` 从最新尾部建立检查点，以不透明分页游标和边界指纹补读、去重。
  读取器只访问指定 Session，不通过 initialize 探针枚举其他 Session，不调用 Resume、
  原生订阅或任何执行 RPC。暂时断线不重置游标，明确历史缺口才重新定位。
- Core 持久保存 reader、cursor、epoch、当前快照与缺口。变化时原子追加不可变快照、
  Round 和事件；无变化不增生 Round。新 Round 只保留当前 Follow 窗口和原始导入引用，
  旧 Round 保留先前检查点。Git checkpoint 仍独立于原生代码，不能以 Follow 偷偷捕获 cwd。
- 当前视图按条数与内容字节预算裁剪，旧材料仍在旧快照；缺失、裁剪、reset 都必须标记。
  停止先持久化 fence，后取消 I/O；迟到成功/失败不能恢复已停止的 reader。
- Core 重启可恢复 Owner 已授权的只读 reader，但必须重验能力。读取失败后保留 cursor，
  重试前再次核对来源；降级或未验证的读取器不能凭旧记录继续。
- WebGUI 通过现有 SSE 刷新状态，展示 active/retrying/stopped、当前快照和缺口；新内容
  不自动跳走当前审阅。Start/Stop 都是显式 Owner 操作，不增加浏览器驱动的同步循环。
- Share 不继承 Follow。`includeEvents` 只适用于 managed 输出；原生实时分享必须
  另行选择一个已成功读取的活动 Follow、当前预览窗口及 `message/tool/notice` 类别，
  确认当前窗口和后续窗口。消息包含人的输入及 Agent 回复，工具记录可能含参数、
  结果、路径和代码；类别选择不是自动脱敏，`includeCode:false` 也不清洗文本里的代码。
- `nativeLive` 固定绑定 Follow ID、epoch、准确来源与类别。分享发布事务验证预览 ID
  仍为当前窗口，并登记初始投影；窗口变化要求刷新重确认，不偷偷更换预览。
  Follow 的 Snapshot/Round/cursor 与公开窗口 membership 在同一事务提交。
- 每个 `(shareId,snapshotId)` 保存不可变的已过滤投影。只读详情仅返回静态选择及最新
  公开窗口；旧批注使用受同一 membership 限制的单快照接口取回原文，不以 stable entry ID
  或相同 Provider Session 身份扩大授权。停止后新 Follow 不继承授权，旧窗口仍可审阅。
  同一静态窗口若也被 nativeLive 授权，后续窗口推进不会缩回其已有授权并集；详情和
  精确单快照接口始终返回一致的不可变公开内容。
- 每个 Share 最多公开 128 个原生窗口或 64 MiB 投影；达到预算停止追加公开窗口，保留
  已公开材料和批注。Owner 可撤销并重新预览分享，不能删除旧 membership 让锚点失效。
  分享发布时同时记录本 Thread 的 event 起点，起点以前的事件不升级为实时通知。
  SSE 每批轻量检查窗口 membership、逐条检查 Share 有效性，只通知获准快照 ID、状态与 revision，不发送原始输出、cursor、
  Provider 错误或私有 Round ID；持续连接和重连使用同一边界。
- 精确 Open 入口仅接受 Owner 的快照 ID 和明确确认，再重读准确来源的当前 Read/Open
  能力。仅向固定 Codex Desktop bundle 发送最小 `codex://threads/<UUID>`；不接受 cwd、
  prompt 或任意 URL。任何曾关联 managed Run 的同一 ID（包括已关闭或其他 Thread）
  均拒绝打开；未确认身份/关闭及切换状态也阻断。此入口不是原生 Writer 交还。
  HTTP 202 只表示系统接收了打开请求，不证明原生界面显示正确。生产 Open 仍禁用。

## M4：可离线的 Fork，不是原生 Session 搬家

`.tcx.json` v1 保存来源 Thread/Round、baseline Git 可达对象、sealed patch、已捕获
untracked、显式选中的 Evidence/SessionSnapshot。baseline 包含可达历史，不只是当前
diff，Owner 必须确认历史和上下文可导出。材料默认不可信，完整性哈希不是作者认证。

接收者验证后建立自己的 bare Git 对象库与 detached worktree，以一个事务保存新 Thread、
Round 0、快照、Evidence 与来源事件。snapshot ID 在本机重新分配并保存原始映射；
SQLite event seq 只作本机 cursor，不充当跨机器身份。保留来源 Git baseline，
最终 patch 仍相对该 baseline。

已捕获文件后来被 ignore 规则隐藏时仍属于该 Thread 的导出范围；范围来自不可变 capture
或累计 capturedPaths，不扫描所有 ignored 内容。删除的文件不复活，已 tracked 的文件
不重复生成 patch。后续 Round 固定的是 baseline 下代码树，不承诺复刻历史 Git refs/index。

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

- `internal/domain/review.go`、`follow.go`、`native_live.go`
- `internal/storage/migrations.go`、`session_capture.go`、`follows.go`、`native_live.go`、`forks.go`
- `internal/server/review.go`、`review_successor.go`、`sealed_code.go`、`follow.go`、`native_live.go`、`native_open.go`、`share_projection.go`、`share_lifecycle.go`、`continuation.go`、`bundles.go`
- `internal/transfer/`、`internal/gitstate/`
- `packages/web/src/components/SessionReview.tsx`、`ContinuationPanel.tsx`
- [验证门槛](../validation/test-gates.md)
