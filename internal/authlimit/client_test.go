package authlimit

import "testing"

func TestSignM2M(t *testing.T) {
	signature := SignM2M("secret", "123", map[string]any{
		"b": "2",
		"a": "1",
		"c": []string{"ignored"},
	})
	if signature == "" {
		t.Fatal("expected signature")
	}
	again := SignM2M("secret", "123", map[string]any{"a": "1", "b": "2"})
	if signature != again {
		t.Fatal("signature should sort keys and ignore complex values")
	}
}

func TestRandomPasswordMatchesAuthLimitRules(t *testing.T) {
	password, err := randomPassword()
	if err != nil {
		t.Fatalf("randomPassword() error = %v", err)
	}
	if !validAuthLimitPassword(password) {
		t.Fatalf("generated password does not match auth-limit rules: %q", password)
	}
}

func TestValidateOperatorCredentials(t *testing.T) {
	if err := validateOperatorCredentials("cfgcenter_ops", "Aa1!2345"); err != nil {
		t.Fatalf("expected valid credentials: %v", err)
	}
	if err := validateOperatorCredentials("config_center_operator", "Aa1!2345"); err == nil {
		t.Fatal("expected legacy long username to be rejected")
	}
	if err := validateOperatorCredentials("cfgcenter_ops", "Aa1!234567890123456789"); err == nil {
		t.Fatal("expected long password to be rejected")
	}
}
