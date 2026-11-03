# 本人 @ 与分身委托

日期：2026-11-03。分支 `codex/shadow-activity`，基线 `9f2b69f`。用户要求开启自己 @ 自己，或提供本人把工作交给分身的方式。

## 本次范围

复用已有成员选择、结构化 mention 和默认分身直接输入：本人在根批注或回复中选择 @ 自己，Core 把准确 human 作者和来源交给 S。短问题直接回复原讨论，必要时由模型创建已有 Work；没有新增派活表单、任务状态或第二套等待关系。

私有收件原因使用 `mention`，不同时当作 `own_source_change`；本人没有自己的未读提醒。默认路线、暂停、离线、direct_only、静音及同 ID 去重沿用已有规则。分身自己的公开回复不会循环触发自己。

同时修复旧普通观察失败挡住新直接输入的问题。新输入可独立读取与处理，已失败批次保留错误；不会自动重放旧观察或将旧认识标成成功。关注引用校验失败本身未修复，[11 月 2 日复核](2026-11-02-shadow-activity-live-demo.md)中的普通讨论／材料限制仍成立。

## 工程验证

- `go test ./cmd/... ./internal/...` 与相同产品包范围的 `go vet` 通过。仓库所有受跟踪 Go 源码均在这两个目录；直接 `./...` 被已有忽略目录 `bin/go-mod` 内的依赖模块干扰，本次未删除或移动这些缓存。
- `go test -race ./internal/collab ./internal/shadow ./internal/mcp` 通过。
- Web check 1366 条翻译、29 个测试文件共 257 项、两套嵌入 production build 通过；生成物无差异。
- 新回归覆盖发起者根批注／回复、加入成员准确身份与本地空间映射、首次消费、同请求去重、当前与保存关注模式、25 条普通积压后的新直接输入、失败期间到达、重启、并发直接输入、错误保留及失败输入不自动循环重试。已有分身自发消息排除回归通过。

## 真实模型验证

`scripts/verify-local-shadow.py --self-mentions` 使用专用 Git 仓库、独立配置、新原生会话、同一 Mac 三个 Core 和真实 TLS。仅使用 `gpt-5.6-luna` / openai / low。

临时 CLI 已两次验证加入成员在根批注和具体回复中 @ 自己，暂停期间持久接收、恢复后准确原处回复、无本人未读、无重复委托及无额外 Work；离线手动重试、原 thread 和记忆恢复也通过。最终 stdio 读取返回 `installation_unavailable`：临时文件名为 `teamcross-self-mention`，不满足 InstalledBuild 对启动器 basename 必须为 `teamcross` 的要求。这两次未算作整条脚本通过。随后使用成品 App 内 CLI，在全新 fixture 完整通过同一脚本，包含最后的 stdio MCP 读取。

原始日志及证据只在本地 `/private/tmp/teamcross-self-mention-native-20261103{,-b}/` 和 `/private/tmp/teamcross-self-mention-*.log`；脚本保存 `self-mentions.json`、`report.json` 与 stdio 的准确结果。第二次 self-mentions.json 的 SHA-256 为 `e6122f60443d0b112b107613cd7c4eb48f548e71cc40c04e8fd7a6cea18bb516`。两次脚本均通过 finally 关闭自身三个 Core。未做双 Mac 或 TUI／Desktop 交互验收。

## 成品安装与当前测试空间

- 版本 `0.2.6-dev.shadow-self-mention.20261103` / build 213；源码 `2e68a946a6d80c3657bc705e65ab224f36f8ac2b`，release.json 的 `dirty:false`。
- arm64 / macOS 14.0；ad-hoc 签名，未进行 Developer ID 签名或 Apple 公证。
- `make release` 与 `make verify-release` 通过：DMG 校验与安装、图标、签名、CLI/App 版本及安装、Core 与 App 生命周期。
- 成品 CLI 的 `--self-mentions` 完整真实模型验收通过。报告 `/private/tmp/teamcross-self-mention-packaged-20261103/evidence/report.json`，原生 thread `01a1a0d1-d514-71e1-86a3-c3af30c8b0f6`；三个测试 Core 均正常退出。
- 已安装到 `/Applications/Team Cross.app`，`/opt/homebrew/bin/teamcross` 继续指向 App helper；二者与运行 Core 版本及提交一致。原 App 保存在本工作树 `.local-backups/install/2026-11-03-shadow-self-mention/original-installed.app`。升级前两个分身均空闲，无审批；通过既有有界升级协议替换 Core，没有删除协作数据或重放输入。
- DMG SHA-256：`07bddd85855ef93bfac9d20b501ed8ee7806c0190989aa06dd1bab3862c79199`。
- CLI tar.gz SHA-256：`7a4cbb03410c99697693bf2634f595a0b7d2089a924666667d93c5c21aaab1ec`。

当前测试空间 `9a87d731-c0a2-443b-a102-6e6ab90b95e2` 的既有 Luna 分身完成 F 委托：在讨论 `8d91e49a-cd45-40ae-8dac-eec6ed1f3bf0` 的本人回复 `4b8ce2ca-fd73-4836-81fe-338543fb21c2` 下写入回复 `4508a196-4996-4fd4-997c-af492092c026`：“本人委托已收到：当前状态为评审通过，演示名额为 35。”旧普通观察错误和旧 queued 事件仍保留，未创建 Work。两条本人写入后未读数保持 2。

同轮先到的 E 算术问题 `deb1ddbe-d28c-445d-9f11-125f8ac92bac` 虽已通过 turn/start 接收，后来 F 经 turn/steer 进入后，模型仅回答 F。此时 E 的 processed 只表示原生轮次结束，不能当作已作答。未隐藏这次漏答，也没有自动重放旧业务消息。

为该实际问题补强 S 的输入提示：新批次追加而不替换前面的未答要求；结束前逐一回复或明确派发已有 Work；普通关注事实无须公开回复，相同准确来源只处理一次。没有新增状态机或回复完成账本。验收脚本新增两个独立讨论、执行中追加第二条、同一原生轮的真实 steer 与两处准确答复检查；在 `/private/tmp/teamcross-self-mention-concurrent-20261103/` 已通过。问题为 6+7、14+5，分身分别回复 13、19，准确回复对应原讨论。该提示变更后全部产品 Go test/vet 和 shadow race 再次通过。最终安装包与当前空间后续复核另补。
