package policy

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aiveto/veto/internal/atomicfile"
)

type store interface {
	put(rec confirmation) error
	get(id string) (confirmation, bool)
	findApproved(id string, now time.Time) (confirmation, bool)
	claim(id string) (bool, error)
	remove(rec confirmation)
}

type memoryStore struct {
	pending  map[string]confirmation
	approved map[string]string
}

func (m *memoryStore) put(rec confirmation) error {
	if m.pending == nil {
		m.pending = map[string]confirmation{}
	}
	if m.approved == nil {
		m.approved = map[string]string{}
	}
	m.pending[rec.ID] = rec
	if rec.ApprovedID != "" && rec.Status == statusApproved {
		m.approved[rec.ApprovedID] = rec.ID
	}
	if rec.Status == statusConsumed {
		delete(m.approved, rec.ApprovedID)
	}
	return nil
}

func (m *memoryStore) get(id string) (confirmation, bool) {
	rec, ok := m.pending[id]
	return rec, ok
}

func (m *memoryStore) findApproved(id string, now time.Time) (confirmation, bool) {
	if id == "" {
		return confirmation{}, false
	}
	pendingID, ok := m.approved[id]
	if !ok {
		return confirmation{}, false
	}
	rec, ok := m.pending[pendingID]
	if !ok || rec.ApprovedID != id || rec.Status != statusApproved || expired(rec, now) {
		m.remove(rec)
		delete(m.approved, id)
		return confirmation{}, false
	}
	return rec, true
}

func (m *memoryStore) claim(string) (bool, error) {
	return true, nil
}

func (m *memoryStore) remove(rec confirmation) {
	delete(m.pending, rec.ID)
	if rec.ApprovedID != "" {
		delete(m.approved, rec.ApprovedID)
	}
}

type fileStore struct {
	memoryStore
	dir string
}

func (f *fileStore) put(rec confirmation) error {
	if err := f.write(rec); err != nil {
		return err
	}
	return f.memoryStore.put(rec)
}

func (f *fileStore) get(id string) (confirmation, bool) {
	if plainID(id) {
		if rec, ok := f.read(id); ok {
			_ = f.memoryStore.put(rec)
			return rec, true
		}
	}
	return f.memoryStore.get(id)
}

func (f *fileStore) findApproved(id string, now time.Time) (confirmation, bool) {
	if rec, ok := f.memoryStore.findApproved(id, now); ok {
		disk, ok := f.read(rec.ID)
		if !ok || disk.ApprovedID != id || disk.Status != statusApproved || expired(disk, now) {
			f.remove(rec)
		} else {
			_ = f.memoryStore.put(disk)
			return disk, true
		}
	}
	entries, err := os.ReadDir(filepath.Join(f.dir, "confirmations"))
	if err != nil {
		return confirmation{}, false
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		rec, ok := f.read(strings.TrimSuffix(entry.Name(), ".json"))
		if !ok {
			continue
		}
		if expired(rec, now) {
			f.remove(rec)
			continue
		}
		if rec.ApprovedID == id && rec.Status == statusApproved {
			_ = f.memoryStore.put(rec)
			return rec, true
		}
	}
	return confirmation{}, false
}

func (f *fileStore) claim(id string) (bool, error) {
	if !plainID(id) {
		return false, errors.New("approval id")
	}
	dir := filepath.Join(f.dir, "confirmations")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, fmt.Errorf("approval dir: %w", err)
	}
	handle, err := os.OpenFile(filepath.Join(dir, id+".claimed"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return false, nil
		}
		return false, fmt.Errorf("approval: %w", err)
	}
	if err := handle.Close(); err != nil {
		return false, fmt.Errorf("approval: %w", err)
	}
	return true, nil
}

func (f *fileStore) remove(rec confirmation) {
	if id := filepath.Base(rec.ID); plainID(id) {
		dir := filepath.Join(f.dir, "confirmations")
		_ = os.Remove(filepath.Join(dir, id+".json"))
		_ = os.Remove(filepath.Join(dir, id+".claimed"))
	}
	f.memoryStore.remove(rec)
}

func (f *fileStore) write(rec confirmation) error {
	id := filepath.Base(rec.ID)
	if !plainID(id) {
		return errors.New("approval id")
	}
	dir := filepath.Join(f.dir, "confirmations")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("approval dir: %w", err)
	}
	body, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("approval: %w", err)
	}
	if err := atomicfile.Write(filepath.Join(dir, id+".json"), body, 0); err != nil {
		return fmt.Errorf("approval: %w", err)
	}
	return nil
}

func (f *fileStore) read(id string) (confirmation, bool) {
	id = filepath.Base(id)
	if !plainID(id) {
		return confirmation{}, false
	}
	body, err := os.ReadFile(filepath.Join(f.dir, "confirmations", id+".json"))
	if err != nil {
		return confirmation{}, false
	}
	var rec confirmation
	if err := json.Unmarshal(body, &rec); err != nil || rec.ID != id {
		return confirmation{}, false
	}
	return rec, true
}
