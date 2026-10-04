// Package result is an HTTP result and the invoke errors returned to a caller.
package result

import "fmt"

type (
	// Page is the slice of a list response that was returned.
	Page struct {
		Offset   int `json:"offset"`
		Limit    int `json:"limit,omitempty"`
		Returned int `json:"returned"`
	}

	HTTPResult struct {
		Status    int
		Body      string
		Code      string
		Retryable bool
		Truncated bool
		Page      *Page
		HTTP      bool // a response was received
		Sent      bool // the request was given to the transport
	}

	ParamError struct {
		Operation string
		Name      string
	}

	// BodyError is a request body that does not match the operation's schema. It does not carry the value.
	BodyError struct {
		Operation string
		Path      string
		Reason    string
	}

	// AuthError is a required credential that is not set. It does not carry the secret.
	AuthError struct {
		Name   string
		Detail string
	}
)

func (e BodyError) Error() string {
	return fmt.Sprintf("operation %s: body %s: %s", e.Operation, e.Path, e.Reason)
}

func (e AuthError) Error() string {
	if e.Detail != "" {
		if e.Name == "" {
			return e.Detail
		}
		return e.Name + " " + e.Detail
	}
	if e.Name == "" {
		return "credential is unset"
	}
	return e.Name + " is unset"
}

func (e ParamError) Error() string {
	if e.Name == "" {
		return fmt.Sprintf("operation %s: empty path parameter", e.Operation)
	}
	return fmt.Sprintf("operation %s: %s required", e.Operation, e.Name)
}
