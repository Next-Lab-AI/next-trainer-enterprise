package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
)

var ErrVersionIncompatible = errors.New("protocol: version incompatible")

type Frame struct {
	Type            string          `json:"type"`
	ProtocolVersion int             `json:"protocol_version"`
	CommandID       string          `json:"command_id,omitempty"`
	Seq             int64           `json:"seq,omitempty"`
	Payload         json.RawMessage `json:"payload"`
}

func NewFrame(msgType string, commandID string, seq int64, payload any) (Frame, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return Frame{}, fmt.Errorf("protocol: marshal payload: %w", err)
	}
	return Frame{
		Type:            msgType,
		ProtocolVersion: Version,
		CommandID:       commandID,
		Seq:             seq,
		Payload:         raw,
	}, nil
}

func Encode(f Frame) ([]byte, error) {
	if f.ProtocolVersion != Version {
		return nil, fmt.Errorf("%w: frame version %d, local %d", ErrVersionIncompatible, f.ProtocolVersion, Version)
	}
	return json.Marshal(f)
}

func Decode(data []byte) (Frame, error) {
	var f Frame
	if err := json.Unmarshal(data, &f); err != nil {
		return Frame{}, fmt.Errorf("protocol: decode frame: %w", err)
	}
	if f.ProtocolVersion != Version {
		return Frame{}, fmt.Errorf("%w: frame version %d, local %d", ErrVersionIncompatible, f.ProtocolVersion, Version)
	}
	return f, nil
}

func DecodePayload[T any](f Frame) (T, error) {
	var p T
	if err := json.Unmarshal(f.Payload, &p); err != nil {
		return p, fmt.Errorf("protocol: decode %s payload: %w", f.Type, err)
	}
	return p, nil
}
