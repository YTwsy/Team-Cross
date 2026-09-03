---
title: Team Cross 验证门槛
kind: validation
status: seed
---

# Team Cross 验证门槛

不同门槛证明的范围不同。结论必须明确说明实际运行了哪一层，不能用 Mock、本机进程或
初始化探活替代两台真实 Mac、真实 Provider Turn 或特定网络路径。

## 默认自动化门槛

Go Core：

```sh
go test ./...
go vet ./...
```

WebGUI：

```sh
pnpm --filter @teamcross/web check
pnpm --filter @teamcross/web test
pnpm --filter @teamcross/web build
```

Agent Bridge：

```sh
pnpm --filter @teamcross/agent-bridge check
pnpm --filter @teamcross/agent-bridge test
pnpm --filter @teamcross/agent-bridge build
```

这组门槛证明纯逻辑、Mock Adapter、事件映射、组件交互和 production build 在测试环境中
通过。它不证明主机凭据可用、两台 Mac 可达或公网 DERP relay 成功。

## 并发与生命周期门槛

修改 SQLite 写事务、SSE、租约、命令 fencing、Share 生命周期或 Bridge 进程管理时，
运行相关 race test，例如：

```sh
go test -race ./cmd/teamcross ./internal/bridgeclient ./internal/server ./internal/storage
```

这证明测试覆盖到的本机并发路径未触发 Go race detector；它不证明分布式时序在真实断网、
睡眠或换网条件下已经验收。

## 本机完整闭环

本机验收至少需要两个独立 data dir/进程，或一个主机与两个独立 join proxy，完成：

- 捕获真实 dirty Git 状态和选定 untracked 文件；
- 创建隔离 worktree，并确认原 checkout 未出现 Agent 文件；
- 运行 Mock Agent Turn、input request、interrupt 和 Round 封存；
- 创建 Share，并通过 join proxy 连接；
- 两位 participant 同时出现，只有一位获得 Controller；
- Controller 能发送 Agent 命令，Observer 仍能批注；
- 相同 `commandId` 重放不产生重复写入；
- SSE 使用 cursor 恢复；
- export patch 可在全新 baseline checkout 上 `git apply --binary`；
- 应用后的文件与 Thread worktree 一致。

这证明单机进程、API、浏览器和真实 Git 闭环。即使 Tailcat listener 预热成功，只要 join
最终选择的是 LAN，也不能宣称 Tailcat 数据路径或 DERP relay 已通过。

## 两台 Mac 网络验收

在宣称自动 fallback 可用于真实协作者前，至少覆盖：

- 同 LAN 且无 Tailscale，选择 LAN；
- mDNS 被阻断但邀请地址可达，仍选择 LAN；
- LAN 隔离且双方 Tailnet 可达，选择 Tailnet；
- Tailnet ACL 阻断或仅一端有 Tailscale，回退 Tailcat；
- Tailcat direct 不可达但 DERP 可用，仍成功，并取得底层明确的 relay 证据；
- 邀请过期或撤销后停止，不继续 fallback；
- 断线换网后重新择路，并从 SSE cursor 恢复。

每一项都要记录两端版本、实际选中 transport 和 handshake 结果。仅看到端口建立或
Tailcat 初始化不算通过。

## 真实 Provider 门槛

真实 Codex/Claude 测试必须 opt-in：

- Codex initialize probe 只证明 app-server 可启动并支持所需方法，不证明真实 Turn。
- Claude credential check 只证明配置存在，不证明 SDK Run 或 sandbox 行为。
- 真实 Turn 应在临时 repository/Thread 中运行，确认文件只能写入 Thread worktree、
  event 映射完整、interrupt/input/switch 符合 Provider 语义，并检查原 checkout 未变化。
- Claude 测试不得把 API key 传入 Bash tool 子进程。

## 当前初始原型的证据边界

截至 2026-09-03，首个提交候选树已经通过本文列出的默认自动化、关键 race test、本机
双 participant 闭环、真实 Codex app-server initialize 和 Tailcat ephemeral listener
初始化。由于验证时 Tailscale LocalAPI 未处于 Running 且未配置 Claude credential，尚
不能声称真实 Tailnet、强制 DERP relay、两台 Mac 或真实 Claude Turn 已验证。这个快照
只对应首个提交候选树；后续结论必须记录新的日期、提交或工作树身份与实际命令范围。

当这些状态变化时，更新本节或拆分新的带日期验证 source；不要把一次性完整日志写入 Wiki。

## 2026-09-03 Session-first 实现工作树

以下结论针对已提交为 `a66ec3e` 的 M1/M2/离线 Fork 实现切片，不代表 M1–M4 全部验收。

已运行并通过：

- Go 全量 `go test ./...`、`go vet ./...`；
- `go test -race` 覆盖 `cmd/teamcross`、`internal/bridgeclient`、
  `internal/gitstate`、`internal/server`、`internal/storage`、
  `internal/share`、`internal/transfer`；
