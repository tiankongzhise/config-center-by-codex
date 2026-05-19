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
