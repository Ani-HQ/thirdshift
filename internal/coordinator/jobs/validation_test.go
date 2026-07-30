package jobs

import (
	"testing"

	"github.com/Ani-HQ/thirdshift/internal/shared/protocol"
)

func TestValidateChatCompletionP0Restrictions(t *testing.T) {
	model := ModelInfo{
		ID:           "thirdshift-tiny-chat-v1",
		Capabilities: []string{"chat_completions"},
		Limits:       ModelLimits{MaxInputTokens: 8, MaxOutputTokens: 16, MaxRequestBytes: 512},
	}
	tests := []struct {
		name string
		body string
		code string
	}{
		{
			name: "stream rejected",
			body: `{"model":"thirdshift-tiny-chat-v1","messages":[{"role":"user","content":"hi"}],"stream":true}`,
			code: CodeInvalidRequest,
		},
		{
			name: "tools rejected",
			body: `{"model":"thirdshift-tiny-chat-v1","messages":[{"role":"user","content":"hi"}],"tools":[],"stream":false}`,
			code: CodeInvalidRequest,
		},
		{
			name: "image content rejected by decoder",
			body: `{"model":"thirdshift-tiny-chat-v1","messages":[{"role":"user","content":[{"type":"image_url"}]}],"stream":false}`,
			code: CodeInvalidRequest,
		},
		{
			name: "unknown fields rejected",
			body: `{"model":"thirdshift-tiny-chat-v1","messages":[{"role":"user","content":"hi"}],"stream":false,"modalities":["text","audio"]}`,
			code: CodeInvalidRequest,
		},
		{
			name: "wrong model rejected",
			body: `{"model":"thirdshift-other-chat-v1","messages":[{"role":"user","content":"hi"}],"stream":false}`,
			code: CodeModelNotFound,
		},
		{
			name: "input limit rejected",
			body: `{"model":"thirdshift-tiny-chat-v1","messages":[{"role":"user","content":"this prompt is intentionally much too long for the small test limit"}],"stream":false}`,
			code: CodeInvalidRequest,
		},
		{
			name: "output limit rejected",
			body: `{"model":"thirdshift-tiny-chat-v1","messages":[{"role":"user","content":"hi"}],"max_tokens":17,"stream":false}`,
			code: CodeInvalidRequest,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, apiErr := DecodeChatCompletionRequest([]byte(tt.body))
			if apiErr.Code == "" {
				_, apiErr = ValidateChatCompletion(req, model, len(tt.body))
			}
			if apiErr.Code != tt.code {
				t.Fatalf("code = %q, want %q; err=%#v", apiErr.Code, tt.code, apiErr)
			}
		})
	}
}

func TestValidateChatCompletionAccepted(t *testing.T) {
	model := ModelInfo{
		ID:           "thirdshift-tiny-chat-v1",
		Capabilities: []string{"chat_completions"},
		Limits:       ModelLimits{MaxInputTokens: 64, MaxOutputTokens: 16, MaxRequestBytes: 512},
	}
	body := []byte(`{"model":"thirdshift-tiny-chat-v1","messages":[{"role":"user","content":"hi"}],"temperature":0.4,"max_tokens":8,"stream":false}`)
	req, apiErr := DecodeChatCompletionRequest(body)
	if apiErr.Code != "" {
		t.Fatalf("decode error: %#v", apiErr)
	}
	offer, apiErr := ValidateChatCompletion(req, model, len(body))
	if apiErr.Code != "" {
		t.Fatalf("validate error: %#v", apiErr)
	}
	if offer.MaxTokens != 8 || len(offer.Messages) != 1 || offer.Messages[0] != (protocol.ChatMessage{Role: "user", Content: "hi"}) {
		t.Fatalf("bad protocol request: %#v", offer)
	}
}
