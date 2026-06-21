package crypto

import "testing"

func TestEncryptDecryptRoundTrip(t *testing.T) {
	c, err := New("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, secret := range []string{"", "hunter2", "a-very-long-smtp-app-password-1234567890"} {
		enc, err := c.Encrypt(secret)
		if err != nil {
			t.Fatalf("Encrypt(%q): %v", secret, err)
		}
		if enc == secret && secret != "" {
			t.Fatalf("ciphertext equals plaintext for %q", secret)
		}
		dec, err := c.Decrypt(enc)
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}
		if dec != secret {
			t.Fatalf("round trip mismatch: got %q want %q", dec, secret)
		}
	}
}

func TestEncryptProducesDistinctCiphertext(t *testing.T) {
	c, _ := New("0123456789abcdef0123456789abcdef")
	a, _ := c.Encrypt("same")
	b, _ := c.Encrypt("same")
	if a == b {
		t.Fatal("expected distinct ciphertexts due to random nonce")
	}
}

func TestNewRejectsEmptyKey(t *testing.T) {
	if _, err := New(""); err == nil {
		t.Fatal("expected error for empty key")
	}
}

func TestNewAcceptsAnyNonEmptyKey(t *testing.T) {
	c, err := New("short")
	if err != nil {
		t.Fatalf("expected short non-empty key to be accepted: %v", err)
	}
	enc, _ := c.Encrypt("secret")
	dec, _ := c.Decrypt(enc)
	if dec != "secret" {
		t.Fatal("round trip failed with short key")
	}
}

func TestDecryptRejectsGarbage(t *testing.T) {
	c, _ := New("0123456789abcdef0123456789abcdef")
	if _, err := c.Decrypt("!!!notbase64!!!"); err == nil {
		t.Fatal("expected error decrypting garbage")
	}
}
