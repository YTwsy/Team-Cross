# 协作空间邀请与关闭体验验证（2026-09-19）

本记录覆盖可复用空间链接、在原空间启用共同执行、逐成员开放执行访问，以及无成员只读空间的关闭与重新开放。产品规则见 [协作空间](../decisions/collaboration-spaces.md)，接口字段见 [协议](../protocol.md)。

## 环境与范围

- 基于 `7a7f7b3` 的本地工作树；macOS 14.8.5（23J423）、Darwin arm64、Go 1.27.1、Node 24.18.0。
- Web：React 19.1.1、Vite 7.1.7、Vitest 3.2.4；真实 Chrome 153.0.8010.52，通过 Playwright 操作嵌入式 production build。
- 浏览器连接同机三个独立测试 Core，成员请求经 TLS；来源历史与原生运行时为测试替身，专用临时 Git 目录，不读取个人会话、不调用真实模型。
- Tailcat 使用现有 `v0.6.0` 联网测试，在宿主机执行；仍是同机多节点，不证明两台 Mac、不同网络或持续强制 DERP。

## 工程检查

以下全部通过：

```sh
go test ./...
go vet ./...
go test -race ./internal/collab ./internal/mcp ./internal/sharing ./internal/nativecodex ./internal/nativeclaude ./internal/service ./internal/cliinstall
pnpm --dir packages/web check
pnpm --dir packages/web test
pnpm --dir packages/web build
go build -o bin/teamcross ./cmd/teamcross
git diff --check
```

Web 为 5 个文件、66 项测试。构建产物同步到 `internal/webassets/dist`。本机沙箱默认 Go 缓存不可写，检查使用任务专用临时缓存与已安装的 Go 工具链；没有修改模块依赖或个人缓存。

## 自动化覆盖

- [共享成员测试](../../../../internal/sharing/membership_test.go)：同一链接并发接纳独立凭据，相同凭据重试复用成员；普通取回保留链接，重置请求幂等；重置或关闭后旧链接拒绝新加入，现有成员保留。
- [空间测试](../../../../internal/collab/materials_test.go)：启用执行保留空间、链接、成员 ID 与材料作者；旧审阅成员和后来通过同一链接加入的人保持材料访问；未授权时拒绝原生历史、文件、事件、RPC、连接及原生批注，不能自行授予访问。授权与收回后访问范围相应改变，收回后材料仍可读取。
- 执行授权有独立可取消的上下文。收回取消在途原生请求；再次授权不恢复旧请求，成员资格仍有效。
- 已被移除的本机成员记录不复用于后续明确加入；仍持有效链接的人重新加入会获得新身份，原凭据继续失效。
- 未生成邀请和已生成但无人加入的只读空间都可关闭；关闭状态持久化，重启后仍保持，随后可以重新开放。
- [成员集成测试](../../../../internal/collab/members_test.go)：三成员使用同一链接、独立输入归属、移除指定成员；[Web 生命周期测试](../../../../packages/web/src/test/space-lifecycle.test.tsx)覆盖关闭、重新开放、链接管理及原空间启用执行。
- `TestLiveTailcatMembership`、`TestLiveTailcatCollaboration` 通过，包含同一链接加入第三位参与者以及定向输入交接与 WebSocket 路径。首次沙箱尝试受 `netmon.New: operation not permitted` 阻止，宿主机运行通过。

## 浏览器复核

1. 未生成邀请的空空间可直接点击“关闭空间”，确认后刷新仍显示关闭；从“重新开放空间”生成链接后恢复共享。已有未使用链接的空空间同样可关闭。
2. 两名成员通过同一链接加入，页面显示三人。在原空间启用共同执行，无第二次邀请弹窗；API 对比确认空间 ID、链接摘要和两个成员 ID 均未变化，已有材料保留。
3. 给 Bob 开放执行访问后，Bob 可见协作上下文和申请输入入口；Carol 仍处于阅读与讨论界面。收回 Bob 的执行访问后，其界面恢复阅读与讨论，身份和材料保留。
4. 邀请面板可重置、关闭和重新开放链接；关闭链接时两位成员仍可读取原材料。面板明确区分链接管理与整个空间的关闭。
5. 详情页在 1440、1024、768 CSS 像素及深浅主题下实际截图并目视复核；长路径换行、成员操作可达、页面无水平溢出。邀请弹窗另检查 1440 浅色和 768 深色。Escape 关闭弹窗后焦点回到“邀请成员”。三个页面的浏览器控制台均无错误或警告。

截图位于本地忽略目录 `output/playwright/teamcross-space-ux-20260919/`，包括 `unshared-1440-light.png`、`invite-1440-light.png`、`invite-768-dark.png` 和六种 `execution-<width>-<theme>.png`。不把邀请 secret 写入记录或截图。

## 边界与收尾

本次未运行真实 Codex/Claude 模型、原生 TUI/Desktop 或两台 Mac 验收。共享链接只在当前共享持续期间有效，结束共享或 Core 重启后的新共享不延续原成员凭据。移除成员撤销其当前资格；仍持有效链接的人可以重新加入，如需停止接纳应关闭或重置链接。

测试使用独立数据目录和命名浏览器会话；结束时由测试 fixture 关闭自身 Core 与监听，关闭本次 Playwright 浏览器，不按程序名终止用户客户端。本记录不表示已发布版本或远端提交。
