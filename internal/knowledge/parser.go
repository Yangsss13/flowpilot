package knowledge

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	pdflib "github.com/ledongthuc/pdf"
)

const ParserVersion = "flowpilot-parser-v2"
const maxParserOutputBytes = 64 << 20

type ParsedBlock struct {
	Text    string `json:"text"`
	Section string `json:"section,omitempty"`
	Page    int    `json:"page,omitempty"`
	Slide   int    `json:"slide,omitempty"`
}

type ParserLimits struct {
	MaxArchiveFiles int           `json:"max_archive_files"`
	MaxArchiveBytes int64         `json:"max_archive_bytes"`
	MaxArchiveRatio int           `json:"max_archive_ratio"`
	MaxArchiveDepth int           `json:"max_archive_depth"`
	MaxPDFPages     int           `json:"max_pdf_pages"`
	MaxPPTSlides    int           `json:"max_ppt_slides"`
	Timeout         time.Duration `json:"-"`
}

type Parser interface {
	Parse(ctx context.Context, path, extension string, limits ParserLimits) ([]ParsedBlock, error)
}

type SubprocessParser struct {
	executable string
}

func NewSubprocessParser() (*SubprocessParser, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("find parser executable: %w", err)
	}
	return &SubprocessParser{executable: executable}, nil
}

type parserRequest struct {
	Path      string       `json:"path"`
	Extension string       `json:"extension"`
	Limits    ParserLimits `json:"limits"`
}

func (p *SubprocessParser) Parse(ctx context.Context, path, extension string, limits ParserLimits) ([]ParsedBlock, error) {
	if limits.Timeout <= 0 {
		return nil, fmt.Errorf("parser timeout must be positive")
	}
	parseCtx, cancel := context.WithTimeout(ctx, limits.Timeout)
	defer cancel()
	requestBody, err := json.Marshal(parserRequest{Path: path, Extension: extension, Limits: limits})
	if err != nil {
		return nil, fmt.Errorf("encode parser request: %w", err)
	}
	command := exec.CommandContext(parseCtx, p.executable, "knowledge-parse")
	command.Stdin = bytes.NewReader(requestBody)
	command.Env = minimalParserEnvironment()
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open parser output: %w", err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start parser: %w", err)
	}
	output, readErr := io.ReadAll(io.LimitReader(stdout, maxParserOutputBytes+1))
	waitErr := command.Wait()
	if parseCtx.Err() != nil {
		return nil, fmt.Errorf("parser timed out: %w", parseCtx.Err())
	}
	if readErr != nil {
		return nil, fmt.Errorf("read parser output: %w", readErr)
	}
	if len(output) > maxParserOutputBytes {
		return nil, fmt.Errorf("parser output exceeds limit")
	}
	if waitErr != nil {
		return nil, fmt.Errorf("parser failed: %w", waitErr)
	}
	var blocks []ParsedBlock
	if err := json.Unmarshal(output, &blocks); err != nil {
		return nil, fmt.Errorf("decode parser output: %w", err)
	}
	if len(blocks) == 0 {
		return nil, fmt.Errorf("parser produced no text")
	}
	return blocks, nil
}

func minimalParserEnvironment() []string {
	keys := []string{"PATH", "Path", "SYSTEMROOT", "SystemRoot", "TEMP", "TMP"}
	values := make([]string, 0, len(keys))
	seen := make(map[string]struct{})
	for _, key := range keys {
		value := os.Getenv(key)
		lower := strings.ToLower(key)
		if value != "" {
			if _, exists := seen[lower]; !exists {
				values = append(values, key+"="+value)
				seen[lower] = struct{}{}
			}
		}
	}
	return values
}

// RunParserCommand is invoked before normal server initialization when the
// executable is started with the private knowledge-parse command.
func RunParserCommand(input io.Reader, output io.Writer) error {
	decoder := json.NewDecoder(io.LimitReader(input, 64<<10))
	decoder.DisallowUnknownFields()
	var request parserRequest
	if err := decoder.Decode(&request); err != nil {
		return fmt.Errorf("decode parser request: %w", err)
	}
	blocks, err := ParseFile(request.Path, request.Extension, request.Limits)
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(blocks)
}

func ParseFile(filename, extension string, limits ParserLimits) ([]ParsedBlock, error) {
	switch strings.ToLower(extension) {
	case ".txt":
		return parseText(filename)
	case ".md":
		return parseMarkdown(filename)
	case ".pdf":
		return parsePDF(filename, limits.MaxPDFPages)
	case ".docx":
		return parseDOCX(filename, limits)
	case ".pptx":
		return parsePPTX(filename, limits)
	default:
		return nil, fmt.Errorf("unsupported parser format")
	}
}

