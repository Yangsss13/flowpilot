package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Yangsss13/flowpilot/internal/config"
)

const maxTranscriptBytes = 32 << 20

type TranscriptSegment struct {
	StartMS int64  `json:"start_ms"`
	EndMS   int64  `json:"end_ms"`
	Text    string `json:"text"`
}

type Transcriber interface {
	Transcribe(ctx context.Context, audioPath, temporaryDir string) ([]TranscriptSegment, error)
}

type WhisperCPPTranscriber struct {
	executable string
	model      string
	language   string
	threads    int
	timeout    time.Duration
	runner     CommandRunner
	limiter    *limiter
}

func NewWhisperCPPTranscriber(cfg config.KnowledgeConfig, runner CommandRunner) *WhisperCPPTranscriber {
	if runner == nil {
		runner = OSCommandRunner{}
	}
	return &WhisperCPPTranscriber{
		executable: cfg.WhisperPath, model: cfg.WhisperModelPath, language: cfg.WhisperLanguage,
		threads: cfg.WhisperThreads, timeout: cfg.ASRTimeout, runner: runner, limiter: newLimiter(cfg.ASRConcurrency),
	}
}

func (w *WhisperCPPTranscriber) Transcribe(ctx context.Context, audioPath, temporaryDir string) ([]TranscriptSegment, error) {
	if runtime.GOOS == "windows" && (!isASCII(w.executable) || !isASCII(w.model) || !isASCII(audioPath) || !isASCII(temporaryDir)) {
		return nil, fmt.Errorf("%w: whisper.cpp paths must contain only ASCII characters on Windows", ErrMediaRuntime)
	}
	if strings.TrimSpace(w.model) == "" {
		return nil, fmt.Errorf("%w: WHISPER_MODEL_PATH is not configured", ErrMediaRuntime)
	}
	if info, err := os.Stat(w.model); err != nil || info.IsDir() {
		return nil, fmt.Errorf("%w: Whisper model is unavailable", ErrMediaRuntime)
	}
	if err := w.limiter.acquire(ctx); err != nil {
		return nil, err
	}
	defer w.limiter.release()
	prefix := filepath.Join(temporaryDir, "transcript")
	commandCtx, cancel := context.WithTimeout(ctx, w.timeout)
	defer cancel()
	_, err := w.runner.Run(commandCtx, w.executable, []string{
		"-m", w.model, "-f", audioPath, "-l", w.language, "-t", strconv.Itoa(w.threads),
		"-oj", "-of", prefix,
	}, maxMediaCommandOutput)
	if err != nil {
		return nil, fmt.Errorf("transcribe media audio: %w", err)
	}
	file, err := os.Open(prefix + ".json")
	if err != nil {
		return nil, fmt.Errorf("open Whisper transcript: %w", err)
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxTranscriptBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read Whisper transcript: %w", err)
	}
	if len(content) > maxTranscriptBytes {
		return nil, fmt.Errorf("Whisper transcript exceeds output limit")
	}
	return parseWhisperJSON(content)
}

func parseWhisperJSON(content []byte) ([]TranscriptSegment, error) {
	var response struct {
		Transcription []struct {
			Timestamps struct {
				From string `json:"from"`
				To   string `json:"to"`
			} `json:"timestamps"`
			Offsets struct {
				From int64 `json:"from"`
				To   int64 `json:"to"`
			} `json:"offsets"`
			Text string `json:"text"`
		} `json:"transcription"`
		Segments []struct {
			Start float64 `json:"start"`
			End   float64 `json:"end"`
			Text  string  `json:"text"`
		} `json:"segments"`
	}
	if err := json.Unmarshal(content, &response); err != nil {
		return nil, fmt.Errorf("decode Whisper transcript: %w", err)
	}
	segments := make([]TranscriptSegment, 0, len(response.Transcription)+len(response.Segments))
	for _, value := range response.Transcription {
		start, end := value.Offsets.From, value.Offsets.To
		if end <= start {
			start = parseASRTimestamp(value.Timestamps.From)
			end = parseASRTimestamp(value.Timestamps.To)
		}
		if text := strings.TrimSpace(value.Text); text != "" && end >= start {
			segments = append(segments, TranscriptSegment{StartMS: start, EndMS: end, Text: text})
		}
	}
	for _, value := range response.Segments {
		if text := strings.TrimSpace(value.Text); text != "" && value.End >= value.Start {
			segments = append(segments, TranscriptSegment{StartMS: int64(value.Start * 1000), EndMS: int64(value.End * 1000), Text: text})
		}
	}
	if len(segments) == 0 {
		return nil, fmt.Errorf("%w: ASR produced no transcript", ErrMediaInvalid)
	}
	return segments, nil
}

func parseASRTimestamp(value string) int64 {
	value = strings.TrimSpace(strings.ReplaceAll(value, ",", "."))
	parts := strings.Split(value, ":")
	if len(parts) != 3 {
		return 0
	}
	hours, _ := strconv.ParseFloat(parts[0], 64)
	minutes, _ := strconv.ParseFloat(parts[1], 64)
	seconds, _ := strconv.ParseFloat(parts[2], 64)
	return int64((hours*3600 + minutes*60 + seconds) * 1000)
}
