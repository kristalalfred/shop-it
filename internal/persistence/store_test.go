package persistence

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDurableTransactionsAndIsolation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := Open(path, map[string][]int{})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(func(data *map[string][]int) error { (*data)["items"] = []int{1, 2}; return nil }); err != nil {
		t.Fatal(err)
	}
	read, err := s.Read()
	if err != nil {
		t.Fatal(err)
	}
	read["items"][0] = 999
	if err := s.Update(func(data *map[string][]int) error { (*data)["items"][0] = 88; return errors.New("abort") }); err == nil {
		t.Fatal("transaction committed")
	}
	reopened, err := Open(path, map[string][]int{})
	if err != nil {
		t.Fatal(err)
	}
	read, err = reopened.Read()
	if err != nil || read["items"][0] != 1 {
		t.Fatalf("state corrupted: %v, %v", read, err)
	}
	stat, err := os.Stat(path)
	if err != nil || stat.Mode().Perm() != 0600 {
		t.Fatalf("state permissions: %v, %v", stat, err)
	}
}

func TestWriteFailureDoesNotCommit(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open[int]("", 7)
	if err != nil {
		t.Fatal(err)
	}
	s.path = filepath.Join(blocker, "state.json")
	if err := s.Update(func(data *int) error { *data = 8; return nil }); err == nil {
		t.Fatal("write unexpectedly succeeded")
	}
	value, _ := s.Read()
	if value != 7 {
		t.Fatalf("failed write changed state: %d", value)
	}
}

func TestExclusiveDataDirectory(t *testing.T) {
	dir := t.TempDir()
	unlock, err := Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if close, err := Lock(dir); err == nil {
		close()
		t.Fatal("second service acquired data directory")
	}
	unlock()
	close, err := Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	close()
}

func TestUpgradesRunOnceAndNewerStateIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"data":[1,2]}`), 0600); err != nil {
		t.Fatal(err)
	}
	upgrades := 0
	double := func(data json.RawMessage) (json.RawMessage, error) {
		upgrades++
		var values []int
		if err := json.Unmarshal(data, &values); err != nil {
			return nil, err
		}
		for i := range values {
			values[i] *= 2
		}
		return json.Marshal(values)
	}
	for range 2 {
		s, err := Open(path, []int{}, double)
		if err != nil {
			t.Fatal(err)
		}
		if values, _ := s.Read(); len(values) != 2 || values[1] != 4 || upgrades != 1 {
			t.Fatalf("upgraded state: %v after %d upgrades", values, upgrades)
		}
	}
	if _, err := Open(path, []int{}); err == nil {
		t.Fatal("state from a newer version was accepted")
	}
}