func parseText(filename string) ([]ParsedBlock, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("read text document: %w", err)
	}
	paragraphs := splitParagraphs(string(data))
	blocks := make([]ParsedBlock, 0, len(paragraphs))
	for _, paragraph := range paragraphs {
		blocks = append(blocks, ParsedBlock{Text: paragraph})
	}
	return requireBlocks(blocks)
}

func parseMarkdown(filename string) ([]ParsedBlock, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("open Markdown document: %w", err)
	}
	defer file.Close()
	var blocks []ParsedBlock
	var paragraph []string
	section := ""
	flush := func() {
		text := normalizeSpace(strings.Join(paragraph, "\n"))
		if text != "" {
			blocks = append(blocks, ParsedBlock{Text: text, Section: section})
		}
		paragraph = paragraph[:0]
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if heading, ok := markdownHeading(line); ok {
			flush()
			section = heading
			blocks = append(blocks, ParsedBlock{Text: heading, Section: section})
			continue
		}
		if line == "" {
			flush()
			continue
		}
		paragraph = append(paragraph, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read Markdown document: %w", err)
	}
	flush()
	return requireBlocks(blocks)
}

func markdownHeading(line string) (string, bool) {
	trimmed := strings.TrimLeft(line, "#")
	count := len(line) - len(trimmed)
	if count < 1 || count > 6 || len(trimmed) == 0 || !unicode.IsSpace(rune(trimmed[0])) {
		return "", false
	}
	value := strings.TrimSpace(trimmed)
	return value, value != ""
}

func parsePDF(filename string, maxPages int) ([]ParsedBlock, error) {
	file, reader, err := pdflib.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("open PDF: %w", err)
	}
	defer file.Close()
	pageCount := reader.NumPage()
	if pageCount <= 0 || pageCount > maxPages {
		return nil, fmt.Errorf("PDF page count exceeds limit")
	}
	blocks := make([]ParsedBlock, 0, pageCount)
	for number := 1; number <= pageCount; number++ {
		page := reader.Page(number)
		if page.V.IsNull() {
			continue
		}
		text, err := page.GetPlainText(nil)
		if err != nil {
			return nil, fmt.Errorf("extract PDF page %d: %w", number, err)
		}
		text = normalizeSpace(text)
		if text != "" {
			blocks = append(blocks, ParsedBlock{Text: text, Page: number})
		}
	}
	return requireBlocks(blocks)
}

func parseDOCX(filename string, limits ParserLimits) ([]ParsedBlock, error) {
	archive, err := zip.OpenReader(filename)
	if err != nil {
		return nil, fmt.Errorf("open DOCX: %w", err)
	}
	defer archive.Close()
	if err := validateZipFiles(archive.File, limits); err != nil {
		return nil, err
	}
	entry := zipEntry(archive.File, "word/document.xml")
	if entry == nil {
		return nil, fmt.Errorf("DOCX document body is missing")
	}
	stream, err := entry.Open()
	if err != nil {
		return nil, fmt.Errorf("open DOCX document body: %w", err)
	}
	defer stream.Close()
	return parseWordXML(stream)
}

func parseWordXML(input io.Reader) ([]ParsedBlock, error) {
	decoder := xml.NewDecoder(input)
	var blocks []ParsedBlock
	var text strings.Builder
	section := ""
	style := ""
	inParagraph := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("decode DOCX XML: %w", err)
		}
		switch value := token.(type) {
		case xml.StartElement:
			switch value.Name.Local {
			case "p":
				inParagraph = true
				text.Reset()
				style = ""
			case "pStyle":
				for _, attribute := range value.Attr {
					if attribute.Name.Local == "val" {
						style = attribute.Value
					}
				}
			case "t":
				var valueText string
				if err := decoder.DecodeElement(&valueText, &value); err != nil {
					return nil, fmt.Errorf("decode DOCX text: %w", err)
				}
				text.WriteString(valueText)
			case "tab":
				if inParagraph {
					text.WriteByte(' ')
				}
			}
		case xml.EndElement:
			if value.Name.Local == "p" && inParagraph {
				paragraph := normalizeSpace(text.String())
				if paragraph != "" {
					if strings.HasPrefix(strings.ToLower(style), "heading") || strings.HasPrefix(strings.ToLower(style), "title") {
						section = paragraph
					}
					blocks = append(blocks, ParsedBlock{Text: paragraph, Section: section})
				}
				inParagraph = false
			}
		}
	}
	return requireBlocks(blocks)
}

