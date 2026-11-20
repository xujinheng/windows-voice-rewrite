package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	meetingcore "github.com/xujinheng/windows-voice-rewrite/voice-rewrite/internal/core/meeting"
)

func newTestMeetingCore() *meetingSession {
	return newMeetingCore(defaults(), func(string, ...any) {}, func(Config) error { return nil })
}

func TestMeetingSettingsStoreFailureDoesNotMutateState(t *testing.T) {
	original := defaults()
	storeError := errors.New("disk full")
	var notified bool
	s := newMeetingCore(original, func(string, ...any) {}, func(Config) error { return storeError })
	defer s.runtime.Stop()
	s.onConfig = func(Config) { notified = true }
	if err := s.updateProfile("new background", "new terms"); !errors.Is(err, storeError) {
		t.Fatalf("save error = %v", err)
	}
	if s.configSnapshot() != original || notified {
		t.Fatal("failed save modified in-memory configuration")
	}
}

func TestMeetingSettingsUseInjectedStore(t *testing.T) {
	var persisted Config
	var calls int
	s := newMeetingCore(defaults(), func(string, ...any) {}, func(cfg Config) error {
		persisted = cfg
		calls++
		return nil
	})
	defer s.runtime.Stop()
	var notified Config
	s.onConfig = func(cfg Config) { notified = cfg }
	if err := s.updateProfile("  project background  ", "  vocabulary  "); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || persisted.MeetingBackground != "project background" || persisted.MeetingTerms != "vocabulary" {
		t.Fatalf("unexpected stored config: %+v, calls=%d", persisted, calls)
	}
	if notified != persisted || s.configSnapshot() != persisted {
		t.Fatal("session/app config not synchronized after successful save")
	}
}

func TestMeetingProfileRejectsOverlongTextBeforeSave(t *testing.T) {
	var calls int
	s := newMeetingCore(defaults(), func(string, ...any) {}, func(Config) error {
		calls++
		return nil
	})
	defer s.runtime.Stop()
	err := s.updateProfile(" "+strings.Repeat("字", meetingcore.MaxBackgroundRunes), "terms")
	if err == nil || err.Error() != "背景最多 4000 字，术语最多 2000 字" || calls != 0 {
		t.Fatalf("background: %v calls=%d", err, calls)
	}
	err = s.updateProfile("bg", strings.Repeat("词", meetingcore.MaxTermsRunes+1))
	if err == nil || err.Error() != "背景最多 4000 字，术语最多 2000 字" || calls != 0 {
		t.Fatalf("terms: %v calls=%d", err, calls)
	}
	if err = s.updateProfile(strings.Repeat("字", meetingcore.MaxBackgroundRunes), strings.Repeat("词", meetingcore.MaxTermsRunes)); err != nil || calls != 1 {
		t.Fatalf("limit accepted: %v calls=%d", err, calls)
	}
}

func TestMeetingSettingsRejectMissingStore(t *testing.T) {
	s := newMeetingCore(defaults(), func(string, ...any) {}, nil)
	defer s.runtime.Stop()
	if err := s.updateProfile("project", "terms"); err == nil {
		t.Fatal("missing store silently accepted config update")
	}
}

func TestMeetingHistoryIsNotDiscardedAfterTwelveTurns(t *testing.T) {
	s := newTestMeetingCore()
	defer s.runtime.Stop()
	for id := uint64(1); id <= 30; id++ {
		s.view.captions = meetingcore.UpsertCaption(s.view.captions, id, "test sentence", true, true)
	}
	if len(s.view.captions) != 30 || s.view.captions[0].ID != 1 {
		t.Fatalf("reader lost early captions: %#v", s.view.captions)
	}
}

func TestMeetingStreamingDoesNotClearTranslation(t *testing.T) {
	s := newTestMeetingCore()
	defer s.runtime.Stop()
	s.view.captions = meetingcore.UpsertCaption(s.view.captions, 1, "the first phrase", false, true)
	s.updateCaption(1, 1, "the first phrase", "第一句")
	s.view.captions = meetingcore.UpsertCaption(s.view.captions, 1, "the first phrase continues", false, false)
	if len(s.view.captions) != 1 || s.view.captions[0].Chinese != "第一句" {
		t.Fatalf("translation flickered away: %#v", s.view.captions)
	}
	s.view.captions = meetingcore.UpsertCaption(s.view.captions, 1, "", false, false)
	if s.view.captions[0].Source == "" {
		t.Fatal("empty stream chunk erased readable transcript")
	}
}

func TestMeetingPauseInvalidatesOldWork(t *testing.T) {
	s := newTestMeetingCore()
	defer s.runtime.Stop()
	ctx, epoch, _ := s.runtime.Snapshot()
	s.setCapturePaused(true)
	if ctx.Err() == nil {
		t.Fatal("capture/request context not canceled")
	}
	s.applyWork(epoch, func() { s.view.status = "stale" })
	if s.view.status == "stale" {
		t.Fatal("stale work overwrote pause")
	}
	s.setCapturePaused(false)
	next, e, _ := s.runtime.Snapshot()
	if next.Err() != nil || e == epoch {
		t.Fatal("resume failed")
	}
}
func TestMeetingSerialInference(t *testing.T) {
	var g meetingcore.Scheduler
	ctx, release, err := g.Acquire(context.Background(), meetingcore.PriorityCaption)
	defer release()
	if err != nil || ctx.Err() != nil {
		t.Fatal(err)
	}
	waiting, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, _, err = g.Acquire(waiting, meetingcore.PriorityPreview); err == nil {
		t.Fatal("parallel work admitted")
	}
}

func TestMeetingPauseRemovesAbandonedLiveCaption(t *testing.T) {
	s := newTestMeetingCore()
	defer s.runtime.Stop()
	s.view.captions = []meetingcore.Caption{{ID: 1, Source: "final", Final: true}, {ID: 2, Source: "unfinished", Live: "unfinished"}}
	s.setCapturePaused(true)
	s.setCapturePaused(false)
	if len(s.view.captions) != 1 || !s.view.captions[0].Final {
		t.Fatalf("abandoned live caption %#v", s.view.captions)
	}
}

func TestMeetingCancellationDiscardsUnfinishedLines(t *testing.T) {
	s := newTestMeetingCore()
	defer s.runtime.Stop()
	s.view.reply.Displayed.Lines[0] = meetingcore.Line{English: "I would"}
	s.view.reply.Displayed.Lines[1] = meetingcore.Line{English: "Could you clarify?", Chinese: "能解释吗", Complete: true}
	s.view.expression = meetingcore.Line{English: "Partial"}
	s.setCapturePaused(true)
	if s.view.reply.Displayed.Lines[0].English != "" || s.view.reply.Displayed.Lines[1].English == "" || s.view.expression.English != "" {
		t.Fatal("unfinished output retained")
	}
}

func TestMeetingLatestExpressionWins(t *testing.T) {
	s := newTestMeetingCore()
	defer s.runtime.Stop()
	s.mu.Lock()
	first, done, epoch, old := s.runtime.NewExpression(time.Second)
	s.mu.Unlock()
	defer done()
	s.mu.Lock()
	_, nextDone, _, current := s.runtime.NewExpression(time.Second)
	s.mu.Unlock()
	defer nextDone()
	if first.Err() == nil {
		t.Fatal("old expression not canceled")
	}
	s.applyExpression(epoch, current, func() { s.view.expressionStatus = "latest" })
	s.applyExpression(epoch, old, func() { s.view.expressionStatus = "stale" })
	if s.view.expressionStatus != "latest" {
		t.Fatal("old expression overwrote current")
	}
}
