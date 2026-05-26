package store

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"time"
)

const ttlEnvelopePrefix = "__cache_engine_ttl_v1__:"

type ttlEnvelope struct {
	ExpiresAtUnixNano int64  `json:"expiresAtUnixNano"`
	Value             string `json:"value"`
}

func cloneBytes(src []byte) []byte {
	if src == nil {
		return nil
	}
	dst := make([]byte, len(src))
	copy(dst, src)
	return dst
}

func encodeForStore(value []byte, expiresAt time.Time) ([]byte, error) {
	if expiresAt.IsZero() {
		return cloneBytes(value), nil
	}

	payload, err := json.Marshal(ttlEnvelope{
		ExpiresAtUnixNano: expiresAt.UnixNano(),
		Value:             base64.StdEncoding.EncodeToString(value),
	})
	if err != nil {
		return nil, err
	}

	encoded := make([]byte, 0, len(ttlEnvelopePrefix)+len(payload))
	encoded = append(encoded, ttlEnvelopePrefix...)
	encoded = append(encoded, payload...)
	return encoded, nil
}

func decodeFromStore(raw []byte) ([]byte, time.Time, error) {
	if !bytes.HasPrefix(raw, []byte(ttlEnvelopePrefix)) {
		return cloneBytes(raw), time.Time{}, nil
	}

	var env ttlEnvelope
	if err := json.Unmarshal(raw[len(ttlEnvelopePrefix):], &env); err != nil {
		// Treat malformed envelopes as legacy raw payloads so older data keeps working.
		return cloneBytes(raw), time.Time{}, nil
	}

	value, err := base64.StdEncoding.DecodeString(env.Value)
	if err != nil {
		return nil, time.Time{}, err
	}

	return value, time.Unix(0, env.ExpiresAtUnixNano), nil
}

func ttlFromExpiry(expiresAt time.Time) (time.Duration, bool) {
	if expiresAt.IsZero() {
		return 0, false
	}
	ttl := time.Until(expiresAt)
	if ttl <= 0 {
		return 0, true
	}
	return ttl, false
}
