package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Yangsss13/flowpilot/internal/config"
)

const maxMediaCommandOutput = 4 << 20

type MediaInfo struct {
	FormatName string
	DurationMS int64
	HasAudio   bool
	HasVideo   bool
	AudioCodec string
	VideoCodec string
	Width      int
	Height     int
}

type Keyframe struct {
	Path   string
	TimeMS int64
}

type MediaProcessor interface {
	Probe(ctx context.Context, path, extension string) (MediaInfo, error)
	ExtractAudio(ctx context.Context, input, output string) error
	ExtractKeyframes(ctx context.Context, input, outputDir string, durationMS int64) ([]Keyframe, error)
}

type FFmpegProcessor struct {
	ffmpegPath  string
	ffprobePath string
	runner      CommandRunner
	config      config.KnowledgeConfig
}

func NewFFmpegProcessor(cfg config.KnowledgeConfig, runner CommandRunner) *FFmpegProcessor {
	if runner == nil {
		runner = OSCommandRunner{}
	}
	return &FFmpegProcessor{ffmpegPath: cfg.FFmpegPath, ffprobePath: cfg.FFprobePath, runner: runner, config: cfg}
}

func CheckMediaRuntime(cfg config.KnowledgeConfig) error {
	if runtime.GOOS == "windows" {
		for name, path := range map[string]string{"whisper.cpp": cfg.WhisperPath, "Whisper model": cfg.WhisperModelPath, "media temp": cfg.MediaTempDir} {
			if !isASCII(path) {
				return fmt.Errorf("%s path must contain only ASCII characters on Windows", name)
			}
		}
	}
	for name, executable := range map[string]string{
		"ffmpeg": cfg.FFmpegPath, "ffprobe": cfg.FFprobePath,
		"whisper.cpp": cfg.WhisperPath, "tesseract": cfg.TesseractPath,
	} {
		if _, err := exec.LookPath(executable); err != nil {
			return fmt.Errorf("%s executable is unavailable", name)
		}
	}
	if cfg.WhisperModelPath == "" {
		return fmt.Errorf("WHISPER_MODEL_PATH is not configured")
	}
	info, err := os.Stat(cfg.WhisperModelPath)
	if err != nil || info.IsDir() || info.Size() < cfg.WhisperModelMinBytes {
		return fmt.Errorf("Whisper model is unavailable")
	}
	for _, language := range strings.Split(cfg.OCRLanguages, "+") {
		language = strings.TrimSpace(language)
		if language == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(cfg.TesseractDataDir, language+".traineddata")); err != nil {
			return fmt.Errorf("Tesseract language %s is unavailable", language)
		}
	}
	return nil
}

func isASCII(value string) bool {
	for _, character := range value {
		if character > 127 {
			return false
		}
	}
	return true
}

func (p *FFmpegProcessor) Probe(ctx context.Context, input, extension string) (MediaInfo, error) {
	probeCtx, cancel := context.WithTimeout(ctx, p.config.ProbeTimeout)
	defer cancel()
	output, err := p.runner.Run(probeCtx, p.ffprobePath, []string{
		"-v", "error", "-show_entries", "format=format_name,duration:stream=codec_type,codec_name,width,height",
		"-of", "json", input,
	}, maxMediaCommandOutput)
	if err != nil {
		return MediaInfo{}, err
	}
	info, err := parseProbeOutput(output)
	if err != nil {
		return MediaInfo{}, err
	}
	if err := p.validate(info, extension); err != nil {
		return MediaInfo{}, err
	}
	return info, nil
}

func (p *FFmpegProcessor) ExtractAudio(ctx context.Context, input, output string) error {
	commandCtx, cancel := context.WithTimeout(ctx, p.config.FFmpegTimeout)
	defer cancel()
	_, err := p.runner.Run(commandCtx, p.ffmpegPath, []string{
		"-nostdin", "-y", "-i", input, "-vn", "-ac", "1", "-ar", "16000",
		"-c:a", "pcm_s16le", "-threads", strconv.Itoa(p.config.FFmpegThreads), output,
	}, maxMediaCommandOutput)
	if err != nil {
		return fmt.Errorf("extract media audio: %w", err)
	}
	return nil
}

