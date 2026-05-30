package crypto

import (
	"bytes"
	"testing"
)

func TestECDHKeyExchange(t *testing.T) {
	privA, pubBytesA, err := GenerateEphemeralKeyPair()
	if err != nil {
		t.Fatalf("generating keypair A: %v", err)
	}
	privB, pubBytesB, err := GenerateEphemeralKeyPair()
	if err != nil {
		t.Fatalf("generating keypair B: %v", err)
	}

	keyA, err := DeriveSharedSecret(privA, pubBytesB)
	if err != nil {
		t.Fatalf("deriving secret A->B: %v", err)
	}
	keyB, err := DeriveSharedSecret(privB, pubBytesA)
	if err != nil {
		t.Fatalf("deriving secret B->A: %v", err)
	}

	if !bytes.Equal(keyA, keyB) {
		t.Fatal("shared secrets do not match")
	}
}

func TestEncryptDecrypt(t *testing.T) {
	priv, pubBytes, err := GenerateEphemeralKeyPair()
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	key, err := DeriveSharedSecret(priv, pubBytes)
	if err != nil {
		t.Fatalf("deriving key: %v", err)
	}

	plaintext := []byte("hayate-high-speed-zero-copy-transfer")
	ciphertext, err := Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("encryption failed: %v", err)
	}
	if bytes.Equal(plaintext, ciphertext) {
		t.Fatal("ciphertext should not equal plaintext")
	}

	decrypted, err := Decrypt(key, ciphertext)
	if err != nil {
		t.Fatalf("decryption failed: %v", err)
	}
	if !bytes.Equal(plaintext, decrypted) {
		t.Fatalf("decrypted mismatch: expected %q, got %q", plaintext, decrypted)
	}
}

func TestCachedCipherInPlace(t *testing.T) {
	priv, pubBytes, err := GenerateEphemeralKeyPair()
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	key, err := DeriveSharedSecret(priv, pubBytes)
	if err != nil {
		t.Fatalf("deriving key: %v", err)
	}

	aead, err := NewCipher(key)
	if err != nil {
		t.Fatalf("creating cipher: %v", err)
	}
	ng, err := NewNonceGen()
	if err != nil {
		t.Fatalf("creating nonce gen: %v", err)
	}

	plaintext := []byte("hayate-cached-cipher-in-place-optimization-test-vector")

	encDst := make([]byte, 12+len(plaintext)+16)
	decDst := make([]byte, len(plaintext))

	ciphertext, err := EncryptInPlace(aead, ng, plaintext, encDst)
	if err != nil {
		t.Fatalf("encrypt in-place: %v", err)
	}
	if bytes.Equal(plaintext, ciphertext) {
		t.Fatal("ciphertext should not equal plaintext")
	}

	decrypted, err := DecryptInPlace(aead, ciphertext, decDst)
	if err != nil {
		t.Fatalf("decrypt in-place: %v", err)
	}
	if !bytes.Equal(plaintext, decrypted) {
		t.Fatalf("decrypted mismatch: expected %q, got %q", plaintext, decrypted)
	}
}

func TestNonceGenUniqueness(t *testing.T) {
	ng, err := NewNonceGen()
	if err != nil {
		t.Fatalf("creating nonce gen: %v", err)
	}

	seen := make(map[string]struct{})
	buf := make([]byte, 12)
	for i := 0; i < 10000; i++ {
		n := ng.Next(buf)
		key := string(n)
		if _, exists := seen[key]; exists {
			t.Fatalf("duplicate nonce at iteration %d", i)
		}
		seen[key] = struct{}{}
	}
}

func BenchmarkEncryptInPlace(b *testing.B) {
	priv, pubBytes, _ := GenerateEphemeralKeyPair()
	key, _ := DeriveSharedSecret(priv, pubBytes)
	aead, _ := NewCipher(key)
	ng, _ := NewNonceGen()

	plaintext := make([]byte, 4*1024*1024)
	dst := make([]byte, 12+len(plaintext)+16)

	b.SetBytes(int64(len(plaintext)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = EncryptInPlace(aead, ng, plaintext, dst)
	}
}

func TestGeneratePassphrase(t *testing.T) {
	passphrase, err := GeneratePassphrase(4)
	if err != nil {
		t.Fatalf("failed to generate passphrase: %v", err)
	}
	words := bytes.Split([]byte(passphrase), []byte("-"))
	if len(words) != 4 {
		t.Fatalf("expected 4 words, got %d", len(words))
	}
}

func TestDeriveKEK(t *testing.T) {
	salt := []byte("test-salt-123456")
	passphrase := "apple-bacon-cabin-dance"

	key1 := DeriveKEK(passphrase, salt)
	key2 := DeriveKEK(passphrase, salt)

	if !bytes.Equal(key1, key2) {
		t.Fatal("DeriveKEK should be deterministic for same inputs")
	}

	key3 := DeriveKEK("different-passphrase", salt)
	if bytes.Equal(key1, key3) {
		t.Fatal("DeriveKEK should differ for different passphrases")
	}

	key4 := DeriveKEK(passphrase, []byte("different-salt"))
	if bytes.Equal(key1, key4) {
		t.Fatal("DeriveKEK should differ for different salts")
	}
}
