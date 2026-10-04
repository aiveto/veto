package capability

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
)

// Params is invoke arguments. Numbers stay digits, not float64.
type Params map[string]any

func (p *Params) UnmarshalJSON(b []byte) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var raw map[string]any
	if err := dec.Decode(&raw); err != nil {
		return err
	}
	*p = raw
	return nil
}

func (p *Params) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	val, err := dec.ReadValue()
	if err != nil {
		return err
	}
	return p.UnmarshalJSON(val)
}

// DecodeInvoke reads tool arguments from the raw JSON the host sent.
func DecodeInvoke(raw []byte) (InvokeArgs, error) {
	var args InvokeArgs
	if len(raw) == 0 {
		return args, nil
	}
	return args, json.Unmarshal(raw, &args)
}
