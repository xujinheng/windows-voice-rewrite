import XCTest
@testable import VoiceRewriteInteraction

final class HostStatusTests: XCTestCase {
    func testCaptureStatusKeepsTheMacSentences() {
        XCTAssertEqual(
            captureStatus(InteractionFailure.secureField, password: "密码"),
            "密码")
        XCTAssertEqual(
            captureStatus(InteractionFailure.invalidSelection, password: "密码"),
            "听写需要空光标位置；如有选中文字，请先取消选择")
        XCTAssertEqual(
            captureStatus(InteractionFailure.systemError(7), password: "密码"),
            "读取目标文本框失败（Accessibility 错误 7）")
        XCTAssertTrue(
            captureStatus(InteractionFailure.cancelled, password: "密码")
                .hasPrefix("无法安全锁定外部光标（"))
        XCTAssertEqual(
            rewriteCaptureStatus(InteractionFailure.secureField, password: "选区"),
            "选区")
        XCTAssertTrue(rewriteCaptureStatus(InteractionFailure.notEditable, password: "选区").hasPrefix("无法开始语音选区处理："))
    }

    func testDictationRecordingSentencesStayMacOnly() {
        XCTAssertEqual(recordingIdle(), "未录音")
        XCTAssertEqual(dictationCaretLocked(), "已锁定原应用光标；识别后将验证并写入")
        XCTAssertTrue(dictationNoTarget().contains("未指定外部文本框"))
        XCTAssertEqual(dictationMicrophoneFailed("denied"), "无法启动麦克风：denied")
        XCTAssertEqual(dictationInserted(), "已安全插入原目标")
        XCTAssertNotEqual(rewriteNoTarget(), rewriteNoTargetForSelection())
        XCTAssertEqual(rewriteReplaced(), "原选区验证通过并已替换")
        XCTAssertEqual(rewriteTaskFailed("取消"), "文本任务失败或已取消：取消")
        XCTAssertEqual(rewritePreviewOnly(), "仅预览结果；未获模型路由修改授权")
        XCTAssertEqual(dictationRecognizeFailed("timeout"), "识别失败：timeout")
        XCTAssertNotEqual(helperProbeDisconnected(), helperNotConnectedProbe())
        XCTAssertNotEqual(helperHandshakeFailed(), helperHandshakeFailedStatus())
        XCTAssertNotEqual(probeTranscribeFailed(), probeTranscribeIncomplete())
        XCTAssertNotEqual(probeTaskFailed(), probeTaskIncomplete())
        XCTAssertNotEqual(probeTaskFailed(), probeTaskCanceled())
        XCTAssertEqual(probeRewriteListeningASR(), "ASR：正在识别语音修改指令…")
        XCTAssertNotEqual(settingsSaved(), "设置已保存。快捷键和录音设备将在重启 VoiceRewrite 后生效。")
        XCTAssertNotEqual(settingsSaved(), settingsSavedLog())
        XCTAssertEqual(settingsSaveFailed("io"), "无法安全保存设置：io")
        XCTAssertNotEqual(updateNotChecked(), updateChecking())
        XCTAssertEqual(updateChecking(), "正在检查更新")
        XCTAssertNotEqual(updateChecking(), "检查更新…")
    }

    func testShortcutInstallReusesTheSameKeyboard() {
        XCTAssertEqual(shortcutInstall(validPlan: false, sameKeyboard: true), .rejected)
        XCTAssertEqual(shortcutInstall(validPlan: true, sameKeyboard: true), .reuse)
        XCTAssertEqual(shortcutInstall(validPlan: true, sameKeyboard: false), .replace)
        XCTAssertTrue(shortcutRestoreKeepsKeyboard(hasPrevious: true, sameKeyboard: true))
        XCTAssertFalse(shortcutRestoreKeepsKeyboard(hasPrevious: false, sameKeyboard: true))
        XCTAssertEqual(shortcutPlanLabel(dictation: "Option+R", textEdit: "Option+T", rewrite: "Option+E"), "Option+R 听写；Option+T 文本指令；Option+E 语音修改选区")
        XCTAssertEqual(shortcutSaveRejected(), "快捷键冲突或不受 macOS 支持；设置未保存")
        XCTAssertEqual(shortcutPlanRejected(), "快捷键冲突或不受 macOS 支持；原快捷键保持有效")
    }

