package database

import "testing"

func TestValidateEncryptionKey(t *testing.T) {
	cases := map[string]struct {
		key     string
		wantErr bool
	}{
		"unset":        {"", true},
		"too short":    {"secret", true},
		"63 hex chars": {"e4b6836c113e437c8353013501173098d2f496cd9d70afa96896057d495d88c", true},
		"64 hex chars": {"e4b6836c113e437c8353013501173098d2f496cd9d70afa96896057d495d88cd", false},
		"65 hex chars": {"e4b6836c113e437c8353013501173098d2f496cd9d70afa96896057d495d88cd0", true},
		"64 non-hex":   {"zzb6836c113e437c8353013501173098d2f496cd9d70afa96896057d495d88cd", true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv(EncryptionKeyEnvVar, tc.key)
			err := ValidateEncryptionKey()
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for key %q, got nil", tc.key)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error for key %q, got %v", tc.key, err)
			}
		})
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	t.Setenv(EncryptionKeyEnvVar, "e4b6836c113e437c8353013501173098d2f496cd9d70afa96896057d495d88cd")

	const token = "sentry-personal-token-abc123"
	ciphertext, err := encrypt(token)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	plaintext, err := decrypt(ciphertext)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if plaintext != token {
		t.Fatalf("expected %q, got %q", token, plaintext)
	}
}
