// Package settingsform describes the shared native settings layout.
package settingsform

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	coreconfig "github.com/xujinheng/windows-voice-rewrite/core/config"
	"github.com/xujinheng/windows-voice-rewrite/core/contract"
	"github.com/xujinheng/windows-voice-rewrite/core/localasr"
	"github.com/xujinheng/windows-voice-rewrite/core/provider"
	"github.com/xujinheng/windows-voice-rewrite/core/serviceprobe"
	"github.com/xujinheng/windows-voice-rewrite/core/ux"
)

// ConnectionForm reuses the controls and coordinates already used by Windows.
// Native hosts transform these logical top-left rectangles into native points.
type NativeForm struct {
	Width       int32           `json:"width"`
	Height      int32           `json:"height"`
	Hint        Rect            `json:"hint"`
	Running     string          `json:"running"`
	Busy        string          `json:"busy"`
	CaptureHint string          `json:"captureHint"`
	Pages       []NativePage    `json:"pages"`
	Controls    []NativeControl `json:"controls"`
}
type NativePage struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Subtitle     string `json:"subtitle"`
	Nav          Rect   `json:"nav"`
	SubtitleRect Rect   `json:"subtitleRect"`
}
type NativeControl struct {
	ID        string        `json:"id"`
	Page      string        `json:"page"`
	Label     string        `json:"label"`
	Kind      contract.Kind `json:"kind"`
	Rect      Rect          `json:"rect"`
	LabelRect Rect          `json:"labelRect"`
}

func ConnectionForm() NativeForm {
	hint, _ := contract.Find("capture_rewrite")
	form := NativeForm{Width: DesignWidth, Height: DesignHeight, Hint: SettingsHint(), Running: serviceprobe.StatusRunning, Busy: ux.SaveWhileBusy, CaptureHint: hint.Hint}
	for i, page := range contract.Pages() {
		form.Pages = append(form.Pages, NativePage{ID: page.ID, Title: page.Title, Subtitle: page.Subtitle, Nav: NavButton(i), SubtitleRect: Rect{X: 224, Y: 114, W: 460, H: 42}})
		row := 0
		controls := contract.PageControls(page.ID, contract.Both)
		if page.ID == "input" {
			controls = nil
			for _, control := range contract.PageControls(page.ID, contract.MacOS) {
				if control.NativeMac {
					controls = append(controls, control)
				}
			}
		}
		for _, control := range controls {
			item := NativeControl{ID: control.ID, Page: page.ID, Label: control.Label, Kind: control.Kind}
			switch control.Kind {
			case contract.Text, contract.Secret, contract.Shortcut, contract.Stepper:
				item.LabelRect, item.Rect = FieldLabel(row), FieldEdit(row)
				if control.Kind == contract.Shortcut {
					item.Rect = ShortcutEdit()
					item.Rect.Y += int32(row) * RowStep
				} else if control.ID == "asr_model" || control.ID == "text_model" {
					item.Rect = ModelEdit(row)
				}
			case contract.Button:
				switch control.ID {
				case "probe_asr", "probe_llm":
					item.Rect = ProbeButtonBox()
				case "select_asr_directory":
					item.Rect = DeployFolderBox()
				case "deploy_asr":
					item.Rect = DeployButtonBox()
				case "capture_rewrite", "capture_dictation":
					item.Rect = CaptureButton(row - 1)
				default:
					item.Rect = ModelDiscoverButton(1)
				}
			case contract.Status:
				item.Rect = ProbeStatusBox()
				item.Label = serviceprobe.StatusIdle
			case contract.Note:
				item.Rect = Rect{X: 226, Y: 338, W: 565, H: 74}
			}
			form.Controls = append(form.Controls, item)
			if control.Stacked {
				row++
			}
		}
	}
	for _, entry := range []struct {
		id     string
		anchor Anchor
	}{
		{"save", AnchorSave}, {"advanced", AnchorAdvanced}, {"close", AnchorClose},
	} {
		control, _ := contract.Find(entry.id)
		form.Controls = append(form.Controls, NativeControl{
			ID: entry.id, Page: "save", Label: control.Label, Kind: control.Kind,
			Rect: FooterButton(entry.anchor),
		})
	}
	return form
}

