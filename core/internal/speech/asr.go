package speech

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/xujinheng/windows-voice-rewrite/core/asrtext"
	"github.com/xujinheng/windows-voice-rewrite/core/audio"
	coreconfig "github.com/xujinheng/windows-voice-rewrite/core/config"
	"github.com/xujinheng/windows-voice-rewrite/core/provider"
	"github.com/xujinheng/windows-voice-rewrite/core/ux"
)

// OneClick and some llama.cpp/faster-whisper endpoints return JSON, while
// other OpenAI-compatible servers return plain text. Never silently treat
// malformed JSON as a dictated sentence.
func ParseASRResponse(data []byte, contentType string) (string, error) {
	var payload struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(data, &payload); err == nil {
		return payload.Text, nil
	}
	var rawText string
	if err := json.Unmarshal(data, &rawText); err == nil {
		return rawText, nil
	}
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(strings.ToLower(contentType), "text/plain") &&
		!strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") &&
		len(data) <= (3<<20)-1 {
		return trimmed, nil
	}
	return "", errors.New(ux.BadASRResponse)
}

func EncodeWAV(pcm []byte) ([]byte, error) {
	if len(pcm) == 0 || len(pcm)%2 != 0 {
		return nil, errors.New(ux.EmptyRecording)
	}
	if len(pcm) > audio.PCMBytesPerSecond*coreconfig.RecordSecondsMax {
		return nil, errors.New(ux.RecordingTooLong)
	}
	b := make([]byte, 44+len(pcm))
	copy(b[0:4], "RIFF")
	binary.LittleEndian.PutUint32(b[4:8], uint32(len(b)-8))
	copy(b[8:12], "WAVE")
	copy(b[12:16], "fmt ")
	binary.LittleEndian.PutUint32(b[16:20], 16)
	binary.LittleEndian.PutUint16(b[20:22], 1) // PCM
	binary.LittleEndian.PutUint16(b[22:24], 1) // mono
	binary.LittleEndian.PutUint32(b[24:28], audio.CaptureSampleRate)
	binary.LittleEndian.PutUint32(b[28:32], audio.PCMBytesPerSecond)
	binary.LittleEndian.PutUint16(b[32:34], 2)
	binary.LittleEndian.PutUint16(b[34:36], 16)
	copy(b[36:40], "data")
	binary.LittleEndian.PutUint32(b[40:44], uint32(len(pcm)))
	copy(b[44:], pcm)
	return b, nil
}

// One end-to-end ASR budget for both desktop hosts.
const TranscribeTimeout = 220 * time.Second

// Transcribe sends the exact multipart request expected by Handy-compatible ASR.
func Transcribe(ctx context.Context, cfg coreconfig.Config, pcm []byte) (string, error) {
	return TranscribeWithPrompt(ctx, cfg, pcm, "")
}

// TranscribeWithPrompt is for Meeting-only vocabulary hints. Ordinary
// dictation continues to send no Meeting profile to its ASR endpoint.
func TranscribeWithPrompt(ctx context.Context, cfg coreconfig.Config, pcm []byte, prompt string) (string, error) {
	chunks := audio.SplitPCMForASR(pcm)
	results := make([]string, 0, len(chunks))
	for i, chunk := range chunks {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		part, err := transcribeChunk(ctx, cfg, chunk, prompt)
		if err != nil {
			if len(chunks) > 1 {
				return "", fmt.Errorf("语音第 %d/%d 段识别失败: %w", i+1, len(chunks), err)
			}
			return "", err
		}
		results = append(results, part)
	}
	return audio.JoinASRChunks(results), nil
}

// transcribeChunk is one HTTP request; the session epoch's cancellation propagates
// through the request context to stop outstanding network I/O immediately.
func NewASRRequest(ctx context.Context, cfg coreconfig.Config, pcm []byte) (*http.Request, error) {
	return newASRRequestWithPrompt(ctx, cfg, pcm, "")
}

func newASRRequestWithPrompt(ctx context.Context, cfg coreconfig.Config, pcm []byte, prompt string) (*http.Request, error) {
	wav, err := EncodeWAV(pcm)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	w := multipart.NewWriter(&out)
	f, e := w.CreateFormFile("file", "voice.wav")
	if e != nil {
		return nil, e
	}
	if _, e = f.Write(wav); e != nil {
		return nil, e
	}
	if e = w.WriteField("model", cfg.ASRModel); e != nil {
		return nil, e
	}
	if e = w.WriteField("response_format", "json"); e != nil {
		return nil, e
	}
	if prompt != "" {
		if e = w.WriteField("prompt", prompt); e != nil {
			return nil, e
		}
	}
	if e = w.Close(); e != nil {
		return nil, e
	}
	req, e := http.NewRequestWithContext(ctx, "POST", provider.Endpoint(cfg.ASRBaseURL, "/audio/transcriptions"), &out)
	if e != nil {
		return nil, e
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	if cfg.ASRAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.ASRAPIKey)
	}
	return req, nil
}

func transcribeChunk(ctx context.Context, cfg coreconfig.Config, pcm []byte, prompt string) (string, error) {
	req, err := newASRRequestWithPrompt(ctx, cfg, pcm, prompt)
	if err != nil {
		return "", err
	}
	resp, e := provider.RequestClient(90 * time.Second).Do(req)
	if e != nil {
		return "", fmt.Errorf("语音转写请求失败（请检查 ASR API 地址及网络）: %w", e)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", provider.HTTPError("ASR", resp.StatusCode)
	}
	response, e := io.ReadAll(io.LimitReader(resp.Body, 3<<20))
	if e != nil {
		return "", fmt.Errorf("读取 ASR 响应失败: %w", e)
	}
	transcript, e := ParseASRResponse(response, resp.Header.Get("Content-Type"))
	if e != nil {
		return "", e
	}
	result := asrtext.Clean(transcript)
	if result == "" {
		return "", errors.New(ux.NoSpeech)
	}
	return result, nil
}
