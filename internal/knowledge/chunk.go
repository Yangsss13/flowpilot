package knowledge

import (
	"fmt"
	"strings"

	"github.com/Yangsss13/flowpilot/internal/rag"
)

// BuildChunks keeps source-location metadata attached to every chunk. Adjacent
// short blocks with the same page/slide/section are merged so headings and
// their paragraphs remain useful retrieval context. Different locations are
// never merged, so citations remain meaningful.
func BuildChunks(blocks []ParsedBlock, maxRunes int) ([]rag.Chunk, error) {
	if maxRunes <= 0 {
		return nil, fmt.Errorf("chunk size must be positive")
	}
	var chunks []rag.Chunk
	var pending string
	var location ParsedBlock
	flush := func() {
		if pending == "" {
			return
		}
		chunks = append(chunks, rag.Chunk{
			Index: len(chunks), Text: pending, Section: location.Section,
			Page: location.Page, Slide: location.Slide,
		})
		pending = ""
	}
	for _, block := range blocks {
		text := strings.TrimSpace(block.Text)
		if text == "" {
			continue
		}
		for _, part := range splitStructuredText(text, maxRunes) {
			if pending == "" {
				pending = part
				location = block
				continue
			}
			sameLocation := location.Section == block.Section && location.Page == block.Page && location.Slide == block.Slide
			if sameLocation && len([]rune(pending))+1+len([]rune(part)) <= maxRunes {
				pending += "\n" + part
				continue
			}
			flush()
			pending = part
			location = block
		}
	}
	flush()
	if len(chunks) == 0 {
		return nil, fmt.Errorf("document contains no text chunks")
	}
	return chunks, nil
}

func splitStructuredText(text string, maxRunes int) []string {
	runes := []rune(text)
	var parts []string
	for len(runes) > maxRunes {
		cut := maxRunes
		for index := maxRunes; index > maxRunes/2; index-- {
			if runes[index-1] == '\n' || runes[index-1] == '。' || runes[index-1] == '.' || runes[index-1] == '！' || runes[index-1] == '？' {
				cut = index
				break
			}
		}
		if value := strings.TrimSpace(string(runes[:cut])); value != "" {
			parts = append(parts, value)
		}
		runes = runes[cut:]
	}
	if value := strings.TrimSpace(string(runes)); value != "" {
		parts = append(parts, value)
	}
	return parts
}
