package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/herewei/warded/internal/adapters/storage"
	"github.com/herewei/warded/internal/domain"
)

func TestWhitelistAddExact(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := storage.NewJSONStore(dir)
	rt := domain.LocalWardRuntime{
		Site:       domain.SiteGlobal,
		WardID:     "ward_123",
		WardSecret: "wrd_123",
		WardStatus: domain.WardStatusActive,
		Domain:     "demo.warded.me",
	}
	if err := store.SaveWardRuntime(context.Background(), rt); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	out, err := runCmd(t, nil, "whitelist", []string{"add", "--exact", "/webhook/github", "--data-dir=" + dir})
	if err != nil {
		t.Fatalf("unexpected error: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Added whitelist rule: exact /webhook/github") {
		t.Fatalf("unexpected output: %s", out)
	}

	loaded, err := store.LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	if len(loaded.AuthWhitelist) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(loaded.AuthWhitelist))
	}
	if loaded.AuthWhitelist[0].Type != "exact" || loaded.AuthWhitelist[0].Path != "/webhook/github" {
		t.Fatalf("unexpected rule: %+v", loaded.AuthWhitelist[0])
	}
}

func TestWhitelistAddDuplicateRejected(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := storage.NewJSONStore(dir)
	rt := domain.LocalWardRuntime{
		Site:          domain.SiteGlobal,
		WardID:        "ward_123",
		WardSecret:    "wrd_123",
		WardStatus:    domain.WardStatusActive,
		Domain:        "demo.warded.me",
		AuthWhitelist: []domain.AuthWhitelistRule{{Type: "exact", Path: "/webhook/github"}},
	}
	if err := store.SaveWardRuntime(context.Background(), rt); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	_, cmdErr := runCmd(t, nil, "whitelist", []string{"add", "--exact", "/webhook/github", "--data-dir=" + dir})
	if cmdErr == nil {
		t.Fatal("expected duplicate error")
	}
	if !strings.Contains(cmdErr.Error(), "rule already exists") {
		t.Fatalf("unexpected error: %v", cmdErr)
	}
}

func TestWhitelistRemoveExact(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := storage.NewJSONStore(dir)
	rt := domain.LocalWardRuntime{
		Site:          domain.SiteGlobal,
		WardID:        "ward_123",
		WardSecret:    "wrd_123",
		WardStatus:    domain.WardStatusActive,
		Domain:        "demo.warded.me",
		AuthWhitelist: []domain.AuthWhitelistRule{{Type: "exact", Path: "/webhook/github"}},
	}
	if err := store.SaveWardRuntime(context.Background(), rt); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	out, err := runCmd(t, nil, "whitelist", []string{"remove", "--exact", "/webhook/github", "--data-dir=" + dir})
	if err != nil {
		t.Fatalf("unexpected error: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Removed whitelist rule: exact /webhook/github") {
		t.Fatalf("unexpected output: %s", out)
	}

	loaded, err := store.LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	if len(loaded.AuthWhitelist) != 0 {
		t.Fatalf("expected 0 rules, got %d", len(loaded.AuthWhitelist))
	}
}

func TestWhitelistRemoveNotFound(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := storage.NewJSONStore(dir)
	rt := domain.LocalWardRuntime{
		Site:       domain.SiteGlobal,
		WardID:     "ward_123",
		WardSecret: "wrd_123",
		WardStatus: domain.WardStatusActive,
		Domain:     "demo.warded.me",
	}
	if err := store.SaveWardRuntime(context.Background(), rt); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	_, cmdErr := runCmd(t, nil, "whitelist", []string{"remove", "--exact", "/webhook/github", "--data-dir=" + dir})
	if cmdErr == nil {
		t.Fatal("expected not-found error")
	}
	if !strings.Contains(cmdErr.Error(), "rule not found") {
		t.Fatalf("unexpected error: %v", cmdErr)
	}
}

func TestWhitelistListJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := storage.NewJSONStore(dir)
	rt := domain.LocalWardRuntime{
		Site:          domain.SiteGlobal,
		WardID:        "ward_123",
		WardSecret:    "wrd_123",
		WardStatus:    domain.WardStatusActive,
		Domain:        "demo.warded.me",
		AuthWhitelist: []domain.AuthWhitelistRule{{Type: "exact", Path: "/webhook/github"}},
	}
	if err := store.SaveWardRuntime(context.Background(), rt); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	out, err := runCmd(t, []string{"--format", "json"}, "whitelist", []string{"list", "--data-dir=" + dir})
	if err != nil {
		t.Fatalf("unexpected error: %v\noutput: %s", err, out)
	}

	m := parseJSONOutput(t, out)
	if !m["ok"].(bool) {
		t.Fatalf("expected ok=true, got: %v", m)
	}
	data, ok := m["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got: %T", m["data"])
	}
	if data["count"].(float64) != 1 {
		t.Fatalf("expected count=1, got: %v", data["count"])
	}
}

