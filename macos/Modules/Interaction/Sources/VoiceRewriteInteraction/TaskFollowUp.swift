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
    /// Go authorizes a reviewable typed edit; native host checks the original selection.
    public var manualReview: Bool

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
        replaceText: String? = nil,
        manualReview: Bool = false
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
        self.manualReview = manualReview
    }
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
