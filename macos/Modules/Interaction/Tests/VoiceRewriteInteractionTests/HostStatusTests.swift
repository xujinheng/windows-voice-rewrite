import XCTest
@testable import VoiceRewriteInteraction

final class HostStatusTests: XCTestCase {
    func testInsertionFailureDistinguishesUncertainDeliveryFromChangedTarget() {
        let args = (rewrite: false, passwordRewrite: "密码改写", passwordDictation: "密码听写")
        let uncertain = insertionStatus(InteractionFailure.writeNotConfirmed,
                                        rewrite: args.rewrite, passwordRewrite: args.passwordRewrite,
                                        passwordDictation: args.passwordDictation)
        XCTAssertTrue(uncertain.contains("避免重复粘贴"))
        let changed = insertionStatus(InteractionFailure.selectionChanged,
                                      rewrite: args.rewrite, passwordRewrite: args.passwordRewrite,
                                      passwordDictation: args.passwordDictation)
        XCTAssertTrue(changed.contains("光标"))
        let focus = insertionStatus(InteractionFailure.focusChanged,
                                    rewrite: args.rewrite, passwordRewrite: args.passwordRewrite,
                                    passwordDictation: args.passwordDictation)
        XCTAssertTrue(focus.contains("焦点"))
    }

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

    func testDictationArmStillRecordsAfterACaptureFailure() {
        let missing = dictationArm(hasTarget: false, tokenValid: true, captured: false, captureFailure: nil)
        XCTAssertEqual(missing.interactionStatus, dictationNoTarget())
        XCTAssertFalse(missing.keepInsert)
        XCTAssertTrue(missing.startMicrophone)

        let failed = dictationArm(hasTarget: true, tokenValid: true, captured: false, captureFailure: "密码")
        XCTAssertEqual(failed.interactionStatus, "密码")
        XCTAssertTrue(failed.startMicrophone)
        XCTAssertFalse(failed.keepInsert)

        let locked = dictationArm(hasTarget: true, tokenValid: true, captured: true, captureFailure: nil)
        XCTAssertEqual(locked.interactionStatus, dictationCaretLocked())
        XCTAssertTrue(locked.keepInsert)
        XCTAssertTrue(locked.startMicrophone)

        let stale = dictationArm(hasTarget: true, tokenValid: false, captured: true, captureFailure: nil)
        XCTAssertNil(stale.interactionStatus)
        XCTAssertFalse(stale.keepInsert)
        XCTAssertFalse(stale.startMicrophone)

        let staleMiss = dictationArm(hasTarget: false, tokenValid: false, captured: false, captureFailure: nil)
        XCTAssertEqual(staleMiss.interactionStatus, dictationNoTarget())
        XCTAssertFalse(staleMiss.startMicrophone)

        let recording = dictationMicrophoneReady(started: true, tokenValid: true, failure: "")
        XCTAssertEqual(recording.recordingStatus, dictationRecording())
        XCTAssertTrue(recording.showRecording)
        XCTAssertFalse(recording.cancelMicrophone)

        let expired = dictationMicrophoneReady(started: true, tokenValid: false, failure: "")
        XCTAssertNil(expired.recordingStatus)
        XCTAssertTrue(expired.cancelMicrophone)
        XCTAssertFalse(expired.showRecording)

        let broken = dictationMicrophoneReady(started: false, tokenValid: true, failure: "忙")
        XCTAssertEqual(broken.recordingStatus, dictationMicrophoneFailed("忙"))
        XCTAssertTrue(broken.showError)
        XCTAssertFalse(broken.cancelMicrophone)
    }