func (p *FFmpegProcessor) ExtractKeyframes(ctx context.Context, input, outputDir string, durationMS int64) ([]Keyframe, error) {
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return nil, fmt.Errorf("create keyframe directory: %w", err)
	}
	intervalSeconds := p.config.KeyframeInterval.Seconds()
	filter := fmt.Sprintf("select=eq(n\\,0)+gte(t-prev_selected_t\\,%.3f),scale=1280:-2:force_original_aspect_ratio=decrease,format=yuvj420p", intervalSeconds)
	pattern := filepath.Join(outputDir, "frame-%06d.jpg")
	commandCtx, cancel := context.WithTimeout(ctx, p.config.FFmpegTimeout)
	defer cancel()
	_, err := p.runner.Run(commandCtx, p.ffmpegPath, []string{
		"-nostdin", "-y", "-i", input, "-an", "-vf", filter,
		"-fps_mode", "vfr", "-frames:v", strconv.Itoa(p.config.MaxKeyframes), "-q:v", "3",
		"-threads", strconv.Itoa(p.config.FFmpegThreads), pattern,
	}, maxMediaCommandOutput)
	if err != nil {
		return nil, fmt.Errorf("extract media keyframes: %w", err)
	}
	paths, err := filepath.Glob(filepath.Join(outputDir, "frame-*.jpg"))
	if err != nil {
		return nil, fmt.Errorf("list media keyframes: %w", err)
	}
	sort.Strings(paths)
	frames := make([]Keyframe, 0, len(paths))
	for index, path := range paths {
		timestamp := int64(float64(index) * intervalSeconds * 1000)
		if timestamp > durationMS {
			timestamp = durationMS
		}
		frames = append(frames, Keyframe{Path: path, TimeMS: timestamp})
	}
	return frames, nil
}

func parseProbeOutput(output []byte) (MediaInfo, error) {
	var response struct {
		Format struct {
			FormatName string `json:"format_name"`
			Duration   string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			CodecType string `json:"codec_type"`
			CodecName string `json:"codec_name"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		return MediaInfo{}, fmt.Errorf("%w: ffprobe returned invalid JSON", ErrMediaInvalid)
	}
	duration, err := strconv.ParseFloat(response.Format.Duration, 64)
	if err != nil || duration <= 0 {
		return MediaInfo{}, fmt.Errorf("%w: media duration is missing", ErrMediaInvalid)
	}
	info := MediaInfo{FormatName: response.Format.FormatName, DurationMS: int64(duration * 1000)}
	for _, stream := range response.Streams {
		switch stream.CodecType {
		case "audio":
			if !info.HasAudio {
				info.HasAudio = true
				info.AudioCodec = stream.CodecName
			}
		case "video":
			if !info.HasVideo {
				info.HasVideo = true
				info.VideoCodec = stream.CodecName
				info.Width = stream.Width
				info.Height = stream.Height
			}
		}
	}
	return info, nil
}

func (p *FFmpegProcessor) validate(info MediaInfo, extension string) error {
	if info.DurationMS <= 0 || time.Duration(info.DurationMS)*time.Millisecond > p.config.MaxMediaDuration {
		return fmt.Errorf("%w: media duration exceeds the configured limit", ErrMediaInvalid)
	}
	extension = strings.ToLower(extension)
	if !probeFormatMatches(extension, info.FormatName) {
		return fmt.Errorf("%w: media container does not match its extension", ErrMediaInvalid)
	}
	if IsVideoFormat(extension) {
		if !info.HasVideo || !allowedVideoCodec(info.VideoCodec) {
			return fmt.Errorf("%w: video codec is not supported", ErrMediaInvalid)
		}
		if info.Width <= 0 || info.Height <= 0 || info.Width > p.config.MaxVideoWidth || info.Height > p.config.MaxVideoHeight {
			return fmt.Errorf("%w: video resolution exceeds the configured limit", ErrMediaInvalid)
		}
	} else if !info.HasAudio {
		return fmt.Errorf("%w: audio stream is required", ErrMediaInvalid)
	}
	if info.HasAudio && !allowedAudioCodec(info.AudioCodec) {
		return fmt.Errorf("%w: audio codec is not supported", ErrMediaInvalid)
	}
	return nil
}

func probeFormatMatches(extension, format string) bool {
	format = strings.ToLower(format)
	switch extension {
	case ".mp3":
		return strings.Contains(format, "mp3")
	case ".wav":
		return strings.Contains(format, "wav")
	case ".m4a", ".mp4", ".mov":
		return strings.Contains(format, "mov") || strings.Contains(format, "mp4") || strings.Contains(format, "m4a")
	case ".webm":
		return strings.Contains(format, "webm") || strings.Contains(format, "matroska")
	default:
		return false
	}
}

func allowedAudioCodec(codec string) bool {
	switch strings.ToLower(codec) {
	case "mp3", "aac", "alac", "opus", "vorbis", "flac", "pcm_u8", "pcm_s8",
		"pcm_s16le", "pcm_s16be", "pcm_s24le", "pcm_s24be", "pcm_s32le", "pcm_s32be",
		"pcm_f32le", "pcm_f64le", "pcm_alaw", "pcm_mulaw":
		return true
	default:
		return false
	}
}

func allowedVideoCodec(codec string) bool {
	switch strings.ToLower(codec) {
	case "h264", "hevc", "vp8", "vp9", "av1", "mpeg4", "prores", "mjpeg":
		return true
	default:
		return false
	}
}
