package rediskey

import "testing"

// TestKeyFormats 验证 Redis 键格式稳定，避免实现层拼错前缀。
func TestKeyFormats(t *testing.T) {
	if got := Session("abc"); got != "uno:session:abc" {
		t.Fatalf("Session: %s", got)
	}
	if got := PlayerRoom(42); got != "uno:player_room:42" {
		t.Fatalf("PlayerRoom: %s", got)
	}
	if got := RoomSnapshot("R1"); got != "uno:room:R1" {
		t.Fatalf("RoomSnapshot: %s", got)
	}
	if got := RoomIndex(); got != "uno:room_index" {
		t.Fatalf("RoomIndex: %s", got)
	}
}
