import Foundation

@main
enum InstanceProbe {
    static func main() throws {
        let args = CommandLine.arguments
        let instance = try AppInstance(dataDirectory: args[1])
        let request = AppInstance.Request(id: args[2], urls: Array(args.dropFirst(3)))
        print(instance.forward(request) ? "true" : "false")
    }
}
