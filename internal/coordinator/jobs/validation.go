package jobs

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/Ani-HQ/thirdshift/internal/shared/protocol"
)

const MaxPublicRequestBytes = 256 * 1024

func DecodeChatCompletionRequest(body []byte) (ChatCompletionRequest, APIError) {
	var req ChatCompletionRequest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return ChatCompletionRequest{}, APIError{Code: CodeInvalidRequest, Message: "Request body must be valid JSON using supported chat completion fields.", Retryable: false, Status: 400}
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return ChatCompletionRequest{}, APIError{Code: CodeInvalidRequest, Message: "Request body must contain exactly one JSON object.", Retryable: false, Status: 400}
	}
	return req, APIError{}
}

func ValidateChatCompletion(req ChatCompletionRequest, model ModelInfo, bodyBytes int) (protocol.ChatRequest, APIError) {
	if req.Model == "" {
		return protocol.ChatRequest{}, invalid("model is required")
	}
	if req.Model != model.ID {
		return protocol.ChatRequest{}, APIError{Code: CodeModelNotFound, Message: "Model not found.", Retryable: false, Status: 404}
	}
	if !capabilityEnabled(model.Capabilities, "chat_completions") {
		return protocol.ChatRequest{}, APIError{Code: CodeModelUnavailable, Message: "Model does not support chat completions.", Retryable: false, Status: 409}
	}
	maxRequestBytes := model.Limits.MaxRequestBytes
	if maxRequestBytes <= 0 || maxRequestBytes > MaxPublicRequestBytes {
		maxRequestBytes = MaxPublicRequestBytes
	}
	if bodyBytes > maxRequestBytes {
		return protocol.ChatRequest{}, APIError{Code: CodeInvalidRequest, Message: fmt.Sprintf("Request body exceeds the %d byte limit for this model.", maxRequestBytes), Retryable: false, Status: 413}
	}
	if req.Stream {
		return protocol.ChatRequest{}, invalid("stream must be false")
	}
	if len(req.Tools) > 0 && string(req.Tools) != "null" {
		return protocol.ChatRequest{}, invalid("tools and function calling are not supported")
	}
	if len(req.ToolChoice) > 0 && string(req.ToolChoice) != "null" {
		return protocol.ChatRequest{}, invalid("tools and function calling are not supported")
	}
	if len(req.Messages) == 0 {
		return protocol.ChatRequest{}, invalid("messages must contain at least one text message")
	}
	estimatedInputTokens := 0
	for _, message := range req.Messages {
		switch message.Role {
		case "system", "user", "assistant":
		default:
			return protocol.ChatRequest{}, invalid("messages contain an unsupported role")
		}
		if strings.TrimSpace(message.Content) == "" {
			return protocol.ChatRequest{}, invalid("messages must be text-only with non-empty content")
		}
		estimatedInputTokens += estimateTokens(message.Content)
	}
	maxInputTokens := model.Limits.MaxInputTokens
	if maxInputTokens <= 0 {
		maxInputTokens = 4096
	}
	if estimatedInputTokens > maxInputTokens {
		return protocol.ChatRequest{}, APIError{Code: CodeInvalidRequest, Message: "Input exceeds the model token limit.", Retryable: false, Status: 413}
	}
	maxOutputTokens := model.Limits.MaxOutputTokens
	if maxOutputTokens <= 0 {
		maxOutputTokens = 1024
	}
	if req.MaxTokens <= 0 {
		req.MaxTokens = maxOutputTokens
	}
	if req.MaxTokens > maxOutputTokens {
		return protocol.ChatRequest{}, invalid(fmt.Sprintf("max_tokens exceeds the %d token output limit for this model", maxOutputTokens))
	}
	if req.Temperature != nil && (*req.Temperature < 0 || *req.Temperature > 2) {
		return protocol.ChatRequest{}, invalid("temperature must be between 0 and 2")
	}
	return protocol.ChatRequest{
		Messages:    req.Messages,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Stream:      false,
	}, APIError{}
}

func RequestHash(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func invalid(message string) APIError {
	return APIError{Code: CodeInvalidRequest, Message: message, Retryable: false, Status: 400}
}

func capabilityEnabled(capabilities []string, wanted string) bool {
	for _, capability := range capabilities {
		if capability == wanted {
			return true
		}
	}
	return false
}

func estimateTokens(content string) int {
	runes := len([]rune(content))
	if runes == 0 {
		return 0
	}
	return (runes + 3) / 4
}
