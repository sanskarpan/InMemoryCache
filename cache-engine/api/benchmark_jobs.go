package api

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
)

type benchmarkJobStore struct {
	path string
}

func newBenchmarkJobStore(path string) *benchmarkJobStore {
	if path == "" {
		return nil
	}
	return &benchmarkJobStore{path: path}
}

func (s *benchmarkJobStore) ready() error {
	if s == nil {
		return nil
	}
	dir := filepath.Dir(s.path)
	return os.MkdirAll(dir, 0o750)
}

func (s *benchmarkJobStore) load() ([]*BenchmarkJob, error) {
	if s == nil {
		return nil, nil
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var jobs []*BenchmarkJob
	if err := json.Unmarshal(data, &jobs); err != nil {
		return nil, err
	}
	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].StartedAt.After(jobs[j].StartedAt)
	})
	return jobs, nil
}

func (s *benchmarkJobStore) save(jobs []*BenchmarkJob) error {
	if s == nil {
		return nil
	}
	if err := s.ready(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(jobs, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
