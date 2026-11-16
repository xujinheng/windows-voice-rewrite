package ux

import "testing"

func TestResultCardTitles(t *testing.T) {
	for _, tc := range []struct{ intent, title string }{
		{"edit", "改写预览"},
		{"summarize", "AI 摘要"},
		{"translate", "翻译结果"},
		{"ambiguous", "AI 回答"},
		{"answer", "AI 解释"},
		{"", "AI 解释"},
	} {
		if got := ResultCardTitle(tc.intent); got != tc.title {
			t.Errorf("ResultCardTitle(%q)=%q want %q", tc.intent, got, tc.title)
		}
	}
	if HistoryTitle("edit") != "AI edit" || HistoryTitle("  ") != "AI 结果" {
		t.Fatal("history title")
	}
	if HistoryRestoreTitle("听写结果") != "听写结果 · 历史结果" {
		t.Fatal("restore title")
	}
	if DictationNotInserted != "听写结果 · 未自动插入" || DictationCopyManually != "听写结果 · 请手动复制" {
		t.Fatal("dictation card titles")
	}
	if CopyAction != "复制结果" || ApplyAction != "应用到原文" || CloseGlyph != "×" {
		t.Fatal("result actions")
	}
	if CopySucceeded != "已复制结果到剪贴板" || CopyFailed != "复制失败，请尝试选中正文复制" {
		t.Fatal("copy status")
	}
	if SaveWhileBusy != "请先完成当前录音／改写，再保存" {
		t.Fatal("save while busy")
	}
	if VoicePaused != "已暂停 · 鼠标侧键恢复正常功能" || PauseWhileBusy != "当前任务仍在处理；结束后可暂停语音操作。" {
		t.Fatal("pause")
	}
	if DictationTooShort != "语音太短，无法转写" || RewriteTooShort != "录音太短（至少约 0.1 秒）；请再试一次" {
		t.Fatal("short capture")
	}
	if PasswordDictation != "密码输入框不接受自动听写，请使用普通输入方式" ||
		PasswordSelection != "密码输入框不允许读取选中文字或发送给 AI" ||
		PasswordRewrite != "已拒绝在密码输入框中自动改写" {
		t.Fatal("password refusal")
	}
}
