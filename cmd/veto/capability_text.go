package main

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"

	jsonv2 "encoding/json/v2"
)

func presentCapability(name string, body []byte) (string, bool) {
	switch name {
	case "search":
		return presentSearch(body)
	case "describe":
		return presentDescribe(body)
	case "invoke":
		return presentInvoke(body)
	default:
		return "", false
	}
}

func presentSearch(body []byte) (string, bool) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || bytes.Equal(trimmed, []byte("[]")) {
		return "no operations\n", true
	}
	var hits []struct {
		Call string `json:"call"`
	}
	if err := jsonv2.Unmarshal(trimmed, &hits); err != nil {
		return "", false
	}
	if len(hits) == 0 {
		return "no operations\n", true
	}
	var b strings.Builder
	for _, hit := range hits {
		if hit.Call == "" {
			continue
		}
		b.WriteString(hit.Call)
		b.WriteByte('\n')
	}
	if b.Len() == 0 {
		return "no operations\n", true
	}
	return b.String(), true
}

func presentDescribe(body []byte) (string, bool) {
	var view struct {
		Call      string `json:"call"`
		Relation  string `json:"relation"`
		Question  string `json:"question"`
		Task      string `json:"task"`
		Operation struct {
			ID             string   `json:"ID"`
			Method         string   `json:"Method"`
			PathTemplate   string   `json:"PathTemplate"`
			ResponseFields []string `json:"ResponseFields"`
			Params         []struct {
				Name     string `json:"Name"`
				In       string `json:"In"`
				Required bool   `json:"Required"`
			} `json:"Params"`
		} `json:"operation"`
	}
	if err := jsonv2.Unmarshal(bytes.TrimSpace(body), &view); err != nil {
		return "", false
	}
	if view.Task != "" {
		line := view.Task
		if view.Question != "" {
			line += ": " + view.Question
		}
		return line + "\n", true
	}
	line := view.Call
	if line == "" {
		line = view.Operation.ID
	}
	if line == "" {
		return "", false
	}
	var b strings.Builder
	if view.Operation.Method != "" && view.Operation.PathTemplate != "" {
		b.WriteString(view.Operation.Method)
		b.WriteByte(' ')
		b.WriteString(view.Operation.PathTemplate)
		b.WriteByte('\n')
	}
	b.WriteString(line)
	b.WriteByte('\n')
	for _, p := range view.Operation.Params {
		if p.In != "path" && !p.Required {
			continue
		}
		b.WriteString(p.Name)
		b.WriteString(" required\n")
	}
	if len(view.Operation.ResponseFields) > 0 {
		b.WriteString(strings.Join(view.Operation.ResponseFields, " "))
		b.WriteByte('\n')
	}
	if view.Relation != "" {
		b.WriteString(view.Relation)
		b.WriteByte('\n')
	}
	return b.String(), true
}

func presentInvoke(body []byte) (string, bool) {
	var out struct {
		Status      string `json:"status"`
		OperationID string `json:"operation_id"`
		HTTPStatus  int    `json:"http_status"`
		Body        string `json:"body"`
		Code        string `json:"code"`
		Error       string `json:"error"`
		Why         string `json:"why"`
	}
	if err := jsonv2.Unmarshal(bytes.TrimSpace(body), &out); err != nil {
		return "", false
	}
	id := out.OperationID
	if id == "" {
		id = "invoke"
	}
	if out.Status == "ok" {
		line := id + ": ok"
		if out.Body != "" && out.Body != "null" {
			line += "\n" + out.Body
		}
		return line + "\n", true
	}
	why := invokeDetail(id, out.Why, out.Error)
	if why == "" {
		why = out.Status
	}
	if why == "" {
		return "", false
	}
	line := id + ": " + why
	if out.HTTPStatus != 0 {
		line += " " + strconv.Itoa(out.HTTPStatus)
		if out.Code != "" {
			line += " " + out.Code
		}
	}
	return line + "\n", true
}

func invokeDetail(id, why, errText string) string {
	msg := strings.TrimSpace(errText)
	msg = strings.TrimPrefix(msg, "operation "+id+": ")
	if msg != "" && (why == "" || why == "parameter" || why == "catalog") {
		return msg
	}
	if why != "" {
		return why
	}
	return msg
}

func invokeArgs(position []string, operation string, pairs []string) ([]string, error) {
	args := append([]string(nil), position...)
	if operation != "" {
		switch {
		case len(args) == 0:
			args = []string{operation}
		case strings.HasPrefix(strings.TrimSpace(args[0]), "{"):
			return nil, errors.New("operation flag repeats a JSON body")
		case args[0] != operation:
			return nil, fmt.Errorf("operation is %s and %s", args[0], operation)
		}
	}
	if len(pairs) == 0 {
		return args, nil
	}
	if len(args) == 0 {
		return nil, errors.New("operation required")
	}
	if len(args) > 1 || strings.HasPrefix(strings.TrimSpace(args[0]), "{") {
		return nil, errors.New("param repeats a JSON body")
	}
	params := map[string]string{}
	for _, pair := range pairs {
		key, value, ok := strings.Cut(pair, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return nil, fmt.Errorf("param %q is not key=value", pair)
		}
		params[key] = value
	}
	raw, err := jsonv2.Marshal(params)
	if err != nil {
		return nil, err
	}
	return append(args, string(raw)), nil
}
