package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/twitch-social-bot/internal/config"
	"github.com/twitch-social-bot/internal/domain"
	"github.com/twitch-social-bot/internal/i18n"
	"github.com/twitch-social-bot/internal/llm"
	"github.com/twitch-social-bot/internal/persona"
)

const (
	cReset  = "\x1b[0m"
	cBold   = "\x1b[1m"
	cDim    = "\x1b[2m"
	cGreen  = "\x1b[38;2;52;211;153m"
	cMint   = "\x1b[38;2;110;231;183m"
	cPurple = "\x1b[38;2;192;132;252m"
	cYellow = "\x1b[38;2;250;204;21m"
	cCyan   = "\x1b[38;2;56;189;248m"
	cGray   = "\x1b[38;2;107;114;128m"
	cWhite  = "\x1b[38;2;229;231;235m"
)

// AppVersion is displayed on the top-right of the CLI header card when set.
var AppVersion = ""

// RenderCard prints the modern terminal header card with the truecolor dino
// mascot on the left and clean bot status on the right.
func RenderCard(w io.Writer, cfg *config.Config, loc *i18n.Manager, active *domain.PersonaSpec) {
	const innerWidth = 76
	const leftPad = 2
	const gap = 4
	const rightWidth = innerWidth - leftPad - dinoArtWidth - gap

	langDisplay := fmt.Sprintf("[%s] %s", loc.ActiveID(), loc.ActiveName())
	personaDisplay := "-"
	if active != nil {
		personaDisplay = fmt.Sprintf("%s (%s)", active.BotName, active.ID)
	}

	provider := llm.DetectProvider(cfg.LLMProvider, cfg.LLMBaseURL)
	modelName := cfg.LLMModel
	if modelName == "" {
		modelName = provider.FallbackModel
	}
	modelDisplay := provider.ColorANSI + provider.Name + " • " + modelName + cReset

	apiKeyStatus := cYellow + loc.Get("cli_status_missing_key") + cReset
	if trimmedKey := strings.TrimSpace(cfg.LLMAPIKey); trimmedKey != "" {
		masked := trimmedKey
		if len(trimmedKey) > 8 {
			masked = trimmedKey[:4] + "..." + trimmedKey[len(trimmedKey)-4:]
		}
		apiKeyStatus = cMint + loc.Get("cli_status_ready") + " (" + masked + ")" + cReset
	}

	twitchStatus := cCyan + loc.Get("cli_status_local_mode") + cReset
	if strings.TrimSpace(cfg.TwitchClientID) != "" {
		botName := cfg.TwitchBotLogin
		if botName == "" {
			botName = cfg.TwitchBroadcasterLogin
		}
		twitchStatus = cPurple + "@" + botName + " -> @" + cfg.TwitchBroadcasterLogin + cReset
	}

	titleLine := cBold + cGreen + "Twitch Social Bot" + cReset
	if AppVersion != "" {
		pad := (rightWidth - 2) - len("Twitch Social Bot") - utf8.RuneCountInString(AppVersion)
		if pad < 1 {
			pad = 1
		}
		titleLine += strings.Repeat(" ", pad) + cGray + AppVersion + cReset
	}

	rightLines := []string{
		"",
		titleLine,
		cDim + cWhite + loc.Get("cli_subtitle") + cReset,
		cGray + strings.Repeat("─", rightWidth-2) + cReset,
		formatKV(loc.Get("cli_label_language"), langDisplay),
		formatKV(loc.Get("cli_label_persona"), personaDisplay),
		formatKV(loc.Get("cli_label_model"), modelDisplay),
		formatKV(loc.Get("cli_label_apikey"), apiKeyStatus),
		formatKV(loc.Get("cli_label_twitch"), twitchStatus),
		formatKV(loc.Get("cli_label_config"), fmt.Sprintf("%d facts • %dms cooldown", cfg.MaxViewerFacts, cfg.GlobalCooldown.Milliseconds())),
		"",
		"",
	}

	fmt.Fprintf(w, "\n%s╭%s╮%s\n", cGreen, strings.Repeat("─", innerWidth), cReset)
	fmt.Fprintf(w, "%s│%s%s%s│%s\n", cGreen, cReset, strings.Repeat(" ", innerWidth), cGreen, cReset)

	totalRows := len(dinoArtRows)
	if len(rightLines) > totalRows {
		totalRows = len(rightLines)
	}

	for i := 0; i < totalRows; i++ {
		art := strings.Repeat(" ", dinoArtWidth)
		if i < len(dinoArtRows) {
			art = dinoArtRows[i]
		}
		info := ""
		if i < len(rightLines) {
			info = rightLines[i]
		}
		infoVis := visibleLen(info)
		trail := innerWidth - leftPad - dinoArtWidth - gap - infoVis
		if trail < 0 {
			trail = 0
		}
		fmt.Fprintf(w, "%s│%s%s%s%s%s%s%s│%s\n",
			cGreen, cReset,
			strings.Repeat(" ", leftPad),
			art,
			strings.Repeat(" ", gap),
			info,
			strings.Repeat(" ", trail),
			cGreen, cReset,
		)
	}

	fmt.Fprintf(w, "%s│%s%s%s│%s\n", cGreen, cReset, strings.Repeat(" ", innerWidth), cGreen, cReset)
	fmt.Fprintf(w, "%s╰%s╯%s\n\n", cGreen, strings.Repeat("─", innerWidth), cReset)
}

