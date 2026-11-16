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
    static let viewDiagnosticLog = "查看诊断日志"

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

enum FailureCopy {
    static func status(prefix: String, message: String) -> String {
        owned(message) ? message : prefix + message
    }

    static func owned(_ message: String) -> Bool {
        switch message {
        case "ASR 没有识别到语音，请检查麦克风输入设备", "录音为空或 PCM 长度无效", "录音超出时长限制", "ASR 响应不是有效的 JSON {text:...} 或 text/plain", "选中文字为空", "语音指令为空", "文本模型返回空 choices", "文本模型返回空内容；请换用非 thinking 模式的 Instruct 文本模型", "听写结果为空", "录音太短（至少约 0.1 秒）或 PCM 格式无效", "语音太短，无法转写", "录音太短（至少约 0.1 秒）；请再试一次", "密码输入框不接受自动听写，请使用普通输入方式", "密码输入框不允许读取选中文字或发送给 AI", "已拒绝在密码输入框中自动改写", "语音接口不支持上传的音频格式（HTTP 415）。请确认 ASR 服务提供 /v1/audio/transcriptions，而不只是 /v1/chat/completions。", "API 验证失败。检查服务地址、模型 ID 和 API Key。", "无法连接模型服务。请启动 ASR / LLM 服务并检查端口，然后在设置中测试连接。", "模型服务响应超时。检查模型是否仍在加载、GPU 内存与网络连接。", "原选区或编辑窗口发生变化，自动写入已取消。请重新选中文字再试。", "该按键不能用作全局录音快捷键", "Windows 系统键组合不支持录音快捷键", "单键仅支持 F1–F24；字母、数字和方向键请搭配 Ctrl 或 Alt", "Shift+字母会干扰打字，请同时使用 Ctrl 或 Alt", "请按下快捷键", "Ctrl 重复", "Alt 重复", "Shift 重复", "Pause/Break 没有可靠的松开事件", "听写快捷键无效，请重新录入（支持功能键和 Ctrl/Alt 组合键）", "听写和改写不能占用同一个鼠标侧键", "键盘听写快捷键当前只支持按住说话模式（record_mode=hold）", "Mouse4/Mouse5 目前仅支持 record_mode=hold", "Pause/Break 不支持按住录音模式", "Record and exit hotkeys must differ":
            return true
        default:
            if message.hasPrefix("未知修饰键 ") || message.hasSuffix("不能与另一语音功能或退出快捷键相同") || message.hasPrefix("mouse button ") {
                return true
            }
            let head = "选中文字超过 "
            let tail = " 字限制"
            guard message.hasPrefix(head), message.hasSuffix(tail) else { return false }
            let count = message.dropFirst(head.count).dropLast(tail.count)
            return !count.isEmpty && count.utf8.allSatisfy { $0 >= 48 && $0 <= 57 }
        }
    }
}
