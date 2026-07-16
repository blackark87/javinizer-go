package token

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
)

// TokenPrefix is the prefix prepended to every generated API token.
const TokenPrefix = "jv_"

// GenerateToken creates a new random API token, returning the full token, its 8-char display prefix, and any error.
func GenerateToken() (fullToken string, tokenPrefix string, err error) {
	bytes := make([]byte, 16)
	if _, err = rand.Read(bytes); err != nil {
		return "", "", err
	}
	hexStr := hex.EncodeToString(bytes)
	fullToken = TokenPrefix + hexStr
	tokenPrefix = hexStr[:8]
	return fullToken, tokenPrefix, nil
}

// HashToken returns the SHA-256 hex hash of a token for secure storage.
func HashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}