func formatKV(label, val string) string {
	pad := 10 - utf8.RuneCountInString(label)
	if pad < 1 {
		pad = 1
	}
	return cGray + label + strings.Repeat(" ", pad) + ": " + cWhite + val + cReset
}

func visibleLen(s string) int {
	n := 0
	inEsc := false
	for _, r := range s {
		if r == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEsc = false
			}
			continue
		}
		n++
	}
	return n
}

// selectOption renders an arrow-navigable menu. Returns selected index (0..len-1) or -1 on Esc.
func selectOption(in io.Reader, reader *bufio.Reader, out io.Writer, title, hint string, options []string, initialIdx int) (int, error) {
	if len(options) == 0 {
		return -1, nil
	}
	idx := initialIdx
	if idx < 0 || idx >= len(options) {
		idx = 0
	}

	interactive := in == os.Stdin && IsInteractiveTTY()
	drawMenu := func(first bool) {
		if !first && interactive {
			// Move cursor up to overwrite previous render: title + blank + N options + hint
			linesUp := len(options) + 2
			fmt.Fprintf(out, "\x1b[%dA\r\x1b[J", linesUp)
		}
		fmt.Fprintf(out, "  %s%s?%s %s%s%s\n", cBold, cYellow, cReset, cBold, title, cReset)
		for i, opt := range options {
			if i == idx {
				fmt.Fprintf(out, "  %s%s❯ %s%s\n", cBold, cGreen, opt, cReset)
			} else {
				fmt.Fprintf(out, "    %s%s%s\n", cGray, opt, cReset)
			}
		}
		if hint != "" {
			fmt.Fprintf(out, "  %s%s%s\n", cDim, hint, cReset)
		} else {
			fmt.Fprintln(out)
		}
	}

	drawMenu(true)

	for {
		ev, text, err := ReadNavKey(in, reader)
		if err != nil {
			return -1, err
		}
		switch ev {
		case KeyUp:
			idx = (idx - 1 + len(options)) % len(options)
			drawMenu(false)
		case KeyDown:
			idx = (idx + 1) % len(options)
			drawMenu(false)
		case KeyEnter:
			return idx, nil
		case KeyEsc:
			return -1, nil
		default:
			if num, convErr := strconv.Atoi(strings.TrimSpace(text)); convErr == nil && num >= 1 && num <= len(options) {
				return num - 1, nil
			}
			if !interactive && text == "" {
				return idx, nil
			}
		}
	}
}

func clearScreenIfTTY(in io.Reader, out io.Writer) {
	if in == os.Stdin && IsInteractiveTTY() {
		fmt.Fprint(out, "\x1b[2J\x1b[H")
	}
}

