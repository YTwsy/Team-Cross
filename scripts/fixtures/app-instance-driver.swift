import AppKit

// Native launch/quit events, scoped to the fixture's explicit App paths/PIDs.
let args = CommandLine.arguments
if args[1] == "quit" {
    let accepted = NSRunningApplication(processIdentifier: Int32(args[2])!)?.terminate() ?? false
    print(accepted ? "true" : "false")
    exit(accepted ? 0 : 1)
}
let configuration = NSWorkspace.OpenConfiguration()
configuration.createsNewApplicationInstance = args[1] != "reopen"
configuration.allowsRunningApplicationSubstitution = false
configuration.activates = false
configuration.environment = ProcessInfo.processInfo.environment
let completion: (NSRunningApplication?, Error?) -> Void = { app, error in
    if let error { fputs(error.localizedDescription + "\n", stderr); exit(1) }
    print(app!.processIdentifier)
    exit(0)
}
let application = URL(fileURLWithPath: args[2])
if args.count > 3 {
    NSWorkspace.shared.open(args.dropFirst(3).map { URL(string: $0)! }, withApplicationAt: application, configuration: configuration, completionHandler: completion)
} else {
    NSWorkspace.shared.openApplication(at: application, configuration: configuration, completionHandler: completion)
}
RunLoop.main.run()