func TestWhitelistNoCommittedRuntime(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	_, cmdErr := runCmd(t, nil, "whitelist", []string{"list", "--data-dir=" + dir})
	if cmdErr == nil {
		t.Fatal("expected error for no committed runtime")
	}
	if !strings.Contains(cmdErr.Error(), "no committed ward runtime found") {
		t.Fatalf("unexpected error: %v", cmdErr)
	}
}

func TestWhitelistMultiWardRequiresSelector(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, id := range []string{"ward_111", "ward_222"} {
		store := storage.NewJSONStore(dir)
		rt := domain.LocalWardRuntime{
			Site:       domain.SiteGlobal,
			WardID:     id,
			WardSecret: "wrd_" + id,
			WardStatus: domain.WardStatusActive,
			Domain:     id + ".warded.me",
		}
		if err := store.SaveWardRuntime(context.Background(), rt); err != nil {
			t.Fatalf("save runtime: %v", err)
		}
	}

	_, cmdErr := runCmd(t, nil, "whitelist", []string{"list", "--data-dir=" + dir})
	if cmdErr == nil {
		t.Fatal("expected error for multiple wards without selector")
	}
	if !strings.Contains(cmdErr.Error(), "multiple committed wards found") {
		t.Fatalf("unexpected error: %v", cmdErr)
	}
}

