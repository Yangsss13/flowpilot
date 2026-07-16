package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/Yangsss13/flowpilot/internal/config"
	"github.com/Yangsss13/flowpilot/internal/domain"
	"github.com/Yangsss13/flowpilot/internal/rag"
)

const MediaPipelineVersion = "flowpilot-media-v1"

type MediaProgress func(stage domain.IngestionStage, progress int) error

type MediaInput struct {
	Job       domain.IngestionJob
	Document  domain.Document
	Version   domain.DocumentVersion
	Path      string
	Extension string
}

type FrameObservation struct {
	TimeMS int64
	Text   string
}

type MediaPipeline struct {
	storage     ObjectStorage
	repository  *GormRepository
	processor   MediaProcessor
	transcriber Transcriber
	ocr         OCRExtractor
	config      config.KnowledgeConfig
	limiter     *limiter
}

func NewMediaPipeline(storage ObjectStorage, repository *GormRepository, processor MediaProcessor, transcriber Transcriber, ocr OCRExtractor, cfg config.KnowledgeConfig) *MediaPipeline {
	return &MediaPipeline{
		storage: storage, repository: repository, processor: processor,
		transcriber: transcriber, ocr: ocr, config: cfg, limiter: newLimiter(cfg.MediaConcurrency),
	}
}

func (p *MediaPipeline) Process(ctx context.Context, input MediaInput, progress MediaProgress) ([]rag.Chunk, error) {
	if err := p.limiter.acquire(ctx); err != nil {
		return nil, err
	}
	defer p.limiter.release()
	if err := p.CleanupVersionArtifacts(ctx, input.Version.ID); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(p.config.MediaTempDir, 0o700); err != nil {
		return nil, fmt.Errorf("create configured media temp directory: %w", err)
	}
	temporaryDir, err := os.MkdirTemp(p.config.MediaTempDir, "flowpilot-media-*")
	if err != nil {
		return nil, fmt.Errorf("create media workspace: %w", err)
	}
	defer os.RemoveAll(temporaryDir)
	succeeded := false
	defer func() {
		if !succeeded {
			_ = p.CleanupVersionArtifacts(context.Background(), input.Version.ID)
		}
	}()

	if err := progress(domain.IngestionStageProbe, 15); err != nil {
		return nil, err
	}
	info, err := p.processor.Probe(ctx, input.Path, input.Extension)
	if err != nil {
		return nil, err
	}
	var transcript []TranscriptSegment
	if info.HasAudio {
		if err := progress(domain.IngestionStageExtractAudio, 25); err != nil {
			return nil, err
		}
		audioPath := filepath.Join(temporaryDir, "audio.wav")
		if err := p.processor.ExtractAudio(ctx, input.Path, audioPath); err != nil {
			return nil, err
		}
		if err := p.storeFileArtifact(ctx, input, domain.ArtifactAudio, audioPath, "audio/wav", 0, info.DurationMS); err != nil {
			return nil, err
		}
		if err := progress(domain.IngestionStageTranscribe, 40); err != nil {
			return nil, err
		}
		transcript, err = p.transcriber.Transcribe(ctx, audioPath, temporaryDir)
		if err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(transcript)
		if err != nil {
			return nil, fmt.Errorf("encode media transcript: %w", err)
		}
		if err := p.storeBytesArtifact(ctx, input, domain.ArtifactTranscript, ".json", "application/json", encoded, 0, info.DurationMS); err != nil {
			return nil, err
		}
	}

	var observations []FrameObservation
	if info.HasVideo && IsVideoFormat(input.Extension) {
		if err := progress(domain.IngestionStageKeyframes, 55); err != nil {
			return nil, err
		}
		frames, err := p.processor.ExtractKeyframes(ctx, input.Path, filepath.Join(temporaryDir, "frames"), info.DurationMS)
		if err != nil {
			return nil, err
		}
		for _, frame := range frames {
			if err := p.storeFileArtifact(ctx, input, domain.ArtifactKeyframe, frame.Path, "image/jpeg", frame.TimeMS, frame.TimeMS); err != nil {
				return nil, err
			}
		}
		if err := progress(domain.IngestionStageOCR, 65); err != nil {
			return nil, err
		}
		observations, err = p.ocrFrames(ctx, frames)
		if err != nil {
			return nil, err
		}
		if err := progress(domain.IngestionStageOCR, 75); err != nil {
			return nil, err
		}
	}
	if err := progress(domain.IngestionStageMerge, 78); err != nil {
		return nil, err
	}
	chunks, err := MergeMediaChunks(transcript, observations, p.config.ChunkMaxRunes, p.config.KeyframeInterval.Milliseconds())
	if err != nil {
		return nil, err
	}
	succeeded = true
	return chunks, nil
}

func (p *MediaPipeline) ocrFrames(ctx context.Context, frames []Keyframe) ([]FrameObservation, error) {
	results := make([]FrameObservation, len(frames))
	var wait sync.WaitGroup
	var firstErr error
	var mu sync.Mutex
	for index, frame := range frames {
		index, frame := index, frame
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, err := p.ocr.Extract(ctx, frame.Path)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}
			results[index] = FrameObservation{TimeMS: frame.TimeMS, Text: strings.TrimSpace(result.Text)}
		}()
	}
	wait.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	filtered := results[:0]
	for _, result := range results {
		if result.Text != "" {
			filtered = append(filtered, result)
		}
	}
	return filtered, nil
}

