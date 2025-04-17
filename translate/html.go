package main

import (
	"io"
	"strings"

	"golang.org/x/net/html"
)

func isHTML(text string) bool {
	return strings.Contains(text, "<html") ||
		strings.Contains(text, "<p") ||
		strings.Contains(text, "<div") ||
		strings.Contains(text, "<span")
}

func getTextFromHTML(input string) ([]html.Token, []string, []int, error) {
	input = replaceNonUTF8Characters(input)
	var tokens []html.Token
	var texts []string
	var counts []int

	tokenizer := html.NewTokenizer(strings.NewReader(input))
	for {
		tt := tokenizer.Next()
		switch tt {
		case html.ErrorToken:
			if tokenizer.Err() == io.EOF {
				return tokens, texts, counts, nil
			}
			return nil, nil, nil, tokenizer.Err()
		case html.TextToken:
			text := strings.TrimSpace(string(tokenizer.Text()))
			if text != "" {
				sentences := splitSentences(text)
				texts = append(texts, sentences...)
				counts = append(counts, len(sentences))
			}
		}

		tokens = append(tokens, tokenizer.Token())
	}
}

func reconstructHTML(tokens []html.Token, translatedSentences []string, sentenceCounts []int) string {
	var sb strings.Builder
	textIndex := 0
	sentenceIndex := 0

	for _, token := range tokens {
		switch token.Type {
		case html.TextToken:
			count := sentenceCounts[sentenceIndex]
			for range count {
				if textIndex < len(translatedSentences) {
					sb.WriteString(translatedSentences[textIndex])
					textIndex++
				}
			}
			sentenceIndex++
		default:
			sb.WriteString(token.String())
		}
	}

	return sb.String()
}

func replaceNonUTF8Characters(text string) string {
	return strings.ToValidUTF8(text, "")
}