    func testSpokenAndTypedSelectionCapturesStayDifferent() {
        let staleVoice = rewriteVoiceAfterCapture(tokenValid: false, tooLong: "太长")
        XCTAssertTrue(staleVoice.cancelTask)
        XCTAssertTrue(staleVoice.leave)
        XCTAssertFalse(staleVoice.startMicrophone)
        XCTAssertNil(staleVoice.interactionStatus)
        XCTAssertNil(staleVoice.recordingStatus)

        let longVoice = rewriteVoiceAfterCapture(tokenValid: true, tooLong: "太长")
        XCTAssertTrue(longVoice.cancelTask)
        XCTAssertFalse(longVoice.leave)
        XCTAssertFalse(longVoice.startMicrophone)
        XCTAssertEqual(longVoice.interactionStatus, "太长")
        XCTAssertEqual(longVoice.recordingStatus, recordingIdle())
        XCTAssertTrue(longVoice.showError)
        XCTAssertNil(longVoice.llmProbe)

        let readyVoice = rewriteVoiceAfterCapture(tokenValid: true, tooLong: nil)
        XCTAssertTrue(readyVoice.startMicrophone)
        XCTAssertFalse(readyVoice.cancelTask)
        XCTAssertFalse(readyVoice.leave)

        let expired = rewriteVoiceMicrophoneReady(tokenValid: false)
        XCTAssertTrue(expired.cancelMicrophone)
        XCTAssertTrue(expired.cancelTask)
        XCTAssertTrue(expired.leave)
        XCTAssertFalse(expired.beginRecording)
        XCTAssertNil(expired.recordingStatus)

        let recording = rewriteVoiceMicrophoneReady(tokenValid: true)
        XCTAssertTrue(recording.beginRecording)
        XCTAssertFalse(recording.cancelMicrophone)
        XCTAssertFalse(recording.leave)
        XCTAssertEqual(recording.recordingStatus, rewriteRecording())
        XCTAssertEqual(recording.interactionStatus, rewriteSelectionLocked())

        let hidden = rewriteVoiceCaptureFailed(tokenValid: false, status: "密码")
        XCTAssertTrue(hidden.cancelMicrophone)
        XCTAssertTrue(hidden.cancelTask)
        XCTAssertFalse(hidden.showError)
        XCTAssertNil(hidden.interactionStatus)
        XCTAssertFalse(hidden.leave)

        let shown = rewriteVoiceCaptureFailed(tokenValid: true, status: "密码")
        XCTAssertEqual(shown.interactionStatus, "密码")
        XCTAssertEqual(shown.recordingStatus, recordingIdle())
        XCTAssertTrue(shown.showError)
        XCTAssertTrue(shown.cancelMicrophone)
        XCTAssertTrue(shown.cancelTask)

        let staleTyped = typedSelectionAfterCapture(tokenValid: false, tooLong: "太长")
        XCTAssertTrue(staleTyped.cancelTask)
        XCTAssertTrue(staleTyped.leave)
        XCTAssertFalse(staleTyped.stopProcessing)
        XCTAssertFalse(staleTyped.sendTask)
        XCTAssertNil(staleTyped.interactionStatus)

        let longTyped = typedSelectionAfterCapture(tokenValid: true, tooLong: "太长")
        XCTAssertTrue(longTyped.cancelTask)
        XCTAssertTrue(longTyped.leave)
        XCTAssertTrue(longTyped.stopProcessing)
        XCTAssertFalse(longTyped.sendTask)
        XCTAssertEqual(longTyped.interactionStatus, "太长")
        XCTAssertEqual(longTyped.llmProbe, probeTaskIncomplete())
        XCTAssertNil(longTyped.recordingStatus)
        XCTAssertFalse(longTyped.startMicrophone)

        let send = typedSelectionAfterCapture(tokenValid: true, tooLong: nil)
        XCTAssertTrue(send.sendTask)
        XCTAssertFalse(send.cancelTask)
        XCTAssertFalse(send.leave)
        XCTAssertFalse(send.stopProcessing)

        let ignored = typedSelectionStartFailed(tokenValid: false, detail: "断开")
        XCTAssertFalse(ignored.cancelTask)
        XCTAssertFalse(ignored.cancelMicrophone)
        XCTAssertFalse(ignored.stopProcessing)
        XCTAssertNil(ignored.interactionStatus)

        let failed = typedSelectionStartFailed(tokenValid: true, detail: "断开")
        XCTAssertFalse(failed.cancelTask)
        XCTAssertFalse(failed.cancelMicrophone)
        XCTAssertTrue(failed.stopProcessing)
        XCTAssertEqual(failed.interactionStatus, rewriteTaskNotStarted("断开"))
        XCTAssertEqual(failed.llmProbe, probeTaskIncomplete())
        XCTAssertTrue(failed.showError)
    }

