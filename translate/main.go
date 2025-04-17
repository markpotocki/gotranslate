package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/translate"
	"github.com/aws/aws-xray-sdk-go/instrumentation/awsv2"
	jsoniter "github.com/json-iterator/go"
	"github.com/sentencizer/sentencizer"
	"golang.org/x/net/html"
	"golang.org/x/sync/errgroup"
)

var (
	translateTableName = os.Getenv("TRANSLATE_TABLE_NAME")
	region             = os.Getenv("AWS_REGION")

	json = jsoniter.ConfigCompatibleWithStandardLibrary
)

const (
	defaultTranslateTableName = "TranslateCache"
	defaultAWSRegion          = "us-east-1"
)

func init() {
	if translateTableName == "" {
		translateTableName = defaultTranslateTableName
	}
	if region == "" {
		region = defaultAWSRegion
	}
}

// TranslateRequest represents the request structure for the translation API
type TranslateRequest struct {
	// SourceLanguage is the language code of the source text
	SourceLanguage string `json:"source_language"`
	// TargetLanguage is the language code of the target text
	TargetLanguage string `json:"target_language"`
	// Text is the text to be translated
	Text string `json:"text"`
}

// TranslateResponse represents the response structure for the translation API
type TranslateResponse struct {
	// TranslatedText is the translated text
	TranslatedText string `json:"translated_text"`
	// DetectedLanguage is the detected language of the source text
	DetectedLanguage string `json:"detected_language,omitempty"`
	// TranslationConfidence is the confidence score of the translation
	TranslationConfidence float64 `json:"translation_confidence,omitempty"`
}

// CacheItem represents a cached translation item
type CacheItem struct {
	// Hash is the unique identifier for the cached item
	Hash string
	// TranslatedText is the translated text
	TranslatedText string
	// SourceText is the original text
	SourceText string
	// SourceLanguage is the language code of the source text
	SourceLanguage string
	// TargetLanguage is the language code of the target text
	TargetLanguage string
}

type DynamoDBClient interface {
	GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
	PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
}

type TranslateClient interface {
	TranslateText(ctx context.Context, params *translate.TranslateTextInput, optFns ...func(*translate.Options)) (*translate.TranslateTextOutput, error)
	ListLanguages(ctx context.Context, params *translate.ListLanguagesInput, optFns ...func(*translate.Options)) (*translate.ListLanguagesOutput, error)
}

func main() {
	cfg, err := config.LoadDefaultConfig(context.Background(), config.WithRegion(region))
	if err != nil {
		panic(fmt.Sprintf("failed to load configuration, %v", err))
	}

	// Setup xray tracing for sdks
	awsv2.AWSV2Instrumentor(&cfg.APIOptions)

	// Create DynamoDB and Translate clients
	dynamoClient := dynamodb.NewFromConfig(cfg)
	translateClient := translate.NewFromConfig(cfg)

	h := &handler{
		dynamoClient:    dynamoClient,
		translateClient: translateClient,
	}

	lambda.Start(h.handle)
}

type handler struct {
	dynamoClient    DynamoDBClient
	translateClient TranslateClient
}

