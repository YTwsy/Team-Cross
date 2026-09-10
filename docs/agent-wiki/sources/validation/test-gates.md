# 验证门槛

本页说明变更后应运行什么，以及每种检查能支持什么结论。指定版本的实际结果见 [2026-09-10 安装验收](onboarding-macos-2026-09-10.md) 与 [2026-09-09 原生协作验收](native-collaboration-2026-09-09.md)，这些记录不是后续提交自动通过的证明。

## 按变更选择检查

| 变更范围 | 应完成的检查 |
| --- | --- |
| 仅 Markdown、导航或文档迁移 | 相对链接、代码路径、旧引用、索引可达性与 `git diff --check`；不需要启动模型或测试客户端 |
| 产品代码 | 下列 Go 与 Web 工程门槛，按修改范围补相关回归 |
| Git 预览、目录创建或恢复 | 原目录分支/HEAD/暂存区/文件不变；worktree 不复制四类未提交内容；子目录映射、新会话 ID 与来源关联 |
| 输入协调、共享或进程生命周期 | 相关 race test，以及直接连接/MCP 共用输入者、重复请求、审批、断线、结束访问和恢复 |
| 原生协议、客户端启动或账户路由 | 除协议测试外，使用专门会话实测对应 TUI/Desktop；分别验证登录、历史、输入、审批和 A 上执行 |
| 模型与推理设置 | 来源继承、客户端选择、失败不更新显示、通知、恢复与写入归属；真实调用仅使用 Luna |
| WebGUI | 类型检查、交互测试、production build，更新嵌入式资源，并进行实际浏览器和截图复核 |

## 工程命令

在仓库根目录运行：

```sh
go test ./...
go vet ./...
go test -race ./internal/collab ./internal/mcp ./internal/sharing ./internal/nativecodex ./internal/service ./internal/cliinstall
pnpm --filter @teamcross/web check
pnpm --filter @teamcross/web test
pnpm --filter @teamcross/web build
go build -o bin/teamcross ./cmd/teamcross
```

修改 `packages/web/src/` 后，提交重新生成的 `internal/webassets/dist/`。纯文档修改无需重建这些产物。若沙箱阻止 Go 缓存写入，可将 `GOCACHE`、`GOMODCACHE` 指向 `/private/tmp` 下的任务专用目录。

旧 Node Agent Bridge 已移除，不再运行或恢复旧 Bridge 的 check/test/build 门槛。

## 现有自动化入口

| 文件 | 重点覆盖 |
| --- | --- |
| [workspace_test.go](../../../../internal/workspace/workspace_test.go) | 两种目录模式、Git 现场保留、文件路径范围 |
| [collab_test.go](../../../../internal/collab/collab_test.go) | 创建恢复不发送 prompt、输入归属、去重、审批、原生与工具并行、访问范围、本机登录路由 |
| [lifecycle_test.go](../../../../internal/collab/lifecycle_test.go) / [membership_test.go](../../../../internal/sharing/membership_test.go) | 首次加入期限、持久成员、丢失响应、主动离开、空闲释放、并发请求和旧连接隔离 |
| [model_test.go](../../../../internal/collab/model_test.go) | 模型继承、设置更新、拒绝请求、恢复与通知 |
| [process_test.go](../../../../internal/nativecodex/process_test.go) | 客户端配置与启动不强制模型 |
| [server_test.go](../../../../internal/mcp/server_test.go) | STDIO 读取不发送输入，保留输入文本与请求 ID |
| [service_test.go](../../../../internal/service/service_test.go) / [onboarding_test.go](../../../../internal/collab/onboarding_test.go) | 实例身份、控制协议、稳定 opt 路径、邀请预览、Core 心跳与输入申请 |
| [cliinstall_test.go](../../../../internal/cliinstall/cliinstall_test.go) | 参数与带引号路径、未知命令保护、并发安装、重复移除和 App 更新后的命令行为 |
| [verify-release.py](../../../../scripts/verify-release.py) / [verify-homebrew.py](../../../../scripts/verify-homebrew.py) | CLI/App/DMG、App 命令安装、独立 Homebrew 前缀安装、双向互斥、升级与卸载 |
| [flows.test.tsx](../../../../packages/web/src/test/flows.test.tsx) | 首页、创建、邀请失败、输入交接状态与模型显示 |

## 真实模型和客户端

[TestLiveCodex](../../../../internal/collab/live_test.go) 默认跳过。显式选择一个新的专用测试目录后运行：

```sh
TEAMCROSS_LIVE_DIR=/private/tmp/teamcross-fresh-fixture \
  go test ./internal/collab -run '^TestLiveCodex$' -v -count=1 -timeout=7m
```

该用例建立测试仓库、来源和两个 fork，执行证明文件操作，并验证结束共享后独立原生进程可取得写入锁、只读不会重新占用、恢复保留同一 ID。全部真实模型调用限定 `gpt-5.6-luna`；不要把测试配置写入产品默认值。模拟模型名称只能证明转发、状态和拒绝语义，不能证明其他模型实际可用。

完成上述 fixture 后，可不发送 prompt 地检查真实 A/B 原生网关交接：

```sh
TEAMCROSS_LIVE_EXISTING_FIXTURE=/private/tmp/teamcross-fresh-fixture \
  go test ./internal/collab -run '^TestLiveNativeHandoff$' -v -count=1 -timeout=1m
```

该检查只接受 `TestLiveCodex` 生成的专用 manifest，不能指向普通用户会话。当前邀请与释放结果见 [2026-09-10 验收](membership-and-release-2026-09-10.md)。

原目录与新 worktree 分别覆盖直接 TUI、直接 Desktop、辅助 TUI、辅助 Desktop，共八种组合。实测核对执行主机、目录、fork、读写结果、审批、输入交接、并行辅助、客户端切换、重连、邀请失效和结束访问。

同机两个 Core、两个浏览器或模拟运行时不能证明两台 Mac 的 LAN。跨设备验收需实际使用 A/B 两台 Mac，记录客户端版本、网络条件与相应操作结果；不使用 `--test-loopback` 替代真实 LAN。

## 界面与测试收尾

页面检查包括首页、创建、详情、加入、设置和关键失败状态；在 1440、1024、768 CSS 像素及深浅主题下检查布局。核对实际视口、长路径、水平溢出、滚动、焦点、键盘和主要动作可达性，并实际查看截图。

完成后关闭本次启动的专用 Desktop/TUI、Core、app-server、浏览器页面及其测试辅助进程。先确认 PID、父子关系或测试数据目录，再退出对应实例；保留用户正在使用的 Codex 和其他浏览器内容，不按程序名批量终止。测试会话、仓库和验收材料按本次约定保留。

新增实际验收记录应注明日期、版本、环境、执行的检查及未覆盖范围；同步 [验收导航](../../wiki/concepts/validation-gates.md)。
