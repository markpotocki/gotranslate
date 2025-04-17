package main

import (
	"slices"
	"testing"

	"golang.org/x/net/html"
)

func TestIsHTML(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "HTML with <html> tag",
			input:    "<html><body>Hello</body></html>",
			expected: true,
		},
		{
			name:     "HTML with <p> tag",
			input:    "<p>Hello</p>",
			expected: true,
		},
		{
			name:     "HTML with <div> tag",
			input:    "<div>Hello</div>",
			expected: true,
		},
		{
			name:     "HTML with <span> tag",
			input:    "<span>Hello</span>",
			expected: true,
		},
		{
			name:     "Plain text without HTML tags",
			input:    "Hello, world!",
			expected: false,
		},
		{
			name:     "Empty string",
			input:    "",
			expected: false,
		},
		{
			name:     "HTML-like text without valid tags",
			input:    "This is <not> HTML",
			expected: false,
		},
		{
			name:     "HTML with nested tags",
			input:    "<div><p>Hello</p></div>",
			expected: true,
		},
		{
			name:     "HTML with attributes",
			input:    `<div class="test">Hello</div>`,
			expected: true,
		},
		{
			name:     "HTML with incomplete tag",
			input:    "<html",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isHTML(tt.input)
			if got != tt.expected {
				t.Errorf("isHTML(%q) = %v, expected %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestGetTextFromHTML(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		expectedTokens int
		expectedTexts  []string
		expectedCounts []int
		wantErr        bool
	}{
		{
			name:           "Valid HTML with text",
			input:          "<html><body><p>Hello world. How are you?</p></body></html>",
			expectedTokens: 7, // Includes opening/closing tags and text tokens
			expectedTexts:  []string{"Hello world.", "How are you?"},
			expectedCounts: []int{2}, // Two sentences in the single text node
			wantErr:        false,
		},
		{
			name:           "HTML with nested tags",
			input:          "<div><p>Hello.</p><p>How are you?</p></div>",
			expectedTokens: 8,
			expectedTexts:  []string{"Hello.", "How are you?"},
			expectedCounts: []int{1, 1}, // One sentence per text node
			wantErr:        false,
		},
		{
			name:           "HTML with no text",
			input:          "<html><body><div></div></body></html>",
			expectedTokens: 6,
			expectedTexts:  []string{},
			expectedCounts: []int{}, // No sentences in the text nodes
			wantErr:        false,
		},
		{
			name:           "Empty input",
			input:          "",
			expectedTokens: 0,
			expectedTexts:  []string{},
			expectedCounts: []int{},
			wantErr:        false,
		},
		{
			name:           "Malformed HTML",
			input:          "<html><body><p>Hello world",
			expectedTokens: 4, // Includes incomplete tags
			expectedTexts:  []string{"Hello world"},
			expectedCounts: []int{1},
			wantErr:        false,
		},
		{
			name:           "HTML with special characters",
			input:          "<p>Hello &amp; welcome!</p>",
			expectedTokens: 3,
			expectedTexts:  []string{"Hello & welcome!"},
			expectedCounts: []int{1},
			wantErr:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens, texts, counts, err := getTextFromHTML(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("getTextFromHTML() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if len(tokens) != tt.expectedTokens {
				t.Errorf("getTextFromHTML() tokens length = %d, expected %d", len(tokens), tt.expectedTokens)
			}

			if !slices.Equal(texts, tt.expectedTexts) {
				t.Errorf("getTextFromHTML() texts = %v, expected %v", texts, tt.expectedTexts)
			}

			if !slices.Equal(counts, tt.expectedCounts) {
				t.Errorf("getTextFromHTML() counts = %v, expected %v", counts, tt.expectedCounts)
			}
		})
	}
}

func TestReconstructHTML(t *testing.T) {
	tests := []struct {
		name                string
		tokens              []html.Token
		translatedSentences []string
		sentenceCounts      []int
		expected            string
	}{
		{
			name: "Simple HTML with one text node",
			tokens: []html.Token{
				{Type: html.StartTagToken, Data: "p"},
				{Type: html.TextToken, Data: "Hello world."},
				{Type: html.EndTagToken, Data: "p"},
			},
			translatedSentences: []string{"Hola mundo."},
			sentenceCounts:      []int{1},
			expected:            "<p>Hola mundo.</p>",
		},
		{
			name: "HTML with multiple text nodes",
			tokens: []html.Token{
				{Type: html.StartTagToken, Data: "div"},
				{Type: html.TextToken, Data: "Hello."},
				{Type: html.StartTagToken, Data: "p"},
				{Type: html.TextToken, Data: "How are you?"},
				{Type: html.EndTagToken, Data: "p"},
				{Type: html.EndTagToken, Data: "div"},
			},
			translatedSentences: []string{"Hola.", "¿Cómo estás?"},
			sentenceCounts:      []int{1, 1},
			expected:            "<div>Hola.<p>¿Cómo estás?</p></div>",
		},
		{
			name: "HTML with no text nodes",
			tokens: []html.Token{
				{Type: html.StartTagToken, Data: "div"},
				{Type: html.EndTagToken, Data: "div"},
			},
			translatedSentences: []string{},
			sentenceCounts:      []int{},
			expected:            "<div></div>",
		},
		{
			name: "HTML with nested tags",
			tokens: []html.Token{
				{Type: html.StartTagToken, Data: "div"},
				{Type: html.StartTagToken, Data: "p"},
				{Type: html.TextToken, Data: "Hello world."},
				{Type: html.EndTagToken, Data: "p"},
				{Type: html.EndTagToken, Data: "div"},
			},
			translatedSentences: []string{"Hola mundo."},
			sentenceCounts:      []int{1},
			expected:            "<div><p>Hola mundo.</p></div>",
		},
		{
			name: "HTML with unmatched sentence counts",
			tokens: []html.Token{
				{Type: html.StartTagToken, Data: "p"},
				{Type: html.TextToken, Data: "Hello world."},
				{Type: html.EndTagToken, Data: "p"},
			},
			translatedSentences: []string{"Hola mundo.", "Extra sentence"},
			sentenceCounts:      []int{1},
			expected:            "<p>Hola mundo.</p>",
		},
		{
			name: "HTML with special characters",
			tokens: []html.Token{
				{Type: html.StartTagToken, Data: "p"},
				{Type: html.TextToken, Data: "Hello & welcome!"},
				{Type: html.EndTagToken, Data: "p"},
			},
			translatedSentences: []string{"Hola & bienvenido!"},
			sentenceCounts:      []int{1},
			expected:            "<p>Hola & bienvenido!</p>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reconstructHTML(tt.tokens, tt.translatedSentences, tt.sentenceCounts)
			if got != tt.expected {
				t.Errorf("reconstructHTML() = %q, expected %q", got, tt.expected)
			}
		})
	}
}
