package main

import (
	"testing"

	"voicerewrite/internal/core/voiceflow"
)

func TestVoiceEventBridgeTraceV1(t *testing.T) {
	h := startBridge(t, defaultOptions())
	defer h.close()
	send := func(id string, ev voiceflow.Event) response {
		t.Helper()
		h.send(map[string]any{"v": 1, "id": id, "op": "voice_event", "event": ev})
		reply := h.receive()
		if reply.ID != id || !reply.OK {
			t.Fatalf("voice_event %s: %+v", id, reply)
		}
		return reply
	}
	makeEvent := func(kind voiceflow.EventType, command string) voiceflow.Event {
		return voiceflow.Event{
			Version: voiceflow.Version, Type: kind, SessionID: "s-1",
			Generation: 10, CommandID: command,
		}
	}
	find := func(r response, kind voiceflow.ActionType) string {
		t.Helper()
		result := object(t, r.Result)
		for _, entry := range result["effects"].([]any) {
			action := object(t, entry)
			if action["type"] == string(kind) {
				if action["session_id"] != "s-1" || action["generation"] != float64(10) {
					t.Fatalf("effect lost identity: %+v", action)
				}
				return action["command_id"].(string)
			}
		}
		t.Fatalf("no %s action in %+v", kind, result)
		return ""
	}
	arm := makeEvent(voiceflow.RecordArm, "arm")
	arm.Mode, arm.CanInsert = voiceflow.Dictation, true
	find(send("1", arm), voiceflow.ShowPreview)
	start := find(send("2", makeEvent(voiceflow.Press, "press")), voiceflow.StartNativeCapture)
	ready := makeEvent(voiceflow.RecordReady, "ready")
	ready.RequestID, ready.ReceiptID, ready.Success = start, start, true
	send("3", ready)
	stop := find(send("4", makeEvent(voiceflow.Release, "release")), voiceflow.StopNativeCapture)
	stopped := makeEvent(voiceflow.AudioStopped, "audio-done")
	stopped.RequestID, stopped.ReceiptID, stopped.Success = stop, stop, true
	asr := find(send("5", stopped), voiceflow.SendASR)
	reply := makeEvent(voiceflow.ASRResult, "asr-done")
	reply.RequestID, reply.ReceiptID, reply.Text, reply.Success = asr, asr, "Voice test", true
	write := find(send("6", reply), voiceflow.AttemptNativeWrite)
	duplicated := send("7", reply)
	if !object(t, duplicated.Result)["duplicate"].(bool) {
		t.Fatalf("duplicate event was not rejected: %+v", duplicated)
	}
	receipt := makeEvent(voiceflow.WriteReceipt, "write-confirmed")
	receipt.RequestID, receipt.ReceiptID, receipt.Success = write, write, true
	end := send("8", receipt)
	find(end, voiceflow.AppendHistory)
	if object(t, end.Result)["phase"] != string(voiceflow.Success) {
		t.Fatalf("unexpected final state %+v", end)
	}
	late := makeEvent(voiceflow.ASRResult, "late-asr")
	late.RequestID, late.ReceiptID, late.Text, late.Success = asr, asr, "wrong", true
	if object(t, send("9", late).Result)["accepted"].(bool) {
		t.Fatal("late ASR was allowed to write after completion")
	}
}

func TestVoiceEventBridgeVersionAndTimeoutErrors(t *testing.T) {
	h := startBridge(t, defaultOptions())
	defer h.close()
	h.send(map[string]any{"v": 1, "id": "a", "op": "voice_event",
		"event": map[string]any{"v": 2, "type": "record_arm", "session_id": "s",
			"generation": 1, "command_id": "cmd", "mode": "dictation"}})
	reply := h.receive()
	if reply.OK || reply.Error == nil || reply.Error.Code != "voice_event_version" {
		t.Fatalf("invalid nested event version accepted: %+v", reply)
	}
	h.send(map[string]any{"v": 1, "id": "b", "op": "voice_event",
		"event": map[string]any{"v": 1, "type": "timeout", "session_id": "s",
			"generation": 1, "command_id": "cmd"}})
	reply = h.receive()
	if reply.OK || reply.Error == nil || reply.Error.Code != "invalid_voice_event" {
		t.Fatalf("unattributed timeout accepted: %+v", reply)
	}
}
