import Foundation

@main
enum LegacyReceiver {
    static func main() throws {
        let directory = CommandLine.arguments[1]
        let instance = try AppInstance(dataDirectory: directory)
        var count = 0
        guard try instance.claim(receive: { _ in
            count += 1
            try? String(count).write(toFile: directory + "/count", atomically: true, encoding: .utf8)
            return true
        }) else { fatalError("fixture lock not acquired") }
        defer { instance.close() }
        try "ready".write(toFile: directory + "/ready", atomically: true, encoding: .utf8)
        RunLoop.main.run()
    }
}
