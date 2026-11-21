package db

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestCustomModelExtraBodyRoundTripAndInvalidation(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	repo := NewRepo(database)

	row := `{"providerAlias":"openrouter","id":"anthropic/claude-x:free","type":"llm",` +
		`"extraBody":{"provider":{"order":["anthropic"]},"seed":12345678901234567}}`
	if err := repo.SetKV("customModels", "openrouter|anthropic/claude-x:free|llm", row); err != nil {
		t.Fatalf("SetKV: %v", err)
	}
	customs, err := repo.GetCustomModels()
	if err != nil || len(customs) != 1 {
		t.Fatalf("GetCustomModels = %v, %v", customs, err)
	}
	if got := string(customs[0].ExtraBody); !strings.Contains(got, `"seed":12345678901234567`) {
		t.Fatalf("ExtraBody = %s, want the stored bytes", got)
	}
	extra := repo.CustomModelExtraBody("openrouter", "anthropic/claude-x:free")
	if string(extra["seed"]) != "12345678901234567" {
		t.Fatalf("CustomModelExtraBody = %v", extra)
	}

	// A save through SetKV must be visible without a restart.
	cleared := `{"providerAlias":"openrouter","id":"anthropic/claude-x:free","type":"llm"}`
	if err := repo.SetKV("customModels", "openrouter|anthropic/claude-x:free|llm", cleared); err != nil {
		t.Fatalf("SetKV clear: %v", err)
	}
	if extra := repo.CustomModelExtraBody("openrouter", "anthropic/claude-x:free"); extra != nil {
		t.Fatalf("after clear CustomModelExtraBody = %v, want nil", extra)
	}

	// A row that never went through save validation (an import) is skipped.
	if _, err := database.Exec(`INSERT INTO kv (scope, key, value) VALUES ('customModels', ?, ?)`,
		"or|bad|llm", `{"providerAlias":"or","id":"bad","extraBody":{"model":"other"}}`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	repo.InvalidateCustomModelExtraBody()
	if extra := repo.CustomModelExtraBody("openrouter", "bad"); extra != nil {
		t.Fatalf("reserved-key row indexed: %v", extra)
	}
}

func TestParseExtraBody(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr error
	}{
		{"object", `{"provider":{"sort":"price"}}`, nil},
		{"array", `[1]`, ErrExtraBodyNotObject},
		{"null", `null`, ErrExtraBodyNotObject},
		{"oversize", `{"x":"` + strings.Repeat("a", ExtraBodyMaxBytes) + `"}`, ErrExtraBodyTooLarge},
		{"reserved", `{"messages":[]}`, ErrExtraBodyReservedKey},
		{"reserved Model", `{"Model":"x"}`, ErrExtraBodyReservedKey},
		{"reserved STREAM", `{"STREAM":true}`, ErrExtraBodyReservedKey},
		{"reserved Messages", `{"Messages":[]}`, ErrExtraBodyReservedKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseExtraBody([]byte(tt.raw))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ParseExtraBody(%s) err = %v, want %v", tt.name, err, tt.wantErr)
			}
		})
	}
}

const extraBodyTestKey = "openrouter|vendor/m|llm"

func extraBodyTestRow(topK int) string {
	return fmt.Sprintf(`{"providerAlias":"openrouter","id":"vendor/m","extraBody":{"top_k":%d}}`, topK)
}

// An invalidation that lands while a snapshot is being built must win: the
// in-flight build may serve its own caller, but it must not be cached.
func TestCustomModelExtraBodyInvalidationDuringBuild(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	repo := NewRepo(database)
	if err := repo.SetKV("customModels", extraBodyTestKey, extraBodyTestRow(1)); err != nil {
		t.Fatalf("SetKV: %v", err)
	}
	afterExtraBodyRead = func() {
		afterExtraBodyRead = nil
		if err := repo.SetKV("customModels", extraBodyTestKey, extraBodyTestRow(2)); err != nil {
			t.Errorf("concurrent SetKV: %v", err)
		}
	}
	t.Cleanup(func() { afterExtraBodyRead = nil })

	repo.CustomModelExtraBody("openrouter", "vendor/m") // build races the write
	if got := string(repo.CustomModelExtraBody("openrouter", "vendor/m")["top_k"]); got != "2" {
		t.Fatalf("top_k = %s after the racing write, want 2 (stale snapshot cached)", got)
	}
}

// A failed read must not cache an empty index: the next call retries.
func TestCustomModelExtraBodyReadFailureNotCached(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	repo := NewRepo(database)
	if err := repo.SetKV("customModels", extraBodyTestKey, extraBodyTestRow(3)); err != nil {
		t.Fatalf("SetKV: %v", err)
	}
	if _, err := database.Exec(`ALTER TABLE kv RENAME TO kv_hidden`); err != nil {
		t.Fatalf("hide kv: %v", err)
	}
	if extra := repo.CustomModelExtraBody("openrouter", "vendor/m"); extra != nil {
		t.Fatalf("failed read returned %v", extra)
	}
	if _, err := database.Exec(`ALTER TABLE kv_hidden RENAME TO kv`); err != nil {
		t.Fatalf("restore kv: %v", err)
	}
	if got := string(repo.CustomModelExtraBody("openrouter", "vendor/m")["top_k"]); got != "3" {
		t.Fatalf("top_k = %q after recovery, want 3 (empty index was cached)", got)
	}
}

func TestExtraBodyProviderAllowed(t *testing.T) {
	for alias, want := range map[string]bool{"openrouter": true, "or": true, "openai": false, "opencode": false, "": false} {
		if got := ExtraBodyProviderAllowed(alias); got != want {
			t.Errorf("ExtraBodyProviderAllowed(%q) = %v, want %v", alias, got, want)
		}
	}
}
