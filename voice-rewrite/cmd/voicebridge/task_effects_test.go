package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"voicerewrite/internal/core/delivery"
	"voicerewrite/internal/core/jsonl"
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
			request := request{
				Version: jsonl.Version, ID: string(rune('a' + index)), Op: "task_receipt",
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
	send := func(id, state string) bool {
		t.Helper()
		packet, err := json.Marshal(request{
			Version: jsonl.Version, ID: id, Op: "task_receipt",
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
