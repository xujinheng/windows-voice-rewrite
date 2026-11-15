package main

import (
	"context"
	"errors"
	"testing"
	"time"

	meetingcore "voicerewrite/internal/core/meeting"
)

func newTestMeetingCore() *meetingSession {
	return newMeetingCore(defaults(), func(string, ...any) {}, func(Config) error { return nil })
}

func TestMeetingSettingsStoreFailureDoesNotMutateState(t *testing.T) {
	original := defaults()
	storeError := errors.New("disk full")
	var notified bool
	s := newMeetingCore(original, func(string, ...any) {}, func(Config) error { return storeError })
	defer s.stop()
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
	defer s.stop()
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

func TestMeetingSettingsRejectMissingStore(t *testing.T) {
	s := newMeetingCore(defaults(), func(string, ...any) {}, nil)
	defer s.stop()
	if err := s.updateProfile("project", "terms"); err == nil {
		t.Fatal("missing store silently accepted config update")
	}
}

func TestMeetingHistoryIsNotDiscardedAfterTwelveTurns(t *testing.T) {
	s := newTestMeetingCore()
	defer s.stop()
	for id := uint64(1); id <= 30; id++ {
		s.upsertSourceLocked(id, "test sentence", true, true)
	}
	if len(s.view.captions) != 30 || s.view.captions[0].id != 1 {
		t.Fatalf("reader lost early captions: %#v", s.view.captions)
	}
}

func TestMeetingStreamingDoesNotClearTranslation(t *testing.T) {
	s := newTestMeetingCore()
	defer s.stop()
	s.upsertSourceLocked(1, "the first phrase", false, true)
	s.updateCaption(1, 1, "the first phrase", "第一句")
	s.upsertSourceLocked(1, "the first phrase continues", false, false)
	if len(s.view.captions) != 1 || s.view.captions[0].chinese != "第一句" {
		t.Fatalf("translation flickered away: %#v", s.view.captions)
	}
	s.upsertSourceLocked(1, "", false, false)
	if s.view.captions[0].source == "" {
		t.Fatal("empty stream chunk erased readable transcript")
	}
}

func TestMeetingPauseInvalidatesOldWork(t *testing.T) {
	s := newTestMeetingCore()
	defer s.stop()
	ctx, epoch := s.workSnapshot()
	s.setCapturePaused(true)
	if ctx.Err() == nil {
		t.Fatal("capture/request context not canceled")
	}
	s.applyWork(epoch, func() { s.view.status = "stale" })
	if s.view.status == "stale" {
		t.Fatal("stale work overwrote pause")
	}
	s.setCapturePaused(false)
	next, e := s.workSnapshot()
	if next.Err() != nil || e == epoch {
		t.Fatal("resume failed")
	}
}
func TestMeetingPendingRepliesKeepReadableBatch(t *testing.T) {
	s := newTestMeetingCore()
	defer s.stop()
	s.view.replies[0] = meetingLine{english: "old", complete: true}
	s.view.replySource = "old question"
	batch := [3]meetingLine{{english: "new", chinese: "新", complete: true}}
	s.publishReplies(batch, "new question", "check")
	if s.view.replies[0].english != "old" || !s.view.replyPending {
		t.Fatal("readable reply replaced")
	}
	s.acceptPendingReplies()
	if s.view.replies[0].english != "new" || s.view.replySource != "new question" || s.view.replyPending {
		t.Fatal("switch failed")
	}
}
func TestMeetingAdaptiveInterval(t *testing.T) {
	if got := meetingPreviewInterval(100 * time.Millisecond); got != 800*time.Millisecond {
		t.Fatal(got)
	}
	if got := meetingPreviewInterval(5 * time.Second); got != 2400*time.Millisecond {
		t.Fatal(got)
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

func TestMeetingReplyBatchKeepsReceivingRemainingOptions(t *testing.T) {
	s := newTestMeetingCore()
	defer s.stop()
	s.replySerial = 1
	batch := [3]meetingLine{{english: "first", chinese: "一", complete: true}}
	s.publishRepliesFor(1, 1, batch, "question", "")
	batch[1] = meetingLine{english: "second", chinese: "二", complete: true}
	s.publishRepliesFor(1, 1, batch, "question", "")
	if s.view.replies[1].english != "second" || s.view.replyPending {
		t.Fatal("same batch became pending")
	}
	s.replySerial = 2
	batch[0].english = "new"
	s.publishRepliesFor(1, 2, batch, "question", "")
	if s.view.replies[0].english != "first" || !s.view.replyPending {
		t.Fatal("regeneration replaced current reply")
	}
	s.acceptPendingReplies()
	batch[2] = meetingLine{english: "third", chinese: "三", complete: true}
	s.publishRepliesFor(1, 2, batch, "question", "")
	if s.view.replies[2].english != "third" {
		t.Fatal("accepted live batch stopped updating")
	}
}

func TestMeetingPauseRemovesAbandonedLiveCaption(t *testing.T) {
	s := newTestMeetingCore()
	defer s.stop()
	s.view.captions = []meetingCaption{{id: 1, source: "final", final: true}, {id: 2, source: "unfinished", live: "unfinished"}}
	s.setCapturePaused(true)
	s.setCapturePaused(false)
	if len(s.view.captions) != 1 || !s.view.captions[0].final {
		t.Fatalf("abandoned live caption %#v", s.view.captions)
	}
}

func TestMeetingFinalCancelsActivePreview(t *testing.T) {
	s := newTestMeetingCore()
	defer s.stop()
	ctx, cancel := context.WithCancel(s.ctx)
	s.activeASRCancel = cancel
	s.activeASRID = 1
	work, epoch := s.workSnapshot()
	q := make(chan meetingAudioTurn, 1)
	s.queueAudio(q, meetingAudioTurn{final: true, id: 1, epoch: epoch, ctx: work})
	if ctx.Err() == nil {
		t.Fatal("final waits behind preview")
	}
	if len(q) != 1 {
		t.Fatal("final not queued")
	}
}

func TestMeetingCancellationDiscardsUnfinishedLines(t *testing.T) {
	s := newTestMeetingCore()
	defer s.stop()
	s.view.replies[0] = meetingLine{english: "I would"}
	s.view.replies[1] = meetingLine{english: "Could you clarify?", chinese: "能解释吗", complete: true}
	s.view.expression = meetingLine{english: "Partial"}
	s.setCapturePaused(true)
	if s.view.replies[0].english != "" || s.view.replies[1].english == "" || s.view.expression.english != "" {
		t.Fatal("unfinished output retained")
	}
}

func TestMeetingLatestExpressionWins(t *testing.T) {
	s := newTestMeetingCore()
	defer s.stop()
	s.mu.Lock()
	first, done, epoch, old := s.expressionContextLocked(time.Second)
	s.mu.Unlock()
	defer done()
	s.mu.Lock()
	_, nextDone, _, current := s.expressionContextLocked(time.Second)
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
