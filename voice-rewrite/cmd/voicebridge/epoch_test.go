package main

import (
    "bytes"
    "encoding/json"
    "strings"
    "testing"
)

func TestBridgeVoiceEpochTraceAndLateReceipt(t *testing.T) {
    events := []string{
        `{"v":1,"id":"r1","op":"voice_epoch","epoch_event":"begin","session_id":"task-A","generation":1}`,
        `{"v":1,"id":"r2","op":"voice_epoch","epoch_event":"stage","session_id":"task-A","generation":1,"stage":"asr","audio_bytes":32000}`,
        `{"v":1,"id":"r3","op":"voice_epoch","epoch_event":"bind","session_id":"task-A","generation":1,"request_id":"provider-A"}`,
        `{"v":1,"id":"r4","op":"voice_epoch","epoch_event":"cancel","session_id":"task-A","generation":1,"reason":"user"}`,
        `{"v":1,"id":"r5","op":"voice_epoch","epoch_event":"authorize","session_id":"task-A","generation":1,"stage":"asr"}`,
        `{"v":1,"id":"r6","op":"voice_epoch","epoch_event":"begin","session_id":"task-B","generation":2}`,
        `{"v":1,"id":"r7","op":"voice_epoch","epoch_event":"receipt","session_id":"task-A","generation":1,"stage":"native_write"}`,
        `{"v":1,"id":"r8","op":"voice_epoch","epoch_event":"authorize","session_id":"task-B","generation":2}`,
    }
    var out bytes.Buffer
    if err := serve(strings.NewReader(strings.Join(events,"\n")+"\n"), &out); err != nil {
        t.Fatal(err)
    }
    lines := bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n"))
    if len(lines) != len(events) { t.Fatalf("got %d replies: %s",len(lines),out.String()) }
    allowed := []bool{true,true,true,true,false,true,false,true}
    for i, line := range lines {
        var got struct {
            OK bool `json:"ok"`
            Result struct {
                Allowed bool `json:"allowed"`
                Effects []struct {
                    ID string `json:"id"`
                    Type string `json:"type"`
                    RequestID string `json:"request_id"`
                } `json:"effects"`
            } `json:"result"`
        }
        if err:=json.Unmarshal(line,&got);err!=nil {t.Fatal(err)}
        if !got.OK || got.Result.Allowed!=allowed[i] {
            t.Fatalf("reply %d got %+v expected allowed=%v",i,got,allowed[i])
        }
        if i==3 {
            if len(got.Result.Effects)!=4 ||
               got.Result.Effects[0].Type!="cancel_request" ||
               got.Result.Effects[0].RequestID!="provider-A" {
                t.Fatalf("cancel did not emit matching provider request: %+v",got.Result.Effects)
            }
            used:=map[string]bool{}
            for _,effect:=range got.Result.Effects {
                if used[effect.ID] {t.Fatal("duplicate effect id")}
                used[effect.ID]=true
            }
        }
        if i==6 && len(got.Result.Effects)>0 {
            t.Fatal("stale native write receipt emitted effects")
        }
    }
}