func TestWhitelistSelectByIndex(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, id := range []string{"ward_111", "ward_222"} {
		store := storage.NewJSONStore(dir)
		rt := domain.LocalWardRuntime{
			Site:       domain.SiteGlobal,
			WardID:     id,
			WardSecret: "wrd_" + id,
			WardStatus: domain.WardStatusActive,
			Domain:     id + ".warded.me",
		}
		if err := store.SaveWardRuntime(context.Background(), rt); err != nil {
			t.Fatalf("save runtime: %v", err)
		}
	}

	out, err := runCmd(t, nil, "whitelist", []string{"list", "1", "--data-dir=" + dir})
	if err != nil {
		t.Fatalf("unexpected error: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "No whitelist rules configured") {
		t.Fatalf("expected empty list message, got: %s", out)
	}
}

func TestWhitelistSelectByWardID(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, id := range []string{"ward_111", "ward_222"} {
		store := storage.NewJSONStore(dir)
		rt := domain.LocalWardRuntime{
			Site:       domain.SiteGlobal,
			WardID:     id,
			WardSecret: "wrd_" + id,
			WardStatus: domain.WardStatusActive,
			Domain:     id + ".warded.me",
		}
		if err := store.SaveWardRuntime(context.Background(), rt); err != nil {
			t.Fatalf("save runtime: %v", err)
		}
	}

	out, err := runCmd(t, nil, "whitelist", []string{"list", "--ward-id", "ward_222", "--data-dir=" + dir})
	if err != nil {
		t.Fatalf("unexpected error: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "No whitelist rules configured") {
		t.Fatalf("expected empty list message, got: %s", out)
	}
}

func TestWhitelistAddWithIndex(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, id := range []string{"ward_111", "ward_222"} {
		store := storage.NewJSONStore(dir)
		rt := domain.LocalWardRuntime{
			Site:       domain.SiteGlobal,
			WardID:     id,
			WardSecret: "wrd_" + id,
			WardStatus: domain.WardStatusActive,
			Domain:     id + ".warded.me",
		}
		if err := store.SaveWardRuntime(context.Background(), rt); err != nil {
			t.Fatalf("save runtime: %v", err)
		}
	}

	// Add to first ward via index
	out, err := runCmd(t, nil, "whitelist", []string{"add", "1", "--exact", "/webhook", "--data-dir=" + dir})
	if err != nil {
		t.Fatalf("unexpected error: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Added whitelist rule: exact /webhook") {
		t.Fatalf("unexpected output: %s", out)
	}

	// Verify it went to ward_111, not ward_222
	store := storage.NewJSONStore(dir)
	rt111, _ := store.LoadRuntimeByID(context.Background(), "ward_111")
	rt222, _ := store.LoadRuntimeByID(context.Background(), "ward_222")
	if len(rt111.AuthWhitelist) != 1 {
		t.Fatalf("expected ward_111 to have 1 rule, got %d", len(rt111.AuthWhitelist))
	}
	if len(rt222.AuthWhitelist) != 0 {
		t.Fatalf("expected ward_222 to have 0 rules, got %d", len(rt222.AuthWhitelist))
	}
}

func TestWhitelistAddPrefix(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := storage.NewJSONStore(dir)
	rt := domain.LocalWardRuntime{
		Site:       domain.SiteGlobal,
		WardID:     "ward_123",
		WardSecret: "wrd_123",
		WardStatus: domain.WardStatusActive,
		Domain:     "demo.warded.me",
	}
	if err := store.SaveWardRuntime(context.Background(), rt); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	out, err := runCmd(t, nil, "whitelist", []string{"add", "--prefix", "/callbacks/", "--data-dir=" + dir})
	if err != nil {
		t.Fatalf("unexpected error: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Added whitelist rule: prefix /callbacks/") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestWhitelistNotBlockedByExpiredStatus(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := storage.NewJSONStore(dir)
	rt := domain.LocalWardRuntime{
		Site:       domain.SiteGlobal,
		WardID:     "ward_123",
		WardSecret: "wrd_123",
		WardStatus: domain.WardStatusExpired,
		Domain:     "demo.warded.me",
	}
	if err := store.SaveWardRuntime(context.Background(), rt); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	out, err := runCmd(t, nil, "whitelist", []string{"add", "--exact", "/webhook", "--data-dir=" + dir})
	if err != nil {
		t.Fatalf("unexpected error: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Added whitelist rule: exact /webhook") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestWhitelistAddRequiresExactOrPrefix(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := storage.NewJSONStore(dir)
	rt := domain.LocalWardRuntime{
		Site:       domain.SiteGlobal,
		WardID:     "ward_123",
		WardSecret: "wrd_123",
		WardStatus: domain.WardStatusActive,
		Domain:     "demo.warded.me",
	}
	if err := store.SaveWardRuntime(context.Background(), rt); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	_, cmdErr := runCmd(t, nil, "whitelist", []string{"add", "/webhook", "--data-dir=" + dir})
	if cmdErr == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(cmdErr.Error(), "either --exact or --prefix is required") {
		t.Fatalf("unexpected error: %v", cmdErr)
	}
}

func TestWhitelistAddRequiresLeadingSlash(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := storage.NewJSONStore(dir)
	rt := domain.LocalWardRuntime{
		Site:       domain.SiteGlobal,
		WardID:     "ward_123",
		WardSecret: "wrd_123",
		WardStatus: domain.WardStatusActive,
		Domain:     "demo.warded.me",
	}
	if err := store.SaveWardRuntime(context.Background(), rt); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	_, cmdErr := runCmd(t, nil, "whitelist", []string{"add", "--exact", "webhook", "--data-dir=" + dir})
	if cmdErr == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(cmdErr.Error(), "path must start with '/'") {
		t.Fatalf("unexpected error: %v", cmdErr)
	}
}

func TestWhitelistRemovePreservesDifferentType(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := storage.NewJSONStore(dir)
	rt := domain.LocalWardRuntime{
		Site:   domain.SiteGlobal,
		WardID: "ward_123",
		AuthWhitelist: []domain.AuthWhitelistRule{
			{Type: "exact", Path: "/webhook"},
			{Type: "prefix", Path: "/webhook"},
		},
		WardSecret: "wrd_123",
		WardStatus: domain.WardStatusActive,
		Domain:     "demo.warded.me",
	}
	if err := store.SaveWardRuntime(context.Background(), rt); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	out, err := runCmd(t, nil, "whitelist", []string{"remove", "--exact", "/webhook", "--data-dir=" + dir})
	if err != nil {
		t.Fatalf("unexpected error: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Removed whitelist rule: exact /webhook") {
		t.Fatalf("unexpected output: %s", out)
	}

	loaded, err := store.LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	if len(loaded.AuthWhitelist) != 1 {
		t.Fatalf("expected 1 remaining rule, got %d", len(loaded.AuthWhitelist))
	}
	if loaded.AuthWhitelist[0].Type != "prefix" {
		t.Fatalf("expected prefix rule to remain, got: %+v", loaded.AuthWhitelist[0])
	}
}