// Update applies all portable settings edits once for every native host.
// Filesystem writes, microphone selection and platform shortcut capabilities
// belong to the host, not this business decision.
func Update(cfg coreconfig.Config, values map[string]string) (coreconfig.Config, error) {
	fields := [6]string{cfg.ASRBaseURL, cfg.ASRModel, cfg.ASRAPIKey, cfg.LLMBaseURL, cfg.TextModel, cfg.LLMAPIKey}
	for id, value := range values {
		if index, ok := contract.InputIndex(id); ok {
			fields[index] = value
		}
	}
	cfg, err := coreconfig.WithConnectionFields(cfg, fields)
	if err != nil {
		return cfg, err
	}
	for id, value := range values {
		switch id {
		case "rewrite_shortcut":
			cfg.HotkeyRecord = value
		case "dictation_shortcut":
			cfg.DictationHotkey = value
		case "asr_deploy_dir":
			cfg.ASRDeployDir = value
		case "max_record_seconds":
			if cfg.MaxRecordSeconds, err = coreconfig.RecordingSeconds(value); err != nil {
				return cfg, err
			}
		}
	}
	return coreconfig.Normalize(cfg)
}

// ConnectionAction runs settings updates and diagnostics with exactly the same
// config, provider and probe owners called by the Windows settings window.
// The caller owns the configuration file and native widgets.
func ConnectionAction(ctx context.Context, raw []byte) (any, error) {
	var req struct {
		Action string            `json:"action"`
		Config json.RawMessage   `json:"config"`
		Values map[string]string `json:"values"`
		Kind   string            `json:"kind"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	if len(req.Config) == 0 {
		return nil, errors.New("missing settings configuration")
	}
	cfg, err := coreconfig.Parse(req.Config)
	if err != nil {
		return nil, err
	}
	if req.Action == "form" {
		directory := cfg.ASRDeployDir
		if directory == "" {
			directory = localasr.DefaultDirectory()
		}
		return map[string]any{"form": ConnectionForm(), "values": map[string]string{
			"asr_base_url": cfg.ASRBaseURL, "asr_model": cfg.ASRModel, "asr_api_key": cfg.ASRAPIKey,
			"llm_base_url": cfg.LLMBaseURL, "text_model": cfg.TextModel, "llm_api_key": cfg.LLMAPIKey,
			"rewrite_shortcut": cfg.HotkeyRecord, "dictation_shortcut": cfg.DictationHotkey,
			"max_record_seconds": strconv.Itoa(cfg.MaxRecordSeconds),
			"asr_deploy_dir":     directory,
		}}, nil
	}
	cfg, err = Update(cfg, req.Values)
	if err != nil {
		return nil, err
	}
	switch req.Action {
	case "save":
		data, err := coreconfig.Marshal(cfg)
		if err != nil {
			return nil, err
		}
		return map[string]string{"config": string(data), "status": ux.WindowsSaveImmediate}, nil
	case "probe", "discover":
		var base, key, model string
		switch req.Kind {
		case "ASR":
			base, key, model = cfg.ASRBaseURL, cfg.ASRAPIKey, cfg.ASRModel
		case "LLM":
			base, key, model = cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.TextModel
		default:
			return nil, errors.New("unknown service")
		}
		if req.Action == "probe" {
			return map[string]string{"status": serviceprobe.ProbeModel(ctx, req.Kind, base, key, model)}, nil
		}
		models, err := provider.ListModels(ctx, base, key)
		if err != nil {
			return nil, err
		}
		if len(models) > MaxModelMenuChoices {
			models = models[:MaxModelMenuChoices]
		}
		if len(models) == 0 {
			return map[string]any{"models": models, "status": ux.NoModelsFound}, nil
		}
		return map[string]any{"models": models}, nil
	default:
		return nil, errors.New("unknown settings action")
	}
}
