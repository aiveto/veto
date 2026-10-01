package agentmeta

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/aiveto/veto/catalog"
	"gopkg.in/yaml.v3"
)

type (
	// Nil pointers leave the derived value.
	Entry struct {
		Operation    string   `yaml:"operation"`
		SideEffect   *string  `yaml:"side_effect"`
		Confirmation *bool    `yaml:"confirmation"`
		Permissions  []string `yaml:"permissions"`
		Idempotency  *string  `yaml:"idempotency"`
		Retry        *string  `yaml:"retry"`
		Exposure     *string  `yaml:"exposure"`
	}

	File struct {
		Operations []Entry `yaml:"operations"`
	}
)

func Load(path string) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return File{}, fmt.Errorf("read agent file: %w", err)
	}
	var f File
	if err := decodeStrict(data, &f); err != nil {
		return File{}, fmt.Errorf("parse agent file: %w", err)
	}
	return f, nil
}

func decodeStrict(data []byte, out any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	var extra yaml.Node
	err := dec.Decode(&extra)
	if err == nil {
		return errors.New("extra document")
	}
	if !errors.Is(err, io.EOF) {
		return err
	}
	if err := rejectDup(&doc, map[*yaml.Node]struct{}{}); err != nil {
		return err
	}
	known := yaml.NewDecoder(bytes.NewReader(data))
	known.KnownFields(true)
	return known.Decode(out)
}

func rejectDup(n *yaml.Node, active map[*yaml.Node]struct{}) error {
	if n == nil {
		return nil
	}
	if _, seen := active[n]; seen {
		return errors.New("yaml alias cycle")
	}
	active[n] = struct{}{}
	defer delete(active, n)
	switch n.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, child := range n.Content {
			if err := rejectDup(child, active); err != nil {
				return err
			}
		}
	case yaml.MappingNode:
		keys := map[string]struct{}{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i].Value
			if _, ok := keys[key]; ok {
				return fmt.Errorf("duplicate field %q", key)
			}
			keys[key] = struct{}{}
			if err := rejectDup(n.Content[i+1], active); err != nil {
				return err
			}
		}
	case yaml.AliasNode:
		return rejectDup(n.Alias, active)
	case yaml.ScalarNode:
		return nil
	}
	return nil
}

func Apply(cat *catalog.Catalog, f File) error {
	if cat == nil {
		return errors.New("missing catalog")
	}
	cat.Finalize()
	for _, e := range f.Operations {
		op := cat.ByID(e.Operation)
		if op == nil {
			return fmt.Errorf("unknown operation %q", e.Operation)
		}
		if e.SideEffect != nil {
			side := catalog.SideEffect(*e.SideEffect)
			switch side {
			case catalog.SideEffectNone, catalog.SideEffectWrite, catalog.SideEffectDestructive:
				op.SideEffect = side
			default:
				return fmt.Errorf("side effect %q is not none, write, or destructive", *e.SideEffect)
			}
		}
		if e.Confirmation != nil {
			op.RequiresConfirmation = *e.Confirmation
		} else if e.SideEffect != nil && op.SideEffect == catalog.SideEffectDestructive {
			op.RequiresConfirmation = true
		}
		if e.Permissions != nil {
			op.Permissions = append([]string(nil), e.Permissions...)
		}
		if e.Idempotency != nil {
			op.Idempotency = *e.Idempotency
		}
		if e.Retry != nil {
			op.Retry = *e.Retry
		}
		if e.Exposure != nil {
			switch *e.Exposure {
			case catalog.ExposureDirect, catalog.ExposureGrouped, catalog.ExposureDiscovery:
				op.Exposure = *e.Exposure
			default:
				return fmt.Errorf("exposure %q is not direct, grouped, or discovery-only", *e.Exposure)
			}
		}
	}
	return nil
}

func Confirmations(f File) map[string]*bool {
	out := map[string]*bool{}
	for _, e := range f.Operations {
		if e.Confirmation == nil {
			continue
		}
		v := *e.Confirmation
		out[e.Operation] = &v
	}
	return out
}

func ChangedConfirmations(base, next map[string]*bool) map[string]bool {
	seen := map[string]struct{}{}
	for id := range base {
		seen[id] = struct{}{}
	}
	for id := range next {
		seen[id] = struct{}{}
	}
	out := map[string]bool{}
	for id := range seen {
		b, bok := base[id]
		n, nok := next[id]
		if bok != nok || (bok && (b == nil || n == nil || *b != *n)) {
			out[id] = true
		}
	}
	return out
}
