import Foundation

// The Core owns the saved preference. The shell reads it at startup so its
// menu has the right language before the service finishes launching.
enum AppLanguage {
    static var mode = "auto"
    static var resolved = systemLanguage()

    static func systemLanguage() -> String {
        let first = Locale.preferredLanguages.first?.lowercased() ?? ""
        return first == "zh" || first.hasPrefix("zh-") || first.hasPrefix("zh_") ? "zh-CN" : "en"
    }

    static func load(dataDirectory: String) {
        guard let data = try? Data(contentsOf: URL(fileURLWithPath: dataDirectory).appendingPathComponent("settings.json")),
              let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            adopt(mode: "auto")
            return
        }
        adopt(mode: object["uiLanguage"] as? String)
    }

    static func adopt(mode selected: String?, resolved language: String? = nil) {
        mode = ["auto", "zh-CN", "en"].contains(selected ?? "") ? selected! : "auto"
        resolved = language == "zh-CN" || language == "en" ? language! : (mode == "auto" ? systemLanguage() : mode)
    }

    static func text(_ chinese: String) -> String {
        let name = resolved == "zh-CN" ? "zh-Hans" : "en"
        guard let path = Bundle.main.path(forResource: name, ofType: "lproj"),
              let bundle = Bundle(path: path) else { return chinese }
        return bundle.localizedString(forKey: chinese, value: chinese, table: nil)
    }

    static func format(_ chinese: String, _ arguments: CVarArg...) -> String {
        String(format: text(chinese), locale: Locale(identifier: resolved), arguments: arguments)
    }
}
