package main

import meetingcore "voicerewrite/internal/core/meeting"

type meetingMetrics struct {
	Stage                                                        string
	Pending, Dropped                                             int
	ASRFirstMS, ASRDoneMS, TranslationFirstMS, TranslationDoneMS int64
}

type meetingView struct {
	status                             string
	captions                           []meetingcore.Caption
	reply                              meetingcore.ReplyState
	expressionStatus, expressionSource string
	expression                         meetingcore.Line
	sourceName, asrMode                string
	level                              int
	capturePaused                      bool
	metrics                            meetingMetrics
}