// RunInteractive runs first-time onboarding if .env/API key is missing, then presents
// the arrow-navigated 5-option main CLI dashboard.
func RunInteractive(in io.Reader, out io.Writer, envPath string, cfg *config.Config, loc *i18n.Manager, pm *persona.Manager) (bool, *config.Config, error) {
	reader := bufio.NewReader(in)

	_, statErr := os.Stat(envPath)
	needsOnboarding := os.IsNotExist(statErr) || strings.TrimSpace(cfg.LLMAPIKey) == ""
	if needsOnboarding {
		if err := runOnboarding(in, reader, out, envPath, cfg, loc, pm); err != nil {
			return false, cfg, err
		}
	}

	for {
		clearScreenIfTTY(in, out)
		var active *domain.PersonaSpec
		if pm != nil {
			active = pm.Current()
		}
		RenderCard(out, cfg, loc, active)

		menuOpts := []string{
			loc.Get("cli_menu_start"),
			loc.Get("cli_menu_setup"),
			fmt.Sprintf("%s (%s)", loc.Get("cli_menu_language"), loc.ActiveID()),
			loc.Get("cli_menu_persona"),
			loc.Get("cli_menu_exit"),
		}
		choice, err := selectOption(in, reader, out, loc.Get("cli_menu_prompt"), loc.Get("cli_arrow_hint"), menuOpts, 0)
		if err != nil {
			return false, cfg, err
		}

		switch choice {
		case 0: // Start Bot
			fmt.Fprintf(out, "\n  %s%s%s\n\n", cGreen, loc.Get("cli_starting_bot"), cReset)
			return true, cfg, nil

		case 1: // Configure .env
			if wizErr := runSetupWizard(in, reader, out, envPath, cfg, loc, pm); wizErr != nil {
				fmt.Fprintf(out, "  %sError: %v%s\n", cYellow, wizErr, cReset)
			}

		case 2: // Switch Language
			if langErr := runLanguageSelector(in, reader, out, envPath, cfg, loc); langErr != nil {
				fmt.Fprintf(out, "  %sError: %v%s\n", cYellow, langErr, cReset)
			}

		case 3: // Switch Persona
			if perErr := runPersonaSelector(in, reader, out, envPath, cfg, loc, pm); perErr != nil {
				fmt.Fprintf(out, "  %sError: %v%s\n", cYellow, perErr, cReset)
			}

		case 4, -1: // Exit or Esc
			return false, cfg, nil
		}
	}
}

