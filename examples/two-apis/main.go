package main

import (
	"bytes"
	"context"
	"encoding/json"
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
	cat, err := loadCatalog()
	if err != nil {
		return err
	}
	sem := semantics.NewDerived(cat)
	if err := printSurface(cat, sem); err != nil {
		return err
	}
	rec, err := telemetry.Record()
	if err != nil {
		return err
	}
	defer rec.Stop(ctx)

	orders := serve(ordersHandler)
	customers := serve(customersHandler)
	defer orders.Close()
	defer customers.Close()
	pointAt(cat, orders.url, customers.url)

	loop, err := agent.New(cat, sem, execute.Client{Auth: map[string]string{"bearerAuth": secret}})
	if err != nil {
		return err
	}
	if err := followOrder(ctx, loop, orders, customers); err != nil {
		return err
	}
	if err := deleteOrder(ctx, loop, orders); err != nil {
		return err
	}
	text := replay.FromSpans(rec.Spans(), true).String()
	fmt.Printf("trace:\n%s", text)
	if strings.Contains(text, secret) {
		return fmt.Errorf("trace contains the secret")
	}
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

func loadCatalog() (*catalog.Catalog, error) {
	dir, err := exampleDir()
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
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
		return "", fmt.Errorf("locate example")
	}
	return filepath.Dir(file), nil
}

func printSurface(cat *catalog.Catalog, sem *semantics.Derived) error {
	names := mcpserver.ToolNames(cat, nil, false, false)
	fmt.Printf("operations: %d\n", len(cat.Operations))
	for _, op := range cat.Operations {
		fmt.Printf("  %s %s %s\n", op.ID, op.Method, op.PathTemplate)
	}
	fmt.Printf("tools: %d\n", len(names))
	for _, name := range names {
		fmt.Printf("  %s\n", name)
	}
	pack := runctx.NewBuilder(0).Build(cat, []runctx.Turn{{Role: "user", Content: "get order"}}, cat.ByID("orders.get"), sem, nil)
	note := pack.Serialize()
	fmt.Printf("note:\n%s\n", note)
	described, err := describe(cat, sem)
	if err != nil {
		return err
	}
	fmt.Printf("describe:\n%s\n", described)
	joined := note + "\n" + described
	fmt.Printf("raw openapi in note: %t\n", runctx.ContainsRawSpec(joined))
	if !strings.Contains(joined, "Order.customerId identifies customers.get") {
		return fmt.Errorf("note missing the declared link")
	}
	if runctx.ContainsRawSpec(joined) {
		return fmt.Errorf("note contains the raw document")
	}
	if len(names) != 3 || len(cat.Operations) < len(names) {
		return fmt.Errorf("operations %d tools %d", len(cat.Operations), len(names))
	}
	return nil
}

func describe(cat *catalog.Catalog, sem *semantics.Derived) (string, error) {
	raw, err := (&mcpserver.Server{Catalog: cat, Semantics: sem}).Describe("orders.get")
	if err != nil {
		return "", err
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, raw, "", "  "); err != nil {
		return "", err
	}
	return pretty.String(), nil
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

func followOrder(ctx context.Context, loop *agent.Loop, orders, customers *served) error {
	calls, err := loop.Follow(ctx, "orders.get", map[string]string{"id": "123"}, "")
	if err != nil {
		return err
	}
	for _, call := range calls {
		fmt.Printf("call %s status=%s body=%s\n", call.OperationID, call.Status, call.Body)
	}
	fmt.Println("orders http:")
	for _, line := range orders.hits {
		fmt.Println(line)
	}
	fmt.Println("customers http:")
	for _, line := range customers.hits {
		fmt.Println(line)
	}
	if len(calls) != 2 || calls[0].OperationID != "orders.get" || calls[1].OperationID != "customers.get" {
		return fmt.Errorf("follow did not walk the link")
	}
	if !strings.Contains(calls[0].Body, `"customerId":"7"`) {
		return fmt.Errorf("orders.get body %s", calls[0].Body)
	}
	if len(customers.hits) != 1 || !strings.Contains(customers.hits[0], "GET /customers/7") {
		return fmt.Errorf("customers.get was not called for 7")
	}
	if !strings.Contains(customers.hits[0], "Bearer "+secret) {
		return fmt.Errorf("customers.get went out without the secret")
	}
	return nil
}

func deleteOrder(ctx context.Context, loop *agent.Loop, orders *served) error {
	before := len(orders.hits)
	held, err := loop.Invoke(ctx, "orders.delete", map[string]string{"id": "123"}, "")
	if err != nil {
		return err
	}
	fmt.Printf("orders.delete before status=%s http=%d\n", held.Status, len(orders.hits)-before)
	if held.Status != "confirmation_required" || len(orders.hits) != before {
		return fmt.Errorf("delete went out before yes")
	}
	sent, err := loop.Invoke(ctx, "orders.delete", map[string]string{"id": "123"}, held.ApprovalID)
	if err != nil {
		return err
	}
	fmt.Printf("orders.delete after status=%s\n", sent.Status)
	for _, line := range orders.hits[before:] {
		fmt.Println(line)
	}
	if sent.Status != "ok" || len(orders.hits) != before+1 {
		return fmt.Errorf("delete did not go out after yes")
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
