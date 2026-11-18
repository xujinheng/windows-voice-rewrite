package ux

import (
	"reflect"
	"testing"

	"voicerewrite/internal/core/delivery"
)

func TestDictationTaskEffect(t *testing.T) {
	for _, tc := range []struct {
		name, text  string
		target      bool
		phase       int
		write, card bool
		history     int
	}{
		{"empty", "  ", true, taskError, false, false, 0},
		{"without target", "你好", false, taskFallback, false, true, 1},
		{"write", "你好", true, taskApplying, true, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := DictationTaskEffect(tc.text, tc.target)
			if got.Phase != tc.phase || (got.InsertText != "") != tc.write ||
				(got.CardText != "") != tc.card || len(got.History) != tc.history {
				t.Fatalf("effect mismatch: %+v", got)
			}
		})
	}
}

func TestSelectedTaskEffect(t *testing.T) {
	for _, tc := range []struct {
		name, route, body, instruction        string
		spoken, write, review, card, canceled bool
		history, phase                        int
	}{
		{"spoken replace", "replace", "新", "修改", true, true, false, false, false, 2, taskApplying},
		{"spoken preview", "preview", "答", "解释", true, false, false, true, true, 2, taskAnswer},
		{"typed replace", "replace", "新", "修改", false, false, true, true, false, 1, taskAnswer},
		{"typed preview", "preview", "答", "解释", false, false, false, true, true, 1, taskAnswer},
		{"blank model", "replace", "  ", "修改", true, false, false, false, true, 0, taskError},
		{"missing spoken command", "replace", "新", "", true, false, false, false, true, 0, taskError},
		{"unknown route", "modify", "新", "修改", true, false, false, false, true, 0, taskError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := SelectedTaskEffect("edit", tc.body, tc.instruction, tc.route, tc.spoken)
			if got.Phase != tc.phase || (got.ReplaceText != "") != tc.write ||
				got.ManualReview != tc.review || (got.CardText != "") != tc.card ||
				got.CancelTask != tc.canceled || len(got.History) != tc.history {
				t.Fatalf("effect mismatch: %+v", got)
			}
		})
	}
}

func TestTaskWriteEffectReceiptMatrix(t *testing.T) {
	for _, kind := range []delivery.Kind{delivery.Dictation, delivery.ReplaceSelection} {
		for _, tc := range []struct {
			state delivery.State
			phase int
			card  bool
		}{
			{delivery.Blocked, taskFallback, true},
			{delivery.SentUnverified, taskAnswer, false},
			{delivery.Confirmed, taskSuccess, false},
			{delivery.Uncertain, taskFallback, true},
			{delivery.State(255), taskFallback, true},
		} {
			got := TaskWriteEffect(kind, tc.state, "abc", "copy", "native error")
			if got.Phase != tc.phase || (got.CardText != "") != tc.card ||
				got.ReplaceText != "" || got.InsertText != "" || len(got.History) != 0 {
				t.Fatalf("kind=%v receipt=%v: %+v", kind, tc.state, got)
			}
			if tc.state == delivery.SentUnverified && got.InteractionStatus == "原选区验证通过并已替换" {
				t.Fatalf("unverified may not claim confirmed: %+v", got)
			}
		}
	}
}

func TestTaskEffectsDeterministic(t *testing.T) {
	for _, effect := range []TaskEffect{
		DictationTaskEffect("word", true),
		SelectedTaskEffect("translate", "answer", "翻译", "preview", true),
		TaskWriteEffect(delivery.Dictation, delivery.Uncertain, "word", "title", "error"),
	} {
		var again TaskEffect
		switch {
		case effect.InsertText != "":
			again = DictationTaskEffect("word", true)
		case len(effect.History) != 0:
			again = SelectedTaskEffect("translate", "answer", "翻译", "preview", true)
		default:
			again = TaskWriteEffect(delivery.Dictation, delivery.Uncertain, "word", "title", "error")
		}
		if !reflect.DeepEqual(effect, again) {
			t.Fatalf("nondeterministic %+v vs %+v", effect, again)
		}
	}
}
