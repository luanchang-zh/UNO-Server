// Package protocol 定义 WebSocket 消息外壳与基础消息类型（编解码）。
package protocol

import (
	"encoding/json"
	"fmt"
)

// 消息类型常量。
const (
	// TypeHello 服务端在鉴权成功后下发的身份确认。
	TypeHello = "hello"
	// TypePing 客户端心跳。
	TypePing = "ping"
	// TypePong 服务端心跳响应。
	TypePong = "pong"
	// TypeError 服务端错误通知。
	TypeError = "error"
)

// Envelope 是所有 WebSocket 消息的统一外壳。
type Envelope struct {
	// Type 消息类型，决定 Payload 结构。
	Type string `json:"type"`
	// RequestID 可选，客户端生成，用于请求-响应关联。
	RequestID string `json:"request_id,omitempty"`
	// Payload 业务体，延迟解析。
	Payload json.RawMessage `json:"payload,omitempty"`
}

// HelloPayload 为 hello 消息体。
type HelloPayload struct {
	PlayerID int64  `json:"player_id"`
	Nickname string `json:"nickname"`
}

// ErrorPayload 为 error 消息体。
type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Decode 将原始 JSON 解析为 Envelope。
func Decode(data []byte) (Envelope, error) {
	var envelope Envelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return Envelope{}, fmt.Errorf("decode envelope: %w", err)
	}
	if envelope.Type == "" {
		return Envelope{}, fmt.Errorf("decode envelope: empty type")
	}
	return envelope, nil
}

// Encode 将 Envelope 编码为 JSON 字节。
func Encode(envelope Envelope) ([]byte, error) {
	data, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("encode envelope: %w", err)
	}
	return data, nil
}

// NewEnvelope 构造带 payload 的消息外壳。
func NewEnvelope(messageType, requestID string, payload any) (Envelope, error) {
	envelope := Envelope{Type: messageType, RequestID: requestID}
	if payload == nil {
		return envelope, nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return Envelope{}, fmt.Errorf("marshal payload: %w", err)
	}
	envelope.Payload = raw
	return envelope, nil
}
