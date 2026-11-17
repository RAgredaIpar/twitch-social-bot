package cli_test

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/twitch-social-bot/internal/cli"
	"github.com/twitch-social-bot/internal/domain"
	"github.com/twitch-social-bot/internal/persona"
)

func TestImageFileToANSI_Generates12Rows(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "avatar.png")

	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	purple := color.RGBA{R: 145, G: 70, B: 255, A: 255}
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			if x >= 8 && x < 24 && y >= 8 && y < 24 {
				img.SetRGBA(x, y, purple)
			} else {
				img.SetRGBA(x, y, white)
			}
		}
	}

	f, err := os.Create(imgPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	rows, err := cli.ImageFileToANSI(imgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 12 {
		t.Fatalf("expected 12 ANSI rows, got %d", len(rows))
	}
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, "38;2;145;70;255") {
		t.Fatalf("expected purple pixel color in ANSI output")
	}
}

func TestAvatarForPersona_BuiltInAndCustom(t *testing.T) {
	mochi := cli.AvatarForPersona(&domain.PersonaSpec{ID: "cohost_amigos"})
	celeste := cli.AvatarForPersona(&domain.PersonaSpec{ID: "drama_romance"})
	lyra := cli.AvatarForPersona(&domain.PersonaSpec{ID: "rpg_facciones"})
	dante := cli.AvatarForPersona(&domain.PersonaSpec{ID: "clan_jerarquia"})

	if len(mochi) != 12 || len(celeste) != 12 || len(lyra) != 12 || len(dante) != 12 {
		t.Fatalf("expected 12 rows for all built-in personas")
	}
	if strings.Join(mochi, "") == strings.Join(dante, "") {
		t.Fatalf("expected distinct avatars for Mochi and Dante")
	}

	customRows := []string{"custom-row-1"}
	custom := cli.AvatarForPersona(&domain.PersonaSpec{ID: "pirata", AvatarRows: customRows})
	if len(custom) != 1 || custom[0] != "custom-row-1" {
		t.Fatalf("expected custom AvatarRows to take precedence, got %+v", custom)
	}

	// Verify persona YAML parser preserves avatar_rows round-trip
	yamlText := "id: \"pirata\"\nname: \"Pirata\"\nbot_name: \"Barbanegra\"\ntrigger_prefix: \"!bot\"\nmax_positive_delta: 5\nmax_negative_delta: -8\nbase_persona: \"Pirata\"\navatar_rows:\n  - \"\\x1b[38;2;255;0;0m▀\\x1b[0m\"\ntiers:\n  - id: \"a\"\n    name: \"A\"\n    min: -100\n    max: 100\n    behavior: \"B\"\nslots: []\n"
	spec, err := persona.ParseYAML([]byte(yamlText))
	if err != nil {
		t.Fatalf("failed to parse custom YAML with avatar_rows: %v", err)
	}
	if len(spec.AvatarRows) != 1 || !strings.Contains(spec.AvatarRows[0], "\x1b[38;2;255;0;0m") {
		t.Fatalf("expected parsed ANSI escape in AvatarRows, got %+v", spec.AvatarRows)
	}
}