    func testSidePreviewStaysHiddenWhileBusy() {
        XCTAssertEqual(sidePreview(recording: true, starting: false, processing: false, dictation: true), .hidden)
        XCTAssertEqual(sidePreview(recording: false, starting: true, processing: false, dictation: false), .hidden)
        XCTAssertEqual(sidePreview(recording: false, starting: false, processing: true, dictation: true), .hidden)
        XCTAssertEqual(sidePreview(recording: false, starting: false, processing: false, dictation: true), .dictation)
        XCTAssertEqual(sidePreview(recording: false, starting: false, processing: false, dictation: false), .rewrite)
        XCTAssertEqual(previewDismiss(pending: true), PreviewDismiss(allowNextHold: true, dismissIndicator: true))
        XCTAssertEqual(previewDismiss(pending: false), PreviewDismiss(allowNextHold: true, dismissIndicator: false))
        let earlier = Date(timeIntervalSince1970: 10)
        let now = Date(timeIntervalSince1970: 20)
        let shown = indicatorTiming(wasVisible: false, wasPending: false, wasPhase: 1, pending: true, phase: 1, now: now, started: earlier, changed: earlier)
        XCTAssertEqual(shown.started, now)
        XCTAssertEqual(shown.changed, now)
        let same = indicatorTiming(wasVisible: true, wasPending: true, wasPhase: 1, pending: true, phase: 1, now: now, started: earlier, changed: earlier)
        XCTAssertEqual(same.started, earlier)
        XCTAssertEqual(same.changed, earlier)
        let phase = indicatorTiming(wasVisible: true, wasPending: true, wasPhase: 1, pending: true, phase: 2, now: now, started: earlier, changed: earlier)
        XCTAssertEqual(phase.started, earlier)
        XCTAssertEqual(phase.changed, now)
        let pending = indicatorTiming(wasVisible: true, wasPending: false, wasPhase: 1, pending: true, phase: 1, now: now, started: earlier, changed: earlier)
        XCTAssertEqual(pending.started, earlier)
        XCTAssertEqual(pending.changed, now)
    }

    func testShortcutRefreshStopsBeforeTheLastParse() {
        XCTAssertEqual(shortcutRefresh(tokenValid: false, parsed: true, remaining: 0), .stop)
        XCTAssertEqual(shortcutRefresh(tokenValid: true, parsed: false, remaining: 0), .stop)
        XCTAssertEqual(shortcutRefresh(tokenValid: true, parsed: true, remaining: 2), .parseNext)
        XCTAssertEqual(shortcutRefresh(tokenValid: true, parsed: true, remaining: 1), .parseNext)
        XCTAssertEqual(shortcutRefresh(tokenValid: true, parsed: true, remaining: 0), .register)
        XCTAssertFalse(shortcutRefreshStores(registered: false))
        XCTAssertTrue(shortcutRefreshStores(registered: true))
    }

    func testSettingsSaveRefusalKeepsBusyAheadOfTheCredential() {
        XCTAssertEqual(settingsSaveRefusal(busy: true, credentialTooLong: true, planValid: false), .busy)
        XCTAssertEqual(settingsSaveRefusal(busy: false, credentialTooLong: true, planValid: false), .credential)
        XCTAssertEqual(settingsSaveRefusal(busy: false, credentialTooLong: false, planValid: false), .shortcut)
        XCTAssertEqual(settingsSaveRefusal(busy: false, credentialTooLong: false, planValid: true), .ready)
    }

    func testVoicePauseAndFreshSideMouseUseDifferentSentences() {
        let busy = voicePauseTurn(busy: true, currentlyPaused: false, busyStatus: "忙", pausedStatus: "停", idleStatus: "待")
        XCTAssertFalse(busy.toggle)
        XCTAssertEqual(busy.interactionStatus, "忙")
        XCTAssertNil(busy.sideStatus)

        let pause = voicePauseTurn(busy: false, currentlyPaused: false, busyStatus: "忙", pausedStatus: "停", idleStatus: "待")
        XCTAssertTrue(pause.toggle)
        XCTAssertNil(pause.interactionStatus)
        XCTAssertEqual(pause.sideStatus, "停")

        let resume = voicePauseTurn(busy: false, currentlyPaused: true, busyStatus: "忙", pausedStatus: "停", idleStatus: "待")
        XCTAssertTrue(resume.toggle)
        XCTAssertEqual(resume.sideStatus, "待")
        XCTAssertNotEqual(resume.sideStatus, sideMouseListening())

        XCTAssertEqual(sideMouseStart(fresh: true, paused: true), .paused)
        XCTAssertEqual(sideMouseStart(fresh: true, paused: false), .listening)
        XCTAssertEqual(sideMouseStart(fresh: false, paused: false), .unchanged)
        XCTAssertEqual(sideMouseStart(fresh: false, paused: true), .unchanged)
        XCTAssertNotEqual(sideMouseListening(), "待")
    }

