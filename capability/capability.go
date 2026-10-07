// Package capability is search, describe, and invoke. MCP and JSON call Server.
package capability

import (
	"reflect"
	"strings"
)

const (
	SearchName          = "capabilities_search"
	SearchCommand       = "search"
	SearchDescription   = "Search operations. Hits include id, call, related ids, related_calls, and confirmation. Paginate with offset and limit."
	DescribeName        = "capabilities_describe"
	DescribeCommand     = "describe"
	DescribeDescription = "Describe one operation by id. Returns the operation, the call line, related ids, and the relation note."
	InvokeName          = "capabilities_invoke"
	InvokeCommand       = "invoke"
	InvokeDescription   = "Invoke an operation through policy and HTTP. params values are strings. params.body may be a JSON object and is sent as the request body. confirmation_required includes a pending id. That id does not run the call; code pending_approval means submit the id from veto approve. why names the stage: parameter, policy, catalog, upstream, missing auth, or held until you approve. sent is true when the request reached the transport. http is true when a response was received. idempotency_key is a stable retry key after sent without a response. When chat approval is on and the host supports elicitation, the host asks the person; accept runs the call, and decline leaves the pending id. veto approve records the approval and prints the id a later invoke accepts once. preview stops before a token URL and before upstream HTTP. fields names the JSON fields a successful call returns. With no fields, the body is unchanged. truncated means the page walk or body cap stopped. A successful call includes next_calls when a relation names a path or query value in the response. A missing field is omitted, and a header is not copied."
)

type (
	SearchArgs struct {
		Query  string `json:"query" jsonschema:"search query"`
		Offset int    `json:"offset,omitempty" jsonschema:"hit offset"`
		Limit  int    `json:"limit,omitempty" jsonschema:"page size"`
	}

	DescribeArgs struct {
		OperationID string `json:"operation_id" jsonschema:"operation id"`
	}

	InvokeArgs struct {
		OperationID string   `json:"operation_id" jsonschema:"operation id"`
		Params      Params   `json:"params,omitempty" jsonschema:"parameters; strings, or a JSON object for body"`
		ApprovalID  string   `json:"approval_id,omitempty" jsonschema:"approved id from veto approve; a pending id does not run the call"`
		Token       string   `json:"token,omitempty" jsonschema:"user token for this call when the scheme source is invoke"`
		Preview     bool     `json:"preview,omitempty" jsonschema:"resolve, validate, and check policy, then stop before a token URL and before upstream HTTP"`
		Fields      []string `json:"fields,omitempty" jsonschema:"response fields to return; omit them to keep the whole body"`
		Offset      int      `json:"offset,omitempty" jsonschema:"page offset when fields are set"`
		Limit       int      `json:"limit,omitempty" jsonschema:"page size when fields are set"`
		Idempotency string   `json:"idempotency_key,omitempty" jsonschema:"stable key for a retry after sent without a response"`
	}

	// PinArgs is invoke for a pinned tool. The pin is the operation.
	PinArgs struct {
		Params      Params   `json:"params,omitempty" jsonschema:"parameters; strings, or a JSON object for body"`
		ApprovalID  string   `json:"approval_id,omitempty" jsonschema:"approved id from veto approve; a pending id does not run the call"`
		Token       string   `json:"token,omitempty" jsonschema:"user token for this call when the scheme source is invoke"`
		Preview     bool     `json:"preview,omitempty" jsonschema:"resolve, validate, and check policy, then stop before a token URL and before upstream HTTP"`
		Fields      []string `json:"fields,omitempty" jsonschema:"response fields to return; omit them to keep the whole body"`
		Offset      int      `json:"offset,omitempty" jsonschema:"page offset when fields are set"`
		Limit       int      `json:"limit,omitempty" jsonschema:"page size when fields are set"`
		Idempotency string   `json:"idempotency_key,omitempty" jsonschema:"stable key for a retry after sent without a response"`
	}

	// Line is one skill request. Exactly one of Search, Describe, or Invoke is set.
	Line struct {
		Search   *SearchArgs   `json:"search,omitempty"`
		Describe *DescribeArgs `json:"describe,omitempty"`
		Invoke   *InvokeArgs   `json:"invoke,omitempty"`
		Caller   string        `json:"caller,omitempty"`
	}

	Capability struct {
		Name        string       `json:"name"`
		Command     string       `json:"command"`
		Description string       `json:"description"`
		Input       []InputField `json:"input"`
	}

	InputField struct {
		Name        string `json:"name"`
		Type        string `json:"type"`
		Required    bool   `json:"required,omitempty"`
		Description string `json:"description,omitempty"`
	}

	Help struct {
		Capabilities []Capability `json:"capabilities"`
	}
)

func All() []Capability {
	return []Capability{
		{Name: SearchName, Command: SearchCommand, Description: SearchDescription, Input: inputFields(SearchArgs{})},
		{Name: DescribeName, Command: DescribeCommand, Description: DescribeDescription, Input: inputFields(DescribeArgs{})},
		{Name: InvokeName, Command: InvokeCommand, Description: InvokeDescription, Input: inputFields(InvokeArgs{})},
	}
}

func HelpJSON() Help {
	return Help{Capabilities: All()}
}

func ByCommand(name string) (Capability, bool) {
	for _, c := range All() {
		if c.Command == name {
			return c, true
		}
	}
	return Capability{}, false
}

func inputFields(sample any) []InputField {
	t := reflect.TypeOf(sample)
	out := make([]InputField, 0, t.NumField())
	for f := range t.Fields() {
		name, typ, required := jsonField(f)
		if name == "" || name == "-" {
			continue
		}
		out = append(out, InputField{
			Name:        name,
			Type:        typ,
			Required:    required,
			Description: f.Tag.Get("jsonschema"),
		})
	}
	return out
}

func jsonField(f reflect.StructField) (name, typ string, required bool) {
	tag := f.Tag.Get("json")
	name, opt, _ := strings.Cut(tag, ",")
	if name == "" {
		name = f.Name
	}
	required = opt != "omitempty" && name != "-"
	typ = jsonType(f.Type.Kind())
	return name, typ, required
}

func jsonType(k reflect.Kind) string {
	switch {
	case k == reflect.Map:
		return "object"
	case k == reflect.Slice:
		return "array"
	case k == reflect.Bool:
		return "boolean"
	case k >= reflect.Int && k <= reflect.Int64:
		return "integer"
	default:
		return "string"
	}
}
