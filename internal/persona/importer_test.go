package persona_test

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/twitch-social-bot/internal/persona"
)

func buildMinimalPNGWithTextChunks(t *testing.T, textChunks map[string]string) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 120, G: 80, B: 220, A: 255})
		}
	}
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatal(err)
	}
	raw := pngBuf.Bytes()
	// Insert tEXt chunks right before the trailing 12-byte IEND chunk
	if len(raw) < 12 {
		t.Fatal("unexpected minimal png size")
	}
	var out bytes.Buffer
	out.Write(raw[:len(raw)-12])
	for k, v := range textChunks {
		payload := append([]byte(k), 0)
		payload = append(payload, []byte(v)...)
		var lenBuf [4]byte
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(payload)))
		out.Write(lenBuf[:])
		chunkType := []byte("tEXt")
		out.Write(chunkType)
		out.Write(payload)
		crc := crc32.ChecksumIEEE(append(chunkType, payload...))
		var crcBuf [4]byte
		binary.BigEndian.PutUint32(crcBuf[:], crc)
		out.Write(crcBuf[:])
	}
	out.Write(raw[len(raw)-12:])
	return out.Bytes()
}

func TestImportFile_PNG_V3PriorityOverV2_AndWriteYAML(t *testing.T) {
	dir := t.TempDir()

	v2JSON := `{"spec":"chara_card_v2","data":{"name":"OldV2","description":"Old"}}`
	v3JSON := `{
		"spec": "chara_card_v3",
		"spec_version": "3.0",
		"data": {
			"name": "Nyx Shadow",
			"nickname": "Nyx",
			"description": "{{char}} is a cheeky cyber-witch hanging out in {{user}}'s stream.",
			"personality": "sharp, chaotic, loyal",
			"scenario": "Late night gaming hangout.",
			"mes_example": "<START>\n{{user}}: hola\n{{char}}: *yawns lazily* llega puntual la próxima vez.",
			"tags": ["cyber", "witch"],
			"character_book": {
				"entries": [
					{"content": "Loves midnight energy drinks.", "enabled": true},
					{"content": "Secret disabled entry.", "enabled": false}
				]
			}
		}
	}`

	pngBytes := buildMinimalPNGWithTextChunks(t, map[string]string{
		"chara": base64.StdEncoding.EncodeToString([]byte(v2JSON)),
		"ccv3":  base64.StdEncoding.EncodeToString([]byte(v3JSON)),
	})

	pngPath := filepath.Join(dir, "nyx.png")
	if err := os.WriteFile(pngPath, pngBytes, 0644); err != nil {
		t.Fatal(err)
	}

	spec, rawImg, err := persona.ImportFile(pngPath)
	if err != nil {
		t.Fatalf("unexpected import error: %v", err)
	}
	if len(rawImg) == 0 {
		t.Fatalf("expected embedded png bytes returned")
	}
	if spec.BotName != "Nyx" || spec.Name != "Nyx Shadow" {
		t.Fatalf("expected V3 nickname/name 'Nyx'/'Nyx Shadow', got %q/%q", spec.BotName, spec.Name)
	}
	if !strings.Contains(strings.Join(spec.Bio, " "), "Nyx is a cheeky cyber-witch") {
		t.Fatalf("expected {{char}} macro replaced in bio, got: %+v", spec.Bio)
	}
	if !strings.Contains(strings.Join(spec.Lore, " "), "midnight energy drinks") || strings.Contains(strings.Join(spec.Lore, " "), "disabled") {
		t.Fatalf("unexpected lore entries: %+v", spec.Lore)
	}
	if len(spec.MessageExamples) != 1 || strings.Contains(spec.MessageExamples[0].Reply, "*") {
		t.Fatalf("expected cleaned message example without asterisk narration, got: %+v", spec.MessageExamples)
	}

	// WriteYAML -> ParseYAML round-trip
	yamlPath := filepath.Join(dir, spec.ID+".yaml")
	if err := persona.WriteYAML(yamlPath, spec); err != nil {
		t.Fatalf("WriteYAML failed: %v", err)
	}
	yamlData, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := persona.ParseYAML(yamlData)
	if err != nil {
		t.Fatalf("ParseYAML round-trip failed: %v\n%s", err, string(yamlData))
	}
	if reloaded.BotName != "Nyx" || len(reloaded.Tiers) != 0 || len(reloaded.Slots) != 0 {
		t.Fatalf("unexpected reloaded spec: %+v", reloaded)
	}
}

func TestImportFile_PNGWithoutMetadata(t *testing.T) {
	dir := t.TempDir()
	plainPNG := buildMinimalPNGWithTextChunks(t, nil)
	path := filepath.Join(dir, "plain.png")
	if err := os.WriteFile(path, plainPNG, 0644); err != nil {
		t.Fatal(err)
	}

	_, _, err := persona.ImportFile(path)
	if !errors.Is(err, persona.ErrNoCardMetadata) {
		t.Fatalf("expected ErrNoCardMetadata, got %v", err)
	}
}

func TestImportFile_CHARX_And_ModularJSON(t *testing.T) {
	dir := t.TempDir()

	// 1. Test .charx archive
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	cw, _ := zw.Create("card.json")
	_, _ = cw.Write([]byte(`{"spec":"chara_card_v3","data":{"name":"Kael","description":"Rogue pilot","group_only_greetings":["Qué pasó equipo!"]}}`))
	iw, _ := zw.Create("assets/icon/main.png")
	_, _ = iw.Write(buildMinimalPNGWithTextChunks(t, nil))
	_ = zw.Close()

	charxPath := filepath.Join(dir, "kael.charx")
	if err := os.WriteFile(charxPath, zipBuf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	spec, imgBytes, err := persona.ImportFile(charxPath)
	if err != nil {
		t.Fatalf("charx import failed: %v", err)
	}
	if spec.BotName != "Kael" || len(imgBytes) == 0 || len(spec.MessageExamples) != 1 {
		t.Fatalf("unexpected charx spec/image: spec=%+v imgLen=%d", spec, len(imgBytes))
	}

	// 2. Test modular JSON format
	modJSON := `{
		"name": "Aria",
		"bio": ["Sharp security analyst."],
		"lore": ["Audited 50 servers."],
		"adjectives": ["sharp", "blunt"],
		"style": {"all": ["Keep it short"], "chat": ["Direct answers only"]},
		"messageExamples": [
			[{"name": "{{user}}", "content": {"text": "works?"}}, {"name": "Aria", "content": {"text": "check the logs first"}}]
		]
	}`
	jsonPath := filepath.Join(dir, "aria.json")
	if err := os.WriteFile(jsonPath, []byte(modJSON), 0644); err != nil {
		t.Fatal(err)
	}
	modSpec, _, err := persona.ImportFile(jsonPath)
	if err != nil {
		t.Fatalf("modular JSON import failed: %v", err)
	}
	if modSpec.BotName != "Aria" || len(modSpec.Bio) != 1 || len(modSpec.Style) != 2 || len(modSpec.MessageExamples) != 1 {
		t.Fatalf("unexpected modular spec: %+v", modSpec)
	}
}
