package rag

import (
	"fmt"
	"strings"
)

func ChunkText(text string, size, overlap int) ([]Chunk, error) {
	if size <= 0 {
		return nil, fmt.Errorf("chunk size must be positive")
	}
	if overlap < 0 || overlap >= size {
		return nil, fmt.Errorf("chunk overlap must be between 0 and size-1")
	}
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if text == "" {
		return nil, fmt.Errorf("document is empty")
	}
	runes := []rune(text)
	step := size - overlap
	chunks := make([]Chunk, 0, (len(runes)+step-1)/step)
	for start := 0; start < len(runes); start += step {
		end := min(start+size, len(runes))
		value := strings.TrimSpace(string(runes[start:end]))
		if value != "" {
			chunks = append(chunks, Chunk{Index: len(chunks), Text: value})
		}
		if end == len(runes) {
			break
		}
	}
	return chunks, nil
}
