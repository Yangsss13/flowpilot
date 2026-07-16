package knowledge

import (
	"archive/zip"
	"bufio"
	"bytes"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type FormatSpec struct {
	MediaType string
	Archive   bool
	Kind      string
}

var SupportedFormats = map[string]FormatSpec{
	".txt":  {MediaType: "text/plain", Kind: "document"},
	".md":   {MediaType: "text/markdown", Kind: "document"},
	".pdf":  {MediaType: "application/pdf", Kind: "document"},
	".docx": {MediaType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", Archive: true, Kind: "document"},
	".pptx": {MediaType: "application/vnd.openxmlformats-officedocument.presentationml.presentation", Archive: true, Kind: "document"},
	".mp3":  {MediaType: "audio/mpeg", Kind: "audio"},
	".wav":  {MediaType: "audio/wav", Kind: "audio"},
	".m4a":  {MediaType: "audio/mp4", Kind: "audio"},
	".mp4":  {MediaType: "video/mp4", Kind: "video"},
	".mov":  {MediaType: "video/quicktime", Kind: "video"},
	".webm": {MediaType: "video/webm", Kind: "video"},
}

type ArchiveLimits struct {
	MaxFiles     int
	MaxBytes     int64
	MaxRatio     int
	MaxPathDepth int
	MaxPPTSlides int
}

func ValidateUploadName(filename string) (string, string, error) {
	filename = strings.TrimSpace(filename)
	if filename == "" || len(filename) > 255 || strings.ContainsRune(filename, 0) ||
		strings.ContainsAny(filename, `/\`) || filepath.IsAbs(filename) || filename == "." || filename == ".." {
		return "", "", fmt.Errorf("%w: filename is invalid", ErrInvalidInput)
	}
	extension := strings.ToLower(filepath.Ext(filename))
	if _, ok := SupportedFormats[extension]; !ok {
		return "", "", fmt.Errorf("%w: unsupported file format", ErrInvalidInput)
	}
	return filename, extension, nil
}

func ValidateFile(reader ReadSeekCloserAt, size int64, extension, declaredType string, limits ArchiveLimits) (string, error) {
	if reader == nil || size <= 0 {
		return "", fmt.Errorf("%w: file is empty", ErrInvalidInput)
	}
	spec, ok := SupportedFormats[extension]
	if !ok {
		return "", fmt.Errorf("%w: unsupported file format", ErrInvalidInput)
	}
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("seek uploaded file: %w", err)
	}
	header := make([]byte, min(size, 512))
	if _, err := io.ReadFull(reader, header); err != nil && err != io.ErrUnexpectedEOF {
		return "", fmt.Errorf("read uploaded file signature: %w", err)
	}
	detected := http.DetectContentType(header)
	declared, _, _ := mime.ParseMediaType(declaredType)
	if !mimeMatches(extension, declared, detected, header) {
		return "", fmt.Errorf("%w: file content does not match its extension or MIME type", ErrInvalidInput)
	}
	if spec.Archive {
		if err := validateOfficeArchive(reader, size, extension, limits); err != nil {
			return "", err
		}
	} else if extension == ".txt" || extension == ".md" {
		if err := validateUTF8(reader); err != nil {
			return "", err
		}
	}
	return spec.MediaType, nil
}

func mimeMatches(extension, declared, detected string, header []byte) bool {
	if extension == ".pdf" {
		return bytes.HasPrefix(header, []byte("%PDF-")) &&
			(declared == "" || declared == "application/pdf" || declared == "application/octet-stream")
	}
	if extension == ".docx" || extension == ".pptx" {
		return bytes.HasPrefix(header, []byte("PK\x03\x04")) &&
			(declared == "" || declared == SupportedFormats[extension].MediaType || declared == "application/zip" || declared == "application/octet-stream")
	}
	if extension == ".wav" {
		return len(header) >= 12 && bytes.Equal(header[:4], []byte("RIFF")) && bytes.Equal(header[8:12], []byte("WAVE")) &&
			declaredMediaAllowed(declared, "audio/wav", "audio/x-wav", "audio/wave")
	}
	if extension == ".mp3" {
		mp3Signature := bytes.HasPrefix(header, []byte("ID3")) || (len(header) >= 2 && header[0] == 0xff && header[1]&0xe0 == 0xe0)
		return mp3Signature && declaredMediaAllowed(declared, "audio/mpeg", "audio/mp3")
	}
	if extension == ".m4a" || extension == ".mp4" || extension == ".mov" {
		isISOBaseMedia := len(header) >= 12 && bytes.Equal(header[4:8], []byte("ftyp"))
		if extension == ".mov" && len(header) >= 8 {
			atom := string(header[4:8])
			isISOBaseMedia = isISOBaseMedia || atom == "moov" || atom == "mdat" || atom == "wide"
		}
		allowed := []string{SupportedFormats[extension].MediaType}
		if extension == ".m4a" {
			allowed = append(allowed, "audio/x-m4a")
		}
		return isISOBaseMedia && declaredMediaAllowed(declared, allowed...)
	}
	if extension == ".webm" {
		return len(header) >= 4 && bytes.Equal(header[:4], []byte{0x1a, 0x45, 0xdf, 0xa3}) && declaredMediaAllowed(declared, "video/webm")
	}
	textDetected := strings.HasPrefix(detected, "text/plain") || detected == "application/octet-stream"
	declaredAllowed := declared == "" || declared == "text/plain" || declared == "text/markdown" || declared == "application/octet-stream"
	return textDetected && declaredAllowed && !bytes.Contains(header, []byte{0})
}

func declaredMediaAllowed(declared string, allowed ...string) bool {
	if declared == "" || declared == "application/octet-stream" {
		return true
	}
	for _, value := range allowed {
		if declared == value {
			return true
		}
	}
	return false
}

func IsMediaFormat(extension string) bool {
	spec, ok := SupportedFormats[strings.ToLower(extension)]
	return ok && (spec.Kind == "audio" || spec.Kind == "video")
}

func IsVideoFormat(extension string) bool {
	spec, ok := SupportedFormats[strings.ToLower(extension)]
	return ok && spec.Kind == "video"
}

func validateUTF8(reader io.ReadSeeker) error {
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("seek text file: %w", err)
	}
	scanner := bufio.NewScanner(reader)
	buffer := make([]byte, 64<<10)
	scanner.Buffer(buffer, 1<<20)
	for scanner.Scan() {
		if !utf8.Valid(scanner.Bytes()) {
			return fmt.Errorf("%w: text document must be UTF-8", ErrInvalidInput)
		}
	}
	if err := scanner.Err(); err != nil {
		if strings.Contains(err.Error(), "token too long") {
			// A long UTF-8 line is valid; validate it by streaming below.
			if _, seekErr := reader.Seek(0, io.SeekStart); seekErr != nil {
				return fmt.Errorf("seek text file: %w", seekErr)
			}
			data, readErr := io.ReadAll(reader)
			if readErr != nil || !utf8.Valid(data) {
				return fmt.Errorf("%w: text document must be UTF-8", ErrInvalidInput)
			}
			return nil
		}
		return fmt.Errorf("read text file: %w", err)
	}
	return nil
}

func validateOfficeArchive(reader io.ReaderAt, size int64, extension string, limits ArchiveLimits) error {
	archive, err := zip.NewReader(reader, size)
	if err != nil {
		return fmt.Errorf("%w: invalid Office archive", ErrInvalidInput)
	}
	if len(archive.File) == 0 || len(archive.File) > limits.MaxFiles {
		return fmt.Errorf("%w: Office archive contains too many files", ErrInvalidInput)
	}
	var total uint64
	foundMarker := false
	slides := 0
	for _, file := range archive.File {
		clean := path.Clean(strings.ReplaceAll(file.Name, "\\", "/"))
		if clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) || strings.ContainsRune(clean, 0) {
			return fmt.Errorf("%w: Office archive contains an unsafe path", ErrInvalidInput)
		}
		if strings.Count(clean, "/")+1 > limits.MaxPathDepth {
			return fmt.Errorf("%w: Office archive nesting is too deep", ErrInvalidInput)
		}
		lower := strings.ToLower(clean)
		if nestedArchiveExtension(path.Ext(lower)) {
			return fmt.Errorf("%w: nested archives are not allowed", ErrInvalidInput)
		}
		total += file.UncompressedSize64
		if total > uint64(limits.MaxBytes) {
			return fmt.Errorf("%w: Office archive expands beyond the configured limit", ErrInvalidInput)
		}
		if file.CompressedSize64 > 0 && file.UncompressedSize64/file.CompressedSize64 > uint64(limits.MaxRatio) {
			return fmt.Errorf("%w: Office archive compression ratio is unsafe", ErrInvalidInput)
		}
		if extension == ".docx" && lower == "word/document.xml" {
			foundMarker = true
		}
		if extension == ".pptx" && strings.HasPrefix(lower, "ppt/slides/slide") && strings.HasSuffix(lower, ".xml") {
			foundMarker = true
			slides++
		}
	}
	if !foundMarker {
		return fmt.Errorf("%w: archive does not contain the expected Office document", ErrInvalidInput)
	}
	if extension == ".pptx" && slides > limits.MaxPPTSlides {
		return fmt.Errorf("%w: presentation has too many slides", ErrInvalidInput)
	}
	return nil
}

func nestedArchiveExtension(extension string) bool {
	switch extension {
	case ".zip", ".rar", ".7z", ".tar", ".gz", ".bz2", ".xz":
		return true
	default:
		return false
	}
}