func (h *handler) handle(ctx context.Context, event events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	request, err := unmarshalRequest([]byte(event.Body))
	if err != nil {
		return events.APIGatewayProxyResponse{
			StatusCode: http.StatusBadRequest,
			Body:       "Invalid request format",
		}, nil
	}

	// Validate the request
	err = validateRequest(request)
	if err != nil {
		return events.APIGatewayProxyResponse{
			StatusCode: http.StatusBadRequest,
			Body:       err.Error(),
		}, nil
	}

	// Check if the target language is supported
	supported, err := doesTargetLanguageExist(ctx, h.translateClient, request.TargetLanguage)
	if err != nil {
		return events.APIGatewayProxyResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       "Error checking supported languages",
		}, nil
	}
	if !supported {
		return events.APIGatewayProxyResponse{
			StatusCode: http.StatusUnprocessableEntity,
			Body:       "Target language not supported",
		}, nil
	}

	// Store our tokens to translate here
	var htmlTokens []html.Token
	var sentences []string
	var wordCounts []int

	// Detect if it is HTML content
	htmlContent := isHTML(request.Text)
	if htmlContent {
		var err error
		htmlTokens, sentences, wordCounts, err = getTextFromHTML(request.Text)
		if err != nil {
			return events.APIGatewayProxyResponse{
				StatusCode: http.StatusInternalServerError,
				Body:       "Error processing HTML content",
			}, nil
		}
	} else {
		// Split the text into sentences
		sentences = splitSentences(request.Text)
	}

	// Iterate over each sentence and translate it
	errGroup, groupCtx := errgroup.WithContext(ctx)
	errGroup.SetLimit(10) // Limit the number of concurrent translations

	translatedSentences := make([]string, len(sentences))

	for idx, tok := range sentences {
		index := idx // Capture the index for the goroutine
		token := tok // Capture the token for the goroutine
		errGroup.Go(func() error {
			cacheItem, useCache, err := shouldCacheBeUsed(groupCtx, h.dynamoClient, request.SourceLanguage, request.TargetLanguage, token)
			if err != nil {
				return fmt.Errorf("error checking cache for token %d: %w", index, err)
			}

			if useCache {
				// Use the cached translation
				translatedSentences[index] = cacheItem.TranslatedText
				return nil
			}

			translateResponse, err := translateLanguage(groupCtx, h.translateClient, token, request.SourceLanguage, request.TargetLanguage)
			if err != nil {
				return fmt.Errorf("error translating token %d: %w", index, err)
			}

			cacheItem = CacheItem{
				Hash:           getHashFromText(fmt.Sprintf("%s-%s-%s", request.SourceLanguage, request.TargetLanguage, token)),
				TranslatedText: translateResponse.TranslatedText,
				SourceText:     token,
				SourceLanguage: request.SourceLanguage,
				TargetLanguage: request.TargetLanguage,
			}

			err = cacheTranslatedText(groupCtx, h.dynamoClient, cacheItem)
			if err != nil {
				return fmt.Errorf("error caching translation for token %d: %w", index, err)
			}

			translatedSentences[index] = translateResponse.TranslatedText
			return nil
		})
	}

	// Wait for all translations to complete
	if err := errGroup.Wait(); err != nil {
		log.Printf("Error during translation: %v", err)
		return events.APIGatewayProxyResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       "Error during translation",
		}, nil
	}

	// Join the translated sentences into a single string
	translatedText := ""
	if htmlContent {
		translatedText = reconstructHTML(htmlTokens, translatedSentences, wordCounts)
	} else {
		translatedText = reconstructPlainText(translatedSentences)
	}

	// Create the response
	response := TranslateResponse{
		TranslatedText: translatedText,
	}

	// Marshal the response to JSON
	responseBody, err := marshalResponse(response)
	if err != nil {
		return events.APIGatewayProxyResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       "Error marshalling response",
		}, nil
	}

	// Return the response
	return events.APIGatewayProxyResponse{
		StatusCode: http.StatusOK,
		Body:       responseBody,
	}, nil
}

func reconstructPlainText(translatedSentences []string) string {
	var sb strings.Builder
	for _, sentence := range translatedSentences {
		sb.WriteString(sentence)
		sb.WriteString(" ") // Add a space between sentences
	}
	return strings.TrimSpace(sb.String()) // Trim any trailing space
}

