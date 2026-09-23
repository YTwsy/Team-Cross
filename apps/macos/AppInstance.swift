import Foundation
import CoreFoundation
import CryptoKit
import Darwin

// This lock belongs to the menu bar shell, never to Core. Keep the lock file
// across exits so concurrent launchers cannot lock different inodes.
final class AppInstance {
    struct Request: Codable {
        var version = 1
        var id = UUID().uuidString
        var urls: [String]
    }
    private struct Receipt: Codable {
        var id: String
        var accepted: Bool
    }

    let dataDirectory: String
    private let name: String
    private var lock: Int32 = -1
    private var port: CFMessagePort?
    private var source: CFRunLoopSource?
    private var receive: ((Request) -> Bool)?
    private var accepted: [String: Date] = [:]

    init(dataDirectory: String?) throws {
        let path = dataDirectory ?? NSHomeDirectory() + "/Library/Application Support/Team Cross Next"
        let url = URL(fileURLWithPath: path, isDirectory: true)
        try FileManager.default.createDirectory(at: url, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        self.dataDirectory = url.standardizedFileURL.resolvingSymlinksInPath().path
        AppLanguage.load(dataDirectory: self.dataDirectory)
        let identity = Data("\(getuid())\n\(self.dataDirectory)".utf8)
        name = "io.github.ytwsy.teamcross.app." + SHA256.hash(data: identity).map { String(format: "%02x", $0) }.joined()
        lock = Darwin.open(self.dataDirectory + "/app.lock", O_CREAT | O_RDWR | O_CLOEXEC | O_NOFOLLOW, 0o600)
        guard lock >= 0 else { throw POSIXError(POSIXErrorCode(rawValue: errno) ?? .EIO) }
    }

    // Called on the main thread, before creating any status item. A separate
    // file lock also prevents a second shell if IPC is temporarily unavailable.
    func claim(receive: @escaping (Request) -> Bool) throws -> Bool {
        if port != nil { return true }
        guard flock(lock, LOCK_EX | LOCK_NB) == 0 else {
            if errno == EWOULDBLOCK { return false }
            throw POSIXError(POSIXErrorCode(rawValue: errno) ?? .EIO)
        }
        self.receive = receive
        var context = CFMessagePortContext(version: 0, info: Unmanaged.passUnretained(self).toOpaque(), retain: nil, release: nil, copyDescription: nil)
        guard let local = CFMessagePortCreateLocal(nil, name as CFString, { _, message, data, info in
            guard message == 1, let data, let info else { return nil }
            let instance = Unmanaged<AppInstance>.fromOpaque(info).takeUnretainedValue()
            return instance.reply(to: data as Data).map { Unmanaged.passRetained($0 as CFData) }
        }, &context, nil) else {
            flock(lock, LOCK_UN)
            throw NSError(domain: "TeamCross", code: 1, userInfo: [NSLocalizedDescriptionKey: AppLanguage.text("无法建立 App 本机通信入口，请稍后重试。")])
        }
        guard let runLoopSource = CFMessagePortCreateRunLoopSource(nil, local, 0) else {
            CFMessagePortInvalidate(local)
            flock(lock, LOCK_UN)
            throw NSError(domain: "TeamCross", code: 1, userInfo: [NSLocalizedDescriptionKey: AppLanguage.text("无法接收 App 打开请求，请稍后重试。")])
        }
        port = local
        source = runLoopSource
        CFRunLoopAddSource(CFRunLoopGetMain(), runLoopSource, .commonModes)
        return true
    }

    private func reply(to data: Data) -> Data? {
        guard data.count <= 1 << 20,
              let request = try? JSONDecoder().decode(Request.self, from: data),
              request.version == 1, UUID(uuidString: request.id) != nil,
              request.urls.count <= 32,
              request.urls.allSatisfy({ value in
                  guard value.utf8.count <= 65536, let url = URL(string: value) else { return false }
                  return url.scheme == "teamcross" && url.host == "join"
              }) else { return nil }
        let now = Date()
        accepted = accepted.filter { now.timeIntervalSince($0.value) < 600 }
        // A receipt acknowledges queuing, not completion of a join or browser
        // action. Retrying after a lost receipt cannot enqueue the request twice.
        let ok = accepted[request.id] != nil || (accepted.count < 512 && receive?(request) == true)
        if ok { accepted[request.id] = now }
        return try? JSONEncoder().encode(Receipt(id: request.id, accepted: ok))
    }

    // Runs off the main thread so a new process can keep receiving launch URL
    // events while the first shell is starting or handling an AppKit dialog.
    func forward(_ request: Request) -> Bool {
        guard let data = try? JSONEncoder().encode(request), data.count <= 1 << 20,
              let remote = CFMessagePortCreateRemote(nil, name as CFString) else { return false }
        var response: Unmanaged<CFData>?
        let result = CFMessagePortSendRequest(remote, 1, data as CFData, 1, 1, CFRunLoopMode.defaultMode.rawValue, &response)
        let reply = response?.takeRetainedValue()
        guard result == kCFMessagePortSuccess, let reply,
              let receipt = try? JSONDecoder().decode(Receipt.self, from: reply as Data) else { return false }
        return receipt.id == request.id && receipt.accepted
    }

    func close() {
        if let port { CFMessagePortInvalidate(port) }
        if let source { CFRunLoopRemoveSource(CFRunLoopGetMain(), source, .commonModes) }
        port = nil
        source = nil
        if lock >= 0 { Darwin.close(lock); lock = -1 }
    }
    deinit { close() }
}