    func testSideMouseNoticesKeepButtonNumbers() {
        XCTAssertEqual(sideMouseUnchecked(), "尚未检查鼠标侧键监听")
        XCTAssertEqual(sideMouseObserved(button: 3, action: .dictation), "已收到 Mouse4 按下；继续按住以开始听写")
        XCTAssertEqual(sideMouseObserved(button: 4, action: .rewrite), "已收到 Mouse5 按下；继续按住以开始改写")
        XCTAssertEqual(sideMouseObserved(button: 2, action: nil), "检测到辅助鼠标按钮 2；当前绑定按钮 3/4")
        XCTAssertEqual(sideMouseHoldStarted(button: nil), "已通过配置的快捷键触发")
        XCTAssertEqual(sideMouseHoldStarted(button: 3), "Mouse4 长按已触发")
        XCTAssertEqual(sideMouseStopped(), "鼠标侧键监听已停止")
    }

    func testServiceProbeKeepsAMatchingMessage() {
        XCTAssertEqual(serviceProbeRunning(name: "ASR", running: "正在检测"), "ASR：正在检测")
        XCTAssertEqual(serviceProbeStatus(name: "ASR", ok: true, kind: "ASR", message: "可用", failure: "x"), "可用")
        XCTAssertEqual(serviceProbeStatus(name: "ASR", ok: true, kind: "LLM", message: "可用", failure: "invalid_reply"), "ASR：检测失败（invalid_reply）")
        XCTAssertEqual(serviceProbeStatus(name: "LLM", ok: false, kind: "LLM", message: "可用", failure: "timeout"), "LLM：检测失败（timeout）")
        XCTAssertEqual(serviceProbeConfigStatus(name: "LLM", detail: "缺密钥"), "LLM：无法读取服务配置（缺密钥）")
    }

    func testTranscriptAndModelRepliesKeepEmptyAndPreviewRules() {
        XCTAssertEqual(transcriptReply(ok: false, text: "a"), .failed)
        XCTAssertEqual(transcriptReply(ok: true, text: nil), .failed)
        XCTAssertEqual(transcriptReply(ok: true, text: "  \n"), .empty)
        XCTAssertEqual(transcriptReply(ok: true, text: "你好"), .ready("你好"))
        XCTAssertEqual(modelReply(ok: false, text: "a", spoken: false, instruction: nil, mayWrite: true, previewOnly: false), .rejected)
        XCTAssertEqual(modelReply(ok: true, text: "a", spoken: true, instruction: "", mayWrite: true, previewOnly: false), .missingSpokenInstruction)
        XCTAssertEqual(modelReply(ok: true, text: "a", spoken: true, instruction: " ", mayWrite: false, previewOnly: true), .preview("a"))
        XCTAssertEqual(modelReply(ok: true, text: "a", spoken: false, instruction: nil, mayWrite: true, previewOnly: false), .write("a"))
    }

