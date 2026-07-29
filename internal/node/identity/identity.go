package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const (
	keyFileName         = "node-key.json"
	credentialsFileName = "credentials.json"
)

type KeyFile struct {
	PublicKey  string `json:"public_key"`
	PrivateKey string `json:"private_key"`
}

type Credentials struct {
	NodeID                  string    `json:"node_id"`
	CoordinatorURL          string    `json:"coordinator_url"`
	PublicKey               string    `json:"public_key"`
	AccessToken             string    `json:"access_token"`
	AccessTokenExpiresAt    time.Time `json:"access_token_expires_at"`
	HardwareFingerprintHash string    `json:"hardware_fingerprint_hash"`
}

func LoadOrCreateKey(dataDir string) (ed25519.PrivateKey, string, error) {
	if dataDir == "" {
		return nil, "", fmt.Errorf("data dir is required")
	}
	path := filepath.Join(dataDir, keyFileName)
	if data, err := os.ReadFile(path); err == nil {
		var keyFile KeyFile
		if err := json.Unmarshal(data, &keyFile); err != nil {
			return nil, "", fmt.Errorf("decode key file: %w", err)
		}
		privateKey, err := base64.StdEncoding.DecodeString(keyFile.PrivateKey)
		if err != nil {
			return nil, "", fmt.Errorf("decode private key: %w", err)
		}
		if len(privateKey) != ed25519.PrivateKeySize {
			return nil, "", fmt.Errorf("private key has %d bytes, want %d", len(privateKey), ed25519.PrivateKeySize)
		}
		return ed25519.PrivateKey(privateKey), keyFile.PublicKey, nil
	} else if !os.IsNotExist(err) {
		return nil, "", fmt.Errorf("read key file: %w", err)
	}

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, "", fmt.Errorf("generate ed25519 keypair: %w", err)
	}
	publicEncoded := base64.StdEncoding.EncodeToString(publicKey)
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, "", fmt.Errorf("create data dir: %w", err)
	}
	body, err := json.MarshalIndent(KeyFile{
		PublicKey:  publicEncoded,
		PrivateKey: base64.StdEncoding.EncodeToString(privateKey),
	}, "", "  ")
	if err != nil {
		return nil, "", fmt.Errorf("marshal key file: %w", err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return nil, "", fmt.Errorf("write key file: %w", err)
	}
	return privateKey, publicEncoded, nil
}

func SaveCredentials(dataDir string, creds Credentials) error {
	if dataDir == "" {
		return fmt.Errorf("data dir is required")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	body, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, credentialsFileName), body, 0o600); err != nil {
		return fmt.Errorf("write credentials: %w", err)
	}
	return nil
}

func LoadCredentials(dataDir string) (Credentials, error) {
	if dataDir == "" {
		return Credentials{}, fmt.Errorf("data dir is required")
	}
	data, err := os.ReadFile(filepath.Join(dataDir, credentialsFileName))
	if err != nil {
		return Credentials{}, fmt.Errorf("read credentials: %w", err)
	}
	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return Credentials{}, fmt.Errorf("decode credentials: %w", err)
	}
	if creds.NodeID == "" || creds.AccessToken == "" || creds.CoordinatorURL == "" {
		return Credentials{}, fmt.Errorf("stored credentials are incomplete; run thirdshift login --invite again")
	}
	return creds, nil
}

func HardwareFingerprintHash() string {
	hostname, _ := os.Hostname()
	sum := sha256.Sum256([]byte(runtime.GOOS + "|" + runtime.GOARCH + "|" + hostname))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func KeyPath(dataDir string) string {
	return filepath.Join(dataDir, keyFileName)
}

func CredentialsPath(dataDir string) string {
	return filepath.Join(dataDir, credentialsFileName)
}
