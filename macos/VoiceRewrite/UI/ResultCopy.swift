// Source: voice-rewrite/internal/core/ux
import Foundation

enum ResultCopy {
    static let dictationHistory = "听写结果"
    static let dictationNotInserted = "听写结果 · 未自动插入"
    static let dictationCopyManually = "听写结果 · 请手动复制"
    static let copySucceeded = "已复制结果到剪贴板"
    static let copyFailed = "复制失败，请尝试选中正文复制"
    static let passwordDictation = "密码输入框不接受自动听写，请使用普通输入方式"
    static let passwordSelection = "密码输入框不允许读取选中文字或发送给 AI"
    static let passwordRewrite = "已拒绝在密码输入框中自动改写"
    static let dictationEmpty = "听写结果为空"
    static let noSpeech = "ASR 没有识别到语音，请检查麦克风输入设备"

    static func cardTitle(intent: String) -> String {
        switch intent {
        case "edit":
            return "改写预览"
        case "summarize":
            return "AI 摘要"
        case "translate":
            return "翻译结果"
        case "ambiguous":
            return "AI 回答"
        default:
            return "AI 解释"
        }
    }

    static func historyTitle(intent: String) -> String {
        var intent = intent
        intent = intent.trimmingCharacters(in: .whitespacesAndNewlines)
        if (intent == "") {
            return "AI 结果"
        }
        return ("AI " + intent)
    }

    static func historyRestoreTitle(_ storedTitle: String) -> String {
        return (storedTitle.trimmingCharacters(in: .whitespacesAndNewlines) + " · 历史结果")
    }

}

enum ServiceProbeCopy {
    static let idle = "尚未检测连接"
    static let running = "正在检测服务…"
    static let stale = "连接配置已修改 · 请重新检测"
}

enum SettingsCopy {
    static let saveWhileBusy = "请先完成当前录音／改写，再保存"
}

enum VoicePauseCopy {
    static let paused = "已暂停 · 鼠标侧键恢复正常功能"
    static let idle = "待机 · 使用配置的听写和改写快捷键"
    static let pause = "暂停语音操作（保留鼠标侧键原功能）"
    static let resume = "恢复语音操作"
    static let busy = "当前任务仍在处理；结束后可暂停语音操作。"
}
