package ledger

import "testing"

func TestChargeForTokensRoundsToNearestMicrodollar(t *testing.T) {
	tests := []struct {
		name       string
		tokens     int
		perMillion int64
		want       int64
	}{
		{name: "zero tokens", tokens: 0, perMillion: 1_000_000, want: 0},
		{name: "zero price", tokens: 10, perMillion: 0, want: 0},
		{name: "whole", tokens: 6, perMillion: 500_000, want: 3},
		{name: "round up", tokens: 1, perMillion: 500_000, want: 1},
		{name: "round down", tokens: 1, perMillion: 499_999, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := chargeForTokens(tt.tokens, tt.perMillion); got != tt.want {
				t.Fatalf("chargeForTokens(%d, %d) = %d, want %d", tt.tokens, tt.perMillion, got, tt.want)
			}
		})
	}
}
