package persistence

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type Store[T any] struct {
	mu      sync.Mutex
	path    string
	version int
	data    T
}

type Upgrade func(json.RawMessage) (json.RawMessage, error)

func Open[T any](path string, initial T, upgrades ...Upgrade) (*Store[T], error) {
	s := &Store[T]{path: path, version: len(upgrades) + 1, data: initial}
	if path == "" {
		return s, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var saved struct {
		Version int             `json:"version"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		return nil, fmt.Errorf("read state %s: %w", path, err)
	}
	if saved.Version < 1 || saved.Version > s.version {
		return nil, fmt.Errorf("unsupported state version in %s", path)
	}
	for _, upgrade := range upgrades[saved.Version-1:] {
		if saved.Data, err = upgrade(saved.Data); err != nil {
			return nil, fmt.Errorf("upgrade state %s: %w", path, err)
		}
	}
	var value T
	if err := json.Unmarshal(saved.Data, &value); err != nil {
		return nil, fmt.Errorf("read state %s: %w", path, err)
	}
	s.data = value
	if saved.Version < s.version {
		if err := s.Update(func(*T) error { return nil }); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Store[T]) Read() (T, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return copyValue(s.data)
}

func (s *Store[T]) Update(change func(*T) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next, err := copyValue(s.data)
	if err != nil {
		return err
	}
	if err := change(&next); err != nil {
		return err
	}
	committed, err := copyValue(next)
	if err != nil {
		return err
	}
	if s.path != "" {
		data, err := json.Marshal(struct {
			Version int `json:"version"`
			Data    T   `json:"data"`
		}{s.version, next})
		if err != nil {
			return err
		}
		if err := writeFile(s.path, data); err != nil {
			return err
		}
	}
	s.data = committed
	return nil
}

func copyValue[T any](value T) (T, error) {
	var out T
	data, err := json.Marshal(value)
	if err == nil {
		err = json.Unmarshal(data, &out)
	}
	return out, err
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".state-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
