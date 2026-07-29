package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type CompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
	Stream      bool          `json:"stream"`
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type CompletionResponse struct {
	Choices []struct {
		Message      ChatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage CompletionUsage `json:"usage"`
}

type CompletionUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func ChatCompletion(ctx context.Context, baseURL string, request CompletionRequest) (CompletionResponse, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return CompletionResponse{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return CompletionResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return CompletionResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return CompletionResponse{}, fmt.Errorf("chat completion failed with HTTP %d", resp.StatusCode)
	}
	var completion CompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&completion); err != nil {
		return CompletionResponse{}, err
	}
	return completion, nil
}
