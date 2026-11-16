import Foundation

public struct UpdateFollowUp: Equatable, Sendable {
    public var status: String
    public var page: String?
    public init(status: String, page: String?) {
        self.status = status
        self.page = page
    }
}

/// An update reply chooses the status line and whether a release page is offered.
/// A missing summary is replaced. An empty summary stays empty. A transport failure wins.
public func updateFollowUp(ok: Bool, summary: String?, newer: Bool, page: String?, errorText: String?, transport: String?) -> UpdateFollowUp {
    if let transport {
        return UpdateFollowUp(status: updateFailedDetail(transport), page: nil)
    }
    if !ok {
        return UpdateFollowUp(status: errorText ?? updateFailed(), page: nil)
    }
    if newer {
        return UpdateFollowUp(status: summary ?? updateMissingSummary(), page: page)
    }
    return UpdateFollowUp(status: summary ?? updateMissingSummary(), page: nil)
}

public struct TaskHistory: Equatable, Sendable {
    public var title: String
    public var value: String
    public init(title: String, value: String) {
        self.title = title
        self.value = value
    }
}

/// The statuses, pill phase, history, and card a finished voice task shows.
/// The host applies it. A successful insert or replacement does not open a card.
public struct TaskFollowUp: Equatable, Sendable {
    public enum Phase: Int, Equatable, Sendable {
        case success = 3
        case error = 4
        case applying = 6
        case answer = 7
        case fallback = 9
    }

    public var recordingStatus: String?
    public var interactionStatus: String?
    public var asrProbe: String?
    public var llmProbe: String?
    public var phase: Phase?
    public var mode: String
    public var transcription: String?
    public var processedText: String?
    public var history: [TaskHistory]
    public var cardTitle: String?
    public var cardText: String?
    public var cancelTask: Bool
    public var insertText: String?
    public var replaceText: String?

    public init(
        recordingStatus: String? = nil,
        interactionStatus: String? = nil,
        asrProbe: String? = nil,
        llmProbe: String? = nil,
        phase: Phase? = nil,
        mode: String = "",
        transcription: String? = nil,
        processedText: String? = nil,
        history: [TaskHistory] = [],
        cardTitle: String? = nil,
        cardText: String? = nil,
        cancelTask: Bool = false,
        insertText: String? = nil,
        replaceText: String? = nil
    ) {
        self.recordingStatus = recordingStatus
        self.interactionStatus = interactionStatus
        self.asrProbe = asrProbe
        self.llmProbe = llmProbe
        self.phase = phase
        self.mode = mode
        self.transcription = transcription
        self.processedText = processedText
        self.history = history
        self.cardTitle = cardTitle
        self.cardText = cardText
        self.cancelTask = cancelTask
        self.insertText = insertText
        self.replaceText = replaceText
    }
}

public func dictationTranscriptFollowUp(
    ok: Bool,
    text: String?,
    transport: String?,
    hasInsertion: Bool,
    failureStatus: String,
    emptyStatus: String,
    historyTitle: String,
    notInsertedTitle: String
) -> TaskFollowUp {
    if let transport {
        return TaskFollowUp(
            recordingStatus: dictationRecognizeFailed(transport),
            asrProbe: probeTranscribeFailed(),
            phase: .error,
            mode: "听写")
    }
    switch transcriptReply(ok: ok, text: text) {
    case .failed:
        return TaskFollowUp(recordingStatus: failureStatus, asrProbe: probeTranscribeFailed(), phase: .error, mode: "听写")
    case .empty:
        return TaskFollowUp(recordingStatus: emptyStatus, asrProbe: probeTranscribeFailed(), phase: .error, mode: "听写")
    case .ready(let recognized):
        if hasInsertion {
            return TaskFollowUp(
                recordingStatus: dictationRecognized(),
                asrProbe: probeTranscribeSucceeded(),
                phase: .applying,
                mode: "听写",
                transcription: recognized,
                history: [TaskHistory(title: historyTitle, value: recognized)],
                insertText: recognized)
        }
        return TaskFollowUp(
            recordingStatus: dictationRecognized(),
            asrProbe: probeTranscribeSucceeded(),
            phase: .fallback,
            mode: "听写",
            transcription: recognized,
            history: [TaskHistory(title: historyTitle, value: recognized)],
            cardTitle: notInsertedTitle,
            cardText: recognized)
    }
}