- Web TypeScript check、40 项 Vitest、production build，嵌入资源已刷新；
- Bridge TypeScript check、20 项 Vitest、编译及编译产物的
  `bridge.ping`/`bridge.info` JSONL readiness；
- 包含新 Web 资源的 Go binary build 和 version 启动；
- 文档本地链接与 `git diff --check`。

新增测试覆盖零执行导入、不可变锚点、详情/列表/下载/SSE 的分享范围、只读 Share 禁止
控制、重复请求、历史 Round Fork、worktree 漂移、初始化失败保留旧 Run、迁移旧数据、
未来 schema 拒绝及 SQL 故障注入回滚。独立审查还覆盖 Git filter/textconv 执行、
symlink 链、大小写/Unicode 冲突、补丁中间树、gitlink、对象闭包、copy/binary 膨胀。

本机行为证据：

- 真实 Go Core + 合成历史 Bridge 的浏览器流程完成 Session 选择、预览、只读创建、
  精确批注和反馈；最终只有一个 Thread/Snapshot/Annotation，Agent Run 为零。
  重启后历史与批注恢复，测试来源仓库未改变。
- 实际编译的 Mock Adapter 完成新 Session、隔离目录写入和新 Round 封存。
- 使用真实临时 Git 仓库验证 staged/unstaged/binary/untracked/受限 symlink；
  来源仓库删除后，导入副本仍可还原和导出 patch。

验证使用现有 Go 1.27 工具链、Node 24 与已安装依赖；没有把全新机器的依赖安装记为
通过。浏览器检查使用合成 Session，不读取用户个人对话，不启动真实模型 Turn。

尚未验收：两台真实 Mac 的分享批注/网络路径、真实接收机器上的发送方离线继续、
真实 Codex/Claude Turn，以及 CLI/Desktop 各自的原生 Follow/Open、同 ID Resume、
旧 Writer 失效和交还。高级原生能力因此保持禁用；静态检查与后续 opt-in 要求见
[原生能力门槛](native-capability-gates.md)。

## 2026-09-03 Follow reader 与 managed Writer 身份收口

以下结论针对 `a66ec3e` 之后的增量：Bridge 已独立提交为 `d6335ac`，随后追加 Core/UI
只读 Follow 与 managed Run 身份/关闭栅栏。原生能力仍未启用，不能将本节等同于 M3/M4
产品验收。

已运行并通过：

- Go 全量 test/vet，以及覆盖 `cmd/teamcross`、`bridgeclient`、`domain`、`gitstate`、
  `server`、`storage`、`share`、`transfer` 的 race tests；
- Web 类型检查、53 项测试、production build，`internal/webassets/dist/` 已更新；
- Bridge 类型检查、34 项测试、build，以及编译产物的 `bridge.ping`/`bridge.info`
  readiness，后者只证明方法注册，不调用真实 Provider；
- Go binary build/version 启动、Markdown 本地链接与 `git diff --check`。

新增回归证明：

- Follow 的初次游标省略、增量 upsert、重复输出、历史 reset、断线保留 cursor、重启
  恢复和重连时能力降级；失败读取不消费检查点，第一轮无变化的成功读取可观察；
- cursor、不可变快照、Round 和事件的事务回滚；v1 升级 v2 保留原有审阅历史；
- 停止 epoch 防止迟到读写复活；Follow 不创建 Run、不触碰原生 Writer，也不扩大 Share；
- 长工具输出按条数/字节裁剪并标记缺口；大量 JSON 转义字符不会撑破 Bridge 行限额；
- Claude 真实 Session ID 延迟返回、事件先于 RPC 响应、重复/冲突 ID、来源 binding
  合并、并发更新与重启；临时占位符不被当作原生身份；
- 历史 Run 的迟到完成不能封存当前 Writer 的 worktree；任何旧 Writer 关闭失败都
  阻断新 Run 首条 prompt，未发送的目标被禁用，Owner 可显式重试关闭；未知关闭结果
  不记录成已关闭。测试覆盖普通 idle、初始化中冲突、激活后关闭中冲突三类时序。

本节使用合成 Session/JSONL Provider 和已有 Mock Adapter；没有运行真实模型 Turn，
没有读取个人历史或接管活跃原生会话。本轮未新增浏览器与双机验收，此前浏览器证据仅
覆盖上节审阅切片。该切片之后追加了下节的实时分享和 Open 门控实现；真实原生验收
仍未完成，CLI/Desktop 同 ID 接管与交还仍未交付。

## 2026-09-03 原生实时 Share 与精确 Open 门控

新增独立 `nativeLive` 授权、固定公开窗口、旧批注精确读取、SSE 起点及单批轻量授权。
schema v3 的 Seed/Follow admission 原子提交、失败回滚、并发创建、停止、重启、撤销、
到期与 128 窗口/64 MiB 预算均使用合成 Session 回归。既有 v1/v2 历史保留；旧冻结
Share 不自动加入 Follow。公开投影读取不依赖私有来源 CAS，正文会验证 hash。

