// Package yamlfile checks one YAML document before a typed decode.
// Alias targets are counted once, so a repeated alias fails by size instead of by time.
package yamlfile

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

const maxNodes = 100_000

// Prepare accepts one document. It rejects a second document, a duplicate key,
// an alias cycle, and an alias graph whose expanded size exceeds the limit.
func Prepare(data []byte) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
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
	_, err = measure(&doc, map[*yaml.Node]int{}, map[*yaml.Node]struct{}{})
	return err
}

// Decode is Prepare followed by a decode that rejects unknown fields.
func Decode(data []byte, out any) error {
	if err := Prepare(data); err != nil {
		return err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func measure(n *yaml.Node, memo map[*yaml.Node]int, active map[*yaml.Node]struct{}) (int, error) {
	if n == nil {
		return 0, nil
	}
	if cost, ok := memo[n]; ok {
		return cost, nil
	}
	if _, seen := active[n]; seen {
		return 0, errors.New("yaml alias cycle")
	}
	active[n] = struct{}{}
	defer delete(active, n)

	cost := 1
	switch n.Kind {
	case yaml.AliasNode:
		inner, err := measure(n.Alias, memo, active)
		if err != nil {
			return 0, err
		}
		var addErr error
		cost, addErr = add(cost, inner)
		if addErr != nil {
			return 0, addErr
		}
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, child := range n.Content {
			inner, err := measure(child, memo, active)
			if err != nil {
				return 0, err
			}
			var addErr error
			cost, addErr = add(cost, inner)
			if addErr != nil {
				return 0, addErr
			}
		}
	case yaml.MappingNode:
		keys := map[string]struct{}{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i].Value
			if _, ok := keys[key]; ok {
				return 0, fmt.Errorf("duplicate field %q", key)
			}
			keys[key] = struct{}{}
			for _, child := range []*yaml.Node{n.Content[i], n.Content[i+1]} {
				inner, err := measure(child, memo, active)
				if err != nil {
					return 0, err
				}
				var addErr error
				cost, addErr = add(cost, inner)
				if addErr != nil {
					return 0, addErr
				}
			}
		}
	case yaml.ScalarNode:
		cost = 1
	}
	memo[n] = cost
	return cost, nil
}

func add(total, n int) (int, error) {
	if n < 0 || total > maxNodes-n {
		return 0, errors.New("yaml alias expansion exceeds limit")
	}
	return total + n, nil
}