func runOnboarding(in io.Reader, reader *bufio.Reader, out io.Writer, envPath string, cfg *config.Config, loc *i18n.Manager, pm *persona.Manager) error {
	clearScreenIfTTY(in, out)
	var active *domain.PersonaSpec
	if pm != nil {
		active = pm.Current()
	}
	RenderCard(out, cfg, loc, active)

	// Step 0: Choose Language first with arrows
	avail := loc.Available()
	langOpts := make([]string, len(avail))
	initLangIdx := 0
	for i, l := range avail {
		langOpts[i] = fmt.Sprintf("[%s] %s", l.ID, l.Name)
		if l.ID == loc.ActiveID() {
			initLangIdx = i
		}
	}
	langIdx, err := selectOption(in, reader, out, loc.Get("cli_lang_title"), loc.Get("cli_arrow_hint"), langOpts, initLangIdx)
	if err != nil {
		return err
	}
	if langIdx >= 0 && langIdx < len(avail) {
		_ = loc.Set(avail[langIdx].ID)
		cfg.BotLanguage = avail[langIdx].ID
	}

	fmt.Fprintf(out, "\n  %s%s── %s ──%s\n\n", cBold, cGreen, loc.Get("cli_onboard_welcome"), cReset)

	// Step 1: AI Provider & API Key (auto-discovers latest model via GET /models)
	fmt.Fprintf(out, "  %s%s%s\n\n", cBold, cCyan+loc.Get("cli_onboard_step1_title"), cReset)
	if err := promptAIProvider(in, reader, out, cfg, loc, true); err != nil {
		return err
	}

	// Step 2: Twitch Developer App
	fmt.Fprintf(out, "\n  %s%s%s\n", cBold, cCyan+loc.Get("cli_onboard_step2_title"), cReset)
	fmt.Fprintf(out, "  %s%s%s\n\n", cDim, loc.Get("cli_onboard_step2_guide"), cReset)
	clientID, err := promptField(reader, out, "TWITCH_CLIENT_ID", cfg.TwitchClientID, false)
	if err != nil {
		return err
	}
	cfg.TwitchClientID = strings.TrimSpace(clientID)

	// Step 3: Broadcaster & Bot Account
	fmt.Fprintf(out, "\n  %s%s%s\n\n", cBold, cCyan+loc.Get("cli_onboard_step3_title"), cReset)
	broadcaster, err := promptField(reader, out, "TWITCH_BROADCASTER_LOGIN", cfg.TwitchBroadcasterLogin, false)
	if err != nil {
		return err
	}
	cfg.TwitchBroadcasterLogin = strings.ToLower(strings.TrimSpace(broadcaster))

	fmt.Fprintln(out)
	botOpts := []string{
		loc.Get("cli_onboard_bot_dedicated"),
		loc.Get("cli_onboard_bot_same"),
	}
	botChoice, err := selectOption(in, reader, out, loc.Get("cli_onboard_bot_mode_prompt"), loc.Get("cli_arrow_hint"), botOpts, 0)
	if err != nil {
		return err
	}
	if botChoice == 0 {
		botLogin, pErr := promptField(reader, out, "TWITCH_BOT_LOGIN", cfg.TwitchBotLogin, false)
		if pErr != nil {
			return pErr
		}
		cfg.TwitchBotLogin = strings.ToLower(strings.TrimSpace(botLogin))
	} else {
		cfg.TwitchBotLogin = cfg.TwitchBroadcasterLogin
	}

	// Step 4: Choose Persona with arrows
	if pm != nil {
		fmt.Fprintf(out, "\n  %s%s%s\n\n", cBold, cCyan+loc.Get("cli_onboard_step4_title"), cReset)
		ids := pm.List()
		if len(ids) > 0 {
			pIdx, pErr := selectOption(in, reader, out, loc.Get("cli_persona_title"), loc.Get("cli_arrow_hint"), ids, 0)
			if pErr != nil {
				return pErr
			}
			if pIdx >= 0 && pIdx < len(ids) {
				if spec, swErr := pm.Switch(ids[pIdx]); swErr == nil {
					cfg.PersonaFile = filepath.Join(cfg.PersonaDir, spec.ID+".yaml")
				}
			}
		}
	}

	if err := config.SaveEnv(envPath, cfg); err != nil {
		return err
	}

	fmt.Fprintf(out, "\n  %s✓ %s%s\n", cBold+cGreen, loc.Get("cli_onboard_done"), cReset)
	if cfg.TwitchClientID != "" {
		if cfg.TwitchBotLogin != "" && cfg.TwitchBotLogin != cfg.TwitchBroadcasterLogin {
			fmt.Fprintf(out, "\n  %s%s%s\n", cYellow, fmt.Sprintf(loc.Get("cli_onboard_oauth_dedicated"), cfg.TwitchBotLogin, cfg.TwitchBotLogin, cfg.TwitchBroadcasterLogin, cfg.TwitchBotLogin), cReset)
		} else {
			acct := cfg.TwitchBotLogin
			if acct == "" {
				acct = cfg.TwitchBroadcasterLogin
			}
			fmt.Fprintf(out, "\n  %s%s%s\n", cYellow, fmt.Sprintf(loc.Get("cli_onboard_oauth_same"), acct), cReset)
		}
	}
	fmt.Fprintf(out, "\n  %s%s%s ", cDim, loc.Get("cli_press_enter"), cReset)
	_, _ = reader.ReadString('\n')
	return nil
}

