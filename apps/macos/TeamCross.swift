import AppKit

// The App owns only its menu. All service decisions stay in the bundled Go launcher.
final class TeamCrossDelegate: NSObject, NSApplicationDelegate {
    private var item: NSStatusItem!
    private let statusItem = NSMenuItem(title: "正在启动…", action: nil, keyEquivalent: "")
    private var timer: Timer?
    private var busy = false
    private var quitting = false
    private var receivedURL = false
    private var pendingURLs: [String] = []
    private var baseURL: URL?
    private var refreshing = false
    private let dataDir = ProcessInfo.processInfo.environment["TEAMCROSS_DATA_DIR"]
    private let cliDir = ProcessInfo.processInfo.environment["TEAMCROSS_CLI_DIR"]
    private var executable: URL { Bundle.main.bundleURL.appendingPathComponent("Contents/Resources/teamcross") }

    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.accessory)
        item = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
        item.button?.image = NSImage(systemSymbolName: "person.2.fill", accessibilityDescription: "Team Cross")
        item.button?.image?.isTemplate = true
        let menu = NSMenu()
        menu.addItem(NSMenuItem(title: "Team Cross", action: nil, keyEquivalent: ""))
        menu.addItem(statusItem)
        menu.addItem(.separator())
        add("打开协作空间", #selector(openHome), to: menu)
        add("加入协作…", #selector(openJoin), to: menu)
        add("启动服务", #selector(startService), to: menu)
        add("诊断与设置", #selector(diagnostics), to: menu)
        add("命令行工具…", #selector(commandLineTools), to: menu)
        menu.addItem(.separator())
        add("退出 Team Cross", #selector(quit), to: menu, key: "q")
        item.menu = menu
        if !receivedURL { launch(route: "") }
        drainInvites()
        timer = Timer.scheduledTimer(withTimeInterval: 5, repeats: true) { [weak self] _ in self?.refresh() }
    }
    private func add(_ title: String, _ action: Selector, to menu: NSMenu, key: String = "") {
        let entry = NSMenuItem(title: title, action: action, keyEquivalent: key); entry.target = self; menu.addItem(entry)
    }
    func application(_ application: NSApplication, open urls: [URL]) {
        for url in urls where url.scheme == "teamcross" && url.host == "join" {
            receivedURL = true
            pendingURLs.append(url.absoluteString)
        }
        if item != nil { drainInvites() }
    }
    private func drainInvites() {
        guard !busy, !pendingURLs.isEmpty else { return }
        let invitation = pendingURLs.removeFirst()
        busy = true
        call(["join", "--preview", "--stdin", "--no-open", "--json"], input: invitation) { result in
            self.busy = false
            switch result {
            case .success(let value): self.openResult(value)
            case .failure(let error): self.showError(error.localizedDescription)
            }
            self.refresh(); self.drainInvites()
        }
    }
    private func call(_ args: [String], input: String? = nil, administrator: Bool = false, completion: @escaping (Result<[String: Any], Error>) -> Void) {
        let binary = executable
        let directory = dataDir
        let commandDirectory = cliDir
        DispatchQueue.global(qos: .userInitiated).async {
            do {
                let process = Process(); process.executableURL = binary
                let cliCommand = ["cli-status", "install-cli", "uninstall-cli"].contains(args.first ?? "")
                let arguments = args + (cliCommand ? (commandDirectory.map { ["--cli-dir", $0] } ?? []) : (directory.map { ["--data-dir", $0] } ?? []))
                if administrator {
                    // Only the install/remove command runs with privilege. Never elevate Core.
                    guard ["install-cli", "uninstall-cli"].contains(args.first ?? "") else { throw NSError(domain: "TeamCross", code: 1, userInfo: [NSLocalizedDescriptionKey: "此操作不支持系统授权"]) }
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
                    throw NSError(domain: "TeamCross", code: Int(process.terminationStatus), userInfo: [NSLocalizedDescriptionKey: String(data: errorData, encoding: .utf8)?.trimmingCharacters(in: .whitespacesAndNewlines) ?? "服务命令未完成", "problemCode": value["code"] as? String ?? ""])
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
        guard !busy else { return }; busy = true
        call(["serve", "--no-open", "--json"]) { result in
            self.busy = false
            switch result {
            case .success(let value): self.openResult(value, route: route)
            case .failure(let error): self.statusItem.title = "服务未启动"; self.showError(error.localizedDescription)
            }
            self.refresh(); self.drainInvites()
        }
    }
    private func refresh() {
        guard !refreshing, !quitting else { return }; refreshing = true
        call(["status", "--json"]) { result in
            self.refreshing = false
            if case .success(let status) = result, status["running"] as? Bool == true {
                self.baseURL = (status["url"] as? String).flatMap(URL.init(string:))
                self.statusItem.title = "服务运行中 · 活动协作 \(status["active"] as? Int ?? 0)"
            } else { self.statusItem.title = "服务已停止 · 可手动启动"; self.baseURL = nil }
        }
    }
    @objc private func openHome() { launch(route: "") }
    @objc private func openJoin() { launch(route: "/#/join") }
    @objc private func startService() { launch(route: "") }
    @objc private func diagnostics() { launch(route: "/#/settings") }
    @objc private func commandLineTools() {
        guard !busy, !quitting else { return }; busy = true
        call(["cli-status", "--json"]) { result in
            self.busy = false
            guard case .success(let status) = result else {
                if case .failure(let error) = result { self.showError(error.localizedDescription) }; return
            }
            NSApp.activate(ignoringOtherApps: true)
            let alert = NSAlert(); alert.messageText = "Team Cross 命令行工具"
            let target = status["target"] as? String ?? "/usr/local/bin/teamcross"
            let command = status["command"] as? String ?? "尚未安装"
            alert.informativeText = "当前命令：\(command)\n安装位置：\(target)\n\n安装后可在终端运行 teamcross。移除命令入口不会停止服务或删除协作数据。"
            var operations: [String] = []
            if status["canInstall"] as? Bool == true && status["installed"] as? Bool != true {
                alert.addButton(withTitle: "安装命令"); operations.append("install-cli")
            }
            if status["canRemove"] as? Bool == true {
                alert.addButton(withTitle: "移除命令"); operations.append("uninstall-cli")
            }
            if let conflict = status["conflict"] as? String, !conflict.isEmpty {
                alert.informativeText += "\n\n已有命令由其他安装管理：\(conflict)。请通过原安装渠道切换。"
            } else if operations.isEmpty {
                alert.informativeText += "\n\n现有命令由安装渠道管理，无需重复安装。"
            }
            alert.addButton(withTitle: "关闭")
            let selected = alert.runModal().rawValue - NSApplication.ModalResponse.alertFirstButtonReturn.rawValue
            if selected >= 0 && selected < operations.count { self.manageCommand(operations[selected]) }
        }
    }
    private func manageCommand(_ command: String, administrator: Bool = false) {
        busy = true
        call([command, "--json"], administrator: administrator) { result in
            self.busy = false
            switch result {
            case .success(let value):
                NSApp.activate(ignoringOtherApps: true)
                let alert = NSAlert(); alert.messageText = command == "install-cli" ? "命令行工具已安装" : "命令入口已移除"
                alert.informativeText = command == "install-cli" ? "打开终端，运行 teamcross。\n\(value["target"] as? String ?? "")" : "App、服务和协作数据继续保留。"
                if command == "install-cli" && value["pathReady"] as? Bool == false { alert.informativeText += "\n如终端找不到命令，请检查 PATH 是否包含该目录。" }
                alert.addButton(withTitle: "完成"); alert.runModal()
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
                let alert = NSAlert(); alert.messageText = "停止本机服务并退出？"
                alert.informativeText = "将断开本机的 \(active) 个活动协作。发起的运行时会停止，参与的协作只断开本机连接。会话、代码和工作目录都会保留。"
                alert.addButton(withTitle: "停止并退出"); alert.addButton(withTitle: "取消")
                if alert.runModal() != .alertFirstButtonReturn { self.quitting = false; return }
            }
            self.call(["stop", "--force", "--json"]) { result in
                switch result { case .success: self.finishQuit(); case .failure(let error): self.quitting = false; self.showError(error.localizedDescription) }
            }
        }
    }
    private func finishQuit() { timer?.invalidate(); NSApp.terminate(nil) }
    func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
        if quitting { return .terminateNow }; quit(); return .terminateCancel
    }
    private func showError(_ message: String) {
        NSApp.activate(ignoringOtherApps: true)
        let alert = NSAlert(); alert.messageText = "Team Cross 需要处理"; alert.informativeText = message
        alert.addButton(withTitle: "知道了"); alert.runModal()
    }
}
let delegate = TeamCrossDelegate()
NSApplication.shared.delegate = delegate
NSApplication.shared.run()
