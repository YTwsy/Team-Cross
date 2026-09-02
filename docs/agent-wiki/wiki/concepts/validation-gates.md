# 验证门槛

当任务需要判断“已经证明了什么”时，先读本页和
`../../sources/validation/test-gates.md`。

## 证据层级

| 层级 | 可以证明 | 不能证明 |
| --- | --- | --- |
| Go/TypeScript 单元测试 | 纯逻辑、协议、状态机、组件和 Adapter mapping | 真实设备、账户或网络 |
| race test | 被覆盖本机路径没有 Go data race | 分布式断网、睡眠、换网时序 |
| Mock 本机闭环 | Git、worktree、REST/SSE、多人租约、批注和 patch 流程 | 真实 Provider 行为 |
| Doctor probe | 依赖存在、Codex initialize、Tailcat listener 可初始化 | 真实 Turn 或特定 transport 已承载业务流量 |
| 两台 Mac 验收 | 对应 LAN/Tailnet/Tailcat 路径与断线恢复 | 未覆盖的 ACL、NAT 或 relay 环境 |
| 真实 Provider Turn | 对应 Provider 的 sandbox、event、工具和切换语义 | 另一 Provider 或另一版本 |

## 默认提交门槛

运行 Go test/vet、Web check/test/build 和 Bridge check/test/build。Web source 变化后必须确认
`internal/webassets/dist/` 已刷新；否则源码测试成功但 Go binary 仍会展示旧界面。

涉及 SQLite、event batching、SSE、lease、command fencing 或 Bridge process lifecycle 时，
增加相关 `go test -race`。

## 网络结论

邀请中出现某个 transport 只证明候选被生成。日志显示 Tailcat initialized 只证明
listener 已建立。只有 join 结果和 handshake 明确选中该 transport，才能证明它承载了
连接；当前 UI 只显示 `tailcat`，不能区分 direct 与 DERP。要宣称 DERP relay，必须另取
底层 relay 证据，而不能由“直连失败后仍成功”猜测。

Tailscale LocalAPI warning 是正常 fallback 信号。没有双方 Running 状态和实际 Tailnet
选中结果时，不得宣称 Tailnet 已验证。

## Provider 结论

Codex app-server initialize 不是实际 Agent Turn。Claude credential 存在也不是 SDK Run。
真实验收需要在临时 Thread 中观察 message、tool、file、input/interrupt 和 Round，并再次
确认原 checkout 不变。

## 失败后记录

一次性完整日志放在测试 artifact 或 issue，不进入 Wiki。只有故障产生可复用的判断边界时，
才把经过脱敏和验证的结论写入 `sources/validation/`，并更新本页。

## 相关来源

- `../../sources/validation/test-gates.md`
- `AGENTS.md`
- `Makefile`
- 各 Go `*_test.go`
- `packages/web/src/test/`
- `packages/agent-bridge/test/`
