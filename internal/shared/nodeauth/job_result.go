package nodeauth

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/anianroid/thirdshift/internal/shared/protocol"
)

func SignJobCompleted(privateKey ed25519.PrivateKey, keyID string, payload protocol.JobCompletedPayload, signedAt time.Time) (protocol.NodeSignature, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return protocol.NodeSignature{}, fmt.Errorf("private key has %d bytes, want %d", len(privateKey), ed25519.PrivateKeySize)
	}
	payload.Signature = nil
	body, err := json.Marshal(payload)
	if err != nil {
		return protocol.NodeSignature{}, fmt.Errorf("marshal job completion payload for signing: %w", err)
	}
	signature := ed25519.Sign(privateKey, body)
	return protocol.NodeSignature{
		KeyID:     keyID,
		Algorithm: "ed25519",
		Value:     base64.StdEncoding.EncodeToString(signature),
		SignedAt:  signedAt.UTC(),
	}, nil
}

func VerifyJobCompleted(publicKey ed25519.PublicKey, payload protocol.JobCompletedPayload) error {
	if len(publicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("public key has %d bytes, want %d", len(publicKey), ed25519.PublicKeySize)
	}
	if payload.Signature == nil {
		return fmt.Errorf("job completion signature is required")
	}
	if payload.Signature.Algorithm != "ed25519" {
		return fmt.Errorf("unsupported job completion signature algorithm %q", payload.Signature.Algorithm)
	}
	signature, err := base64.StdEncoding.DecodeString(payload.Signature.Value)
	if err != nil {
		return fmt.Errorf("decode job completion signature: %w", err)
	}
	unsigned := payload
	unsigned.Signature = nil
	body, err := json.Marshal(unsigned)
	if err != nil {
		return fmt.Errorf("marshal job completion payload for verification: %w", err)
	}
	if !ed25519.Verify(publicKey, body, signature) {
		return fmt.Errorf("job completion signature verification failed")
	}
	return nil
}
