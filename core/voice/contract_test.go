package voice

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	coreconfig "github.com/xujinheng/windows-voice-rewrite/core/config"
)

func TestCommandsEventsStatesSharePlatformIndependentSemantics(t *testing.T) {
	for _, tc := range []struct {
		name               string
		dictation, rewrite int
	}{
		{"Windows mouse", 1, 2},
		{"Windows keyboard", 267, 268},
		{"macOS mouse", 103, 104},
		{"macOS keyboard", 54, 61},
		{"CLI", 1001, 1002},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var core Core
			if event := core.Apply(Command{Kind: CommandBind, Dictation: []int{tc.dictation}, Rewrite: []int{tc.rewrite}}); event.Kind != EventBound {
				t.Fatalf("bind event: %+v", event)
			}
			press := core.Apply(Command{Kind: CommandPress, Code: tc.dictation})
			if press.Kind != EventPressed || press.Token == 0 || press.Role != 1 {
				t.Fatalf("press: %+v", press)
			}
			s := core.Snapshot(tc.dictation, press.Token)
			if !s.Down || !s.Armed || s.HeldRole != 1 || !s.CanActivate || s.Role != 1 {
				t.Fatalf("physical snapshot: %+v", s)
			}
			core.Apply(Command{Kind: CommandBind, Dictation: []int{tc.rewrite}, Rewrite: []int{tc.dictation}})
			if state := core.Snapshot(tc.dictation, press.Token); state.HeldRole != 1 || state.Role != 2 {
				t.Fatalf("rebinding overwrote frozen role: %+v", state)
			}
			if e := core.Apply(Command{Kind: CommandActivate, Code: tc.dictation, Token: press.Token}); e.Kind != EventActivated {
				t.Fatalf("activate: %+v", e)
			}
			if e := core.Apply(Command{Kind: CommandRelease, Code: tc.dictation}); e.Kind != EventReleased || e.Release != ReleaseActive || e.Role != 1 {
				t.Fatalf("release: %+v", e)
			}
			started := core.Apply(Command{Kind: CommandStart})
			if started.Kind != EventStarted || !core.Snapshot(0, started.Token).Valid || core.Snapshot(0, 0).Phase != EventStarted {
				t.Fatal("task start not reflected in state")
			}
			canceled := core.Apply(Command{Kind: CommandCancel})
			if canceled.Kind != EventCanceled || core.Snapshot(0, started.Token).Valid || core.Snapshot(0, 0).Phase != EventCanceled {
				t.Fatal("canceled generation remained valid")
			}
			if data, err := json.Marshal(core.Snapshot(0, 0)); err != nil || !json.Valid(data) {
				t.Fatalf("public snapshot JSON: %s %v", data, err)
			}
			if data, err := json.Marshal(canceled); err != nil || !json.Valid(data) {
				t.Fatalf("public event JSON: %s %v", data, err)
			}
			if e := core.Apply(Command{Kind: CommandShutdown}); e.Kind != EventClosed || !core.Snapshot(0, 0).Closed {
				t.Fatal("shutdown not observable")
			}
			if e := core.Apply(Command{Kind: CommandStart}); e.Kind != EventRejected {
				t.Fatal("shutdown admitted a new task")
			}
		})
	}
}

func TestExecuteEmitsPublicProgressAndResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/audio/transcriptions":
			fmt.Fprint(w, `{"text":"explain this"}`)
		case "/v1/chat/completions":
			fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, `{"intent":"answer","result":"Explanation"}`)
		default:
			t.Errorf("unexpected service path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	cfg := coreconfig.Defaults()
	cfg.ASRBaseURL = server.URL + "/v1"
	cfg.LLMBaseURL = server.URL + "/v1"
	cfg.TextModel = "test-model"
	var core Core
	defer core.Apply(Command{Kind: CommandShutdown})
	started := core.Apply(Command{Kind: CommandStart}).Token
	var events []Event
	result, err := core.Execute(started, cfg, make([]byte, 3200), "Original", true, func(e Event) { events = append(events, e) })
	if err != nil || result.Route != RoutePreview || result.Text != "Explanation" {
		t.Fatalf("shared result: %+v %v", result, err)
	}
	if len(events) != 2 || events[0].Kind != EventTranscribed || events[0].Token != started ||
		events[1].Kind != EventFinished || events[1].Result != result ||
		core.Snapshot(0, started).Phase != EventFinished {
		t.Fatalf("shared events/state: %+v / %+v", events, core.Snapshot(0, started))
	}
}
