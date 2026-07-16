package knowledge

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Yangsss13/flowpilot/internal/config"
)

func TestFFmpegAndTesseractRuntimeIntegration(t *testing.T) {
	sample := os.Getenv("FLOWPILOT_MEDIA_SAMPLE")
	if sample == "" {
		t.Skip("set FLOWPILOT_MEDIA_SAMPLE to run local media runtime integration")
	}
	cfg := config.Load().Knowledge
	processor := NewFFmpegProcessor(cfg, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	info, err := processor.Probe(ctx, sample, filepath.Ext(sample))
	if err != nil {
		t.Fatalf("probe media: %v", err)
	}
	if !info.HasVideo || !info.HasAudio || info.DurationMS <= 0 {
		t.Fatalf("media info = %#v", info)
	}
	directory := t.TempDir()
	audio := filepath.Join(directory, "audio.wav")
	if err := processor.ExtractAudio(ctx, sample, audio); err != nil {
		t.Fatalf("extract audio: %v", err)
	}
	frames, err := processor.ExtractKeyframes(ctx, sample, filepath.Join(directory, "frames"), info.DurationMS)
	if err != nil || len(frames) == 0 {
		t.Fatalf("frames=%#v error=%v", frames, err)
	}
	ocr := NewTesseractOCR(cfg, nil)
	if _, err := ocr.Extract(ctx, frames[0].Path); err != nil {
		t.Fatalf("OCR keyframe: %v", err)
	}
}

func TestWhisperRuntimeIntegration(t *testing.T) {
	sample := os.Getenv("FLOWPILOT_ASR_SAMPLE")
	if sample == "" {
		t.Skip("set FLOWPILOT_ASR_SAMPLE to run local Whisper integration")
	}
	cfg := config.Load().Knowledge
	transcriber := NewWhisperCPPTranscriber(cfg, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	segments, err := transcriber.Transcribe(ctx, sample, t.TempDir())
	if err != nil {
		t.Fatalf("transcribe local speech: %v", err)
	}
	if len(segments) == 0 || segments[0].EndMS <= segments[0].StartMS || segments[0].Text == "" {
		t.Fatalf("segments = %#v", segments)
	}
}
