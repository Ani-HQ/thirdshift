package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestRedactingHandlerKeepsIDsAndDropsSecrets(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewRedactingHandler(slog.NewTextHandler(&buf, nil)))
	logger.Info("request handled",
		"job_id", "job_01J0M000000000000000000000",
		"attempt_id", "att_01J0M000000000000000000000",
		"prompt", "PROMPT_SENTINEL",
		"completion_text", "COMPLETION_SENTINEL",
		"api_key", "tsak_SECRET",
	)
	got := buf.String()
	for _, wanted := range []string{"job_01J0M000000000000000000000", "att_01J0M000000000000000000000", Redacted} {
		if !strings.Contains(got, wanted) {
			t.Fatalf("log output missing %q: %s", wanted, got)
		}
	}
	for _, forbidden := range []string{"PROMPT_SENTINEL", "COMPLETION_SENTINEL", "tsak_SECRET"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("log output contains forbidden value %q: %s", forbidden, got)
		}
	}
}
