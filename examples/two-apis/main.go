// Package main runs two contracts through one catalog.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/mcpserver"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/replay"
	"github.com/aiveto/veto/runctx"
	"github.com/aiveto/veto/semantics"
	"github.com/aiveto/veto/telemetry"
)

const secret = "example-secret"

type served struct {
	url  string
	hits []string
	stop func()
}

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "example: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cat, err := loadCatalog(ctx)
	if err != nil {
		return err
	}
	sem := semantics.NewDerived(cat)
	sentence, err := checkSurface(cat, sem)
	if err != nil {
		return err
	}
	rec, err := telemetry.Record()
	if err != nil {
		return err
	}
	defer func() {
		if stopErr := rec.Stop(ctx); stopErr != nil {
			fmt.Fprintf(os.Stderr, "example: %v\n", stopErr)
		}
	}()

	orders := serve(ordersHandler)
	customers := serve(customersHandler)
	defer orders.Close()
	defer customers.Close()
	pointAt(cat, orders.url, customers.url)

	loop, err := agent.New(cat, sem, execute.Client{Auth: map[string]string{"bearerAuth": secret}})
	if err != nil {
		return err
	}
	customerID, err := followOrder(ctx, loop, orders, customers)
	if err != nil {
		return err
	}
	if err := deleteOrder(ctx, loop, orders); err != nil {
		return err
	}
	text := replay.FromSpans(rec.Spans(), true).String()
	if text == "" {
		return errors.New("trace is empty")
	}
	if strings.Contains(text, secret) {
		return errors.New("trace contains the secret")
	}
	fmt.Printf("Someone asked who placed order 123.\n")
	fmt.Printf("orders.get returned customerId %s.\n", customerID)
	fmt.Printf("customers.get was called for %s because the note said %s.\n", customerID, sentence)
	fmt.Printf("orders.delete sent no HTTP until approved.\n")
	fmt.Printf("The trace left the secret out.\n")
	return nil
}

func serve(fn http.HandlerFunc) *served {
	s := &served{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits = append(s.hits, r.Method+" "+r.URL.Path+" "+r.Header.Get("Authorization"))
		fn(w, r)
	}))
	s.url = srv.URL
	s.stop = srv.Close
	return s
}

func (s *served) Close() {
	if s != nil && s.stop != nil {
		s.stop()
	}
}

func loadCatalog(ctx context.Context) (*catalog.Catalog, error) {
	dir, err := exampleDir()
	if err != nil {
		return nil, err
	}
	orders, err := openapi.Load(ctx, filepath.Join(dir, "orders.yaml"))
	if err != nil {
		return nil, err
	}
	customers, err := openapi.Load(ctx, filepath.Join(dir, "customers.yaml"))
	if err != nil {
		return nil, err
	}
	cat, err := catalog.Merge(orders, customers)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "relations.yaml"))
	if err != nil {
		return nil, fmt.Errorf("read relations: %w", err)
	}
	rels, err := catalog.ParseRelations(data)
	if err != nil {
		return nil, err
	}
	if err := catalog.ApplyRelations(cat, rels); err != nil {
		return nil, err
	}
	return cat, nil
}

func exampleDir() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("locate example")
	}
	return filepath.Dir(file), nil
}

func checkSurface(cat *catalog.Catalog, sem *semantics.Derived) (string, error) {
	names := mcpserver.ToolNames(cat, nil, false, false)
	pack := runctx.NewBuilder(0).Build(cat, []runctx.Turn{{Role: "user", Content: "who placed order 123"}}, cat.ByID("orders.get"), sem, nil)
	note := pack.Serialize()
	described, err := describe(cat, sem)
	if err != nil {
		return "", err
	}
	joined := note + "\n" + described
	sentence := sem.Note("orders.get").Relation
	if sentence == "" || !strings.Contains(note, sentence) {
		return "", errors.New("note missing the declared link")
	}
	if runctx.ContainsRawSpec(joined) {
		return "", errors.New("note contains the raw document")
	}
	if len(names) != 3 || len(cat.Operations) < len(names) {
		return "", fmt.Errorf("operations %d tools %d", len(cat.Operations), len(names))
	}
	return sentence, nil
}

func describe(cat *catalog.Catalog, sem *semantics.Derived) (string, error) {
	raw, err := (&mcpserver.Server{Catalog: cat, Semantics: sem}).Describe("orders.get")
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func pointAt(cat *catalog.Catalog, ordersURL, customersURL string) {
	for i := range cat.Operations {
		op := &cat.Operations[i]
		op.BaseURL = ordersURL
		if strings.HasPrefix(op.ID, "customers.") {
			op.BaseURL = customersURL
		}
	}
}

func followOrder(ctx context.Context, loop *agent.Loop, orders, customers *served) (string, error) {
	calls, err := loop.Follow(ctx, "orders.get", map[string]string{"id": "123"}, "")
	if err != nil {
		return "", err
	}
	if len(calls) != 2 || calls[0].OperationID != "orders.get" || calls[1].OperationID != "customers.get" {
		return "", errors.New("follow did not walk the link")
	}
	id, err := customerID(calls[0].Body)
	if err != nil {
		return "", err
	}
	if len(customers.hits) != 1 || !strings.Contains(customers.hits[0], "GET /customers/"+id) {
		return "", fmt.Errorf("customers.get was not called for %s", id)
	}
	if !strings.Contains(customers.hits[0], "Bearer "+secret) {
		return "", errors.New("customers.get went out without the secret")
	}
	if len(orders.hits) != 1 || !strings.Contains(orders.hits[0], "GET /orders/123") {
		return "", errors.New("orders.get was not called")
	}
	return id, nil
}

func customerID(body string) (string, error) {
	var fields map[string]string
	if err := json.Unmarshal([]byte(body), &fields); err != nil {
		return "", fmt.Errorf("orders.get body: %w", err)
	}
	id := fields["customerId"]
	if id == "" {
		return "", errors.New("orders.get missing customerId")
	}
	return id, nil
}

func deleteOrder(ctx context.Context, loop *agent.Loop, orders *served) error {
	before := len(orders.hits)
	held, err := loop.Invoke(ctx, "orders.delete", map[string]string{"id": "123"}, "")
	if err != nil {
		return err
	}
	if held.Status != "confirmation_required" || len(orders.hits) != before {
		return errors.New("delete went out before yes")
	}
	approved, err := loop.State.Approve(ctx, held.ApprovalID)
	if err != nil {
		return err
	}
	sent, err := loop.Invoke(ctx, "orders.delete", map[string]string{"id": "123"}, approved)
	if err != nil {
		return err
	}
	if sent.Status != "ok" || len(orders.hits) != before+1 {
		return errors.New("delete did not go out after yes")
	}
	last := orders.hits[len(orders.hits)-1]
	if !strings.Contains(last, "DELETE /orders/123") || !strings.Contains(last, "Bearer "+secret) {
		return fmt.Errorf("delete http %s", last)
	}
	return nil
}

func ordersHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.URL.Path == "/orders/123" {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"id":"123","customerId":"7"}`)
		return
	}
	http.NotFound(w, r)
}

func customersHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/customers/7" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprint(w, `{"id":"7"}`)
}
