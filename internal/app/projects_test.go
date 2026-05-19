package app

import "testing"

func TestNormalizeProjectInput(t *testing.T) {
	name, code, description, publicKey, err := normalizeProjectInput(" Demo ", "demo-service", " desc ", "")
	if err != nil {
		t.Fatalf("normalize project: %v", err)
	}
	if name != "Demo" || code != "demo-service" || description != "desc" || publicKey != "" {
		t.Fatalf("unexpected normalized values: %q %q %q %q", name, code, description, publicKey)
	}
}

func TestNormalizeProjectInputRejectsBadCode(t *testing.T) {
	badCodes := []string{"AB", "1demo", "demo_service", "de"}
	for _, code := range badCodes {
		if _, _, _, _, err := normalizeProjectInput("Demo", code, "", ""); err == nil {
			t.Fatalf("expected %q to be rejected", code)
		}
	}
}