    func testRecordingDispatchKeepsSpokenAndDictationProbesApart() {
        let stale = recordingDispatch(tokenValid: false, voiceSelection: true)
        XCTAssertFalse(stale.send)
        XCTAssertFalse(stale.cancelProbes)
        XCTAssertNil(stale.asrProbe)
        XCTAssertNil(stale.llmProbe)

        let spoken = recordingDispatch(tokenValid: true, voiceSelection: true)
        XCTAssertTrue(spoken.send)
        XCTAssertTrue(spoken.spoken)
        XCTAssertTrue(spoken.cancelProbes)
        XCTAssertEqual(spoken.asrProbe, probeRewriteListeningASR())
        XCTAssertEqual(spoken.llmProbe, probeRewriteWaitingLLM())
        XCTAssertNotEqual(spoken.asrProbe, probeTranscribing())

        let dictation = recordingDispatch(tokenValid: true, voiceSelection: false)
        XCTAssertTrue(dictation.send)
        XCTAssertFalse(dictation.spoken)
        XCTAssertTrue(dictation.cancelProbes)
        XCTAssertEqual(dictation.asrProbe, probeTranscribing())
        XCTAssertNil(dictation.llmProbe)

        let hidden = recordingStopFailure(tokenValid: false)
        XCTAssertFalse(hidden.apply)
        XCTAssertFalse(hidden.stopProcessing)
        let shown = recordingStopFailure(tokenValid: true)
        XCTAssertTrue(shown.apply)
        XCTAssertTrue(shown.stopProcessing)
    }

    func testShortcutParseFailurePrefersTheTransportError() {
        XCTAssertEqual(shortcutParseFailure(transport: "断开", userText: "格式不对"), "断开")
        XCTAssertEqual(shortcutParseFailure(transport: nil, userText: "格式不对"), "格式不对")
        XCTAssertEqual(shortcutParseFailure(transport: nil, userText: ""), "")
        XCTAssertEqual(shortcutParseFailure(transport: nil, userText: nil), "无法解析")
    }

    func testHelperConnectionKeepsTheHandshakeSentencesApart() {
        let missing = helperConnectionEffect(.bundleMissing, helperName: "VoiceRewrite", helperVersion: 1)
        XCTAssertEqual(missing.failedDetail, helperBundleMissing())
        XCTAssertNil(missing.serviceStatus)
        XCTAssertFalse(missing.cancelProbes)

        let started = helperConnectionEffect(.startFailed, helperName: "VoiceRewrite", helperVersion: 1)
        XCTAssertEqual(started.failedDetail, helperStartFailed())
        XCTAssertEqual(started.serviceStatus, helperUnavailable())
        XCTAssertNil(started.probeStatus)

        let dropped = helperConnectionEffect(.disconnected("管道关闭"), helperName: "VoiceRewrite", helperVersion: 1)
        XCTAssertEqual(dropped.failedDetail, "管道关闭")
        XCTAssertEqual(dropped.serviceStatus, helperDisconnected())
        XCTAssertEqual(dropped.probeStatus, helperProbeDisconnected())
        XCTAssertNotEqual(dropped.probeStatus, helperNotConnectedProbe())
        XCTAssertTrue(dropped.cancelProbes)
        XCTAssertTrue(dropped.invalidatePending)

        let rejected = helperConnectionEffect(.protocolRejected, helperName: "VoiceRewrite", helperVersion: 1)
        XCTAssertEqual(rejected.failedDetail, helperProtocolRejected())
        XCTAssertEqual(rejected.serviceStatus, helperHandshakeFailedStatus())
        XCTAssertNotEqual(rejected.serviceStatus, helperHandshakeFailed())

        let connected = helperConnectionEffect(.connected, helperName: "VoiceRewrite", helperVersion: 1)
        XCTAssertEqual(connected.connectedLabel, helperConnectedLabel(name: "VoiceRewrite", version: 1))
        XCTAssertEqual(connected.serviceStatus, helperConnected())
        XCTAssertTrue(connected.checkServices)
        XCTAssertTrue(connected.refreshShortcuts)
        XCTAssertTrue(connected.logServiceStatus)
        XCTAssertNil(connected.failedDetail)

        let failed = helperConnectionEffect(.handshakeFailed, helperName: "VoiceRewrite", helperVersion: 1)
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
