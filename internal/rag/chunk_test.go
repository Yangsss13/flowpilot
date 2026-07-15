package rag

import "testing"

func TestChunkTextUsesRuneWindowsWithOverlap(t *testing.T) {
	chunks, err := ChunkText("甲乙丙丁戊己", 4, 1)
	if err != nil {
		t.Fatalf("ChunkText() returned error: %v", err)
	}
	if len(chunks) != 2 || chunks[0].Text != "甲乙丙丁" || chunks[1].Text != "丁戊己" {
		t.Fatalf("chunks = %#v", chunks)
	}
}

func TestChunkTextRejectsInvalidConfigurationAndEmptyText(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		size    int
		overlap int
	}{
		{name: "empty", text: "  ", size: 10},
		{name: "size", text: "text", size: 0},
		{name: "negative overlap", text: "text", size: 10, overlap: -1},
		{name: "large overlap", text: "text", size: 10, overlap: 10},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ChunkText(test.text, test.size, test.overlap); err == nil {
				t.Fatal("ChunkText() returned nil error")
			}
		})
	}
}
