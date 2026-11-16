package meeting

import (
	"strings"
	"testing"
)

func TestButtonFaceKeepsIdleTitles(t *testing.T) {
	want := []string{"冻结画面", "专注字幕", "A−", "A+", "复制回复", "复制英文", "生成建议", "暂停采集", "会议资料", "1 直接回应", "2 澄清一下", "3 争取时间", "无新建议", "改写回复", "回到最新", "放大英文"}
	idle := ButtonView{SelectedReply: -1}
	for i, title := range want {
		if got := ButtonFace(ButtonPause+i, idle); got != title {
			t.Fatalf("button %d face %q", i, got)
		}
	}
	if ButtonFace(ButtonSaveProfile, idle) != "" {
		t.Fatal("profile buttons keep their own window text")
	}
	on := ButtonView{Paused: true, CapturePaused: true, CaptionsOnly: true, ExpressionExpanded: true, Pending: true, SelectedReply: 1}
	if ButtonFace(ButtonPause, on) != "恢复画面" || ButtonFace(ButtonToggleCapture, on) != "恢复采集" {
		t.Fatal("pause and capture")
	}
	if ButtonFace(ButtonCaptions, on) != "完整视图" || ButtonFace(ButtonExpression, on) != "收起英文" {
		t.Fatal("captions and expression")
	}
	if ButtonFace(ButtonAcceptReplies, on) != "查看新建议" || ButtonFace(ButtonReplySecond, on) != "● 2 澄清一下" {
		t.Fatal("pending reply")
	}
	if ButtonFace(ButtonReplyFirst, on) != "1 直接回应" {
		t.Fatal("unselected reply stays plain")
	}
}

func TestStatusTextFollowsPauseAndSource(t *testing.T) {
	if got := StatusText("  Zoom.exe   会议  ", "", "", 0, false, false); got != "正在聆听\r\nZoom.exe 会议" {
		t.Fatalf("idle %q", got)
	}
	long := strings.Repeat("字", 35)
	if got := StatusText(long, "识别中", "聆听", 2, false, false); got != "识别中\r\n"+strings.Repeat("字", 33)+"…  ·  聆听  ·  跳过 2 个过期片段" {
		t.Fatalf("detail %q", got)
	}
	if got := StatusText("", "识别中", "", 0, true, true); got != "采集已暂停 · 不再接收音频\r\n等待音频源" {
		t.Fatalf("capture wins %q", got)
	}
	if got := StatusText("源", "识别中", "", 0, false, true); got != "画面已冻结 · 后台继续处理\r\n源" {
		t.Fatalf("frozen %q", got)
	}
}

func TestHistoryReplyAndExpressionCopy(t *testing.T) {
	if HistoryHeading(4, 0) != "之前的对话 · 4 段" || HistoryHeading(4, 2) != "之前的对话 · 4 段 · 2 条新内容" {
		t.Fatal("history heading")
	}
	if ReplyBody("en", "zh", "", true) != "en\r\nzh" {
		t.Fatal("complete reply")
	}
	if ReplyBody("en", "zh", "注意", false) != "en\r\nzh\r\n正在生成…（完成后可复制或改写）\r\n提示 · 注意" {
		t.Fatal("streaming reply")
	}
	if ReplyBody("", "zh", "", false) != "听到提问后生成建议；新建议可点击“查看新建议”后阅读。" {
		t.Fatal("empty reply")
	}
	if ExpressionShown("") == ExpressionShown("We can do that.") || !strings.Contains(ExpressionShown(""), "听写键") {
		t.Fatal("expression placeholder")
	}
	if LiveCaption(WaitingHint) != "● 等待发言" {
		t.Fatal("live caption")
	}
}