func parsePPTX(filename string, limits ParserLimits) ([]ParsedBlock, error) {
	archive, err := zip.OpenReader(filename)
	if err != nil {
		return nil, fmt.Errorf("open PPTX: %w", err)
	}
	defer archive.Close()
	if err := validateZipFiles(archive.File, limits); err != nil {
		return nil, err
	}
	var slideNames []string
	for _, file := range archive.File {
		lower := strings.ToLower(filepath.ToSlash(file.Name))
		if strings.HasPrefix(lower, "ppt/slides/slide") && strings.HasSuffix(lower, ".xml") {
			slideNames = append(slideNames, file.Name)
		}
	}
	sort.Slice(slideNames, func(i, j int) bool { return numberedXML(slideNames[i]) < numberedXML(slideNames[j]) })
	if len(slideNames) == 0 || len(slideNames) > limits.MaxPPTSlides {
		return nil, fmt.Errorf("PPTX slide count exceeds limit")
	}
	blocks := make([]ParsedBlock, 0, len(slideNames))
	for index, name := range slideNames {
		entry := zipEntry(archive.File, name)
		texts, err := extractXMLTexts(entry)
		if err != nil {
			return nil, fmt.Errorf("extract slide %d: %w", index+1, err)
		}
		noteName := fmt.Sprintf("ppt/notesSlides/notesSlide%d.xml", index+1)
		if note := zipEntry(archive.File, noteName); note != nil {
			noteTexts, noteErr := extractXMLTexts(note)
			if noteErr != nil {
				return nil, fmt.Errorf("extract slide %d notes: %w", index+1, noteErr)
			}
			texts = append(texts, noteTexts...)
		}
		if len(texts) == 0 {
			continue
		}
		section := texts[0]
		blocks = append(blocks, ParsedBlock{Text: normalizeSpace(strings.Join(texts, "\n")), Section: section, Slide: index + 1})
	}
	return requireBlocks(blocks)
}

func extractXMLTexts(entry *zip.File) ([]string, error) {
	if entry == nil {
		return nil, nil
	}
	stream, err := entry.Open()
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	decoder := xml.NewDecoder(stream)
	var texts []string
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return texts, nil
		}
		if err != nil {
			return nil, err
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "t" {
			continue
		}
		var value string
		if err := decoder.DecodeElement(&value, &start); err != nil {
			return nil, err
		}
		if value = normalizeSpace(value); value != "" {
			texts = append(texts, value)
		}
	}
}

func validateZipFiles(files []*zip.File, limits ParserLimits) error {
	if len(files) == 0 || len(files) > limits.MaxArchiveFiles {
		return fmt.Errorf("archive file count exceeds limit")
	}
	var total uint64
	for _, file := range files {
		total += file.UncompressedSize64
		if total > uint64(limits.MaxArchiveBytes) {
			return fmt.Errorf("archive expanded size exceeds limit")
		}
		if file.CompressedSize64 > 0 && file.UncompressedSize64/file.CompressedSize64 > uint64(limits.MaxArchiveRatio) {
			return fmt.Errorf("archive compression ratio exceeds limit")
		}
		clean := filepath.ToSlash(filepath.Clean(file.Name))
		if strings.HasPrefix(clean, "../") || strings.Count(clean, "/")+1 > limits.MaxArchiveDepth {
			return fmt.Errorf("archive path is unsafe")
		}
	}
	return nil
}

func zipEntry(files []*zip.File, name string) *zip.File {
	wanted := strings.ToLower(filepath.ToSlash(name))
	for _, file := range files {
		if strings.ToLower(filepath.ToSlash(file.Name)) == wanted {
			return file
		}
	}
	return nil
}

func numberedXML(name string) int {
	base := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	value, _ := strconv.Atoi(strings.TrimPrefix(strings.ToLower(base), "slide"))
	return value
}

func splitParagraphs(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	parts := strings.FieldsFunc(text, func(r rune) bool { return r == '\n' || r == '\r' })
	paragraphs := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := normalizeSpace(part); value != "" {
			paragraphs = append(paragraphs, value)
		}
	}
	return paragraphs
}

func normalizeSpace(value string) string {
	return strings.TrimSpace(strings.Join(strings.Fields(value), " "))
}

func requireBlocks(blocks []ParsedBlock) ([]ParsedBlock, error) {
	if len(blocks) == 0 {
		return nil, fmt.Errorf("document contains no extractable text")
	}
	return blocks, nil
}
