package chat

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"sync"

	"9router/proxy/internal/db"
	"9router/proxy/internal/log"
)

// extraBodySkipWarn keeps an unparseable client body from flooding the log:
// the first one warns, the rest are debug lines.
var extraBodySkipWarn sync.Once

// applyCustomExtraBody merges the operator's extraBody for provider/model into
// an outbound OpenAI Chat body. The operator wins on a shared top-level key;
// `provider` is deep-merged so a client's own routing preference survives
// where the operator is silent; every other value (reasoning, arrays) is
// replaced whole. Reserved keys are re-checked here because an imported row
// never went through the save validation.
func (h *ChatHandler) applyCustomExtraBody(body []byte, provider, model string) []byte {
	if resolveProviderAlias(provider) != db.ExtraBodyProvider {
		return body
	}
	extra := h.Repo.CustomModelExtraBody(db.ExtraBodyProvider, model)
	if len(extra) == 0 {
		return body
	}
	out, err := mergeExtraBody(body, extra)
	if err != nil {
		logExtraBodySkip(model, err)
		return body
	}
	return out
}

// logExtraBodySkip records a skipped merge without any body content.
func logExtraBodySkip(model string, err error) {
	logged := false
	extraBodySkipWarn.Do(func() {
		logged = true
		log.Warn("extra-body", "client body not mergeable; extraBody skipped", "model", model, "error", err)
	})
	if !logged {
		log.Debug("extra-body", "client body not mergeable; extraBody skipped", "model", model, "error", err)
	}
}

func mergeExtraBody(body []byte, extra map[string]jsontext.Value) ([]byte, error) {
	// jsontext.Value keeps untouched members byte-exact (no float64 rounding).
	var m map[string]jsontext.Value
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, db.ErrExtraBodyNotObject
	}
	for key, value := range extra {
		if db.ExtraBodyDeniedKey(key) {
			continue
		}
		if key == "provider" {
			value = deepMergeJSON(m[key], value)
		}
		m[key] = value
	}
	return json.Marshal(m, json.Deterministic(true))
}

// deepMergeJSON overlays over onto base when both are objects, recursively;
// any other pairing returns over unchanged.
func deepMergeJSON(base, over jsontext.Value) jsontext.Value {
	if base.Kind() != '{' || over.Kind() != '{' {
		return over
	}
	var b, o map[string]jsontext.Value
	if json.Unmarshal(base, &b) != nil || json.Unmarshal(over, &o) != nil {
		return over
	}
	for key, value := range o {
		b[key] = deepMergeJSON(b[key], value)
	}
	out, err := json.Marshal(b, json.Deterministic(true))
	if err != nil {
		return over
	}
	return out
}
