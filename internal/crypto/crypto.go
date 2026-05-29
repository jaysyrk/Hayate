package crypto

import (
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"sync/atomic"

	"golang.org/x/crypto/chacha20poly1305"
)

const KeySize = chacha20poly1305.KeySize

func GenerateEphemeralKeyPair() (*ecdh.PrivateKey, []byte, error) {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generating X25519 key: %w", err)
	}
	return priv, priv.PublicKey().Bytes(), nil
}

func DeriveSharedSecret(priv *ecdh.PrivateKey, peerPubBytes []byte) ([]byte, error) {
	pub, err := ecdh.X25519().NewPublicKey(peerPubBytes)
	if err != nil {
		return nil, fmt.Errorf("parsing peer public key: %w", err)
	}

	secret, err := priv.ECDH(pub)
	if err != nil {
		return nil, fmt.Errorf("ECDH key agreement: %w", err)
	}

	hash := sha256.Sum256(secret)
	return hash[:], nil
}

// NewCipher creates a reusable AEAD cipher from the session key.
// Call once per session; pass the returned cipher to all hot-path functions.
func NewCipher(key []byte) (cipher.AEAD, error) {
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, fmt.Errorf("creating chacha20poly1305 cipher: %w", err)
	}
	return aead, nil
}

// NonceGen produces sequential 96-bit nonces with a random 4-byte prefix.
// Safe for a single session key: the 8-byte counter guarantees uniqueness
// within the session, while the random prefix prevents cross-session reuse.
// Concurrent-safe via atomic increment.
type NonceGen struct {
	prefix  [4]byte
	counter atomic.Uint64
}

// NewNonceGen initializes a nonce generator with a cryptographically random prefix.
func NewNonceGen() (*NonceGen, error) {
	ng := &NonceGen{}
	if _, err := io.ReadFull(rand.Reader, ng.prefix[:]); err != nil {
		return nil, fmt.Errorf("reading random nonce prefix: %w", err)
	}
	return ng, nil
}

// Next writes the next nonce into buf (must be >= 12 bytes) and returns it.
// Lock-free; safe for concurrent goroutines.
func (ng *NonceGen) Next(buf []byte) []byte {
	_ = buf[11] // bounds check hint
	copy(buf[:4], ng.prefix[:])
	binary.LittleEndian.PutUint64(buf[4:12], ng.counter.Add(1))
	return buf[:12]
}

// Encrypt encrypts plaintext using ChaCha20-Poly1305 with a random nonce.
// Returns [nonce || ciphertext || tag]. Allocates a new slice.
func Encrypt(key, plaintext []byte) ([]byte, error) {
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, fmt.Errorf("creating chacha20poly1305 cipher: %w", err)
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("reading random nonce: %w", err)
	}

	ciphertext := aead.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// Decrypt decrypts ciphertext of the form [nonce || ciphertext || tag].
func Decrypt(key, ciphertext []byte) ([]byte, error) {
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, fmt.Errorf("creating chacha20poly1305 cipher: %w", err)
	}

	nonceSize := aead.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short: %d bytes, need >= %d", len(ciphertext), nonceSize)
	}

	plaintext, err := aead.Open(nil, ciphertext[:nonceSize], ciphertext[nonceSize:], nil)
	if err != nil {
		return nil, fmt.Errorf("decrypting ciphertext: %w", err)
	}
	return plaintext, nil
}

// EncryptInPlace seals plaintext into dst using a cached AEAD and nonce generator.
// dst must have capacity >= NonceSize + len(plaintext) + Overhead.
// Returns the sealed slice (which aliases dst). Zero heap allocations.
func EncryptInPlace(aead cipher.AEAD, ng *NonceGen, plaintext, dst []byte) ([]byte, error) {
	nonceSize := aead.NonceSize()
	reqCap := nonceSize + len(plaintext) + aead.Overhead()
	if cap(dst) < reqCap {
		return nil, fmt.Errorf("dst capacity %d too small, need %d", cap(dst), reqCap)
	}

	nonceBuf := dst[:nonceSize]
	ng.Next(nonceBuf)

	sealed := aead.Seal(nonceBuf, nonceBuf, plaintext, nil)
	return sealed, nil
}

// DecryptInPlace opens ciphertext into dst using a cached AEAD.
// dst must have capacity >= len(ciphertext) - NonceSize - Overhead.
// Returns the plaintext slice (which aliases dst). Zero heap allocations.
func DecryptInPlace(aead cipher.AEAD, ciphertext, dst []byte) ([]byte, error) {
	nonceSize := aead.NonceSize()
	if len(ciphertext) < nonceSize+aead.Overhead() {
		return nil, fmt.Errorf("ciphertext too short: %d bytes", len(ciphertext))
	}

	nonce := ciphertext[:nonceSize]
	payload := ciphertext[nonceSize:]

	needed := len(payload) - aead.Overhead()
	if needed < 0 {
		needed = 0
	}
	if cap(dst) < needed {
		return nil, fmt.Errorf("dst capacity %d too small, need %d", cap(dst), needed)
	}

	plaintext, err := aead.Open(dst[:0], nonce, payload, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypting: %w", err)
	}
	return plaintext, nil
}