func (p *MediaPipeline) storeFileArtifact(ctx context.Context, input MediaInput, kind domain.ArtifactKind, path, mediaType string, startMS, endMS int64) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open derived media artifact: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat derived media artifact: %w", err)
	}
	object, err := p.storage.Put(ctx, strings.ToLower(filepath.Ext(path)), file, max(info.Size(), 1))
	if err != nil {
		return err
	}
	return p.recordArtifact(ctx, input, kind, mediaType, object, startMS, endMS)
}

func (p *MediaPipeline) storeBytesArtifact(ctx context.Context, input MediaInput, kind domain.ArtifactKind, extension, mediaType string, content []byte, startMS, endMS int64) error {
	object, err := p.storage.Put(ctx, extension, bytes.NewReader(content), int64(max(len(content), 1)))
	if err != nil {
		return err
	}
	return p.recordArtifact(ctx, input, kind, mediaType, object, startMS, endMS)
}

func (p *MediaPipeline) recordArtifact(ctx context.Context, input MediaInput, kind domain.ArtifactKind, mediaType string, object StoredObject, startMS, endMS int64) error {
	artifact := domain.DocumentArtifact{
		DocumentID: input.Document.ID, VersionID: input.Version.ID, JobID: input.Job.ID,
		Kind: kind, StorageKey: object.Key, MediaType: mediaType, SizeBytes: object.Size,
		Checksum: object.Checksum, StartMS: startMS, EndMS: endMS,
	}
	if err := p.repository.CreateArtifact(ctx, &artifact); err != nil {
		_ = p.storage.Delete(context.Background(), object.Key)
		return err
	}
	return nil
}

func (p *MediaPipeline) CleanupVersionArtifacts(ctx context.Context, versionID uint64) error {
	artifacts, err := p.repository.ListArtifactsForVersion(ctx, versionID)
	if err != nil {
		return err
	}
	for _, artifact := range artifacts {
		if err := p.storage.Delete(ctx, artifact.StorageKey); err != nil {
			return err
		}
	}
	return p.repository.DeleteArtifactRecords(ctx, versionID)
}

func MergeMediaChunks(transcript []TranscriptSegment, observations []FrameObservation, maxRunes int, frameWindowMS int64) ([]rag.Chunk, error) {
	if maxRunes <= 0 {
		return nil, fmt.Errorf("media chunk size must be positive")
	}
	if frameWindowMS <= 0 {
		frameWindowMS = 20_000
	}
	var chunks []rag.Chunk
	for index := 0; index < len(transcript); {
		start := transcript[index].StartMS
		end := transcript[index].EndMS
		parts := []string{strings.TrimSpace(transcript[index].Text)}
		index++
		for index < len(transcript) {
			next := transcript[index]
			candidate := strings.Join(append(parts, strings.TrimSpace(next.Text)), " ")
			gap := next.StartMS - end
			boundary := next.EndMS-start > 45_000 || utf8.RuneCountInString(candidate) > maxRunes || gap > 3_000 ||
				(utf8.RuneCountInString(strings.Join(parts, " ")) >= 200 && sentenceEnded(parts[len(parts)-1]))
			if boundary {
				break
			}
			parts = append(parts, strings.TrimSpace(next.Text))
			end = next.EndMS
			index++
		}
		text := "语音：" + strings.Join(parts, " ")
		if visual := visualTextForRange(observations, start, end); visual != "" {
			text += "\n画面文字：" + visual
		}
		for _, part := range splitStructuredText(text, maxRunes) {
			chunks = append(chunks, rag.Chunk{Index: len(chunks), Text: part, StartMS: start, EndMS: end})
		}
	}
	if len(transcript) == 0 {
		for _, observation := range observations {
			for _, part := range splitStructuredText("画面文字："+observation.Text, maxRunes) {
				chunks = append(chunks, rag.Chunk{Index: len(chunks), Text: part, StartMS: observation.TimeMS, EndMS: observation.TimeMS + frameWindowMS})
			}
		}
	}
	if len(chunks) == 0 {
		return nil, fmt.Errorf("%w: media contains no searchable speech or visible text", ErrMediaInvalid)
	}
	return chunks, nil
}

func visualTextForRange(observations []FrameObservation, start, end int64) string {
	var values []string
	seen := make(map[string]struct{})
	for _, observation := range observations {
		if observation.TimeMS < start || observation.TimeMS > end || observation.Text == "" {
			continue
		}
		if _, exists := seen[observation.Text]; exists {
			continue
		}
		seen[observation.Text] = struct{}{}
		values = append(values, observation.Text)
	}
	return strings.Join(values, "；")
}

func sentenceEnded(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasSuffix(value, ".") || strings.HasSuffix(value, "。") || strings.HasSuffix(value, "!") || strings.HasSuffix(value, "！") ||
		strings.HasSuffix(value, "?") || strings.HasSuffix(value, "？")
}
