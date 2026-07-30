package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ani-HQ/thirdshift/internal/node/runtime/fakellama"
)

func TestMain(m *testing.M) {
	if os.Getenv("THIRDSHIFT_FAKE_LLAMA_SERVER") == "1" {
		os.Exit(fakellama.Main(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func TestLauncherRejectsNonLoopback(t *testing.T) {
	_, err := StartLlamaServer(context.Background(), LaunchConfig{
		ExecutablePath: os.Args[0],
		ModelPath:      filepath.Join(t.TempDir(), "model.gguf"),
		Args: ApprovedArgs{
			Host: "0.0.0.0",
		},
	})
	if err == nil {
		t.Fatal("non-loopback launch succeeded, want error")
	}
}

func TestLauncherHealthShutdownAndLogRedaction(t *testing.T) {
	dir := t.TempDir()
	modelPath := filepath.Join(dir, "model.gguf")
	if err := os.WriteFile(modelPath, []byte("fake model"), 0o644); err != nil {
		t.Fatalf("write model: %v", err)
	}

	prompt := "secret prompt body"
	running, err := StartLlamaServer(context.Background(), LaunchConfig{
		ExecutablePath:  os.Args[0],
		ModelPath:       modelPath,
		WorkDir:         dir,
		LogDir:          dir,
		HealthInterval:  20 * time.Millisecond,
		StartupTimeout:  2 * time.Second,
		ShutdownTimeout: 2 * time.Second,
		MaxLogBytes:     1024,
		Redactions:      []string{prompt},
		Env:             []string{"THIRDSHIFT_FAKE_LLAMA_SERVER=1"},
		Args: ApprovedArgs{
			Host:        "127.0.0.1",
			ContextSize: 128,
			BatchSize:   16,
			GPULayers:   "0",
			Parallel:    1,
		},
	})
	if err != nil {
		t.Fatalf("start fake runtime: %v", err)
	}

	completion, err := ChatCompletion(context.Background(), running.BaseURL, CompletionRequest{
		Model:       "thirdshift-test-chat-v1",
		Messages:    []ChatMessage{{Role: "user", Content: prompt}},
		Temperature: 0.2,
		MaxTokens:   16,
		Stream:      false,
	})
	if err != nil {
		t.Fatalf("chat completion: %v", err)
	}
	if len(completion.Choices) != 1 || !strings.Contains(completion.Choices[0].Message.Content, prompt) {
		t.Fatalf("unexpected completion: %#v", completion)
	}
	if err := running.Stop(context.Background(), 2*time.Second); err != nil {
		t.Fatalf("stop runtime: %v", err)
	}

	logData, err := os.ReadFile(filepath.Join(dir, "llama-server.log"))
	if err != nil {
		t.Fatalf("read runtime log: %v", err)
	}
	if strings.Contains(string(logData), prompt) {
		t.Fatalf("runtime log contains prompt body: %s", string(logData))
	}
	if !strings.Contains(string(logData), "[REDACTED]") {
		t.Fatalf("runtime log does not contain redaction marker: %s", string(logData))
	}
}
