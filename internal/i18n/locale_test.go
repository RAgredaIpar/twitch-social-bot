package i18n_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/twitch-social-bot/internal/i18n"
)

func TestManager_BuiltinDefaultsAndSwitch(t *testing.T) {
	m, err := i18n.NewManager("", "en")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.ActiveID() != "en" {
		t.Fatalf("expected active 'en', got %q", m.ActiveID())
	}
	if got := m.Get("bot_paused"); got != "Bot paused." {
		t.Fatalf("expected 'Bot paused.', got %q", got)
	}

	if err := m.Set("es"); err != nil {
		t.Fatalf("failed switching to es: %v", err)
	}
	if got := m.Get("bot_paused"); got != "Bot pausado." {
		t.Fatalf("expected 'Bot pausado.', got %q", got)
	}
}

func TestManager_CustomLocaleAndFallback(t *testing.T) {
	dir := t.TempDir()
	ptYAML := `id: "pt"
name: "Português"

messages:
  bot_paused: "Bot pausado em PT."
`
	if err := os.WriteFile(filepath.Join(dir, "pt.yaml"), []byte(ptYAML), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := i18n.NewManager(dir, "pt")
	if err != nil {
		t.Fatalf("unexpected error loading custom locale: %v", err)
	}
	if m.ActiveName() != "Português" {
		t.Fatalf("expected 'Português', got %q", m.ActiveName())
	}
	if got := m.Get("bot_paused"); got != "Bot pausado em PT." {
		t.Fatalf("expected custom pt message, got %q", got)
	}
	// Missing key in pt should fall back to English
	if got := m.Get("bot_resumed"); got != "Bot resumed." {
		t.Fatalf("expected English fallback 'Bot resumed.', got %q", got)
	}

	avail := m.Available()
	if len(avail) != 3 {
		t.Fatalf("expected 3 available locales (en, es, pt), got %d", len(avail))
	}
}

func TestManager_UnknownLocaleReturnsError(t *testing.T) {
	if _, err := i18n.NewManager("", "xx"); err == nil {
		t.Fatal("expected error for unknown default locale")
	}
	m, _ := i18n.NewManager("", "en")
	if err := m.Set("zz"); err == nil {
		t.Fatal("expected error for unknown Set locale")
	}
}

func TestManager_SoulDiskAndFallback(t *testing.T) {
	// Fallback without files on disk
	root := t.TempDir()
	locDir := filepath.Join(root, "locales")
	if err := os.MkdirAll(locDir, 0755); err != nil {
		t.Fatal(err)
	}
	m, err := i18n.NewManager(locDir, "es")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.Soul(), "SOUL.md") {
		t.Fatalf("expected built-in Soul fallback, got: %q", m.Soul())
	}

	// Custom SOUL.es.md on disk overrides built-in
	if err := os.WriteFile(filepath.Join(root, "SOUL.es.md"), []byte("Custom Spanish Soul"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "SOUL.md"), []byte("Custom English Soul"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := m.Soul(); got != "Custom Spanish Soul" {
		t.Fatalf("expected disk SOUL.es.md override, got %q", got)
	}
	_ = m.Set("en")
	if got := m.Soul(); got != "Custom English Soul" {
		t.Fatalf("expected disk SOUL.md override, got %q", got)
	}
}