    func testDictationFollowUpInsertsOnlyWhenTheCaretIsHeld() {
        let failed = dictationTranscriptFollowUp(ok: false, text: "x", transport: nil, hasInsertion: true, failureStatus: "ASR失败", emptyStatus: "空", historyTitle: "历史", notInsertedTitle: "未插入")
        XCTAssertEqual(failed.phase, .error)
        XCTAssertEqual(failed.recordingStatus, "ASR失败")
        XCTAssertNil(failed.cardTitle)
        XCTAssertNil(failed.insertText)
        XCTAssertFalse(failed.cancelTask)

        let empty = dictationTranscriptFollowUp(ok: true, text: "  ", transport: nil, hasInsertion: true, failureStatus: "ASR失败", emptyStatus: "空", historyTitle: "历史", notInsertedTitle: "未插入")
        XCTAssertEqual(empty.recordingStatus, "空")
        XCTAssertEqual(empty.phase, .error)
        XCTAssertNil(empty.insertText)

        let held = dictationTranscriptFollowUp(ok: true, text: "你好", transport: nil, hasInsertion: true, failureStatus: "ASR失败", emptyStatus: "空", historyTitle: "历史", notInsertedTitle: "未插入")
        XCTAssertEqual(held.phase, .applying)
        XCTAssertEqual(held.insertText, "你好")
        XCTAssertEqual(held.history, [TaskHistory(title: "历史", value: "你好")])
        XCTAssertNil(held.cardText)
        XCTAssertEqual(held.recordingStatus, dictationRecognized())

        let loose = dictationTranscriptFollowUp(ok: true, text: "你好", transport: nil, hasInsertion: false, failureStatus: "ASR失败", emptyStatus: "空", historyTitle: "历史", notInsertedTitle: "未插入")
        XCTAssertEqual(loose.phase, .fallback)
        XCTAssertEqual(loose.cardTitle, "未插入")
        XCTAssertNil(loose.insertText)

        let dropped = dictationTranscriptFollowUp(ok: true, text: "你好", transport: "断开", hasInsertion: true, failureStatus: "ASR失败", emptyStatus: "空", historyTitle: "历史", notInsertedTitle: "未插入")
        XCTAssertEqual(dropped.recordingStatus, dictationRecognizeFailed("断开"))
        XCTAssertEqual(dropped.phase, .error)
        XCTAssertFalse(dropped.cancelTask)

        let inserted = dictationInsertFollowUp(succeeded: true, status: "失败", manualTitle: "请复制", text: "你好")
        XCTAssertEqual(inserted.phase, .success)
        XCTAssertEqual(inserted.interactionStatus, dictationInserted())
        XCTAssertNil(inserted.cardTitle)

        let manual = dictationInsertFollowUp(succeeded: false, status: "失败", manualTitle: "请复制", text: "你好")
        XCTAssertEqual(manual.phase, .fallback)
        XCTAssertEqual(manual.cardTitle, "请复制")
        XCTAssertEqual(manual.cardText, "你好")
    }

