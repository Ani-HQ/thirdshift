package fakellama

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"time"
)

func Main(args []string) int {
	fs := flag.NewFlagSet("fake-llama-server", flag.ContinueOnError)
	host := fs.String("host", "127.0.0.1", "host")
	port := fs.Int("port", 0, "port")
	model := fs.String("model", "", "model path")
	_ = fs.Int("ctx-size", 0, "context size")
	_ = fs.Int("batch-size", 0, "batch size")
	_ = fs.String("n-gpu-layers", "", "gpu layers")
	_ = fs.String("device", "", "device")
	_ = fs.Int("parallel", 1, "parallel")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if *port == 0 {
		fmt.Fprintln(os.Stderr, "port is required")
		return 2
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "ok",
			"model":  *model,
		})
	})
	mux.HandleFunc("POST /v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		fmt.Fprintln(os.Stdout, "request body: [REDACTED]")
		var request struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(body, &request)
		prompt := ""
		if len(request.Messages) > 0 {
			prompt = request.Messages[len(request.Messages)-1].Content
		}
		response := map[string]any{
			"id":      "chatcmpl_fake",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   request.Model,
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]string{
						"role":    "assistant",
						"content": "fake completion: " + prompt,
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]int{
				"prompt_tokens":     3,
				"completion_tokens": 5,
				"total_tokens":      8,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	})

	server := &http.Server{Addr: *host + ":" + strconv.Itoa(*port), Handler: mux}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
