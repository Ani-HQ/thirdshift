package nodeauth

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type RefreshRequest struct {
	NodeID    string `json:"node_id"`
	Timestamp string `json:"timestamp"`
	Signature string `json:"signature"`
}

func SignRefresh(privateKey ed25519.PrivateKey, nodeID string, timestamp time.Time) (RefreshRequest, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return RefreshRequest{}, fmt.Errorf("private key has %d bytes, want %d", len(privateKey), ed25519.PrivateKeySize)
	}
	if nodeID == "" {
		return RefreshRequest{}, fmt.Errorf("node_id is required")
	}
	ts := timestamp.UTC().Format(time.RFC3339)
	signature := ed25519.Sign(privateKey, refreshMessage(nodeID, ts))
	return RefreshRequest{
		NodeID:    nodeID,
		Timestamp: ts,
		Signature: base64.StdEncoding.EncodeToString(signature),
	}, nil
}

func VerifyRefresh(publicKey ed25519.PublicKey, req RefreshRequest, now time.Time, maxSkew time.Duration) error {
	if len(publicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("public key has %d bytes, want %d", len(publicKey), ed25519.PublicKeySize)
	}
	if req.NodeID == "" || req.Timestamp == "" || req.Signature == "" {
		return fmt.Errorf("refresh request is incomplete")
	}
	timestamp, err := time.Parse(time.RFC3339, req.Timestamp)
	if err != nil {
		return fmt.Errorf("parse refresh timestamp: %w", err)
	}
	if maxSkew <= 0 {
		maxSkew = 5 * time.Minute
	}
	if timestamp.After(now.Add(maxSkew)) || timestamp.Before(now.Add(-maxSkew)) {
		return fmt.Errorf("refresh timestamp outside allowed clock skew")
	}
	signature, err := base64.StdEncoding.DecodeString(req.Signature)
	if err != nil {
		return fmt.Errorf("decode refresh signature: %w", err)
	}
	if !ed25519.Verify(publicKey, refreshMessage(req.NodeID, req.Timestamp), signature) {
		return fmt.Errorf("refresh signature verification failed")
	}
	return nil
}

func refreshMessage(nodeID, timestamp string) []byte {
	var b strings.Builder
	b.WriteString("thirdshift.node.token.refresh")
	b.WriteByte('\n')
	b.WriteString(nodeID)
	b.WriteByte('\n')
	b.WriteString(timestamp)
	b.WriteByte('\n')
	b.WriteString(strconv.Itoa(len(nodeID)))
	return []byte(b.String())
}
