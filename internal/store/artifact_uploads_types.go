package store

import "time"

type ArtifactChunk struct {
	Checksum string
	Data     []byte
}

type ArtifactStagingPolicy struct {
	MaxBytes int64
	TTL      time.Duration
}
