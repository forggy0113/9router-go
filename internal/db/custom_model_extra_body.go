package db

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"strings"

	"9router/proxy/internal/log"
	"9router/proxy/internal/providers"
)

// customModelsScope is the kv scope custom model rows live in.
const customModelsScope = "customModels"

// ExtraBodyProvider is the only canonical provider whose custom models carry an
// extraBody: OpenRouter's routing knobs (provider, reasoning, transforms, ...)
// are request-body members a client usually cannot set itself.
const ExtraBodyProvider = "openrouter"

// ExtraBodyMaxBytes caps a custom model's extraBody. The object is merged into
// every request for that model, so it stays a few provider knobs, not a payload.
const ExtraBodyMaxBytes = 8 << 10

// extraBodyDeniedKeys are the request members the gateway owns: routing,
// conversation, streaming and tool wiring. An operator override of any of them
// would silently rewrite what the client asked for.
var extraBodyDeniedKeys = map[string]struct{}{
	"model": {}, "models": {}, "messages": {}, "input": {}, "prompt": {},
	"stream": {}, "stream_options": {}, "tools": {}, "tool_choice": {},
	"functions": {}, "function_call": {}, "response_format": {},
}

var (
	ErrExtraBodyNotObject   = errors.New("extraBody must be a JSON object")
	ErrExtraBodyTooLarge    = fmt.Errorf("extraBody exceeds %d bytes", ExtraBodyMaxBytes)
	ErrExtraBodyReservedKey = errors.New("extraBody key is reserved for the gateway")
	ErrExtraBodyProvider    = errors.New("extraBody is only supported for openrouter custom models")
)

// ExtraBodyDeniedKey reports whether key is a request member extraBody may not
// set. Case-insensitive: some upstreams match member names loosely.
func ExtraBodyDeniedKey(key string) bool {
	_, denied := extraBodyDeniedKeys[strings.ToLower(key)]
	return denied
}

// ExtraBodyProviderAllowed reports whether a custom model stored under alias
// may carry an extraBody, resolving the alias to its canonical provider id.
func ExtraBodyProviderAllowed(alias string) bool {
	return canonicalProvider(alias) == ExtraBodyProvider
}

func canonicalProvider(alias string) string {
	if canonical, ok := providers.ProviderAliasMap[alias]; ok {
		return canonical
	}
	return alias
}

// ParseExtraBody validates raw as a custom model's extraBody and returns its
// top-level members with their original bytes.
func ParseExtraBody(raw []byte) (map[string]jsontext.Value, error) {
	if len(raw) > ExtraBodyMaxBytes {
		return nil, ErrExtraBodyTooLarge
	}
	var members map[string]jsontext.Value
	if err := json.Unmarshal(raw, &members); err != nil || members == nil {
		return nil, ErrExtraBodyNotObject
	}
	for key := range members {
		if ExtraBodyDeniedKey(key) {
			return nil, fmt.Errorf("%w: %q", ErrExtraBodyReservedKey, key)
		}
	}
	return members, nil
}

// extraBodyIndex maps canonical provider id -> model id -> extraBody members.
type extraBodyIndex map[string]map[string]map[string]jsontext.Value

// extraBodySnapshot is an index plus the invalidation generation it was read
// under; a snapshot older than the current generation is never served.
type extraBodySnapshot struct {
	gen uint64
	idx extraBodyIndex
}

// afterExtraBodyRead is a test seam run between the rows read and the snapshot
// store, where a concurrent invalidation would otherwise be lost.
var afterExtraBodyRead func()

// CustomModelExtraBody returns the extraBody declared for provider/model, or
// nil. It reads an in-memory snapshot, so the request path never queries.
func (r *Repo) CustomModelExtraBody(provider, model string) map[string]jsontext.Value {
	if r == nil {
		return nil
	}
	return r.loadExtraBodyIndex()[provider][model]
}

// InvalidateCustomModelExtraBody retires the snapshot. Every customModels
// write that bypasses SetKV/SetKVMany/DeleteKV (a database import) must call it.
func (r *Repo) InvalidateCustomModelExtraBody() {
	r.extraBodyGen.Add(1)
}

func (r *Repo) loadExtraBodyIndex() extraBodyIndex {
	if s := r.extraBody.Load(); s != nil && s.gen == r.extraBodyGen.Load() {
		return s.idx
	}
	r.extraBodyBuild.Lock()
	defer r.extraBodyBuild.Unlock()
	gen := r.extraBodyGen.Load()
	if s := r.extraBody.Load(); s != nil && s.gen == gen {
		return s.idx
	}
	// gen is captured before the read: an invalidation landing during the
	// build bumps the generation, so this snapshot is rebuilt on next use.
	idx, err := r.buildExtraBodyIndex()
	if err != nil {
		// Not cached: the next request retries instead of serving an empty
		// index until some unrelated write happens to invalidate it.
		log.Warn("extra-body", "custom model read failed", "error", err)
		return idx
	}
	r.extraBody.Store(&extraBodySnapshot{gen: gen, idx: idx})
	return idx
}

// buildExtraBodyIndex reads every row once. A row that fails validation (an
// imported backup can carry anything) is skipped rather than half-applied.
func (r *Repo) buildExtraBodyIndex() (extraBodyIndex, error) {
	idx := extraBodyIndex{}
	customs, err := r.GetCustomModels()
	if afterExtraBodyRead != nil {
		afterExtraBodyRead()
	}
	if err != nil {
		return idx, err
	}
	for _, cm := range customs {
		if len(cm.ExtraBody) == 0 || string(cm.ExtraBody) == "null" {
			continue
		}
		members, err := ParseExtraBody(cm.ExtraBody)
		if err != nil {
			log.Warn("extra-body", "skip custom model extraBody", "provider", cm.ProviderAlias, "model", cm.ID, "error", err)
			continue
		}
		provider := canonicalProvider(cm.ProviderAlias)
		if idx[provider] == nil {
			idx[provider] = map[string]map[string]jsontext.Value{}
		}
		idx[provider][cm.ID] = members
	}
	return idx, nil
}
