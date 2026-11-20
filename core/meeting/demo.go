package meeting

import "github.com/xujinheng/windows-voice-rewrite/voice-rewrite/config"

// Demo is the meeting sample shown when no audio source is connected.
type Demo struct {
	Status, SourceName, ASRMode, Stage string
	Level                              int
	ASRFirstMS, ASRDoneMS              int64
	TranslationFirstMS                 int64
	TranslationDoneMS                  int64
	Captions                           []Caption
	Reply                              ReplyState
	Expression                         Line
}

func Sample() Demo {
	return Demo{
		Status: "演示数据 · 试用界面无需音频或模型", SourceName: "Zoom.exe · 演示会议",
		ASRMode: config.MeetingASRMultipart, Level: 36, Stage: "聆听",
		ASRFirstMS: 340, ASRDoneMS: 720, TranslationFirstMS: 120, TranslationDoneMS: 310,
		Captions: []Caption{
			{Final: true, Source: "The impact seems to be limited to peak hours.", Chinese: "影响似乎仅限于高峰时段。"},
			{Final: true, Source: "We're seeing high tail latency on vector search. How would you start investigating?", Chinese: "向量检索的尾延迟偏高。你会从哪里开始排查？"},
		},
		Reply: ReplyState{Displayed: ReplyBatch{
			Lines: [3]Line{
				{Kind: "ONE", Complete: true, English: "I'd start by separating time spent waiting in the queue from time spent running the search.", Chinese: "我会先把排队耗时和实际检索耗时分开看。"},
				{Kind: "TWO", Complete: true, English: "Is the spike limited to certain queries, or does it affect the whole cluster?", Chinese: "是部分查询出现尖峰，还是整个集群都受影响？"},
				{Kind: "THREE", Complete: true, English: "Let me take a moment to think through where we'd get the clearest signal.", Chinese: "我先理一下从哪里入手能最快弄清情况。"},
			},
			Source: "We're seeing high tail latency on vector search. How would you start investigating?",
			Tip:    "先对照高峰期的 p99 排队时间与分片耗时，再核对是否集中在特定索引或查询类型。",
		}},
		Expression: Line{Kind: "SAY", Complete: true, English: "We can do that, but by Friday I can only share an initial version.", Chinese: "可以做，但周五前我只能给初版。"},
	}
}
