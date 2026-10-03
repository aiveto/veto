package policy

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/aiveto/veto/internal/atomicfile"
)

// Store persists confirmation records. Memory is the default. Files is one machine.
// Claim is consume-once. valkeystore is the replica adapter: SET NX on the pending id.
type Store interface {
	Put(Record) error
	Get(id string) (Record, bool)
	FindApproved(approvedID string) (Record, bool)
	Claim(id string) (bool, error)
	Remove(Record)
}

// Record is one confirmation.
type Record struct {
	ID          string            `json:"id"`
	OperationID string            `json:"operation_id"`
	Params      map[string]string `json:"params,omitempty"`
	Status      string            `json:"status"`
	Expiry      int64             `json:"expiry,omitempty"`
	ApprovedID  string            `json:"approved_id,omitempty"`
	Caller      string            `json:"caller,omitempty"`
}

// Memory keeps records in this process.
type Memory struct {
	pending  map[string]Record
	approved map[string]string
}

func (m *Memory) Put(rec Record) error {
	if m.pending == nil {
		m.pending = map[string]Record{}
	}
	if m.approved == nil {
		m.approved = map[string]string{}
	}
	m.pending[rec.ID] = rec
	if rec.ApprovedID != "" && rec.Status == StatusApproved {
		m.approved[rec.ApprovedID] = rec.ID
	}
	if rec.Status == StatusConsumed {
		delete(m.approved, rec.ApprovedID)
	}
	return nil
}

func (m *Memory) Get(id string) (Record, bool) {
	rec, ok := m.pending[id]
	return rec, ok
}

func (m *Memory) FindApproved(id string) (Record, bool) {
	if id == "" {
		return Record{}, false
	}
	pendingID, ok := m.approved[id]
	if !ok {
		return Record{}, false
	}
	rec, ok := m.pending[pendingID]
	if !ok || rec.ApprovedID != id || rec.Status != StatusApproved {
		m.Remove(rec)
		delete(m.approved, id)
		return Record{}, false
	}
	return rec, true
}

func (m *Memory) Claim(string) (bool, error) {
	return true, nil
}

func (m *Memory) Remove(rec Record) {
	delete(m.pending, rec.ID)
	if rec.ApprovedID != "" {
		delete(m.approved, rec.ApprovedID)
	}
}

// Files keeps records as JSON under Dir/confirmations.
type Files struct {
	Memory
	Dir string
}

func (f *Files) Put(rec Record) error {
	if err := f.write(rec); err != nil {
		return err
	}
	return f.Memory.Put(rec)
}

func (f *Files) Get(id string) (Record, bool) {
	if rec, ok := f.read(id); ok {
		_ = f.Memory.Put(rec)
		return rec, true
	}
	return f.Memory.Get(id)
}

func (f *Files) FindApproved(id string) (Record, bool) {
	if rec, ok := f.Memory.FindApproved(id); ok {
		disk, ok := f.read(rec.ID)
		if !ok || disk.ApprovedID != id || disk.Status != StatusApproved {
			f.Remove(rec)
		} else {
			_ = f.Memory.Put(disk)
			return disk, true
		}
	}
	entries, err := os.ReadDir(filepath.Join(f.Dir, "confirmations"))
	if err != nil {
		return Record{}, false
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		rec, ok := f.read(strings.TrimSuffix(entry.Name(), ".json"))
		if !ok || rec.ApprovedID != id || rec.Status != StatusApproved {
			continue
		}
		_ = f.Memory.Put(rec)
		return rec, true
	}
	return Record{}, false
}

func (f *Files) Claim(id string) (bool, error) {
	id = filepath.Base(id)
	if !plainID(id) {
		return false, errors.New("approval id")
	}
	dir := filepath.Join(f.Dir, "confirmations")
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

func (f *Files) Remove(rec Record) {
	if id := filepath.Base(rec.ID); plainID(id) {
		dir := filepath.Join(f.Dir, "confirmations")
		_ = os.Remove(filepath.Join(dir, id+".json"))
		_ = os.Remove(filepath.Join(dir, id+".claimed"))
	}
	f.Memory.Remove(rec)
}

func (f *Files) write(rec Record) error {
	id := filepath.Base(rec.ID)
	if !plainID(id) {
		return errors.New("approval id")
	}
	dir := filepath.Join(f.Dir, "confirmations")
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

func (f *Files) read(id string) (Record, bool) {
	id = filepath.Base(id)
	if !plainID(id) {
		return Record{}, false
	}
	body, err := os.ReadFile(filepath.Join(f.Dir, "confirmations", id+".json"))
	if err != nil {
		return Record{}, false
	}
	var rec Record
	if err := json.Unmarshal(body, &rec); err != nil || rec.ID != id {
		return Record{}, false
	}
	return rec, true
}
