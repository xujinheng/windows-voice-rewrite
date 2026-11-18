package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"voicerewrite/internal/core/delivery"
	"voicerewrite/internal/core/jsonl"
	sessioncore "voicerewrite/internal/core/session"
	"voicerewrite/internal/core/textedit"
	"voicerewrite/internal/core/ux"
)

// Exercise the actual JSONL dispatch path, not merely a direct Go wrapper.
func TestNativeReceiptJSONLMatchesWindowsCore(t *testing.T) {
	var output bytes.Buffer
	server := &bridgeServer{
		active: make(map[string]*bridgeTask), seen: make(map[string]struct{}),
		output: &output,
	}
	index := 0
	for _, kind := range []struct {
		wire string
		core delivery.Kind
	}{{"dictation", delivery.Dictation}, {"selection", delivery.ReplaceSelection}} {
		for _, state := range []struct {
			wire string
			core delivery.State
		}{{"blocked", delivery.Blocked}, {"sent_unverified", delivery.SentUnverified},
			{"confirmed", delivery.Confirmed}, {"uncertain", delivery.Uncertain}} {
			index++
			sessionID := fmt.Sprintf("receipt-%d", index)
			if !server.voiceEpoch.Apply(sessioncore.Event{Type: sessioncore.EventBegin,
				SessionID: sessionID, Generation: uint64(index)}).Allowed {
				t.Fatal("failed to start Go task epoch")
			}
			request := request{
				Version: jsonl.Version, ID: string(rune('a' + index)), Op: "task_receipt",
				SessionID: sessionID, Generation: uint64(index),
				ReceiptKind: kind.wire, ReceiptState: state.wire,
				ReceiptText: "待写入文本", ReceiptTitle: "结果", ReceiptStatus: "native refusal",
			}
			packet, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			server.dispatch(packet)
			var reply struct {
				OK     bool `json:"ok"`
				Result struct {
					Effect ux.TaskEffect `json:"effect"`
				} `json:"result"`
			}
			if err := json.NewDecoder(&output).Decode(&reply); err != nil {
				t.Fatal(err)
			}
			if !reply.OK {
				t.Fatalf("bridge rejected %q/%q", kind.wire, state.wire)
			}
			windows := ux.TaskWriteEffect(kind.core, state.core,
				"待写入文本", "结果", "native refusal")
			if !reflect.DeepEqual(reply.Result.Effect, windows) {
				t.Fatalf("host mismatch for %s/%s: mac=%+v windows=%+v",
					kind.wire, state.wire, reply.Result.Effect, windows)
			}
			output.Reset()
		}
	}
}

func TestNativeReceiptRejectsUnknownAndDuplicate(t *testing.T) {
	var output bytes.Buffer
	server := &bridgeServer{active: map[string]*bridgeTask{}, seen: map[string]struct{}{}, output: &output}
	if !server.voiceEpoch.Apply(sessioncore.Event{Type: sessioncore.EventBegin, SessionID: "dup", Generation: 1}).Allowed {
		t.Fatal("failed to start duplicate receipt epoch")
	}
	send := func(id, state string) bool {
		t.Helper()
		packet, err := json.Marshal(request{
			Version: jsonl.Version, ID: id, Op: "task_receipt",
			SessionID: "dup", Generation: 1,
			ReceiptKind: "dictation", ReceiptState: state,
		})
		if err != nil {
			t.Fatal(err)
		}
		server.dispatch(packet)
		var reply response
		if err := json.NewDecoder(&output).Decode(&reply); err != nil {
			t.Fatal(err)
		}
		output.Reset()
		return reply.OK
	}
	if send("a", "unexpected") {
		t.Fatal("unknown receipt accepted")
	}
	if send("a", "confirmed") {
		t.Fatal("duplicate request accepted")
	}
	if !send("b", "sent_unverified") {
		t.Fatal("valid receipt rejected")
	}
}

func TestSelectedBridgeEffectsMatchDirectGo(t *testing.T) {
	for _, tc := range []struct {
		spoken                               bool
		intent, body, instruction, wantRoute string
	}{
		{true, "edit", "修改", "请改写", "replace"},
		{true, "summarize", "摘要", "总结", "preview"},
		{false, "edit", "修改", "请改写", "replace"},
		{false, "translate", "翻译", "解释这个", "preview"},
	} {
		r := selectedTaskSuccess("id", textedit.AssistantResult{
			Intent: tc.intent, Result: tc.body,
		}, tc.instruction, tc.spoken)
		payload, ok := r.Result.(map[string]any)
		if !ok {
			t.Fatalf("missing bridge payload: %+v", r)
		}
		route, ok := payload["route"].(string)
		if !ok {
			t.Fatalf("missing route: %+v", payload)
		}
		effect, ok := payload["effect"].(ux.TaskEffect)
		if !ok {
			t.Fatalf("missing effect: %+v", payload)
		}
		if route != tc.wantRoute {
			t.Fatalf("route for %s=%s want %s", tc.intent, route, tc.wantRoute)
		}
		direct := ux.SelectedTaskEffect(tc.intent, tc.body, tc.instruction, route, tc.spoken)
		if !reflect.DeepEqual(effect, direct) {
			t.Fatalf("mac != Windows direct: %+v != %+v", effect, direct)
		}
	}
}

func TestNativeReceiptRejectsCanceledAndTimedOutEpoch(t *testing.T) {
	for i, reason := range []sessioncore.Reason{sessioncore.ReasonUser, sessioncore.ReasonTimeout} {
		var output bytes.Buffer
		server := &bridgeServer{active: map[string]*bridgeTask{}, seen: map[string]struct{}{}, output: &output}
		sid := fmt.Sprintf("terminal-%d", i)
		if !server.voiceEpoch.Apply(sessioncore.Event{Type: sessioncore.EventBegin, SessionID: sid, Generation: 1}).Allowed {
			t.Fatal("could not begin epoch")
		}
		if !server.voiceEpoch.Apply(sessioncore.Event{Type: sessioncore.EventCancel, SessionID: sid,
			Generation: 1, Reason: reason}).Allowed {
			t.Fatal("could not cancel epoch")
		}
		packet, err := json.Marshal(request{Version: jsonl.Version, ID: "late", Op: "task_receipt",
			SessionID: sid, Generation: 1, ReceiptKind: "selection", ReceiptState: "confirmed"})
		if err != nil {
			t.Fatal(err)
		}
		server.dispatch(packet)
		var reply response
		if err := json.NewDecoder(&output).Decode(&reply); err != nil {
			t.Fatal(err)
		}
		if reply.OK || reply.Error == nil || reply.Error.Code != "stale_task" {
			t.Fatalf("post-terminal receipt allowed: %+v", reply)
		}
	}
}
