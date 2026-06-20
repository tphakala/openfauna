package dataset

import (
	"bytes"
	"encoding/json"
)

// MarshalJSON renders v as 2-space-indented JSON without HTML escaping and with
// a trailing newline, matching the hand-authored data file convention (so that
// characters like &, < and > are not rewritten to \u00xx escapes). Map keys are
// emitted in ascending order by encoding/json, keeping output deterministic.
func MarshalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
