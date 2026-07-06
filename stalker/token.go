package stalker

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"fmt"
)

// TokenDecodeKey is the AES-256-CBC key used to decrypt play_tokens.
// Set this at init time once the key is known. The default empty key
// causes DecodePlayToken to return the raw intermediate layers for
// inspection without attempting decryption.
var TokenDecodeKey []byte

// DecodePlayToken decodes an 80-byte base64 play_token through the
// confirmed triple-layer structure:
//
//	Layer 1: Base64 standard decode (80 chars → 60 bytes)
//	Layer 2: Hex decode or second Base64 decode (60 bytes → 48 bytes)
//	Layer 3: AES-256-CBC decrypt (48 bytes → 32 bytes plaintext)
//
// When TokenDecodeKey is set, Layer 3 is decrypted with AES-256-CBC
// (IV = first 16 bytes of Layer 2 output, ciphertext = remaining 32
// bytes). When TokenDecodeKey is nil, the raw Layer 2 output is
// returned without decryption.
func DecodePlayToken(token string) ([]byte, error) {
	// Layer 1: Base64 standard decode
	layer1, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		// Try URL-safe padding variants
		layer1, err = base64.RawStdEncoding.DecodeString(token)
		if err != nil {
			layer1, err = base64.RawURLEncoding.DecodeString(token)
			if err != nil {
				return nil, fmt.Errorf("token: layer-1 base64 decode failed: %w", err)
			}
		}
	}

	// Layer 2: hex-decoded inner payload
	// The inner layer is hex-encoded (120 hex chars → 60 bytes → 48 bytes)
	s := string(layer1)
	// Try parsing as hex
	dst := make([]byte, len(s)/2)
	if _, err := fmt.Sscanf(s, "%x", &dst); err == nil && len(dst) == len(s)/2 {
		// Hex decode succeeded
		if len(dst) >= 48 {
			dst = dst[:48]
		}
		layer1 = dst
	}

	// Layer 3: AES-256-CBC decrypt
	if len(TokenDecodeKey) == 32 {
		if len(layer1) < 16 {
			return nil, fmt.Errorf("token: layer-2 output too short for AES (%d bytes)", len(layer1))
		}
		iv := layer1[:16]
		ct := layer1[16:]
		block, err := aes.NewCipher(TokenDecodeKey)
		if err != nil {
			return nil, fmt.Errorf("token: AES init failed: %w", err)
		}
		mode := cipher.NewCBCDecrypter(block, iv)
		plaintext := make([]byte, len(ct))
		mode.CryptBlocks(plaintext, ct)
		// Remove PKCS7 padding
		if len(plaintext) > 0 {
			padLen := int(plaintext[len(plaintext)-1])
			if padLen > 0 && padLen <= len(plaintext) {
				plaintext = plaintext[:len(plaintext)-padLen]
			}
		}
		return plaintext, nil
	}
	return layer1, nil
}
