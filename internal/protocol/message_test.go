package protocol

import (
	"encoding/json"
	"testing"
)

// TestEncodeDecode_RoundTrip 验证信封编解码往返一致。
func TestEncodeDecode_RoundTrip(t *testing.T) {
	envelope, err := NewEnvelope(TypePing, "req-1", map[string]int{"t": 1})
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}
	data, err := Encode(envelope)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded.Type != TypePing || decoded.RequestID != "req-1" {
		t.Fatalf("字段不一致: %+v", decoded)
	}
	var payload map[string]int
	if err := json.Unmarshal(decoded.Payload, &payload); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if payload["t"] != 1 {
		t.Fatalf("payload.t=%d", payload["t"])
	}
}

// TestDecode_EmptyType 验证缺少 type 时报错。
func TestDecode_EmptyType(t *testing.T) {
	_, err := Decode([]byte(`{"request_id":"x"}`))
	if err == nil {
		t.Fatal("期望错误")
	}
}