    func testSelectedTaskFollowUpKeepsPreviewAndWriteApart() {
        let rejected = selectedTaskFollowUp(ok: false, text: "a", spoken: true, instruction: "说", mayWrite: true, previewOnly: false, transport: nil, failureStatus: "模型失败", historyTitle: "标题", cardTitle: "卡片", spokenHistoryTitle: "语音")
        XCTAssertEqual(rejected.interactionStatus, "模型失败")
        XCTAssertEqual(rejected.recordingStatus, rewriteVoiceIncomplete())
        XCTAssertEqual(rejected.asrProbe, probeVoiceTaskFailed())
        XCTAssertEqual(rejected.llmProbe, probeTaskFailed())
        XCTAssertEqual(rejected.phase, .error)
        XCTAssertTrue(rejected.cancelTask)
        XCTAssertNil(rejected.cardText)

        let quiet = selectedTaskFollowUp(ok: false, text: nil, spoken: false, instruction: nil, mayWrite: false, previewOnly: true, transport: nil, failureStatus: "模型失败", historyTitle: "标题", cardTitle: "卡片", spokenHistoryTitle: "语音")
        XCTAssertNil(quiet.recordingStatus)
        XCTAssertNil(quiet.asrProbe)

        let missing = selectedTaskFollowUp(ok: true, text: "a", spoken: true, instruction: "", mayWrite: true, previewOnly: false, transport: nil, failureStatus: "模型失败", historyTitle: "标题", cardTitle: "卡片", spokenHistoryTitle: "语音")
        XCTAssertEqual(missing.interactionStatus, rewriteInstructionMissing())
        XCTAssertNil(missing.llmProbe)
        XCTAssertTrue(missing.cancelTask)

        let preview = selectedTaskFollowUp(ok: true, text: "结果", spoken: true, instruction: "说清楚", mayWrite: true, previewOnly: true, transport: nil, failureStatus: "模型失败", historyTitle: "标题", cardTitle: "卡片", spokenHistoryTitle: "语音")
        XCTAssertEqual(preview.phase, .answer)
        XCTAssertEqual(preview.interactionStatus, rewritePreviewOnly())
        XCTAssertEqual(preview.transcription, "说清楚")
        XCTAssertEqual(preview.processedText, "结果")
        XCTAssertEqual(preview.history, [TaskHistory(title: "语音", value: "说清楚"), TaskHistory(title: "标题", value: "结果")])
        XCTAssertEqual(preview.cardTitle, "卡片")
        XCTAssertEqual(preview.cardText, "结果")
        XCTAssertNil(preview.replaceText)
        XCTAssertTrue(preview.cancelTask)
        XCTAssertEqual(preview.asrProbe, probeVoiceInstructionReady())
        XCTAssertEqual(preview.llmProbe, probeTaskSucceeded())

        let write = selectedTaskFollowUp(ok: true, text: "结果", spoken: false, instruction: nil, mayWrite: true, previewOnly: false, transport: nil, failureStatus: "模型失败", historyTitle: "标题", cardTitle: "卡片", spokenHistoryTitle: "语音")
        XCTAssertEqual(write.phase, .applying)
        XCTAssertEqual(write.replaceText, "结果")
        XCTAssertNil(write.cardText)
        XCTAssertNil(write.interactionStatus)
        XCTAssertNil(write.recordingStatus)
        XCTAssertFalse(write.cancelTask)
        XCTAssertEqual(write.history, [TaskHistory(title: "标题", value: "结果")])

        let spokenWrite = selectedTaskFollowUp(ok: true, text: "结果", spoken: true, instruction: "说清楚", mayWrite: true, previewOnly: false, transport: nil, failureStatus: "模型失败", historyTitle: "标题", cardTitle: "卡片", spokenHistoryTitle: "语音")
        XCTAssertEqual(spokenWrite.phase, .applying)
        XCTAssertEqual(spokenWrite.replaceText, "结果")
        XCTAssertEqual(spokenWrite.transcription, "说清楚")
        XCTAssertNil(spokenWrite.cardText)
        XCTAssertFalse(spokenWrite.cancelTask)

        let replaced = selectedWriteFollowUp(succeeded: true, status: "没写上", cardTitle: "卡片", text: "结果")
        XCTAssertEqual(replaced.phase, .success)
        XCTAssertEqual(replaced.interactionStatus, rewriteReplaced())
        XCTAssertNil(replaced.cardTitle)

        let kept = selectedWriteFollowUp(succeeded: false, status: "没写上", cardTitle: "卡片", text: "结果")
        XCTAssertEqual(kept.phase, .fallback)
        XCTAssertEqual(kept.cardTitle, "卡片")
        XCTAssertEqual(kept.cardText, "结果")
        XCTAssertFalse(kept.cancelTask)

        let dropped = selectedTaskFollowUp(ok: true, text: "结果", spoken: true, instruction: "说", mayWrite: true, previewOnly: false, transport: "断开", failureStatus: "模型失败", historyTitle: "标题", cardTitle: "卡片", spokenHistoryTitle: "语音")
        XCTAssertEqual(dropped.recordingStatus, rewriteVoiceFailed())
        XCTAssertEqual(dropped.interactionStatus, rewriteTaskFailed("断开"))
        XCTAssertEqual(dropped.llmProbe, probeTaskCanceled())
        XCTAssertTrue(dropped.cancelTask)
        XCTAssertNil(dropped.cardText)
    }

    func testRecordingStopFollowUpCancelsOnlyAVoiceSelection() {
        let voice = recordingStopFollowUp(detail: "没录上", voiceSelection: true, mode: "改写")
        XCTAssertEqual(voice.recordingStatus, dictationCannotRecognize("没录上"))
        XCTAssertEqual(voice.interactionStatus, rewriteVoiceTaskNotStarted())
        XCTAssertEqual(voice.asrProbe, probeTranscribeIncomplete())
        XCTAssertEqual(voice.phase, .error)
        XCTAssertEqual(voice.mode, "改写")
        XCTAssertTrue(voice.cancelTask)

        let dictation = recordingStopFollowUp(detail: "没录上", voiceSelection: false, mode: "听写")
        XCTAssertNil(dictation.interactionStatus)
        XCTAssertFalse(dictation.cancelTask)
    }