func runSetupWizard(in io.Reader, reader *bufio.Reader, out io.Writer, envPath string, cfg *config.Config, loc *i18n.Manager, pm *persona.Manager) error {
	fmt.Fprintf(out, "\n  %s%s── %s ──%s\n", cBold, cCyan, loc.Get("cli_wizard_title"), cReset)
	fmt.Fprintf(out, "  %s%s%s\n\n", cDim, loc.Get("cli_wizard_desc"), cReset)

	if err := promptAIProvider(in, reader, out, cfg, loc, false); err != nil {
		return err
	}

	clientID, err := promptField(reader, out, "TWITCH_CLIENT_ID", cfg.TwitchClientID, false)
	if err != nil {
		return err
	}
	cfg.TwitchClientID = clientID

	broadcaster, err := promptField(reader, out, "TWITCH_BROADCASTER_LOGIN", cfg.TwitchBroadcasterLogin, false)
	if err != nil {
		return err
	}
	cfg.TwitchBroadcasterLogin = strings.ToLower(broadcaster)

	botLogin, err := promptField(reader, out, "TWITCH_BOT_LOGIN", cfg.TwitchBotLogin, false)
	if err != nil {
		return err
	}
	cfg.TwitchBotLogin = strings.ToLower(botLogin)

	factsStr, err := promptField(reader, out, "MAX_VIEWER_FACTS", strconv.Itoa(cfg.MaxViewerFacts), false)
	if err != nil {
		return err
	}
	if n, convErr := strconv.Atoi(factsStr); convErr == nil && n > 0 {
		cfg.MaxViewerFacts = n
	}

	if err := config.SaveEnv(envPath, cfg); err != nil {
		return err
	}
	fmt.Fprintf(out, "\n  %s✓ %s%s\n", cGreen, loc.Get("cli_wizard_saved"), cReset)
	return nil
}

func runLanguageSelector(in io.Reader, reader *bufio.Reader, out io.Writer, envPath string, cfg *config.Config, loc *i18n.Manager) error {
	avail := loc.Available()
	opts := make([]string, len(avail))
	initIdx := 0
	for i, l := range avail {
		opts[i] = fmt.Sprintf("[%s] %s", l.ID, l.Name)
		if l.ID == loc.ActiveID() {
			initIdx = i
		}
	}

	fmt.Fprintln(out)
	idx, err := selectOption(in, reader, out, loc.Get("cli_lang_title"), loc.Get("cli_arrow_hint"), opts, initIdx)
	if err != nil || idx < 0 {
		return err
	}

	if err := loc.Set(avail[idx].ID); err != nil {
		return err
	}
	cfg.BotLanguage = loc.ActiveID()
	if err := config.SaveEnv(envPath, cfg); err != nil {
		return err
	}
	fmt.Fprintf(out, "\n  %s✓ %s%s\n", cGreen, loc.Get("cli_lang_saved"), cReset)
	return nil
}

func runPersonaSelector(in io.Reader, reader *bufio.Reader, out io.Writer, envPath string, cfg *config.Config, loc *i18n.Manager, pm *persona.Manager) error {
	if pm == nil {
		return nil
	}
	ids := pm.List()
	current := pm.Current()
	initIdx := 0
	for i, id := range ids {
		if current != nil && id == current.ID {
			initIdx = i
		}
	}

	fmt.Fprintln(out)
	idx, err := selectOption(in, reader, out, loc.Get("cli_persona_title"), loc.Get("cli_arrow_hint"), ids, initIdx)
	if err != nil || idx < 0 {
		return err
	}

	spec, err := pm.Switch(ids[idx])
	if err != nil {
		return err
	}
	cfg.PersonaFile = filepath.Join(cfg.PersonaDir, spec.ID+".yaml")
	if err := config.SaveEnv(envPath, cfg); err != nil {
		return err
	}
	fmt.Fprintf(out, "\n  %s✓ %s (%s)%s\n", cGreen, loc.Get("cli_persona_saved"), spec.BotName, cReset)
	return nil
}