public func dictationInsertFollowUp(succeeded: Bool, status: String, manualTitle: String, text: String) -> TaskFollowUp {
    if succeeded {
        return TaskFollowUp(interactionStatus: dictationInserted(), phase: .success, mode: "听写")
    }
    return TaskFollowUp(interactionStatus: status, phase: .fallback, mode: "听写", cardTitle: manualTitle, cardText: text)
}

public func recordingStopFollowUp(detail: String, voiceSelection: Bool, mode: String) -> TaskFollowUp {
    TaskFollowUp(
        recordingStatus: dictationCannotRecognize(detail),
        interactionStatus: voiceSelection ? rewriteVoiceTaskNotStarted() : nil,
        asrProbe: probeTranscribeIncomplete(),
        phase: .error,
        mode: mode,
        cancelTask: voiceSelection)
}

public func selectedTaskFollowUp(
    ok: Bool,
    text: String?,
    spoken: Bool,
    instruction: String?,
    mayWrite: Bool,
    previewOnly: Bool,
    transport: String?,
    failureStatus: String,
    historyTitle: String,
    cardTitle: String,
    spokenHistoryTitle: String
) -> TaskFollowUp {
    if let transport {
        return TaskFollowUp(
            recordingStatus: spoken ? rewriteVoiceFailed() : nil,
            interactionStatus: rewriteTaskFailed(transport),
            llmProbe: probeTaskCanceled(),
            phase: .error,
            mode: "改写",
            cancelTask: true)
    }
    switch modelReply(ok: ok, text: text, spoken: spoken, instruction: instruction, mayWrite: mayWrite, previewOnly: previewOnly) {
    case .rejected:
        return TaskFollowUp(
            recordingStatus: spoken ? rewriteVoiceIncomplete() : nil,
            interactionStatus: failureStatus,
            asrProbe: spoken ? probeVoiceTaskFailed() : nil,
            llmProbe: probeTaskFailed(),
            phase: .error,
            mode: "改写",
            cancelTask: true)
    case .missingSpokenInstruction:
        return TaskFollowUp(
            interactionStatus: rewriteInstructionMissing(),
            phase: .error,
            mode: "改写",
            cancelTask: true)
    case .preview(let body):
        return finishedSelection(body, spoken: spoken, instruction: instruction, intentHistory: historyTitle, cardTitle: cardTitle, spokenHistoryTitle: spokenHistoryTitle, write: false)
    case .write(let body):
        return finishedSelection(body, spoken: spoken, instruction: instruction, intentHistory: historyTitle, cardTitle: cardTitle, spokenHistoryTitle: spokenHistoryTitle, write: true)
    }
}

public func selectedWriteFollowUp(succeeded: Bool, status: String, cardTitle: String, text: String) -> TaskFollowUp {
    if succeeded {
        return TaskFollowUp(interactionStatus: rewriteReplaced(), phase: .success, mode: "改写")
    }
    return TaskFollowUp(interactionStatus: status, phase: .fallback, mode: "改写", cardTitle: cardTitle, cardText: text)
}

private func finishedSelection(
    _ body: String,
    spoken: Bool,
    instruction: String?,
    intentHistory: String,
    cardTitle: String,
    spokenHistoryTitle: String,
    write: Bool
) -> TaskFollowUp {
    var history: [TaskHistory] = []
    var transcription: String?
    var recording: String?
    var asr: String?
    if spoken, let instruction {
        transcription = instruction
        recording = rewriteVoiceHandled()
        asr = probeVoiceInstructionReady()
        history.append(TaskHistory(title: spokenHistoryTitle, value: instruction))
    }
    history.append(TaskHistory(title: intentHistory, value: body))
    if write {
        return TaskFollowUp(
            recordingStatus: recording,
            asrProbe: asr,
            llmProbe: probeTaskSucceeded(),
            phase: .applying,
            mode: "改写",
            transcription: transcription,
            processedText: body,
            history: history,
            cardTitle: cardTitle,
            replaceText: body)
    }
    return TaskFollowUp(
        recordingStatus: recording,
        interactionStatus: rewritePreviewOnly(),
        asrProbe: asr,
        llmProbe: probeTaskSucceeded(),
        phase: .answer,
        mode: "改写",
        transcription: transcription,
        processedText: body,
        history: history,
        cardTitle: cardTitle,
        cardText: body,
        cancelTask: true)
}
