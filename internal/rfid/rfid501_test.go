package rfid

import (
	"testing"
)

func TestRFID501EncodeDecode(t *testing.T) {
	barcode := "1301234567"
	encoded := EncodeRFID501Content(barcode)

	// Decode should give us the barcode back
	blocks := make([]string, 8)
	for i := 0; i < 8; i++ {
		blocks[i] = encoded[i*8 : i*8+8]
	}

	decoded := DecodeRFID501(blocks)
	if decoded == nil {
		t.Fatal("decode returned nil")
	}
	if decoded.Content != barcode {
		t.Fatalf("content mismatch: got %q, want %q", decoded.Content, barcode)
	}
	if decoded.U1 != 0x04 {
		t.Fatalf("marker byte: got %02x, want 04", decoded.U1)
	}
	if decoded.U2 != 0x00 {
		t.Fatalf("reserved byte: got %02x, want 00", decoded.U2)
	}
	if decoded.Set != 1 || decoded.Total != 1 {
		t.Fatalf("set/total: got set=%d total=%d, want 1/1", decoded.Set, decoded.Total)
	}
	t.Logf("type=%d (%s), branch=%d, library=%d, custom=%d",
		decoded.Type, decoded.TypeLabel, decoded.Branch, decoded.Library, decoded.Custom)
}

func TestRFID501ShortBarcode(t *testing.T) {
	content := "12345"
	encoded := EncodeRFID501Content(content)
	blocks := make([]string, 8)
	for i := 0; i < 8; i++ {
		blocks[i] = encoded[i*8 : i*8+8]
	}
	decoded := DecodeRFID501(blocks)
	if decoded == nil {
		t.Fatal("decode returned nil")
	}
	if decoded.Content != content {
		t.Fatalf("content mismatch: got %q, want %q", decoded.Content, content)
	}
}

func TestRFID501LongBarcode(t *testing.T) {
	content := "1301234567890123456789" // >16 chars
	encoded := EncodeRFID501Content(content)
	blocks := make([]string, 8)
	for i := 0; i < 8; i++ {
		blocks[i] = encoded[i*8 : i*8+8]
	}
	decoded := DecodeRFID501(blocks)
	if decoded == nil {
		t.Fatal("decode returned nil")
	}
	// Should be truncated to 16 chars
	if decoded.Content != content[:16] {
		t.Fatalf("content mismatch: got %q, want %q", decoded.Content, content[:16])
	}
}

func TestRFID501Blank(t *testing.T) {
	blocks := BlankRFID501()
	if len(blocks) != 3 {
		t.Fatalf("blank: got %d blocks, want 3", len(blocks))
	}
	for i, b := range blocks {
		if b != "00000000" {
			t.Fatalf("blank block %d: got %s, want 00000000", i, b)
		}
	}
}

func TestRFID501Blank3M(t *testing.T) {
	blocks := Blank3MRFID501()
	if len(blocks) != 7 {
		t.Fatalf("blank_3m: got %d blocks, want 7", len(blocks))
	}
	for i := 0; i < 6; i++ {
		if blocks[i] != "55555555" {
			t.Fatalf("blank_3m block %d: got %s, want 55555555", i, blocks[i])
		}
	}
	if blocks[6] != "00000000" {
		t.Fatalf("blank_3m block 6: got %s, want 00000000", blocks[6])
	}
}
