package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	coreconfig "github.com/xujinheng/windows-voice-rewrite/core/config"
	"github.com/xujinheng/windows-voice-rewrite/core/voice"
)

// Two genuinely different clients (the CLI binary entrypoint and a direct Go
// consumer) must exercise the same Core, not duplicate its ASR/LLM pipeline.
func TestCLIAndDirectClientShareResultContract(t *testing.T) {
	for _, tc := range []struct {
		name, spoken, route, text string
		rewrite                   bool
	}{
		{"dictation", "hello", voice.RouteInsert, "hello", false},
		{"spoken answer", "这是什么意思？", voice.RoutePreview, "Explanation", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/audio/transcriptions":
					fmt.Fprintf(w, `{"text":%q}`, tc.spoken)
				case "/v1/chat/completions":
					fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, `{"intent":"answer","result":"Explanation"}`)
				default:
					t.Errorf("unexpected endpoint: %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			cfg := coreconfig.Defaults()
			cfg.ASRBaseURL = server.URL + "/v1"
			cfg.LLMBaseURL = server.URL + "/v1"
			cfg.TextModel = "test-model"
			dir := t.TempDir()
			pcm := make([]byte, 3200)
			pcmPath := filepath.Join(dir, "voice.pcm")
			if err := os.WriteFile(pcmPath, pcm, 0600); err != nil {
				t.Fatal(err)
			}
			cfgJSON, err := coreconfig.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			cfgPath := filepath.Join(dir, "config.json")
			if err := os.WriteFile(cfgPath, cfgJSON, 0600); err != nil {
				t.Fatal(err)
			}
			args := []string{"-config", cfgPath, "-pcm", pcmPath}
			selected := ""
			if tc.rewrite {
				selected = "Original"
				selectedPath := filepath.Join(dir, "selected.txt")
				if err := os.WriteFile(selectedPath, []byte(selected), 0600); err != nil {
					t.Fatal(err)
				}
				args = append(args, "-rewrite", "-selected-file", selectedPath)
			}
			// Spawn the actual independent binary entrypoint, not a test-only
			// direct invocation of its parser.
			cmd := exec.Command("go", append([]string{"run", "."}, args...)...)
			output, err := cmd.Output()
			if err != nil {
				t.Fatalf("CLI process: %v: %s", err, output)
			}
			var viaCLI voice.Result
			if err := json.Unmarshal(output, &viaCLI); err != nil {
				t.Fatal(err)
			}
			var direct voice.Core
			defer direct.Apply(voice.Command{Kind: voice.CommandShutdown})
			viaCore, err := direct.Execute(direct.Apply(voice.Command{Kind: voice.CommandStart}).Token, cfg, pcm, selected, tc.rewrite, nil)
			if err != nil {
				t.Fatal(err)
			}
			if viaCLI != viaCore || viaCLI.Route != tc.route || viaCLI.Text != tc.text {
				t.Fatalf("clients diverged: cli=%+v core=%+v", viaCLI, viaCore)
			}
		})
	}
}