    func testHelperConnectionKeepsTheHandshakeSentencesApart() {
        let missing = helperConnectionEffect(.bundleMissing)
        XCTAssertEqual(missing.failedDetail, helperBundleMissing())
        XCTAssertNil(missing.serviceStatus)
        XCTAssertFalse(missing.cancelProbes)

        let started = helperConnectionEffect(.startFailed)
        XCTAssertEqual(started.failedDetail, helperStartFailed())
        XCTAssertEqual(started.serviceStatus, helperUnavailable())
        XCTAssertNil(started.probeStatus)

        let dropped = helperConnectionEffect(.disconnected("管道关闭"))
        XCTAssertEqual(dropped.failedDetail, "管道关闭")
        XCTAssertEqual(dropped.serviceStatus, helperDisconnected())
        XCTAssertEqual(dropped.probeStatus, helperProbeDisconnected())
        XCTAssertNotEqual(dropped.probeStatus, helperNotConnectedProbe())
        XCTAssertTrue(dropped.cancelProbes)
        XCTAssertTrue(dropped.invalidatePending)

        let rejected = helperConnectionEffect(.protocolRejected)
        XCTAssertEqual(rejected.failedDetail, helperProtocolRejected())
        XCTAssertEqual(rejected.serviceStatus, helperHandshakeFailedStatus())
        XCTAssertNotEqual(rejected.serviceStatus, helperHandshakeFailed())

        let connected = helperConnectionEffect(.connected)
        XCTAssertEqual(connected.connectedLabel, helperConnectedLabel())
        XCTAssertEqual(connected.serviceStatus, helperConnected())
        XCTAssertTrue(connected.checkServices)
        XCTAssertTrue(connected.refreshShortcuts)
        XCTAssertTrue(connected.logServiceStatus)
        XCTAssertNil(connected.failedDetail)

        let failed = helperConnectionEffect(.handshakeFailed)
        XCTAssertEqual(failed.failedDetail, helperHandshakeFailed())
        XCTAssertEqual(failed.serviceStatus, helperUnavailable())
        XCTAssertFalse(failed.checkServices)
    }

    func testUpdateFollowUpOffersAPageOnlyForANewerRelease() {
        let newer = updateFollowUp(ok: true, summary: "有新版本", newer: true, page: "https://example", errorText: "失败", transport: nil)
        XCTAssertEqual(newer.status, "有新版本")
        XCTAssertEqual(newer.page, "https://example")

        let current = updateFollowUp(ok: true, summary: "", newer: false, page: "https://example", errorText: nil, transport: nil)
        XCTAssertEqual(current.status, "")
        XCTAssertNil(current.page)

        let missing = updateFollowUp(ok: true, summary: nil, newer: false, page: nil, errorText: nil, transport: nil)
        XCTAssertEqual(missing.status, updateMissingSummary())

        let rejected = updateFollowUp(ok: false, summary: "有新版本", newer: true, page: "https://example", errorText: nil, transport: nil)
        XCTAssertEqual(rejected.status, updateFailed())
        XCTAssertNil(rejected.page)

        let dropped = updateFollowUp(ok: true, summary: "有新版本", newer: true, page: "https://example", errorText: "失败", transport: "断开")
        XCTAssertEqual(dropped.status, updateFailedDetail("断开"))
        XCTAssertNil(dropped.page)
    }

    func testInsertionStatusKeepsPasswordAndClipboardSentences() {
        XCTAssertEqual(
            insertionStatus(InteractionFailure.secureField, rewrite: true, passwordRewrite: "改写", passwordDictation: "听写"),
            "改写")
        XCTAssertEqual(
            insertionStatus(InteractionFailure.secureField, rewrite: false, passwordRewrite: "改写", passwordDictation: "听写"),
            "听写")
        XCTAssertEqual(
            insertionStatus(InteractionFailure.writeNotConfirmed, rewrite: true, passwordRewrite: "改写", passwordDictation: "听写"),
            "已尝试写入，但无法确认是否生效；请检查目标或复制结果")
        XCTAssertEqual(
            insertionStatus(ClipboardFailure.restoreFailed, rewrite: false, passwordRewrite: "改写", passwordDictation: "听写"),
            "安全剪贴板操作失败；写入状态可能不确定，请检查目标并复制结果")
    }
}
