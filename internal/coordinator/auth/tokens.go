package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Clock interface {
	Now() time.Time
}

type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now().UTC() }

type TokenSigner struct {
	Secret []byte
	Now    func() time.Time
	TTL    time.Duration
}

type Claims struct {
	NodeID string `json:"node_id"`
	Issued int64  `json:"iat"`
	Expiry int64  `json:"exp"`
}

func (s TokenSigner) Issue(nodeID string) (string, time.Time, error) {
	if len(s.Secret) == 0 {
		return "", time.Time{}, fmt.Errorf("access token secret is required")
	}
	if nodeID == "" {
		return "", time.Time{}, fmt.Errorf("node_id is required")
	}
	now := s.now()
	ttl := s.TTL
	if ttl <= 0 {
		ttl = time.Hour
	}
	expiry := now.Add(ttl).UTC()
	payload, err := json.Marshal(Claims{
		NodeID: nodeID,
		Issued: now.Unix(),
		Expiry: expiry.Unix(),
	})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("marshal claims: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	signature := s.sign(encoded)
	return encoded + "." + signature, expiry, nil
}

func (s TokenSigner) Verify(token string) (Claims, error) {
	if len(s.Secret) == 0 {
		return Claims{}, fmt.Errorf("access token secret is required")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return Claims{}, fmt.Errorf("invalid access token shape")
	}
	want := s.sign(parts[0])
	if subtle.ConstantTimeCompare([]byte(want), []byte(parts[1])) != 1 {
		return Claims{}, fmt.Errorf("invalid access token signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Claims{}, fmt.Errorf("decode access token payload: %w", err)
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Claims{}, fmt.Errorf("decode access token claims: %w", err)
	}
	if claims.NodeID == "" {
		return Claims{}, fmt.Errorf("access token missing node_id")
	}
	if !s.now().Before(time.Unix(claims.Expiry, 0)) {
		return Claims{}, fmt.Errorf("access token expired")
	}
	return claims, nil
}

func (s TokenSigner) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s TokenSigner) sign(encodedPayload string) string {
	mac := hmac.New(sha256.New, s.Secret)
	_, _ = mac.Write([]byte(encodedPayload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
