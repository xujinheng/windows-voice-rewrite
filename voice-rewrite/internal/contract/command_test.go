package contract

import "testing"

func TestProbeSlotsFollowCatalogOrder(t *testing.T) {
	asr, ok := ProbeSlot("asr_base_url")
	model, mok := ProbeSlot("asr_model")
	key, kok := ProbeSlot("asr_api_key")
	llm, lok := ProbeSlot("llm_api_key")
	if !ok || !mok || !kok || !lok || asr != 0 || model != 0 || key != 0 || llm != 1 {
		t.Fatalf("slots %d %d %d %d", asr, model, key, llm)
	}
	if _, ok := ProbeSlot("rewrite_shortcut"); ok {
		t.Fatal("shortcut marked a connection field")
	}
	if _, ok := ProbeSlot("missing"); ok {
		t.Fatal("unknown field")
	}
}

func TestSettingsCommandsKeepButtonOrder(t *testing.T) {
	if NavAI != NavSpeech+1 || NavInput != NavSpeech+2 {
		t.Fatalf("nav ids %d %d %d", NavSpeech, NavAI, NavInput)
	}
	action, page := Command(NavSpeech)
	if action != ActionPage || page != 0 {
		t.Fatalf("speech %v %d", action, page)
	}
	action, page = Command(NavInput)
	if action != ActionPage || page != 2 {
		t.Fatalf("input %v %d", action, page)
	}
	if action, _ := Command(SaveButton); action != ActionSave {
		t.Fatal(action)
	}
	if action, _ := Command(CloseButton); action != ActionClose {
		t.Fatal(action)
	}
	if action, _ := Command(AdvancedButton); action != ActionAdvanced {
		t.Fatal(action)
	}
	if action, _ := Command(ProbeASRButton); action != ActionProbeASR {
		t.Fatal(action)
	}
	if action, _ := Command(ProbeLLMButton); action != ActionProbeLLM {
		t.Fatal(action)
	}
	if action, _ := Command(CaptureRewrite); action != ActionCapture {
		t.Fatal(action)
	}
	if action, _ := Command(9999); action != ActionNone {
		t.Fatal(action)
	}
	label, primary := ButtonLabel(SaveButton, false)
	if label != "保存设置" || !primary {
		t.Fatalf("save %q %t", label, primary)
	}
	label, primary = ButtonLabel(ProbeLLMButton, false)
	if label != "测试连接" || primary {
		t.Fatalf("probe %q %t", label, primary)
	}
	label, primary = ButtonLabel(CaptureDictation, false)
	if label != "录入按键" || primary {
		t.Fatalf("capture %q %t", label, primary)
	}
	label, primary = ButtonLabel(CaptureRewrite, true)
	if label != WaitingForShortcut || primary || WaitingForShortcut != "等待按键…" {
		t.Fatalf("waiting %q %t", label, primary)
	}
	if label, primary := ButtonLabel(CloseButton, false); label != "关闭" || primary {
		t.Fatalf("close %q %t", label, primary)
	}
	if label, _ := ButtonLabel(NavSpeech, false); label != "" {
		t.Fatal(label)
	}
}