func shouldCacheBeUsed(ctx context.Context, dynamoClient DynamoDBClient, sourceLanguage, targetLanguage, text string) (CacheItem, bool, error) {
	hashKey := fmt.Sprintf("%s-%s-%s", sourceLanguage, targetLanguage, text)
	hash := getHashFromText(hashKey)

	// Check if the hash exists in the DynamoDB table
	useCache := false
	var cacheItem CacheItem

	response, err := dynamoClient.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(translateTableName),
		Key: map[string]types.AttributeValue{
			"hash": &types.AttributeValueMemberS{
				Value: hash,
			},
		},
	})

	// If the item does not exist, we can skip the cache
	if err != nil {
		return cacheItem, useCache, err
	}

	if response.Item == nil {
		return cacheItem, useCache, nil
	}

	// Build the cache item from the response
	cacheItem = CacheItem{
		Hash:           response.Item["hash"].(*types.AttributeValueMemberS).Value,
		TranslatedText: response.Item["translated_text"].(*types.AttributeValueMemberS).Value,
		SourceText:     response.Item["source_text"].(*types.AttributeValueMemberS).Value,
		SourceLanguage: response.Item["source_language"].(*types.AttributeValueMemberS).Value,
		TargetLanguage: response.Item["target_language"].(*types.AttributeValueMemberS).Value,
	}

	return cacheItem, true, nil
}

func translateLanguage(ctx context.Context, translateClient TranslateClient, text, sourceLanguage, targetLanguage string) (TranslateResponse, error) {
	// Translate the text using the AWS Translate service
	input := &translate.TranslateTextInput{
		SourceLanguageCode: aws.String(sourceLanguage),
		TargetLanguageCode: aws.String(targetLanguage),
		Text:               aws.String(text),
	}

	output, err := translateClient.TranslateText(ctx, input)
	if err != nil {
		return TranslateResponse{}, err
	}

	// TODO - See if we can get detected lang and confidence
	return TranslateResponse{
		TranslatedText: *output.TranslatedText,
	}, nil
}

func cacheTranslatedText(ctx context.Context, dynamoClient DynamoDBClient, item CacheItem) error {
	// Store the translated text in the DynamoDB table
	_, err := dynamoClient.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(translateTableName),
		Item: map[string]types.AttributeValue{
			"hash": &types.AttributeValueMemberS{
				Value: item.Hash,
			},
			"translated_text": &types.AttributeValueMemberS{
				Value: item.TranslatedText,
			},
			"source_text": &types.AttributeValueMemberS{
				Value: item.SourceText,
			},
			"source_language": &types.AttributeValueMemberS{
				Value: item.SourceLanguage,
			},
			"target_language": &types.AttributeValueMemberS{
				Value: item.TargetLanguage,
			},
		},
	})

	return err
}

func doesTargetLanguageExist(ctx context.Context, translateClient TranslateClient, targetLanguage string) (bool, error) {
	languages, err := getSupportedLanguages(ctx, translateClient)
	if err != nil {
		return false, err
	}

	return slices.Contains(languages, targetLanguage), nil
}

func getSupportedLanguages(ctx context.Context, translateClient TranslateClient) ([]string, error) {
	out, err := translateClient.ListLanguages(ctx, &translate.ListLanguagesInput{})
	if err != nil {
		return nil, err
	}

	if out.Languages == nil {
		return nil, fmt.Errorf("no languages returned by AWS Translate")
	}

	languages := make([]string, len(out.Languages))
	for i, lang := range out.Languages {
		languages[i] = *lang.LanguageCode
	}

	return languages, nil
}

func getHashFromText(text string) string {
	hash := sha256.Sum256([]byte(text))
	return hex.EncodeToString(hash[:])
}

func splitSentences(input string) []string {
	segmenter := sentencizer.NewSegmenter("en")
	return segmenter.Segment(input)
}

func unmarshalRequest(body []byte) (TranslateRequest, error) {
	var request TranslateRequest
	err := json.Unmarshal(body, &request)
	if err != nil {
		return request, fmt.Errorf("failed to unmarshal request body: %w", err)
	}

	return request, nil
}

func marshalResponse(response TranslateResponse) (string, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false) // Prevent HTML escaping. This is important for HTML content.
	err := encoder.Encode(response)
	if err != nil {
		return "", fmt.Errorf("failed to marshal response: %w", err)
	}
	return strings.TrimSpace(buf.String()), nil
}

func validateRequest(request TranslateRequest) error {
	if request.SourceLanguage == "" {
		return fmt.Errorf("source_language is required")
	}
	if request.TargetLanguage == "" {
		return fmt.Errorf("target_language is required")
	}
	if request.Text == "" {
		return fmt.Errorf("text is required")
	}
	return nil
}