func promptField(reader *bufio.Reader, out io.Writer, label, current string, secret bool) (string, error) {
	display := current
	if secret && len(current) > 8 {
		display = current[:4] + "..." + current[len(current)-4:]
	}
	if display == "" {
		fmt.Fprintf(out, "  %s: ", label)
	} else {
		fmt.Fprintf(out, "  %s [%s%s%s]: ", label, cDim, display, cReset)
	}
	raw, err := reader.ReadString('\n')
	if err != nil && len(strings.TrimSpace(raw)) == 0 {
		return current, err
	}
	val := strings.TrimSpace(raw)
	if val == "" {
		return current, nil
	}
	return val, nil
}

// PromptMandatoryUpdate displays a blocking update screen and returns true only if the user accepts updating.
func PromptMandatoryUpdate(in io.Reader, out io.Writer, cfg *config.Config, loc *i18n.Manager, pm *persona.Manager, currentVer, latestVer string) (bool, error) {
	reader := bufio.NewReader(in)
	clearScreenIfTTY(in, out)

	var active *domain.PersonaSpec
	if pm != nil {
		active = pm.Current()
	}
	RenderCard(out, cfg, loc, active)

	fmt.Fprintf(out, "  %s%s── %s ──%s\n", cBold, cYellow, loc.Get("cli_update_title"), cReset)
	fmt.Fprintf(out, "  %s\n\n", loc.Format("cli_update_desc", currentVer, latestVer))

	opts := []string{
		loc.Get("cli_update_accept"),
		loc.Get("cli_update_exit"),
	}
	idx, err := selectOption(in, reader, out, loc.Get("cli_menu_prompt"), loc.Get("cli_arrow_hint"), opts, 0)
	if err != nil {
		return false, err
	}
	return idx == 0, nil
}

func promptAIProvider(in io.Reader, reader *bufio.Reader, out io.Writer, cfg *config.Config, loc *i18n.Manager, requireKey bool) error {
	providers := llm.Providers()
	current := llm.DetectProvider(cfg.LLMProvider, cfg.LLMBaseURL)
	opts := make([]string, len(providers))
	initIdx := 0
	for i, p := range providers {
		opts[i] = p.ColorANSI + p.Name + cReset
		if p.ID == current.ID {
			initIdx = i
		}
	}

	idx, err := selectOption(in, reader, out, loc.Get("cli_provider_prompt"), loc.Get("cli_arrow_hint"), opts, initIdx)
	if err != nil || idx < 0 || idx >= len(providers) {
		return err
	}
	chosen := providers[idx]
	cfg.LLMProvider = chosen.ID

	if chosen.ID == "custom" {
		fmt.Fprintln(out)
		for {
			baseURL, uErr := promptField(reader, out, loc.Get("cli_provider_custom_url"), cfg.LLMBaseURL, false)
			if uErr != nil {
				return uErr
			}
			cfg.LLMBaseURL = strings.TrimSpace(baseURL)
			if cfg.LLMBaseURL != "" {
				break
			}
		}
	} else {
		cfg.LLMBaseURL = chosen.DefaultBaseURL
		if chosen.KeyGuideURL != "" {
			fmt.Fprintf(out, "\n  %s%s%s\n", cDim, loc.Format("cli_provider_key_guide", chosen.KeyGuideURL), cReset)
		}
	}

	fmt.Fprintln(out)
	for {
		keyLabel := fmt.Sprintf("LLM_API_KEY (%s%s%s)", chosen.ColorANSI, chosen.Name, cReset)
		apiKey, kErr := promptField(reader, out, keyLabel, cfg.LLMAPIKey, true)
		if kErr != nil {
			return kErr
		}
		cfg.LLMAPIKey = strings.TrimSpace(apiKey)
		if !requireKey || cfg.LLMAPIKey != "" {
			break
		}
	}

	fmt.Fprintf(out, "  %s%s%s\n", cDim, loc.Get("cli_provider_detecting"), cReset)
	model := llm.DiscoverBestModel(context.Background(), nil, chosen.ID, cfg.LLMBaseURL, cfg.LLMAPIKey)
	cfg.LLMModel = model
	fmt.Fprintf(out, "  %s✓ %s%s\n", chosen.ColorANSI, loc.Format("cli_provider_model_selected", model), cReset)
	return nil
}
