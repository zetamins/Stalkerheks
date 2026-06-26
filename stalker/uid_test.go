package stalker

import (
	"testing"
)

func TestGetUID(t *testing.T) {
	p := &Portal{UIDSecret: "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"}
	id := p.GetUID("device_id", "token123")
	if len(id) != 64 { // HMAC-SHA256 hex = 64 chars
		t.Errorf("GetUID length = %d, want 64", len(id))
	}

	// Domain separation: GetUID("a","bc") must NOT equal GetUID("ab","c")
	id1 := p.GetUID("a", "bc")
	id2 := p.GetUID("ab", "c")
	if id1 == id2 {
		t.Errorf("GetUID domain separation failed: GetUID(\"a\",\"bc\") == GetUID(\"ab\",\"c\")")
	}

	// Different inputs produce different outputs
	id3 := p.GetUID("device_id", "token123")
	id4 := p.GetUID("device_id", "token456")
	if id3 == id4 {
		t.Errorf("GetUID should differ for different inputs")
	}

	// Determinism
	id5 := p.GetUID("x")
	id6 := p.GetUID("x")
	if id5 != id6 {
		t.Errorf("GetUID is not deterministic")
	}
}

func TestSignature(t *testing.T) {
	// With explicit Signature set, it should be returned
	p := &Portal{Signature: "explicit"}
	if p.signature() != "explicit" {
		t.Errorf("signature should return explicit value")
	}

	// Without Signature and without UIDSecret, should return empty
	p2 := &Portal{Random: "test"}
	if p2.signature() != "" {
		t.Errorf("signature without UIDSecret should return empty")
	}

	// With UIDSecret but no explicit Signature, should use GetUID
	p3 := &Portal{
		UIDSecret: "secret1234567890123456789012345678901234567890123456789012345678901234",
		Random:    "random123",
	}
	sig := p3.signature()
	if len(sig) != 64 {
		t.Errorf("computed signature length = %d, want 64", len(sig))
	}
	if sig == "explicit" {
		t.Errorf("computed signature should not be the explicit placeholder")
	}
}
