# 2026-09-18 多成员基础验证

基于 `bf00291` 后的多成员实现，分支 `codex/collaboration-spaces`。环境为单台 Apple Silicon Mac、Go 1.27.1、内置浏览器及本地三个独立 Core 数据目录。原生 Provider 使用测试替身；不调用模型、不读取用户 Session、不打开原生客户端。

## 已检查

- `go test ./...`、`go vet ./...` 通过；collab、mcp、sharing、nativecodex、nativeclaude、service、cliinstall 的 race test 通过。
- Web check、55 项 Web 测试和 build 通过；更新内嵌 dist。
- 三个独立 Core 通过 loopback TLS 加入同一协作，B、C 使用不同成员身份和凭据；每份邀请只允许一人，重试恢复原邀请，逐份撤销不影响其他邀请或成员。
- A 明确交给 B 或 C；多人省略接收者拒绝；其他成员仍能排队与讨论。移除正在输入的 B 接回控制并保留 busy，C 继续读取。相同请求 ID 不跨成员复用，改变输入内容拒绝。
- 同名成员的批注回复保持不同 `authorId`；移除成员取消其未完成访问；凭据不能跨协作访问。
- 浏览器实际操作：显示 A/B/C，交给 Carol，移除 Bob 后 Carol 保持输入，键盘接回；生成两份待用邀请，单独撤销其中一份；Carol 无输入权时仍可保存批注。
- DOM 确认实际 CSS 宽度为 1440、1024、768，均无页面横向溢出；查看页面截图，复核深浅主题，未发现控制台错误。浏览器缩放导致物理视口与 CSS 宽度不同，按 `innerWidth` 校正后检查。
- 测试页面关闭、视口恢复、专用 Core 与 TLS 监听通过 fixture 完成标志统一关闭。

## 边界

这是多人成员与输入协调的阶段检查。独立只读空间、多个已发布材料及其版本、可选执行对象尚未完成。未运行真实模型、原生 TUI/Desktop 三方接入、三台 Mac LAN、跨网络 Tailcat 或强制 DERP；旧双人结果不推广为本次多人验收。

浏览器复现入口为 [members_browser_test.go](../../../../internal/collab/members_browser_test.go)：设置新的 `TEAMCROSS_MEMBERS_BROWSER_DIR`，运行 `TestMembersBrowserFixture`，读取目录中的测试地址；创建 `finish` 文件结束，15 分钟自动超时关闭。测试路径和邀请仅保存在本机临时目录，正文不记录真实凭据。
