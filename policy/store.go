package policy

import (
	"context"
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
// Claim is consume-once. valkey is the replica adapter: SET NX on the pending id.
type Store interface {
	Put(ctx context.Context, rec Record) error
	Get(ctx context.Context, id string) (Record, bool)
	FindApproved(ctx context.Context, approvedID string) (Record, bool)
	Claim(ctx context.Context, id string) (bool, error)
	Remove(ctx context.Context, rec Record)
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

func (m *Memory) Put(ctx context.Context, rec Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}
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

func (m *Memory) Get(ctx context.Context, id string) (Record, bool) {
	if ctx.Err() != nil {
		return Record{}, false
	}
	rec, ok := m.pending[id]
	return rec, ok
}

func (m *Memory) FindApproved(ctx context.Context, id string) (Record, bool) {
	if ctx.Err() != nil || id == "" {
		return Record{}, false
	}
	pendingID, ok := m.approved[id]
	if !ok {
		return Record{}, false
	}
	rec, ok := m.pending[pendingID]
	if !ok || rec.ApprovedID != id || rec.Status != StatusApproved {
		m.Remove(ctx, rec)
		delete(m.approved, id)
		return Record{}, false
	}
	return rec, true
}

func (m *Memory) Claim(ctx context.Context, _ string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return true, nil
}

func (m *Memory) Remove(ctx context.Context, rec Record) {
	if ctx.Err() != nil {
		return
	}
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

func (f *Files) Put(ctx context.Context, rec Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := f.write(rec); err != nil {
		return err
	}
	return f.Memory.Put(ctx, rec)
}

func (f *Files) Get(ctx context.Context, id string) (Record, bool) {
	if ctx.Err() != nil {
		return Record{}, false
	}
	if rec, ok := f.read(id); ok {
		_ = f.Memory.Put(ctx, rec)
		return rec, true
	}
	return f.Memory.Get(ctx, id)
}

func (f *Files) FindApproved(ctx context.Context, id string) (Record, bool) {
	if ctx.Err() != nil {
		return Record{}, false
	}
	if rec, ok := f.Memory.FindApproved(ctx, id); ok {
		disk, ok := f.read(rec.ID)
		if !ok || disk.ApprovedID != id || disk.Status != StatusApproved {
			f.Remove(ctx, rec)
		} else {
			_ = f.Memory.Put(ctx, disk)
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
		_ = f.Memory.Put(ctx, rec)
		return rec, true
	}
	return Record{}, false
}

func (f *Files) Claim(ctx context.Context, id string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
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

func (f *Files) Remove(ctx context.Context, rec Record) {
	if ctx.Err() != nil {
		return
	}
	if id := filepath.Base(rec.ID); plainID(id) {
		dir := filepath.Join(f.Dir, "confirmations")
		_ = os.Remove(filepath.Join(dir, id+".json"))
		_ = os.Remove(filepath.Join(dir, id+".claimed"))
	}
	f.Memory.Remove(ctx, rec)
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
