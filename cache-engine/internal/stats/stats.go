package stats

import (
	"sync/atomic"
	"time"
)

// Stats holds atomic counters for cache operations.
type Stats struct {
	Hits          atomic.Int64
	Misses        atomic.Int64
	Sets          atomic.Int64
	Deletes       atomic.Int64
	Evictions     atomic.Int64
	TTLExpiries   atomic.Int64
	BytesStored   atomic.Int64
	WriteStoreOps atomic.Int64
	ReadStoreOps  atomic.Int64
}

// StatsSnapshot is a plain (non-atomic) copy of Stats.
type StatsSnapshot struct {
	Hits          int64   `json:"hits"`
	Misses        int64   `json:"misses"`
	Sets          int64   `json:"sets"`
	Deletes       int64   `json:"deletes"`
	Evictions     int64   `json:"evictions"`
	TTLExpiries   int64   `json:"ttlExpiries"`
	BytesStored   int64   `json:"bytesStored"`
	WriteStoreOps int64   `json:"writeStoreOps"`
	ReadStoreOps  int64   `json:"readStoreOps"`
	HitRate       float64 `json:"hitRate"`
	MissRate      float64 `json:"missRate"`
	EvictionRate  float64 `json:"evictionRate"`
	Timestamp     time.Time `json:"timestamp"`
}

func (s *Stats) HitRate() float64 {
	h, m := s.Hits.Load(), s.Misses.Load()
	if h+m == 0 {
		return 0
	}
	return float64(h) / float64(h+m)
}

func (s *Stats) MissRate() float64 {
	h, m := s.Hits.Load(), s.Misses.Load()
	if h+m == 0 {
		return 0
	}
	return float64(m) / float64(h+m)
}

func (s *Stats) EvictionRate() float64 {
	total := s.Hits.Load() + s.Misses.Load()
	if total == 0 {
		return 0
	}
	return float64(s.Evictions.Load()) / float64(total)
}

func (s *Stats) Snapshot() StatsSnapshot {
	return StatsSnapshot{
		Hits:          s.Hits.Load(),
		Misses:        s.Misses.Load(),
		Sets:          s.Sets.Load(),
		Deletes:       s.Deletes.Load(),
		Evictions:     s.Evictions.Load(),
		TTLExpiries:   s.TTLExpiries.Load(),
		BytesStored:   s.BytesStored.Load(),
		WriteStoreOps: s.WriteStoreOps.Load(),
		ReadStoreOps:  s.ReadStoreOps.Load(),
		HitRate:       s.HitRate(),
		MissRate:      s.MissRate(),
		EvictionRate:  s.EvictionRate(),
		Timestamp:     time.Now(),
	}
}

func (s *Stats) Reset() {
	s.Hits.Store(0)
	s.Misses.Store(0)
	s.Sets.Store(0)
	s.Deletes.Store(0)
	s.Evictions.Store(0)
	s.TTLExpiries.Store(0)
	s.BytesStored.Store(0)
	s.WriteStoreOps.Store(0)
	s.ReadStoreOps.Store(0)
}
