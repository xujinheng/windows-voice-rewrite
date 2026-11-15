package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMeetingSegmenter(t *testing.T) {
	var seg meetingSegmenter
	voice := make([]byte, meetingFrameBytes)
	for i := 0; i < len(voice); i += 2 {
		binary.LittleEndian.PutUint16(voice[i:], 2500)
	}
	silence := make([]byte, meetingFrameBytes)
	var turns [][]byte
	for _, count := range []int{15, 15} {
		for i := 0; i < count; i++ {
			_, pcm := seg.push(voice)
			if pcm != nil {
				t.Fatal("speech ended before a pause")
			}
		}
		for i := 0; i < 25; i++ {
			_, pcm := seg.push(silence)
			if pcm != nil {
				turns = append(turns, pcm)
			}
		}
	}
	if len(turns) != 2 || len(turns[0]) < 15*meetingFrameBytes {
		t.Fatalf("got %d turns, want two short utterances", len(turns))
	}
}

func TestMeetingGate(t *testing.T) {
	for text, want := range map[string]bool{
		"Any thoughts": true, "Why?": true, "Can you elaborate": true,
		"yeah": false, "The build just finished.": false,
		"We'd like to get your take on this": true,
	} {
		if got := meetingNeedsReply(text); got != want {
			t.Errorf("%q: got %v, want %v", text, got, want)
		}
	}
}

func TestMeetingStream(t *testing.T) {
	var got []meetingLine
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"ONE\\tWhat changed?\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"\\t有什么变化？\\nTIP\\t确认改动\\n\"}}]}\n\n" +
		"data: [DONE]\n\n"
	err := readMeetingStream(strings.NewReader(body), func(line meetingLine) { got = append(got, line) })
	if err != nil || len(got) != 2 || got[0].english != "What changed?" || got[0].chinese != "有什么变化？" {
		t.Fatalf("got %#v, %v", got, err)
	}
}

func TestMeetingPCMConverter(t *testing.T) {
	var c meetingPCMConverter
	stereo := make([]byte, 44100*4)
	for i := 0; i < len(stereo); i += 4 {
		binary.LittleEndian.PutUint16(stereo[i:], 3000)
		binary.LittleEndian.PutUint16(stereo[i+2:], 1000)
	}
	var bytes int
	push := func(frame []byte) {
		bytes += len(frame)
		for i := 0; i < len(frame); i += 2 {
			if sample := binary.LittleEndian.Uint16(frame[i:]); sample != 2000 {
				t.Fatalf("mono sample = %d, want 2000", sample)
			}
		}
	}
	c.consume(stereo[:124], 31, push)
	c.consume(stereo[124:], 44100-31, push)
	if bytes != pcmBytesPerSecond {
		t.Fatalf("output = %d bytes, want %d", bytes, pcmBytesPerSecond)
	}
}

func TestMeetingCaptionPlainText(t *testing.T) {
	for input, want := range map[string]string{
		"这是翻译。":                         "这是翻译。",
		"ZH<TAB>这是翻译。":                  "这是翻译。",
		"<think>reasoning</think>这是翻译。": "这是翻译。",
		"<think>unfinished":             "",
	} {
		if got := cleanMeetingCaption(input); got != want {
			t.Errorf("%q: got %q, want %q", input, got, want)
		}
	}
}

func TestMeetingPreviewDuringContinuousSpeech(t *testing.T) {
	var s meetingSegmenter
	frame := make([]byte, meetingFrameBytes)
	for i := 0; i < len(frame); i += 2 {
		binary.LittleEndian.PutUint16(frame[i:], 2000)
	}
	var previews int
	var first []byte
	for i := 0; i < 200; i++ {
		_, final := s.push(frame)
		if final != nil {
			t.Fatal("continuous speech prematurely finalized")
		}
		if snapshot := s.preview(); snapshot != nil {
			previews++
			if first == nil {
				first = snapshot
			}
		}
	}
	if previews != 5 {
		t.Fatalf("want five previews every 0.8 seconds, got %d", previews)
	}
	first[0] = 0
	if s.segment[0] == 0 {
		t.Fatal("snapshot shares capture buffer")
	}
}

func TestMeetingCaptionEmitsBeforeStreamFinishes(t *testing.T) {
	received := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"你好\"}}]}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-received:
		case <-r.Context().Done():
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"世界\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var last string
	err := completeMeeting(ctx, Config{LLMBaseURL: server.URL, TextModel: "test"}, meetingQuery{Mode: "caption", Latest: "Hello world"}, func(line meetingLine) {
		last = line.chinese
		select {
		case received <- struct{}{}:
		default:
		}
	})
	if err != nil || last != "你好世界" {
		t.Fatalf("streamed caption = %q, error = %v", last, err)
	}
}
