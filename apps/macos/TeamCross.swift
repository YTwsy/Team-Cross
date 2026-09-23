import AppKit
@preconcurrency import WebKit
import Carbon

// The App owns its menu and quick view. All service decisions stay in the bundled Go launcher.
final class TeamCrossDelegate: NSObject, NSApplicationDelegate {
    private var item: NSStatusItem!
    private let statusItem = NSMenuItem(title: "", action: nil, keyEquivalent: "")
    private var timer: Timer?
    private var busy = false
    private var quitting = false
    private var receivedURL = false
    private var pendingURLs: [String] = []
    private var baseURL: URL?
    private var refreshing = false
    private var dataDir = ProcessInfo.processInfo.environment["TEAMCROSS_DATA_DIR"]
    private let cliDir = ProcessInfo.processInfo.environment["TEAMCROSS_CLI_DIR"]
    private var executable: URL { Bundle.main.bundleURL.appendingPathComponent("Contents/Resources/teamcross") }
    private var instance: AppInstance?
    private var startupTimer: Timer?
    private var startupDeadline = Date.distantFuture
    private var forwarding = false
    private var forwardedRequest: AppInstance.Request?
    private var shellOnlyExit = false
    private var pendingRoute: String?
    private var serviceMenu: NSMenu?
    private var quickLook: ResourceQuickLook?
    private var hotKey: EventHotKeyRef?
    private var hotKeyHandler: EventHandlerRef?
    private var openingQuickLook = false
    private var menuLabels: [(NSMenuItem, String)] = []
    private var quickEntry: NSMenuItem?
    private var languageEntry: NSMenuItem?
    private var languageChoices: [String: NSMenuItem] = [:]
    private var hotKeyRegistered = false
    private var serviceRunning: Bool?
    private var activeCount = 0

    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.accessory)
        do {
            instance = try AppInstance(dataDirectory: dataDir)
            dataDir = instance?.dataDirectory
        } catch {
            showError(error.localizedDescription); finishShellOnly(); return
        }
        startupDeadline = Date().addingTimeInterval(10)
        // Let initial URL Apple events arrive before deciding to open Home.
        startupTimer = Timer.scheduledTimer(withTimeInterval: 0.1, repeats: true) { [weak self] _ in self?.coordinateStartup() }
    }
    private func coordinateStartup() {
        guard !forwarding, !shellOnlyExit, let instance else { return }
        do {
            if try instance.claim(receive: { [weak self] request in
                guard let self, !self.quitting, self.pendingURLs.count + request.urls.count <= 32 else { return false }
                if request.urls.isEmpty { self.pendingRoute = "" }
                else { self.pendingURLs.append(contentsOf: request.urls) }
                self.drainRequests()
                return true
            }) {
                startupTimer?.invalidate(); startupTimer = nil
                createMenu()
                return
            }
        } catch {
            showError(error.localizedDescription); finishShellOnly(); return
        }
        guard Date() < startupDeadline else {
            showError(AppLanguage.text("已有 Team Cross App 暂时无法接收打开请求。请从现有菜单栏入口打开协作空间，或稍后重试。"))
            finishShellOnly(); return
        }
        if forwardedRequest == nil { forwardedRequest = AppInstance.Request(urls: pendingURLs) }
        guard let request = forwardedRequest else { return }
        forwarding = true
        DispatchQueue.global(qos: .userInitiated).async {
            let accepted = instance.forward(request)
            DispatchQueue.main.async {
                self.forwarding = false
                guard accepted else { return }
                self.pendingURLs.removeFirst(request.urls.count)
                self.forwardedRequest = nil
                if self.pendingURLs.isEmpty { self.finishShellOnly() }
            }
        }
    }
    private func createMenu() {
        item = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
        item.button?.image = NSImage(systemSymbolName: "person.2.fill", accessibilityDescription: "Team Cross")
        item.button?.image?.isTemplate = true
        let menu = NSMenu()
        menu.addItem(NSMenuItem(title: "Team Cross", action: nil, keyEquivalent: ""))
        menu.addItem(statusItem)
        menu.addItem(.separator())
        add("打开协作空间", #selector(openHome), to: menu)
        add("打开资源库", #selector(openLibrary), to: menu)
        add("加入协作…", #selector(openJoin), to: menu)
        add("启动服务", #selector(startService), to: menu)
        add("诊断与设置", #selector(diagnostics), to: menu)
        add("命令行工具…", #selector(commandLineTools), to: menu)
        let languageItem = NSMenuItem(title: "", action: nil, keyEquivalent: "")
        let languageMenu = NSMenu()
        for (mode, title) in [("auto", "跟随系统"), ("zh-CN", "简体中文"), ("en", "English")] {
            let entry = NSMenuItem(title: title, action: #selector(selectLanguage(_:)), keyEquivalent: "")
            entry.target = self
            entry.representedObject = mode
            languageChoices[mode] = entry
            languageMenu.addItem(entry)
        }
        languageItem.submenu = languageMenu
        languageEntry = languageItem
        menu.addItem(languageItem)
        menu.addItem(.separator())
        add("退出 Team Cross", #selector(quit), to: menu, key: "q")
        let quickEntry = NSMenuItem(title: "", action: #selector(toggleQuickLook), keyEquivalent: "")
        quickEntry.target = self
        menu.insertItem(quickEntry, at: 3)
        self.quickEntry = quickEntry
        serviceMenu = menu
        item.button?.target = self
        item.button?.action = #selector(statusClicked)
        item.button?.sendAction(on: [.leftMouseUp, .rightMouseUp])
        var event = EventTypeSpec(eventClass: OSType(kEventClassKeyboard), eventKind: UInt32(kEventHotKeyPressed))
        let context = Unmanaged.passUnretained(self).toOpaque()
        InstallEventHandler(GetApplicationEventTarget(), { _, _, context in
            guard let context else { return OSStatus(eventNotHandledErr) }
            Unmanaged<TeamCrossDelegate>.fromOpaque(context).takeUnretainedValue().toggleQuickLook()
            return noErr
        }, 1, &event, context, &hotKeyHandler)
        hotKeyRegistered = RegisterEventHotKey(UInt32(kVK_ANSI_T), UInt32(controlKey | optionKey), EventHotKeyID(signature: 0x54435253, id: 1), GetApplicationEventTarget(), 0, &hotKey) == noErr
        refreshLanguage()
        if !receivedURL { launch(route: "") }
        drainRequests()
        timer = Timer.scheduledTimer(withTimeInterval: 5, repeats: true) { [weak self] _ in self?.refresh() }
    }
    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        if item != nil { launch(route: "") }
        return false
    }
    private func add(_ title: String, _ action: Selector, to menu: NSMenu, key: String = "") {
        let entry = NSMenuItem(title: AppLanguage.text(title), action: action, keyEquivalent: key)
        entry.target = self
        menuLabels.append((entry, title))
        menu.addItem(entry)
    }
    private func refreshLanguage() {
        for (entry, key) in menuLabels { entry.title = AppLanguage.text(key) }
        quickEntry?.title = AppLanguage.text("协作速览") + (hotKeyRegistered ? "    ⌃⌥T" : "")
        languageEntry?.title = AppLanguage.text("语言")
        languageChoices["auto"]?.title = AppLanguage.text("跟随系统")
        languageChoices["zh-CN"]?.title = AppLanguage.text("简体中文")
        languageChoices["en"]?.title = "English"
        for (mode, entry) in languageChoices { entry.state = mode == AppLanguage.mode ? .on : .off }
        if let running = serviceRunning {
            statusItem.title = running
                ? AppLanguage.format("服务运行中 · 活动协作 %d", activeCount)
                : AppLanguage.text("服务已停止 · 可手动启动")
        } else { statusItem.title = AppLanguage.text("正在启动…") }
        quickLook?.refreshLanguage()
    }
    @objc private func selectLanguage(_ sender: NSMenuItem) {
        guard !busy, let mode = sender.representedObject as? String else { return }
        busy = true
        call(["ui-language", "--set", mode, "--json"]) { result in
            self.busy = false
            switch result {
            case .success(let value):
                AppLanguage.adopt(mode: value["mode"] as? String, resolved: value["resolved"] as? String)
                self.refreshLanguage()
            case .failure(let error): self.showError(error.localizedDescription)
            }
            self.drainRequests()
        }
    }
    func application(_ application: NSApplication, open urls: [URL]) {
        for url in urls where url.scheme == "teamcross" && url.host == "join" {
            receivedURL = true
            pendingURLs.append(url.absoluteString)
        }
        if item != nil { drainRequests() }
    }
    private func drainRequests() {
        guard item != nil, !busy, !quitting else { return }
        guard !pendingURLs.isEmpty else {
            if let route = pendingRoute { pendingRoute = nil; openRoute(route) }
            return
        }
        let invitation = pendingURLs.removeFirst()
        busy = true
        call(["join", "--preview", "--stdin", "--no-open", "--json"], input: invitation) { result in
            self.busy = false
            switch result {
            case .success(let value): self.openResult(value)
            case .failure(let error): self.showError(error.localizedDescription)
            }
            self.refresh(); self.drainRequests()
        }
    }
    private func call(_ args: [String], input: String? = nil, administrator: Bool = false, completion: @escaping (Result<[String: Any], Error>) -> Void) {
        let binary = executable
        let directory = dataDir
        let commandDirectory = cliDir
        let english = AppLanguage.resolved == "en"
        let unauthorizedMessage = AppLanguage.text("此操作不支持系统授权")
        let failureMessage = AppLanguage.text("操作未完成，请查看诊断信息。")
        let incompleteMessage = AppLanguage.text("服务命令未完成")
        DispatchQueue.global(qos: .userInitiated).async {
            do {
                let process = Process(); process.executableURL = binary
                let cliCommand = ["cli-status", "install-cli", "uninstall-cli"].contains(args.first ?? "")
                let arguments = args + (cliCommand ? (commandDirectory.map { ["--cli-dir", $0] } ?? []) : (directory.map { ["--data-dir", $0] } ?? []))
                if administrator {
                    // Only the install/remove command runs with privilege. Never elevate Core.
                    guard ["install-cli", "uninstall-cli"].contains(args.first ?? "") else { throw NSError(domain: "TeamCross", code: 1, userInfo: [NSLocalizedDescriptionKey: unauthorizedMessage]) }
                    let shell = ([binary.path] + arguments).map { "'" + $0.replacingOccurrences(of: "'", with: "'\"'\"'") + "'" }.joined(separator: " ")
                    let literal = shell.replacingOccurrences(of: "\\", with: "\\\\").replacingOccurrences(of: "\"", with: "\\\"").replacingOccurrences(of: "\n", with: "\\n").replacingOccurrences(of: "\r", with: "\\r")
                    process.executableURL = URL(fileURLWithPath: "/usr/bin/osascript")
                    process.arguments = ["-e", "do shell script \"" + literal + "\" with administrator privileges"]
                } else { process.arguments = arguments }
                let stdout = Pipe(), stderr = Pipe(); process.standardOutput = stdout; process.standardError = stderr
                let stdin = Pipe(); process.standardInput = stdin
                try process.run()
                if let input { stdin.fileHandleForWriting.write(Data(input.utf8)) }
                try? stdin.fileHandleForWriting.close()
                let output = stdout.fileHandleForReading.readDataToEndOfFile()
                let errorData = stderr.fileHandleForReading.readDataToEndOfFile()
                process.waitUntilExit()
                let value = (try? JSONSerialization.jsonObject(with: output)) as? [String: Any] ?? [:]
                if process.terminationStatus != 0 {
                    let detail = String(data: errorData, encoding: .utf8)?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
                    let code = value["code"] as? String ?? ""
                    let message = english
                        ? failureMessage + (code.isEmpty ? "" : " (\(code))")
                        : (detail.isEmpty ? incompleteMessage : detail)
                    throw NSError(domain: "TeamCross", code: Int(process.terminationStatus), userInfo: [NSLocalizedDescriptionKey: message, "problemCode": code])
                }
                DispatchQueue.main.async { completion(.success(value)) }
            } catch { DispatchQueue.main.async { completion(.failure(error)) } }
        }
    }
    private func openResult(_ value: [String: Any], route: String = "") {
        if let raw = value["url"] as? String, let url = URL(string: raw + route) {
            baseURL = URL(string: String(raw.split(separator: "#").first ?? "")); NSWorkspace.shared.open(url)
        }
    }
    private func launch(route: String) {
        pendingRoute = route
        drainRequests()
    }
    private func openRoute(_ route: String) {
        busy = true
        call(["serve", "--no-open", "--json"]) { result in
            self.busy = false
            switch result {
            case .success(let value): self.openResult(value, route: route)
            case .failure(let error): self.serviceRunning = false; self.statusItem.title = AppLanguage.text("服务未启动"); self.showError(error.localizedDescription)
            }
            self.refresh(); self.drainRequests()
        }
    }
    private func refresh() {
        guard !refreshing, !quitting else { return }; refreshing = true
        call(["status", "--json"]) { result in
            self.refreshing = false
            if case .success(let status) = result, status["running"] as? Bool == true {
                self.baseURL = (status["url"] as? String).flatMap(URL.init(string:))
                self.serviceRunning = true
                self.activeCount = status["active"] as? Int ?? 0
                AppLanguage.adopt(mode: status["uiLanguage"] as? String, resolved: status["resolvedLanguage"] as? String)
            } else { self.serviceRunning = false; self.baseURL = nil }
            self.refreshLanguage()
        }
    }
    @objc private func statusClicked() {
        if NSApp.currentEvent?.type == .rightMouseUp { showServiceMenu() }
        else { toggleQuickLook() }
    }
    private func showServiceMenu() {
        guard let button = item.button, let menu = serviceMenu else { return }
        quickLook?.dismiss()
        // The status bar button can be VibrantDark while the app is Aqua.
        menu.appearance = NSApp.effectiveAppearance
        menu.popUp(positioning: nil, at: NSPoint(x: 0, y: button.bounds.height), in: button)
    }
    @objc fileprivate func toggleQuickLook() {
        guard !quitting, !openingQuickLook, item != nil else { return }
        if let quickLook, quickLook.visible { quickLook.dismiss(); return }
        openingQuickLook = true
        // Reuse the launcher's service identity checks, including after restart.
        call(["serve", "--no-open", "--json"]) { result in
            self.openingQuickLook = false
            guard !self.quitting else { return }
            switch result {
            case .success(let value):
                guard let raw = value["url"] as? String, let url = URL(string: raw), let button = self.item.button else { return }
                self.baseURL = url
                if self.quickLook?.baseURL != url {
                    self.quickLook?.close()
                    self.quickLook = ResourceQuickLook(baseURL: url, menu: { [weak self] in self?.showServiceMenu() })
                }
                self.quickLook?.show(relativeTo: button)
            case .failure(let error): self.showError(error.localizedDescription)
            }
        }
    }
    @objc private func openLibrary() { launch(route: "/#/library") }
    @objc private func openHome() { launch(route: "") }
    @objc private func openJoin() { launch(route: "/#/join") }
    @objc private func startService() { launch(route: "") }
    @objc private func diagnostics() { launch(route: "/#/settings") }
    @objc private func commandLineTools() {
        guard !busy, !quitting else { return }; busy = true
        call(["cli-status", "--json"]) { result in
            self.busy = false
            defer { self.drainRequests() }
            guard case .success(let status) = result else {
                if case .failure(let error) = result { self.showError(error.localizedDescription) }; return
            }
            NSApp.activate(ignoringOtherApps: true)
            let alert = NSAlert(); alert.messageText = AppLanguage.text("Team Cross 命令行工具")
            let target = status["target"] as? String ?? "/usr/local/bin/teamcross"
            let command = status["command"] as? String ?? AppLanguage.text("尚未安装")
            alert.informativeText = AppLanguage.format("当前命令：%@\n安装位置：%@\n\n安装后可在终端运行 teamcross。移除命令入口不会停止服务或删除协作数据。", command, target)
            var operations: [String] = []
            if status["canInstall"] as? Bool == true && status["installed"] as? Bool != true {
                alert.addButton(withTitle: AppLanguage.text("安装命令")); operations.append("install-cli")
            }
            if status["canRemove"] as? Bool == true {
                alert.addButton(withTitle: AppLanguage.text("移除命令")); operations.append("uninstall-cli")
            }
            if let conflict = status["conflict"] as? String, !conflict.isEmpty {
                alert.informativeText += AppLanguage.format("\n\n已有命令由其他安装管理：%@。请通过原安装渠道切换。", conflict)
            } else if operations.isEmpty {
                alert.informativeText += AppLanguage.text("\n\n现有命令由安装渠道管理，无需重复安装。")
            }
            alert.addButton(withTitle: AppLanguage.text("关闭"))
            let selected = alert.runModal().rawValue - NSApplication.ModalResponse.alertFirstButtonReturn.rawValue
            if selected >= 0 && selected < operations.count { self.manageCommand(operations[selected]) }
        }
    }
    private func manageCommand(_ command: String, administrator: Bool = false) {
        busy = true
        call([command, "--json"], administrator: administrator) { result in
            self.busy = false
            defer { self.drainRequests() }
            switch result {
            case .success(let value):
                NSApp.activate(ignoringOtherApps: true)
                let alert = NSAlert(); alert.messageText = command == "install-cli" ? AppLanguage.text("命令行工具已安装") : AppLanguage.text("命令入口已移除")
                alert.informativeText = command == "install-cli" ? AppLanguage.format("打开终端，运行 teamcross。\n%@", value["target"] as? String ?? "") : AppLanguage.text("App、服务和协作数据继续保留。")
                if command == "install-cli" && value["pathReady"] as? Bool == false { alert.informativeText += AppLanguage.text("\n如终端找不到命令，请检查 PATH 是否包含该目录。") }
                alert.addButton(withTitle: AppLanguage.text("完成")); alert.runModal()
            case .failure(let error):
                if !administrator && (error as NSError).userInfo["problemCode"] as? String == "cli_permission_denied" {
                    self.manageCommand(command, administrator: true)
                } else { self.showError(error.localizedDescription) }
            }
        }
    }
    @objc private func quit() {
        guard !quitting, !busy else { return }; quitting = true
        call(["status", "--json"]) { result in
            guard case .success(let status) = result, status["running"] as? Bool == true else {
                // Missing service is safe to exit. A protocol/identity error must remain reviewable.
                if case .failure(let error) = result { self.quitting = false; self.showError(error.localizedDescription); return }
                self.finishQuit(); return
            }
            let active = status["active"] as? Int ?? 0
            if active > 0 {
                NSApp.activate(ignoringOtherApps: true)
                let alert = NSAlert(); alert.messageText = AppLanguage.text("停止本机服务并退出？")
                alert.informativeText = AppLanguage.format("将断开本机的 %d 个活动协作。发起的运行时会停止，参与的协作只断开本机连接。会话、代码和工作目录都会保留。", active)
                alert.addButton(withTitle: AppLanguage.text("停止并退出")); alert.addButton(withTitle: AppLanguage.text("取消"))
                if alert.runModal() != .alertFirstButtonReturn { self.quitting = false; return }
            }
            self.call(["stop", "--force", "--json"]) { result in
                switch result { case .success: self.finishQuit(); case .failure(let error): self.quitting = false; self.showError(error.localizedDescription) }
            }
        }
    }
    private func finishQuit() { timer?.invalidate(); NSApp.terminate(nil) }
    private func finishShellOnly() {
        shellOnlyExit = true
        startupTimer?.invalidate()
        NSApp.terminate(nil)
    }
    func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
        if shellOnlyExit || quitting { return .terminateNow }
        if item == nil { shellOnlyExit = true; return .terminateNow }
        quit(); return .terminateCancel
    }
    func applicationWillTerminate(_ notification: Notification) {
        quickLook?.close()
        if let hotKey { UnregisterEventHotKey(hotKey) }
        if let hotKeyHandler { RemoveEventHandler(hotKeyHandler) }
        instance?.close()
    }
    private func showError(_ message: String) {
        NSApp.activate(ignoringOtherApps: true)
        let alert = NSAlert(); alert.messageText = AppLanguage.text("Team Cross 需要处理"); alert.informativeText = message
        alert.addButton(withTitle: AppLanguage.text("知道了")); alert.runModal()
    }
}
// The quick view uses the same loopback API as WebGUI. Only personal references
// are persisted by Core, so WebKit and the external browser share one selection.
private final class ResourceQuickLook: NSObject, WKNavigationDelegate, WKScriptMessageHandler, NSWindowDelegate {
    let baseURL: URL
    private let menu: () -> Void
    private let popover = NSPopover()
    private let controller = NSViewController()
    private let web: WKWebView
    private weak var button: NSStatusBarButton?
    private var panel: NSPanel?
    var visible: Bool { popover.isShown || panel?.isVisible == true }
    init(baseURL: URL, menu: @escaping () -> Void) {
        self.baseURL = baseURL
        self.menu = menu
        let configuration = WKWebViewConfiguration()
        configuration.websiteDataStore = .nonPersistent()
        web = WKWebView(frame: NSRect(x: 0, y: 0, width: 470, height: 650), configuration: configuration)
        super.init()
        web.navigationDelegate = self
        configuration.userContentController.add(self, name: "teamcross")
        controller.view = web
        popover.contentSize = NSSize(width: 470, height: 650)
        popover.behavior = .transient
        popover.contentViewController = controller
        var url = URLComponents(url: baseURL, resolvingAgainstBaseURL: false)!
        url.path = "/"; url.fragment = "/library/quick"
        web.load(URLRequest(url: url.url!))
    }
    private func sameOrigin(_ url: URL?) -> Bool {
        guard let url else { return false }
        return url.scheme == baseURL.scheme && url.host == baseURL.host && url.port == baseURL.port
    }
    func show(relativeTo button: NSStatusBarButton) {
        self.button = button
        NSApp.activate(ignoringOtherApps: true)
        if let panel { panel.makeKeyAndOrderFront(nil) }
        else { popover.show(relativeTo: button.bounds, of: button, preferredEdge: .minY) }
        web.window?.makeFirstResponder(web)
        web.evaluateJavaScript("window.dispatchEvent(new Event('focus'))", completionHandler: nil)
    }
    func dismiss() { popover.performClose(nil); panel?.orderOut(nil) }
    func close() {
        dismiss(); panel?.close(); panel = nil
        web.stopLoading()
        web.configuration.userContentController.removeScriptMessageHandler(forName: "teamcross")
    }
    private func open(_ route: String) {
        guard route == "/" || route == "/settings" || route == "/library" || route.range(of: "^/library\\?item=[a-f0-9]{32}$", options: .regularExpression) != nil || route.range(of: "^/collaborations/[A-Za-z0-9-]+$", options: .regularExpression) != nil else { return }
        var target = URLComponents(url: baseURL, resolvingAgainstBaseURL: false)!
        target.path = "/"; target.fragment = route
        if let url = target.url { NSWorkspace.shared.open(url) }
        if panel == nil { dismiss() }
    }
    private func pin() {
        if let current = panel {
            current.orderOut(nil); current.contentViewController = nil; current.delegate = nil
            panel = nil; current.close()
            popover.contentViewController = controller
            if let button { show(relativeTo: button) }
        } else {
            popover.performClose(nil); popover.contentViewController = nil
            let window = NSPanel(contentRect: NSRect(x: 0, y: 0, width: 470, height: 650), styleMask: [.titled, .closable, .resizable, .utilityWindow], backing: .buffered, defer: false)
            window.title = AppLanguage.text("Team Cross · 协作速览")
            window.level = .floating; window.hidesOnDeactivate = false
            window.isReleasedWhenClosed = false
            window.minSize = NSSize(width: 420, height: 440)
            window.contentViewController = controller; window.delegate = self
            window.center(); panel = window
            window.makeKeyAndOrderFront(nil)
        }
        web.evaluateJavaScript("window.dispatchEvent(new CustomEvent('teamcross-pinned', {detail: \(panel != nil ? "true" : "false")}))", completionHandler: nil)
    }
    func windowShouldClose(_ sender: NSWindow) -> Bool {
        sender.orderOut(nil)
        return false
    }
    func refreshLanguage() { panel?.title = AppLanguage.text("Team Cross · 协作速览") }
    func userContentController(_ userContentController: WKUserContentController, didReceive message: WKScriptMessage) {
        guard message.frameInfo.isMainFrame, sameOrigin(message.frameInfo.request.url), let body = message.body as? [String: Any] else { return }
        switch body["action"] as? String {
        case "open": if let route = body["route"] as? String { open(route) }
        case "pin": pin()
        case "menu": menu()
        default: break
        }
    }
    func webView(_ webView: WKWebView, decidePolicyFor navigationAction: WKNavigationAction, decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
        guard sameOrigin(navigationAction.request.url) else { decisionHandler(.cancel); return }
        if let fragment = navigationAction.request.url?.fragment, fragment != "/library/quick", navigationAction.navigationType == .linkActivated {
            open(fragment); decisionHandler(.cancel); return
        }
        decisionHandler(.allow)
    }
}

@main
enum TeamCrossApplication {
    static func main() {
        let delegate = TeamCrossDelegate()
        NSApplication.shared.delegate = delegate
        withExtendedLifetime(delegate) { NSApplication.shared.run() }
    }
}
