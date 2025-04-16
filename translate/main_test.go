package main

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dynamoTypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/translate"
	"github.com/aws/aws-sdk-go-v2/service/translate/types"
)

func TestMarshalResponse(t *testing.T) {
	tests := []struct {
		name     string
		input    TranslateResponse
		expected string
		wantErr  bool
	}{
		{
			name: "Valid response",
			input: TranslateResponse{
				TranslatedText:        "Hola",
				DetectedLanguage:      "en",
				TranslationConfidence: 0.95,
			},
			expected: `{"translated_text":"Hola","detected_language":"en","translation_confidence":0.95}`,
			wantErr:  false,
		},
		{
			name: "Empty response",
			input: TranslateResponse{
				TranslatedText:        "",
				DetectedLanguage:      "",
				TranslationConfidence: 0,
			},
			expected: `{"translated_text":""}`,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := marshalResponse(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("marshalResponse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if string(got) != tt.expected {
				t.Errorf("marshalResponse() = %s, expected %s", string(got), tt.expected)
			}

			// Additional check to ensure the output is valid JSON
			var jsonCheck map[string]interface{}
			if err := json.Unmarshal(got, &jsonCheck); err != nil {
				t.Errorf("marshalResponse() produced invalid JSON: %v", err)
			}
		})
	}
}

