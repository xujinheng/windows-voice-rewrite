package main

import meetingcore "voicerewrite/internal/core/meeting"

// meetingDemoSnapshot is deterministic and never creates a Meeting session or
// accesses capture devices, credentials, ASR endpoints or LLM endpoints.
func meetingDemoSnapshot() map[string]any {
	d := meetingcore.Sample()
	captions := make([]map[string]any, 0, len(d.Captions))
	for i, c := range d.Captions {
		captions = append(captions, map[string]any{
			"id": i + 1, "source": c.Source, "chinese": c.Chinese, "final": c.Final,
		})
	}
	replies := make([]map[string]any, 0, 3)
	for _, line := range d.Reply.Displayed.Lines {
		replies = append(replies, map[string]any{
			"english": line.English, "chinese": line.Chinese, "complete": line.Complete,
		})
	}
	return map[string]any{
		"demo": true, "session_id": "", "capture_paused": false, "epoch": 0,
		"profile_version": uint64(0), "asr_mode": d.ASRMode,
		"source_name": d.SourceName, "status": d.Status,
		"level": d.Level, "captions": captions, "replies": replies,
		"reply_source": d.Reply.Displayed.Source,
		"reply_tip": d.Reply.Displayed.Tip, "reply_status": "",
		"reply_pending": false, "reply_first_ready": meetingcore.FirstReadyReply(d.Reply.Displayed.Lines),
		"captions_only": false, "expression_expanded": false,
		"expression": d.Expression.English, "expression_chinese": d.Expression.Chinese,
		"expression_status": "",
		"metrics": map[string]any{
			"pending": 0, "dropped": 0,
			"asr_first_ms": d.ASRFirstMS, "asr_done_ms": d.ASRDoneMS,
			"translation_first_ms": d.TranslationFirstMS,
			"translation_done_ms": d.TranslationDoneMS,
		},
	}
}
