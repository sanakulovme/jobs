package crm

import (
	"encoding/json"
	"os"
	"strconv"
	"sync"

	"faangjobs/internal/store"
)

// table is a single-file JSON collection with atomic writes and a mutex
// guarding read-modify-write cycles (the CRM's HTTP handlers are the
// concurrent writers here, unlike the crawler's one-process-at-a-time store).
type table[T any] struct {
	mu   sync.Mutex
	path string
}

func newTable[T any](path string) *table[T] { return &table[T]{path: path} }

// Load reads every item, returning an empty (non-nil) slice if the file
// doesn't exist yet.
func (t *table[T]) Load() ([]T, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.loadLocked()
}

func (t *table[T]) loadLocked() ([]T, error) {
	data, err := os.ReadFile(t.path)
	if os.IsNotExist(err) {
		return []T{}, nil
	}
	if err != nil {
		return nil, err
	}
	var items []T
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, err
	}
	return items, nil
}

// Save atomically overwrites the whole collection.
func (t *table[T]) Save(items []T) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.saveLocked(items)
}

func (t *table[T]) saveLocked(items []T) error {
	if items == nil {
		items = []T{}
	}
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	return store.WriteFileAtomic(t.path, data)
}

// Update loads the collection, applies fn, and saves the result — the whole
// cycle under one lock, so concurrent handlers can't interleave a
// read-modify-write and drop each other's change.
func (t *table[T]) Update(fn func(items []T) ([]T, error)) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	items, err := t.loadLocked()
	if err != nil {
		return err
	}
	items, err = fn(items)
	if err != nil {
		return err
	}
	return t.saveLocked(items)
}

// counters is a small persistent map used to mint sequential, human-readable
// ids ("1", "2", ...) — mutable CRM records need stable identity independent
// of content, unlike model.StableID's content hash.
type counters struct {
	mu   sync.Mutex
	path string
}

func newCounters(path string) *counters { return &counters{path: path} }

// Next returns the next id for the given counter name, persisting the
// increment before returning.
func (c *counters) Next(name string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	vals := map[string]int{}
	if data, err := os.ReadFile(c.path); err == nil {
		if err := json.Unmarshal(data, &vals); err != nil {
			return "", err
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}

	vals[name]++
	n := vals[name]

	data, err := json.MarshalIndent(vals, "", "  ")
	if err != nil {
		return "", err
	}
	if err := store.WriteFileAtomic(c.path, data); err != nil {
		return "", err
	}
	return strconv.Itoa(n), nil
}
