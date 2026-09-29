package result

import "fmt"

type (
	HTTPResult struct {
		Status    int
		Body      string
		Code      string
		Retryable bool
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