func TestUnmarshalRequest(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected TranslateRequest
		wantErr  bool
	}{
		{
			name: "Valid request",
			input: `{
				"source_language": "en",
				"target_language": "es",
				"text": "Hello"
			}`,
			expected: TranslateRequest{
				SourceLanguage: "en",
				TargetLanguage: "es",
				Text:           "Hello",
			},
			wantErr: false,
		},
		{
			name:     "Invalid JSON format",
			input:    `{"source_language": "en", "target_language": "es", "text": "Hello"`,
			expected: TranslateRequest{},
			wantErr:  true,
		},
		{
			name:  "Missing fields",
			input: `{"source_language": "en"}`,
			expected: TranslateRequest{
				SourceLanguage: "en",
			},
			wantErr: false,
		},
		{
			name:     "Empty input",
			input:    ``,
			expected: TranslateRequest{},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := unmarshalRequest([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("unmarshalRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if got != tt.expected && !tt.wantErr {
				t.Errorf("unmarshalRequest() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

func TestSplitSentences(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "Single sentence",
			input:    "Hello world.",
			expected: []string{"Hello world."},
		},
		{
			name:     "Multiple sentences",
			input:    "Hello world. How are you? I am fine!",
			expected: []string{"Hello world.", "How are you?", "I am fine!"},
		},
		{
			name:     "Trailing whitespace",
			input:    "Hello world. ",
			expected: []string{"Hello world."},
		},
		{
			name:     "No punctuation",
			input:    "Hello world",
			expected: []string{"Hello world"},
		},
		{
			name:     "Empty input",
			input:    "",
			expected: []string{},
		},
		{
			name:     "Multiple spaces between sentences",
			input:    "Hello world.   How are you?  I am fine!",
			expected: []string{"Hello world.", "How are you?", "I am fine!"},
		},
		{
			name:     "Newline characters",
			input:    "Hello world.\nHow are you?\nI am fine!",
			expected: []string{"Hello world.", "How are you?", "I am fine!"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitSentences(tt.input)
			if len(got) != len(tt.expected) {
				t.Errorf("splitSentences() length = %d, expected length = %d", len(got), len(tt.expected))
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("splitSentences()[%d] = %q, expected %q", i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestGetHashFromText(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Non-empty string",
			input:    "Hello, world!",
			expected: "315f5bdb76d078c43b8ac0064e4a0164612b1fce77c869345bfc94c75894edd3",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name:     "Special characters",
			input:    "!@#$%^&*()",
			expected: "95ce789c5c9d18490972709838ca3a9719094bca3ac16332cfec0652b0236141",
		},
		{
			name:     "Long string",
			input:    "The quick brown fox jumps over the lazy dog",
			expected: "d7a8fbb307d7809469ca9abcb0082e4f8d5651e46d3cdb762d02d0bf37c9e592",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getHashFromText(tt.input)
			if got != tt.expected {
				t.Errorf("getHashFromText() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

func TestGetSupportedLanguages(t *testing.T) {
	tests := []struct {
		name          string
		mockLanguages []string
		mockError     error
		expected      []string
		wantErr       bool
	}{
		{
			name:          "Valid languages",
			mockLanguages: []string{"en", "es", "fr"},
			mockError:     nil,
			expected:      []string{"en", "es", "fr"},
			wantErr:       false,
		},
		{
			name:          "No languages returned",
			mockLanguages: []string{},
			mockError:     nil,
			expected:      []string{},
			wantErr:       false,
		},
		{
			name:          "Error from ListLanguages",
			mockLanguages: nil,
			mockError:     fmt.Errorf("mock error"),
			expected:      nil,
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &MockTranslateClient{
				ListLanguagesFunc: func(ctx context.Context, params *translate.ListLanguagesInput, optFns ...func(*translate.Options)) (*translate.ListLanguagesOutput, error) {
					if tt.mockError != nil {
						return nil, tt.mockError
					}
					languages := make([]types.Language, len(tt.mockLanguages))
					for i, lang := range tt.mockLanguages {
						languages[i] = types.Language{LanguageCode: aws.String(lang)}
					}
					return &translate.ListLanguagesOutput{Languages: languages}, nil
				},
			}

			got, err := getSupportedLanguages(context.Background(), mockClient)
			if (err != nil) != tt.wantErr {
				t.Errorf("getSupportedLanguages() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !slices.Equal(got, tt.expected) {
				t.Errorf("getSupportedLanguages() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

func TestDoesTargetLanguageExist(t *testing.T) {
	tests := []struct {
		name           string
		mockLanguages  []string
		mockError      error
		targetLanguage string
		expected       bool
		wantErr        bool
	}{
		{
			name:           "Target language exists",
			mockLanguages:  []string{"en", "es", "fr"},
			mockError:      nil,
			targetLanguage: "es",
			expected:       true,
			wantErr:        false,
		},
		{
			name:           "Target language does not exist",
			mockLanguages:  []string{"en", "fr"},
			mockError:      nil,
			targetLanguage: "es",
			expected:       false,
			wantErr:        false,
		},
		{
			name:           "No languages returned",
			mockLanguages:  []string{},
			mockError:      nil,
			targetLanguage: "es",
			expected:       false,
			wantErr:        false,
		},
		{
			name:           "Error from getSupportedLanguages",
			mockLanguages:  nil,
			mockError:      fmt.Errorf("mock error"),
			targetLanguage: "es",
			expected:       false,
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &MockTranslateClient{
				ListLanguagesFunc: func(ctx context.Context, params *translate.ListLanguagesInput, optFns ...func(*translate.Options)) (*translate.ListLanguagesOutput, error) {
					if tt.mockError != nil {
						return nil, tt.mockError
					}
					languages := make([]types.Language, len(tt.mockLanguages))
					for i, lang := range tt.mockLanguages {
						languages[i] = types.Language{LanguageCode: aws.String(lang)}
					}
					return &translate.ListLanguagesOutput{Languages: languages}, nil
				},
			}

			got, err := doesTargetLanguageExist(context.Background(), mockClient, tt.targetLanguage)
			if (err != nil) != tt.wantErr {
				t.Errorf("doesTargetLanguageExist() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if got != tt.expected {
				t.Errorf("doesTargetLanguageExist() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

func TestCacheTranslatedText(t *testing.T) {
	tests := []struct {
		name      string
		cacheItem CacheItem
		mockError error
		wantErr   bool
	}{
		{
			name: "Successful cache",
			cacheItem: CacheItem{
				Hash:           "test-hash",
				TranslatedText: "Hola",
				SourceText:     "Hello",
				SourceLanguage: "en",
				TargetLanguage: "es",
			},
			mockError: nil,
			wantErr:   false,
		},
		{
			name: "Error from DynamoDB",
			cacheItem: CacheItem{
				Hash:           "test-hash",
				TranslatedText: "Hola",
				SourceText:     "Hello",
				SourceLanguage: "en",
				TargetLanguage: "es",
			},
			mockError: fmt.Errorf("mock error"),
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &MockDynamoDBClient{
				PutItemFunc: func(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
					return nil, tt.mockError
				},
			}

			err := cacheTranslatedText(context.Background(), mockClient, tt.cacheItem)
			if (err != nil) != tt.wantErr {
				t.Errorf("cacheTranslatedText() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestTranslateLanguage(t *testing.T) {
	tests := []struct {
		name           string
		text           string
		sourceLanguage string
		targetLanguage string
		mockOutput     *translate.TranslateTextOutput
		mockError      error
		expected       TranslateResponse
		wantErr        bool
	}{
		{
			name:           "Successful translation",
			text:           "Hello",
			sourceLanguage: "en",
			targetLanguage: "es",
			mockOutput: &translate.TranslateTextOutput{
				TranslatedText: aws.String("Hola"),
			},
			mockError: nil,
			expected: TranslateResponse{
				TranslatedText: "Hola",
			},
			wantErr: false,
		},
		{
			name:           "Translation error",
			text:           "Hello",
			sourceLanguage: "en",
			targetLanguage: "es",
			mockOutput:     nil,
			mockError:      fmt.Errorf("mock error"),
			expected:       TranslateResponse{},
			wantErr:        true,
		},
		{
			name:           "Empty input text",
			text:           "",
			sourceLanguage: "en",
			targetLanguage: "es",
			mockOutput: &translate.TranslateTextOutput{
				TranslatedText: aws.String(""),
			},
			mockError: nil,
			expected: TranslateResponse{
				TranslatedText: "",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &MockTranslateClient{
				TranslateTextFunc: func(ctx context.Context, params *translate.TranslateTextInput, optFns ...func(*translate.Options)) (*translate.TranslateTextOutput, error) {
					if tt.mockError != nil {
						return nil, tt.mockError
					}
					return tt.mockOutput, nil
				},
			}

			got, err := translateLanguage(context.Background(), mockClient, tt.text, tt.sourceLanguage, tt.targetLanguage)
			if (err != nil) != tt.wantErr {
				t.Errorf("translateLanguage() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if got != tt.expected {
				t.Errorf("translateLanguage() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

func TestShouldCacheBeUsed(t *testing.T) {
	tests := []struct {
		name           string
		sourceLanguage string
		targetLanguage string
		text           string
		mockResponse   *dynamodb.GetItemOutput
		mockError      error
		expectedCache  CacheItem
		expectedUse    bool
		wantErr        bool
	}{
		{
			name:           "Cache hit",
			sourceLanguage: "en",
			targetLanguage: "es",
			text:           "Hello",
			mockResponse: &dynamodb.GetItemOutput{
				Item: map[string]dynamoTypes.AttributeValue{
					"hash":            &dynamoTypes.AttributeValueMemberS{Value: "test-hash"},
					"translated_text": &dynamoTypes.AttributeValueMemberS{Value: "Hola"},
					"source_text":     &dynamoTypes.AttributeValueMemberS{Value: "Hello"},
					"source_language": &dynamoTypes.AttributeValueMemberS{Value: "en"},
					"target_language": &dynamoTypes.AttributeValueMemberS{Value: "es"},
				},
			},
			mockError: nil,
			expectedCache: CacheItem{
				Hash:           "test-hash",
				TranslatedText: "Hola",
				SourceText:     "Hello",
				SourceLanguage: "en",
				TargetLanguage: "es",
			},
			expectedUse: true,
			wantErr:     false,
		},
		{
			name:           "Cache miss",
			sourceLanguage: "en",
			targetLanguage: "es",
			text:           "Hello",
			mockResponse:   &dynamodb.GetItemOutput{Item: nil},
			mockError:      nil,
			expectedCache:  CacheItem{},
			expectedUse:    false,
			wantErr:        false,
		},
		{
			name:           "DynamoDB error",
			sourceLanguage: "en",
			targetLanguage: "es",
			text:           "Hello",
			mockResponse:   nil,
			mockError:      fmt.Errorf("mock error"),
			expectedCache:  CacheItem{},
			expectedUse:    false,
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &MockDynamoDBClient{
				GetItemFunc: func(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
					return tt.mockResponse, tt.mockError
				},
			}

			gotCache, gotUse, err := shouldCacheBeUsed(context.Background(), mockClient, tt.sourceLanguage, tt.targetLanguage, tt.text)
			if (err != nil) != tt.wantErr {
				t.Errorf("shouldCacheBeUsed() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if gotUse != tt.expectedUse {
				t.Errorf("shouldCacheBeUsed() useCache = %v, expected %v", gotUse, tt.expectedUse)
			}

			if gotCache != tt.expectedCache {
				t.Errorf("shouldCacheBeUsed() cacheItem = %v, expected %v", gotCache, tt.expectedCache)
			}
		})
	}
}

func TestHandle(t *testing.T) {
	tests := []struct {
		name                string
		event               events.APIGatewayProxyRequest
		mockTranslateClient *MockTranslateClient
		mockDynamoDBClient  *MockDynamoDBClient
		expectedResponse    events.APIGatewayProxyResponse
		wantErr             bool
	}{
		{
			name: "Valid request with cache hit",
			event: events.APIGatewayProxyRequest{
				Body: `{"source_language":"en","target_language":"es","text":"Hello"}`,
			},
			mockTranslateClient: &MockTranslateClient{
				ListLanguagesFunc: func(ctx context.Context, params *translate.ListLanguagesInput, optFns ...func(*translate.Options)) (*translate.ListLanguagesOutput, error) {
					return &translate.ListLanguagesOutput{
						Languages: []types.Language{
							{LanguageCode: aws.String("es")},
						},
					}, nil
				},
			},
			mockDynamoDBClient: &MockDynamoDBClient{
				GetItemFunc: func(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
					return &dynamodb.GetItemOutput{
						Item: map[string]dynamoTypes.AttributeValue{
							"hash":            &dynamoTypes.AttributeValueMemberS{Value: "test-hash"},
							"translated_text": &dynamoTypes.AttributeValueMemberS{Value: "Hola"},
							"source_text":     &dynamoTypes.AttributeValueMemberS{Value: "Hello"},
							"source_language": &dynamoTypes.AttributeValueMemberS{Value: "en"},
							"target_language": &dynamoTypes.AttributeValueMemberS{Value: "es"},
						},
					}, nil
				},
			},
			expectedResponse: events.APIGatewayProxyResponse{
				StatusCode: http.StatusOK,
				Body:       `{"translated_text":"Hola "}`,
			},
			wantErr: false,
		},
		{
			name: "Valid request with cache miss",
			event: events.APIGatewayProxyRequest{
				Body: `{"source_language":"en","target_language":"es","text":"Hello"}`,
			},
			mockTranslateClient: &MockTranslateClient{
				ListLanguagesFunc: func(ctx context.Context, params *translate.ListLanguagesInput, optFns ...func(*translate.Options)) (*translate.ListLanguagesOutput, error) {
					return &translate.ListLanguagesOutput{
						Languages: []types.Language{
							{LanguageCode: aws.String("es")},
						},
					}, nil
				},
				TranslateTextFunc: func(ctx context.Context, params *translate.TranslateTextInput, optFns ...func(*translate.Options)) (*translate.TranslateTextOutput, error) {
					return &translate.TranslateTextOutput{
						TranslatedText: aws.String("Hola"),
					}, nil
				},
			},
			mockDynamoDBClient: &MockDynamoDBClient{
				GetItemFunc: func(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
					return &dynamodb.GetItemOutput{Item: nil}, nil
				},
				PutItemFunc: func(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
					return &dynamodb.PutItemOutput{}, nil
				},
			},
			expectedResponse: events.APIGatewayProxyResponse{
				StatusCode: http.StatusOK,
				Body:       `{"translated_text":"Hola "}`,
			},
			wantErr: false,
		},
		{
			name: "Invalid request format",
			event: events.APIGatewayProxyRequest{
				Body: `{"source_language":"en","target_language":"es"`,
			},
			mockTranslateClient: &MockTranslateClient{},
			mockDynamoDBClient:  &MockDynamoDBClient{},
			expectedResponse: events.APIGatewayProxyResponse{
				StatusCode: http.StatusBadRequest,
				Body:       "Invalid request format",
			},
			wantErr: false,
		},
		{
			name: "Unsupported target language",
			event: events.APIGatewayProxyRequest{
				Body: `{"source_language":"en","target_language":"xx","text":"Hello"}`,
			},
			mockTranslateClient: &MockTranslateClient{
				ListLanguagesFunc: func(ctx context.Context, params *translate.ListLanguagesInput, optFns ...func(*translate.Options)) (*translate.ListLanguagesOutput, error) {
					return &translate.ListLanguagesOutput{
						Languages: []types.Language{
							{LanguageCode: aws.String("es")},
						},
					}, nil
				},
			},
			mockDynamoDBClient: &MockDynamoDBClient{},
			expectedResponse: events.APIGatewayProxyResponse{
				StatusCode: http.StatusUnprocessableEntity,
				Body:       "Target language not supported",
			},
			wantErr: false,
		},
		{
			name: "Error checking cache",
			event: events.APIGatewayProxyRequest{
				Body: `{"source_language":"en","target_language":"es","text":"Hello"}`,
			},
			mockTranslateClient: &MockTranslateClient{
				ListLanguagesFunc: func(ctx context.Context, params *translate.ListLanguagesInput, optFns ...func(*translate.Options)) (*translate.ListLanguagesOutput, error) {
					return &translate.ListLanguagesOutput{
						Languages: []types.Language{
							{LanguageCode: aws.String("es")},
						},
					}, nil
				},
			},
			mockDynamoDBClient: &MockDynamoDBClient{
				GetItemFunc: func(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
					return nil, fmt.Errorf("mock error")
				},
			},
			expectedResponse: events.APIGatewayProxyResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       "Error during translation",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &handler{
				dynamoClient:    tt.mockDynamoDBClient,
				translateClient: tt.mockTranslateClient,
			}

			got, err := h.handle(context.Background(), tt.event)
			if (err != nil) != tt.wantErr {
				t.Errorf("handle() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if got.StatusCode != tt.expectedResponse.StatusCode || got.Body != tt.expectedResponse.Body {
				t.Errorf("handle() = %v, expected %v", got, tt.expectedResponse)
			}
		})
	}
}

// --
// Mocks
// --

// MockTranslateClient is a mock implementation of the TranslateClient interface
type MockTranslateClient struct {
	ListLanguagesFunc func(ctx context.Context, params *translate.ListLanguagesInput, optFns ...func(*translate.Options)) (*translate.ListLanguagesOutput, error)
	TranslateTextFunc func(ctx context.Context, params *translate.TranslateTextInput, optFns ...func(*translate.Options)) (*translate.TranslateTextOutput, error)
}

func (m *MockTranslateClient) ListLanguages(ctx context.Context, params *translate.ListLanguagesInput, optFns ...func(*translate.Options)) (*translate.ListLanguagesOutput, error) {
	return m.ListLanguagesFunc(ctx, params, optFns...)
}

func (m *MockTranslateClient) TranslateText(ctx context.Context, params *translate.TranslateTextInput, optFns ...func(*translate.Options)) (*translate.TranslateTextOutput, error) {
	return m.TranslateTextFunc(ctx, params, optFns...)
}

// MockDynamoDBClient is a mock implementation of the DynamoDBClient interface
type MockDynamoDBClient struct {
	PutItemFunc func(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	GetItemFunc func(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
}

func (m *MockDynamoDBClient) PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	return m.PutItemFunc(ctx, params, optFns...)
}

func (m *MockDynamoDBClient) GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	return m.GetItemFunc(ctx, params, optFns...)
}
