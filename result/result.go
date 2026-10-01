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
	}

	ParamError struct {
		Operation string
		Name      string
	}
)

func (e ParamError) Error() string {
	if e.Name == "" {
		return fmt.Sprintf("operation %s: empty path parameter", e.Operation)
	}
	return fmt.Sprintf("operation %s: %s required", e.Operation, e.Name)
}
