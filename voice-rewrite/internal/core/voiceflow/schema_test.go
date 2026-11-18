package voiceflow

import (
	_ "embed"
	"encoding/json"
	"strings"
	"testing"
)

//go:embed schema.v1.json
var schemaV1 []byte

func TestPublishedJSONSchemaV1MatchesEventAndEffectCatalog(t *testing.T) {
	var document map[string]any
	if err := json.Unmarshal(schemaV1, &document); err != nil {
		t.Fatalf("invalid schema JSON: %v", err)
	}
	definitions, ok := document["$defs"].(map[string]any)
	if !ok {
		t.Fatal("JSON schema definitions missing")
	}
	checkEnum := func(definition string, field string, wanted []string) {
		t.Helper()
		props := definitions[definition].(map[string]any)["properties"].(map[string]any)
		values := props[field].(map[string]any)["enum"].([]any)
		if len(values) != len(wanted) {
			t.Fatalf("%s.%s contains %d enum values, want %d", definition, field, len(values), len(wanted))
		}
		for _, name := range wanted {
			found := false
			for _, value := range values {
				if value == name {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("%s.%s is missing %q", definition, field, name)
			}
		}
	}
	checkEnum("event", "type", []string{
		string(RecordArm), string(Press), string(Release), string(RecordReady),
		string(RecordLimit), string(AudioStopped), string(ASRResult), string(ModelResult),
		string(WriteReceipt), string(Cancel), string(Timeout), string(FocusChanged),
	})
	checkEnum("action", "type", []string{
		string(StartNativeCapture), string(StopNativeCapture), string(SendASR),
		string(SendModel), string(AttemptNativeWrite), string(ShowPreview),
		string(SetPhase), string(AppendHistory), string(ClearTask),
	})
	for _, name := range []string{"session_id", "generation", "command_id", "request_id", "receipt_id", "timeout_origin"} {
		if !strings.Contains(string(schemaV1), "\""+name+"\"") {
			t.Fatalf("schema omits required contract field %s", name)
		}
	}
}
