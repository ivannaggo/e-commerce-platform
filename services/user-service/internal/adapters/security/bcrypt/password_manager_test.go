package bcrypt

import "testing"

func TestPasswordManagerHashAndCompare(t *testing.T) {
	t.Parallel()

	manager, err := NewPasswordManager(DefaultCost)
	if err != nil {
		t.Fatalf("NewPasswordManager returned error: %v", err)
	}

	hash, err := manager.Hash("StrongPassword1")
	if err != nil {
		t.Fatalf("Hash returned error: %v", err)
	}
	if hash == "" || hash == "StrongPassword1" {
		t.Fatalf("expected non-empty hash different from plaintext, got %q", hash)
	}

	match, err := manager.Compare(hash, "StrongPassword1")
	if err != nil {
		t.Fatalf("Compare returned error: %v", err)
	}
	if !match {
		t.Fatal("expected password to match")
	}

	match, err = manager.Compare(hash, "WrongPassword1")
	if err != nil {
		t.Fatalf("Compare returned error: %v", err)
	}
	if match {
		t.Fatal("expected password mismatch")
	}
}