Open 的测试仅使用注入的 opener：验证固定 bundle/UUID、明确确认、fresh 来源能力、
历史 managed 同 ID 排除及切换互斥，不执行系统打开命令。HTTP 202 仅代表请求已交给
系统，不代表实际 UI、cwd 或 Writer 行为已验收。

已通过 Go 全量 test/vet、相关 domain/storage/server/bridgeclient/share/transfer/
gitstate/cmd race tests；Web 类型检查、74 项单元测试和 production build；Bridge
类型检查、34 项测试、build 与仅 ping/info 的 readiness。Web 回归覆盖预览过期后
强制重确认、停止立即禁用、跨 Thread 异步结果隔离、旧引用三窗口缓存及失败不替换原文。

这些门槛不包含真实 Codex/Claude Turn、CLI/Desktop Follow/Open、双机 transport
或原生同 ID 接管。生产原生能力仍为 false，不得把合成能力标记推广到真实来源。

本切片的浏览器验收尚未取得证据：验证工具在工作区 writable root 的符号链接检查
阶段失败，未打开页面。临时合成 HTTP 夹具能够启动且确认零 Agent Run，但这不证明
浏览器用户故事通过，也不证明真实 Share transport；应在受支持的非符号链接执行
环境重新验收，不能以单元测试或夹具启动替代。

## 2026-09-03 核心闭环与失败路径补齐

以下证据对应 `d6335ac` 之后的待提交整合工作树，不代表 M1–M4 的真实环境验收完成。

最终已运行并通过：

- Go 全量 `go test ./...`、`go vet ./...`；
- `go test -race -count=1` 覆盖 `cmd/teamcross`、`bridgeclient`、`domain`、
  `gitstate`、`server`、`storage`、`share`、`transfer`；
- Web 类型检查、18 个文件共 99 项测试、production build，嵌入资源已刷新；
- Bridge 类型检查、9 个文件共 54 项测试、production build；编译产物的 JSONL
  `bridge.ping` / `bridge.info` readiness 通过，仅两条探活请求，零 Provider 调用；
- Go binary build/version 启动、Markdown 本地链接与 `git diff --check`。

本轮新增证据：

- Share 的并发创建、预热中撤销、迟到成功拒绝发布、scope 持久化失败清理、过期旧
  listener 清理与并发 App.Close；Store 在清理完成后才关闭。
- 远端 command admission 与 Share/capability/participant/revision/lease 同一原子写入。
  八类真实 handler 的迟到 body 在撤销后零 Bridge 调用、无新 command/annotation/lease
  变化；SQLite 写锁等待后使用实际执行时刻判断到期。已失败命令精确重放合同保持。
- Continue/Switch 的归档或激活事件持久化失败禁用零输入目标，不复活旧 Writer。
  两 Adapter 的关闭失败、超时、并发、迟到终态与显式重试以假 RPC/SDK 验证；Codex
  interrupt ACK 不被当作准确 Turn 的完成证据，Claude 不提前标记 closed。
- 代码批注准确绑定 sealed Round/path/side/line，旧新侧同号不混淆；live diff 不能归锚。
  Share 仅返回获准代码 Round；Evidence/代码反馈保留 hash、baseline 与有界引用。
  静态与实时窗口的已授权并集在 Follow 推进后不回缩。
- 只读审阅选择独立代码基线，保存快照与原锚点反馈进入新 Thread；预览和创建零 Run、
  零原生历史重读。预览变化、源 revision 重放、原子导入失败及原仓库不可用均有回归。
- 已捕获文件后来被 ignore 仍进入 Round/Fork/patch；不扫描其余 ignored，不复活删除，
  tracked 不重复，危险路径拒绝。真实 Git rename、移出 index 后仍存在的文件与原
  index/HEAD/对象不变均有测试。
- 使用独立 sender/receiver 数据目录串通真实临时 Git + 实际编译 Mock 的用户故事：
  发送端代码/Evidence/合成 Session 打包 → 接收端导入 → 发送仓库和数据移到不可访问的
  原路径之外 → 接收端重启 → Mock Continue → 新 Round → HTTP patch 导出 → 接收方独立
  baseline 全新应用后 TreeDigest 一致 → 新 Round 可重新导出离线包。

以上是本机 API、组件、合成运行时与 Mock 证据，不包含真实 Codex/Claude Turn、真实
listener/双 Mac transport 或 CLI/Desktop 同 ID 接管与交还。浏览器工具仍受上述符号
链接执行环境限制，本轮未取得新增浏览器证据。原生高级能力继续关闭；需要明确 opt-in
后再运行专用真实 Provider 会话，不能用本轮 managed close 测试替代原生 Writer fencing。
