package auth

import (
	"context"
	"sync"
	"time"
)

type (
	cacheEntry struct {
		mat Material
	}

	tokenCache struct {
		mu sync.Mutex
		m  map[string]cacheEntry
	}
)

func (c *tokenCache) fresh(key string, now time.Time) (Material, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok || !fresh(e.mat.Expires, now) {
		return Material{}, false
	}
	return cloneMaterial(e.mat), true
}

func (c *tokenCache) usable(key string, now time.Time) (Material, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok || !usable(e.mat.Expires, now) {
		return Material{}, false
	}
	return cloneMaterial(e.mat), true
}

func (c *tokenCache) put(key string, mat Material) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = cacheEntry{mat: cloneMaterial(mat)}
}

func (c *tokenCache) delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, key)
}

func cloneMaterial(m Material) Material {
	return Material{
		Headers: cloneMap(m.Headers),
		Query:   cloneMap(m.Query),
		Expires: m.Expires,
		Secrets: append([]string(nil), m.Secrets...),
		Sign:    m.Sign,
	}
}

type (
	flight struct {
		mu sync.Mutex
		m  map[string]*flightCall
	}

	flightCall struct {
		done chan struct{}
		mat  Material
		err  error
	}
)

func (f *flight) Do(ctx context.Context, key string, fn func() (Material, error)) (Material, error) {
	f.mu.Lock()
	if f.m == nil {
		f.m = map[string]*flightCall{}
	}
	if c, ok := f.m[key]; ok {
		f.mu.Unlock()
		select {
		case <-c.done:
			return cloneMaterial(c.mat), c.err
		case <-ctx.Done():
			return Material{}, ctx.Err()
		}
	}
	c := &flightCall{done: make(chan struct{})}
	f.m[key] = c
	f.mu.Unlock()
	c.mat, c.err = fn()
	close(c.done)
	f.mu.Lock()
	delete(f.m, key)
	f.mu.Unlock()
	return cloneMaterial(c.mat), c.err
}
