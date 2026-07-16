package knowledge

import (
	"bufio"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Yangsss13/flowpilot/internal/config"
)

type OCRResult struct {
	Text string
}

type OCRExtractor interface {
	Extract(ctx context.Context, imagePath string) (OCRResult, error)
}

type TesseractOCR struct {
	executable    string
	languages     string
	dataDir       string
	minConfidence int
	timeout       time.Duration
	runner        CommandRunner
	limiter       *limiter
}

func NewTesseractOCR(cfg config.KnowledgeConfig, runner CommandRunner) *TesseractOCR {
	if runner == nil {
		runner = OSCommandRunner{}
	}
	return &TesseractOCR{
		executable: cfg.TesseractPath, languages: cfg.OCRLanguages,
		dataDir:       cfg.TesseractDataDir,
		minConfidence: cfg.OCRMinConfidence, timeout: cfg.OCRTimeout,
		runner: runner, limiter: newLimiter(cfg.OCRConcurrency),
	}
}

func (o *TesseractOCR) Extract(ctx context.Context, imagePath string) (OCRResult, error) {
	if err := o.limiter.acquire(ctx); err != nil {
		return OCRResult{}, err
	}
	defer o.limiter.release()
	commandCtx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()
	arguments := []string{imagePath, "stdout", "-l", o.languages}
	if strings.TrimSpace(o.dataDir) != "" {
		arguments = append(arguments, "--tessdata-dir", o.dataDir)
	}
	arguments = append(arguments, "-c", "tessedit_create_tsv=1")
	output, err := o.runner.Run(commandCtx, o.executable, arguments, maxMediaCommandOutput)
	if err != nil {
		return OCRResult{}, fmt.Errorf("extract keyframe text: %w", err)
	}
	return OCRResult{Text: parseTesseractTSV(string(output), o.minConfidence)}, nil
}

func parseTesseractTSV(value string, minConfidence int) string {
	var words []string
	previous := ""
	scanner := bufio.NewScanner(strings.NewReader(value))
	for scanner.Scan() {
		columns := strings.Split(scanner.Text(), "\t")
		if len(columns) < 12 || columns[0] == "level" {
			continue
		}
		confidence, err := strconv.ParseFloat(columns[10], 64)
		text := strings.TrimSpace(columns[11])
		if err != nil || confidence < float64(minConfidence) || text == "" || text == previous {
			continue
		}
		words = append(words, text)
		previous = text
	}
	return strings.Join(words, " ")
}
