package meeting

import "testing"

func TestPostedMeetingKeepsDemoAvailableWhilePaused(t *testing.T) {
	if PostedMeetingEffect(PostedStart, true, false, false) != PostedIgnore || PostedMeetingEffect(PostedStart, false, false, false) != PostedBegin {
		t.Fatal("start")
	}
	if PostedMeetingEffect(PostedSuggest, false, false, false) != PostedIgnore || PostedMeetingEffect(PostedSuggest, false, true, true) != PostedIgnore || PostedMeetingEffect(PostedSuggest, false, true, false) != PostedReplies {
		t.Fatal("suggest")
	}
	if PostedMeetingEffect(PostedDemo, true, false, false) != PostedBeginDemo || PostedMeetingEffect(PostedStop, false, false, false) != PostedStopMeeting || PostedMeetingEffect(PostedRefresh, true, true, true) != PostedRefreshMeeting {
		t.Fatal("demo")
	}
	dropped := MeetingStopAction(true)
	if dropped.Express || !dropped.DrainRecorder || !dropped.ClearBusy || !dropped.IdleTray || !dropped.ClosePill {
		t.Fatal("canceled meeting")
	}
	live := MeetingStopAction(false)
	if !live.Express || live.DrainRecorder || live.ClearBusy || live.IdleTray || live.ClosePill {
		t.Fatal("live meeting")
	}
}

func TestAcceptButtonNeedsOwnerPendingAndLiveMeeting(t *testing.T) {
	if AcceptEnabled(false, true, false) || AcceptEnabled(true, false, false) || !AcceptEnabled(true, true, false) || AcceptEnabled(true, true, true) {
		t.Fatal("button")
	}
}

func TestApplyCommandTogglesViewFlags(t *testing.T) {
	flags := HUDFlags{SelectedReply: 1, ProfileOpen: true}
	next, effect := ApplyCommand(flags, CommandTogglePause, 0)
	if !next.Paused || effect != EffectRefresh || !next.ProfileOpen {
		t.Fatalf("pause %+v %v", next, effect)
	}
	next, effect = ApplyCommand(next, CommandCaptions, 0)
	if !next.CaptionsOnly || effect != EffectLayoutSaveRefresh {
		t.Fatalf("captions %+v %v", next, effect)
	}
	next, effect = ApplyCommand(next, CommandToggleExpression, 0)
	if !next.ExpressionExpanded || effect != EffectLayoutRefresh {
		t.Fatalf("expression %+v %v", next, effect)
	}
	next, effect = ApplyCommand(next, CommandSelectReply, 2)
	if next.SelectedReply != 2 || effect != EffectSelectReply {
		t.Fatalf("reply %+v %v", next, effect)
	}
	next, effect = ApplyCommand(next, CommandCloseProfile, 0)
	if next.ProfileOpen || effect != EffectCloseProfile {
		t.Fatalf("close %+v %v", next, effect)
	}
	if _, effect = ApplyCommand(next, CommandCopyReply, 0); effect != EffectCopy {
		t.Fatal(effect)
	}
	if !NeedsMeetingOwner(ButtonRequestReplies) || !NeedsMeetingOwner(ButtonToggleCapture) || !NeedsMeetingOwner(ButtonOpenProfile) || NeedsMeetingOwner(ButtonPause) || NeedsMeetingOwner(ButtonAcceptReplies) {
		t.Fatal("owner buttons")
	}
	if _, effect = ApplyCommand(next, CommandNone, 0); effect != EffectNone {
		t.Fatal(effect)
	}
}

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
	if !ReplyCopyEnabled(true, "en") || ReplyCopyEnabled(true, "") || !AcceptEnabled(true, true, false) || AcceptEnabled(true, true, true) || AcceptEnabled(false, true, false) {
		t.Fatal("copy and refine gates")
	}
	if RefineButtonEnabled(true, true, "", false) || !RefineButtonEnabled(true, true, "en", false) || RefineButtonEnabled(false, true, "en", false) || RefineButtonEnabled(true, true, "en", true) {
		t.Fatal("refine button")
	}
}
