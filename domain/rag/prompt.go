package rag

import (
	"fmt"
	"strings"

	"bridge-gateway/domain/chat"
)

const systemRules = `You answer strictly from the numbered passages supplied by the application.
Rules:
- Cite the passage that supports each claim using its number, for example [1].
- Never use knowledge that is not present in the passages.
- Never mix facts from two different documents without saying so.
- Answer the question actually asked, in the language of the question.
- Keep the answer to two or three sentences.
- If the passages do not cover the subject, say so explicitly.`

// BuildMessages builds the system/user pair sent to the chat model. The model
// never receives the bare question without the no-context instruction.
func BuildMessages(question string, retrieved []RetrievedChunk, extraSystem string) []chat.ChatMessage {
	system := systemRules
	if trimmed := strings.TrimSpace(extraSystem); trimmed != "" {
		system = trimmed + "\n\n" + system
	}
	return []chat.ChatMessage{
		{Role: "system", Content: system},
		{Role: "user", Content: userPrompt(question, retrieved)},
	}
}

func userPrompt(question string, retrieved []RetrievedChunk) string {
	question = strings.TrimSpace(question)
	if len(retrieved) == 0 {
		return "No passage matched this question. Say that the indexed documents do not cover it, then answer: " + question
	}
	var b strings.Builder
	b.WriteString("Passages:\n")
	for i, item := range retrieved {
		fmt.Fprintf(&b, "[%d] %s (passage %d)\n%s\n\n", i+1,
			item.Chunk.FileName, item.Chunk.Ordinal, flatten(item.Chunk.Text))
	}
	b.WriteString("Question: ")
	b.WriteString(question)
	return b.String()
}

func flatten(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if len([]rune(text)) <= MaxChunkChars {
		return text
	}
	return string([]rune(text)[:MaxChunkChars]) + "…"
}
