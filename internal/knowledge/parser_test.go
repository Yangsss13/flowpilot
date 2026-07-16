package knowledge

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParsePDFPreservesPageNumber(t *testing.T) {
	path := filepath.Join(t.TempDir(), "document.pdf")
	if err := os.WriteFile(path, minimalPDF("Hello PDF"), 0o600); err != nil {
		t.Fatal(err)
	}
	blocks, err := ParseFile(path, ".pdf", ParserLimits{MaxPDFPages: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 1 || blocks[0].Page != 1 || !strings.Contains(blocks[0].Text, "Hello PDF") {
		t.Fatalf("blocks = %#v", blocks)
	}
}

func TestParseMarkdownPreservesSections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.md")
	if err := os.WriteFile(path, []byte("# Refunds\n\nReturn within 30 days.\n\n## Exceptions\n\nFinal sale."), 0o600); err != nil {
		t.Fatal(err)
	}
	blocks, err := ParseFile(path, ".md", ParserLimits{})
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 4 || blocks[1].Section != "Refunds" || blocks[3].Section != "Exceptions" {
		t.Fatalf("blocks = %#v", blocks)
	}
}

func TestParseDOCXPreservesHeading(t *testing.T) {
	path := filepath.Join(t.TempDir(), "document.docx")
	writeOfficeZip(t, path, map[string]string{
		"word/document.xml": `<w:document xmlns:w="w"><w:body><w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Policy</w:t></w:r></w:p><w:p><w:r><w:t>Refund in 30 days.</w:t></w:r></w:p></w:body></w:document>`,
	})
	blocks, err := ParseFile(path, ".docx", parserTestLimits())
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 2 || blocks[1].Section != "Policy" || blocks[1].Text != "Refund in 30 days." {
		t.Fatalf("blocks = %#v", blocks)
	}
}

func TestParsePPTXPreservesSlideAndNotes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "slides.pptx")
	writeOfficeZip(t, path, map[string]string{
		"ppt/slides/slide1.xml":           `<p:sld xmlns:p="p" xmlns:a="a"><a:t>Quarterly Results</a:t><a:t>Revenue grew.</a:t></p:sld>`,
		"ppt/notesSlides/notesSlide1.xml": `<p:notes xmlns:p="p" xmlns:a="a"><a:t>Mention APAC.</a:t></p:notes>`,
	})
	blocks, err := ParseFile(path, ".pptx", parserTestLimits())
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 1 || blocks[0].Slide != 1 || blocks[0].Section != "Quarterly Results" || !strings.Contains(blocks[0].Text, "Mention APAC") {
		t.Fatalf("blocks = %#v", blocks)
	}
}

func TestBuildChunksKeepsLocationAndRuneBoundary(t *testing.T) {
	chunks, err := BuildChunks([]ParsedBlock{{Text: strings.Repeat("知识", 10), Page: 3, Section: "退款"}}, 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("chunks = %#v", chunks)
	}
	for _, chunk := range chunks {
		if len([]rune(chunk.Text)) > 6 || chunk.Page != 3 || chunk.Section != "退款" {
			t.Fatalf("chunk = %#v", chunk)
		}
	}
}

func TestBuildChunksMergesAdjacentBlocksWithSameLocation(t *testing.T) {
	blocks := []ParsedBlock{
		{Text: "最终技术栈规划：", Section: "技术方案"},
		{Text: "Go、MySQL、Redis、RabbitMQ 和 Qdrant。", Section: "技术方案"},
		{Text: "第二页内容", Section: "技术方案", Page: 2},
	}
	chunks, err := BuildChunks(blocks, 80)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 2 || !strings.Contains(chunks[0].Text, "最终技术栈规划") || !strings.Contains(chunks[0].Text, "RabbitMQ") {
		t.Fatalf("chunks = %#v", chunks)
	}
	if chunks[1].Page != 2 || strings.Contains(chunks[1].Text, "最终技术栈规划") {
		t.Fatalf("location boundary was lost: %#v", chunks[1])
	}
}

func writeOfficeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	output, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(output)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = entry.Write([]byte(content))
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
}

func parserTestLimits() ParserLimits {
	return ParserLimits{MaxArchiveFiles: 100, MaxArchiveBytes: 10 << 20, MaxArchiveRatio: 100, MaxArchiveDepth: 10, MaxPDFPages: 100, MaxPPTSlides: 20}
}

func minimalPDF(text string) []byte {
	var output bytes.Buffer
	output.WriteString("%PDF-1.4\n")
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len("BT /F1 12 Tf 72 720 Td ("+text+") Tj ET"), "BT /F1 12 Tf 72 720 Td ("+text+") Tj ET"),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	offsets := make([]int, len(objects)+1)
	for index, object := range objects {
		offsets[index+1] = output.Len()
		fmt.Fprintf(&output, "%d 0 obj\n%s\nendobj\n", index+1, object)
	}
	xref := output.Len()
	fmt.Fprintf(&output, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for index := 1; index <= len(objects); index++ {
		fmt.Fprintf(&output, "%010d 00000 n \n", offsets[index])
	}
	fmt.Fprintf(&output, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return output.Bytes()
}
