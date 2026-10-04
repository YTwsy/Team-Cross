# 接收会话恢复死锁与重复启动修复

日期：2026-10-04。基线为安装构建 `4d95dec6b40802f626409526b0dddb18adf05835`，本报告随修复代码提交；工程及原生检查针对同一修复工作树执行。环境：macOS 14.8.5 arm64、Go 1.27.1、Node 24.18.0、pnpm 11.19.0、Codex CLI 0.160.0。没有修改用户原数据、会话、MCP 配置或运行进程。

## 故障与修复

接收会话的空 `commands` 使用 `omitempty` 落盘，重载后成为 nil。首次发送在持锁时写入该 map 触发 panic，延迟执行的 `finishCall()` 再次取同一互斥锁，拖住活动统计和控制状态。隔离复现栈为 `Session.RPC` 的 map 写入 → panic → `Session.finishCall` 的 Mutex.Lock；超时取消不能解锁。补建 map 的对照组正常完成。

接收记录重建时补齐命令表，RPC 写入增加防御，并保证异常先释放所持锁再完成调用清理。控制状态对锁竞争有界等待，返回 `core_busy`；无法核实活动时阻止升级。公共启动器探测失败而 `core.lock` 仍被持有时返回 `core_unresponsive`，保留原始探测原因；不重复启动、不删锁、不强停。CLI JSON 失败回执携带同一错误码，供 App 和 MCP 显示。

## 已执行验证

| 检查 | 结果 |
| --- | --- |
| `go test ./...`、`go vet ./...` | 通过 |
| collab、mcp、mcpevents、sharing、nativecodex、nativeclaude、service、cliinstall、pluginpack 的 race test | 通过 |
| 接收记录重启/旧创建失败重试后的首次发送、同 ID 去重、RPC panic 清理 | 通过；使用模拟 Provider，真实存储与业务代码 |
| 控制状态的 App/Session/Receiver/Joined/分享请求锁竞争；Agent 注册表竞争时保护升级 | 通过 |
| HTTP 超时、连接文件缺失/损坏且 Core 仍持锁 | 通过；没有派生另一个 Core 或打开启动日志 |
| Web check/test/build | 通过；16 个测试文件、163 项测试、1002 条翻译；两套嵌入产物与基线一致 |
| `verify-lifecycle.py` | 通过；真实 CLI/stdio、并发与路径别名复用、鉴权停止、崩溃恢复、数据保留；SIGSTOP 仅作用于测试 Core，CLI/MCP 均报告无响应而不重复启动，SIGCONT 后复用原 PID |
| `verify-core-upgrade.py` | 通过；旧插件先启动、五个并发启动者收敛、存活 MCP 使用磁盘新构建、后续 App 替换无须先同步插件、原地址/启动参数/数据保留、未完成投递延后升级、实例与鉴权保护 |
| 真实三 Core 接收恢复与请求链 | 通过；见下文 |

最后补充 CLI JSON 错误回执后，重新运行 cmd/teamcross 和 service 测试/vet、CLI 构建及完整生命周期检查，均通过。未修改 Web 页面或样式；本轮未新增浏览器布局验收。

升级夹具使用真实旧二进制 `d1b3452f8f98decf69c8cba3d5316a717ad4252a` 与当前修复工作树的两个构建，版本均为 `0.2.6-dev`；新构建标记 `receiver-recovery-check` / `receiver-recovery-next` 仅为测试身份，不是 Git SHA，也不作为 DMG 来源。完整本机结果在 `/var/folders/65/c4hfwfmd50bfvkl6fz7k_h1r0000gn/T/teamcross-receiver-upgrade-27q3l7_d/roundtrip/report.json`，未启动模型，脚本关闭自身 Core 和 MCP。

## 原生接收恢复

命令：`python3 scripts/verify-space-workbench.py --fixture-dir <fresh> --teamcross-bin <current> --codex-bin <Codex 0.160.0> --reload-failed-receivers`。

单 Mac、三个隔离 Core、真实 TLS 成员资格、两个独立原生接收会话，模型仅 `gpt-5.6-luna`。夹具先持久化“Codex 路径不可用”的原生创建前失败记录，省略空命令表，重新启动成员 Core，再经产品 API 明确重试和投递。结构化结果如下，保留关键证据而不依赖临时日志：

```json
{"failedReceiversReloadedBeforeFirstDelivery":true,"bootstrapReadAndAccepted":true,"genericPeerDispatch":true,"threeMembersSeeSameProgress":true,"duplicateDidNotReplay":true,"assistantOptionalAndNoExecution":true,"pauseDisablePreserveHistory":true,"coreStatusAfterRestoredDelivery":true,"scopedToolApprovals":7,"completedRequests":3}
```

三条请求包含启动简报接手、向同伴明确分派和同伴完成；结果标记分别核对 `DISPATCH_WORKBENCH_DONE` 与 `PEER_WORKBENCH_DONE`。完整本机调试材料位于 `/private/tmp/teamcross-receiver-retry-gf226qxr/evidence/`。三个 Core、其原生 app-server/MCP 都由夹具关闭。

## 发现的独立限制

另一次尝试在 `thread/start` 后、尚无首个回合时关闭并重启 Core，Codex 0.160.0 的 `thread/resume` 返回 `no rollout found for thread id`。原生历史尚未落盘，这一路径未通过，也未通过隐式新建会话规避；保留原记录。此限制已同步空间工作台契约，不能用上面的“历史创建失败记录恢复”通过覆盖“零回合空会话恢复”。该次夹具进程已清理，未启动模型回合；本机调试材料位于 `/private/tmp/teamcross-receiver-recovery-z3lll7hj/evidence/`。

本轮不证明任意 Desktop 宿主配对、两台 Mac、Tailcat、公网 Cloud 订阅或 Apple 公证。用户现场原 Core 未被重启，原绑定没有被代为重放。
