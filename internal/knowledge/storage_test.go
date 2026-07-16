package knowledge

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalObjectStorageSizeBoundaryAndGeneratedKey(t *testing.T) {
	storage, err := NewLocalObjectStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	object, err := storage.Put(context.Background(), ".txt", strings.NewReader("12345"), 5)
	if err != nil {
		t.Fatalf("put boundary file: %v", err)
	}
	if object.Size != 5 || object.Checksum == "" || strings.Contains(object.Key, "uploaded") || filepath.IsAbs(object.Key) {
		t.Fatalf("stored object = %#v", object)
	}
	_, err = storage.Put(context.Background(), ".txt", strings.NewReader("123456"), 5)
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("oversize error = %v, want invalid input", err)
	}
}

func TestLocalObjectStorageRejectsTraversalKey(t *testing.T) {
	storage, err := NewLocalObjectStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := storage.Open(context.Background(), "../outside.txt"); err == nil {
		t.Fatal("Open accepted traversal key")
	}
	if err := storage.Delete(context.Background(), "C:\\outside.txt"); err == nil {
		t.Fatal("Delete accepted absolute key")
	}
}

func TestValidateUploadNameRejectsClientPaths(t *testing.T) {
	for _, name := range []string{"../policy.md", `..\\policy.md`, "/tmp/policy.md", `C:\\tmp\\policy.md`, "bad\x00.md"} {
		if _, _, err := ValidateUploadName(name); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("ValidateUploadName(%q) error = %v", name, err)
		}
	}
}

func TestValidateFileRejectsMIMEDeception(t *testing.T) {
	file := writeTemporaryFile(t, []byte("not a PDF"), ".pdf")
	defer file.Close()
	if _, err := ValidateFile(file, 9, ".pdf", "application/pdf", testArchiveLimits()); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want invalid input", err)
	}
}

func TestValidateOfficeArchiveRejectsZipBombRatio(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bomb.docx")
	output, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(output)
	entry, err := writer.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(entry, bytes.NewReader(bytes.Repeat([]byte("A"), 1<<20)))
	_ = writer.Close()
	_ = output.Close()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	info, _ := file.Stat()
	limits := testArchiveLimits()
	limits.MaxRatio = 10
	if _, err := ValidateFile(file, info.Size(), ".docx", SupportedFormats[".docx"].MediaType, limits); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want unsafe archive rejection", err)
	}
}

func TestValidateDOCXChecksContainerMarker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fake.docx")
	output, _ := os.Create(path)
	writer := zip.NewWriter(output)
	entry, _ := writer.Create("ppt/slides/slide1.xml")
	_, _ = entry.Write([]byte(`<p:sld/>`))
	_ = writer.Close()
	_ = output.Close()
	file, _ := os.Open(path)
	defer file.Close()
	info, _ := file.Stat()
	if _, err := ValidateFile(file, info.Size(), ".docx", SupportedFormats[".docx"].MediaType, testArchiveLimits()); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want wrong container rejection", err)
	}
}

func writeTemporaryFile(t *testing.T, content []byte, extension string) *os.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input"+extension)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func testArchiveLimits() ArchiveLimits {
	return ArchiveLimits{MaxFiles: 100, MaxBytes: 10 << 20, MaxRatio: 100, MaxPathDepth: 10, MaxPPTSlides: 20}
}
