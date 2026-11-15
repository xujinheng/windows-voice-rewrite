package main

import (
	"strings"

	meetingcore "voicerewrite/internal/core/meeting"
)

// meetingLine is the native HUD view record; the core model's Line is its
// protocol result and is converted at the session boundary.
type meetingLine struct {
	kind, english, chinese string
	complete               bool
}

type meetingMetrics struct {
	Stage                                                        string
	Pending, Dropped                                             int
	ASRFirstMS, ASRDoneMS, TranslationFirstMS, TranslationDoneMS int64
}

type meetingView struct {
	status                                               string
	captions                                             []meetingcore.Caption
	replies                                              [3]meetingLine
	tip, replySource, expressionStatus, expressionSource string
	expression                                           meetingLine
	sourceName, asrMode                                  string
	level                                                int
	capturePaused                                        bool
	metrics                                              meetingMetrics
	pendingReplies                                       [3]meetingLine
	pendingReplySource, pendingTip                       string
	replyPending                                         bool
}

// A finished voice request must be visible even if the user is listening in
// captions-only mode. A background update never steals the user's view.
// Keep progress/error details subordinate to the last copyable English.
func meetingRetainEnglish(previous meetingLine) meetingLine {
	if previous.complete && strings.TrimSpace(previous.english) != "" {
		return previous
	}
	return meetingLine{}
}
