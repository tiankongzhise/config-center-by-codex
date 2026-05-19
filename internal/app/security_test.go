package app

import "testing"

func TestPasswordHashAndCheck(t *testing.T) {
	hash, err := HashPassword("Strong-Password1")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if hash == "Strong-Password1" {
		t.Fatal("password hash must not equal plaintext")
	}
	if !CheckPassword(hash, "Strong-Password1") {
		t.Fatal("expected password check to pass")
	}
	if CheckPassword(hash, "wrong") {
		t.Fatal("expected wrong password to fail")
	}
}

func TestValidatePasswordRejectsWeakPassword(t *testing.T) {
	weakPasswords := []string{"short", "lowercase1", "UPPERCASE1", "NoNumber!", "NoSpecial1"}
	for _, password := range weakPasswords {
		if err := ValidatePassword(password); err == nil {
			t.Fatalf("expected %q to be rejected", password)
		}
	}
}

func TestHashTokenIsStableAndNotPlaintext(t *testing.T) {
	first := HashToken("session-token")
	second := HashToken("session-token")
	if first != second {
		t.Fatal("token hash should be stable")
	}
	if first == "session-token" {
		t.Fatal("token hash must not expose plaintext token")
	}
}
