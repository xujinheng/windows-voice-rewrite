package meeting

import "testing"

func TestMeetingButtonsStayContiguous(t *testing.T) {
	if ButtonExpression-ButtonPause != 15 {
		t.Fatalf("button span %d", ButtonExpression-ButtonPause)
	}
	if ButtonReplySecond != ButtonReplyFirst+1 || ButtonReplyThird != ButtonReplyFirst+2 {
		t.Fatal("reply buttons")
	}
	cases := []struct {
		id      int
		command Command
		index   int
	}{
		{ButtonPause, CommandTogglePause, 0},
		{ButtonCaptions, CommandCaptions, 0},
		{ButtonSmallerText, CommandSmallerText, 0},
		{ButtonLargerText, CommandLargerText, 0},
		{ButtonCopyReply, CommandCopyReply, 0},
		{ButtonCopyExpression, CommandCopyExpression, 0},
		{ButtonRequestReplies, CommandRequestReplies, 0},
		{ButtonToggleCapture, CommandToggleCapture, 0},
		{ButtonOpenProfile, CommandOpenProfile, 0},
		{ButtonReplyFirst, CommandSelectReply, 0},
		{ButtonReplyThird, CommandSelectReply, 2},
		{ButtonAcceptReplies, CommandAcceptReplies, 0},
		{ButtonRefine, CommandRefine, 0},
		{ButtonScrollLatest, CommandScrollLatest, 0},
		{ButtonExpression, CommandToggleExpression, 0},
		{ButtonSaveProfile, CommandSaveProfile, 0},
		{ButtonCloseProfile, CommandCloseProfile, 0},
		{7000, CommandNone, 0},
	}
	for _, item := range cases {
		command, index := CommandFor(item.id)
		if command != item.command || index != item.index {
			t.Fatalf("id %d -> %v %d", item.id, command, index)
		}
	}
	if !ButtonPrimary(ButtonRequestReplies, false, false) || ButtonPrimary(ButtonPause, true, true) {
		t.Fatal("request replies is the steady primary button")
	}
	if !ButtonPrimary(ButtonAcceptReplies, true, false) || ButtonPrimary(ButtonAcceptReplies, false, true) {
		t.Fatal("accept is primary only while a suggestion is waiting")
	}
	if !ButtonPrimary(ButtonExpression, false, true) || ButtonPrimary(ButtonExpression, true, false) {
		t.Fatal("expression is primary only while expanded")
	}
	if !ButtonPrimary(ButtonSaveProfile, false, false) || ButtonPrimary(ButtonCloseProfile, true, true) {
		t.Fatal("save profile stays primary")
	}
	if CopyText(CommandCopyReply, "en", false, "expr") != "" || CopyText(CommandCopyReply, "en", true, "expr") != "en" {
		t.Fatal("reply copy")
	}
	if CopyText(CommandCopyExpression, "en", true, "expr") != "expr" {
		t.Fatal("expression copy")
	}
	if !ReplyCopyEnabled(true, "en") || ReplyCopyEnabled(true, "") || !AcceptEnabled(true, true, false) || AcceptEnabled(true, true, true) || AcceptEnabled(false, true, false) || !RefineEnabled(true, "短一点", false) || RefineEnabled(true, "短一点", true) || RefineEnabled(false, "短一点", false) {
		t.Fatal("copy and refine gates")
	}
}
