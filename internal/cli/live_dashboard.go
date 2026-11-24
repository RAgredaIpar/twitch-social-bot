package cli

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/twitch-social-bot/internal/domain"
	"github.com/twitch-social-bot/internal/engine"
	"github.com/twitch-social-bot/internal/i18n"
)

const liveBoxInner = 74

// LiveDashboardConfig configures the interactive live stream dashboard.
type LiveDashboardConfig struct {
	Out             io.Writer
	Version         string
	Broadcaster     string
	BotLogin        string
	HasTwitch       bool
	Persona         domain.PersonaProvider
	Orchestrator    *engine.Orchestrator
	Logs            *LogBuffer
	Locale          *i18n.Manager
	Events          <-chan engine.ActivityEvent
	EventSubReady   *atomic.Bool
	OnPersonaSwitch func(*domain.PersonaSpec)
	FrameInterval   time.Duration
	StartTime       time.Time
}

// LiveDashboard renders the animated live bot panel (Speaking Ring + Braille EQ +
// latest interaction), a live Logs view ('L'), and a live Persona picker ('P').
type LiveDashboard struct {
	cfg        LiveDashboardConfig
	showLogs   bool
	showPicker bool
	pickerIDs  []string
	pickerIdx  int
	pickerErr  string
	tick       int
	state      string // "idle" | "thinking" | "speaking"
	holdTicks  int
	lastUser   string
	lastReply  string
}

// NewLiveDashboard initializes a LiveDashboard instance.
func NewLiveDashboard(cfg LiveDashboardConfig) *LiveDashboard {
	if cfg.Out == nil {
		cfg.Out = os.Stdout
	}
	if cfg.FrameInterval <= 0 {
		cfg.FrameInterval = 90 * time.Millisecond
	}
	if cfg.StartTime.IsZero() {
		cfg.StartTime = time.Now()
	}
	return &LiveDashboard{
		cfg:   cfg,
		state: "idle",
	}
}

// ToggleLogs switches between the animated view and the live logs view.
func (d *LiveDashboard) ToggleLogs() {
	d.showPicker = false
	d.showLogs = !d.showLogs
}

// ShowingLogs reports whether the live logs view is currently active.
func (d *LiveDashboard) ShowingLogs() bool {
	return d.showLogs
}

// TogglePersonaPicker opens or closes the interactive persona selector overlay.
func (d *LiveDashboard) TogglePersonaPicker() {
	if d.showPicker {
		d.showPicker = false
		d.pickerErr = ""
		return
	}
	if d.cfg.Persona == nil {
		return
	}
	ids := d.cfg.Persona.List()
	if len(ids) == 0 {
		return
	}
	d.pickerIDs = ids
	d.pickerIdx = 0
	d.pickerErr = ""
	if cur := d.cfg.Persona.Current(); cur != nil {
		for i, id := range ids {
			if id == cur.ID {
				d.pickerIdx = i
				break
			}
		}
	}
	d.showLogs = false
	d.showPicker = true
}

// ShowingPersonaPicker reports whether the live persona selector is active.
func (d *LiveDashboard) ShowingPersonaPicker() bool {
	return d.showPicker
}

// MovePersonaCursor moves the persona picker selection up or down with wrap-around.
func (d *LiveDashboard) MovePersonaCursor(delta int) {
	if !d.showPicker || len(d.pickerIDs) == 0 {
		return
	}
	n := len(d.pickerIDs)
	d.pickerIdx = ((d.pickerIdx+delta)%n + n) % n
}

// ConfirmPersonaSelection activates the highlighted persona and closes the picker.
func (d *LiveDashboard) ConfirmPersonaSelection() error {
	if !d.showPicker || d.cfg.Persona == nil || len(d.pickerIDs) == 0 {
		return nil
	}
	targetID := d.pickerIDs[d.pickerIdx]
	spec, err := d.cfg.Persona.Switch(targetID)
	if err != nil {
		d.pickerErr = err.Error()
		return err
	}
	d.showPicker = false
	d.pickerErr = ""
	if d.cfg.OnPersonaSwitch != nil {
		d.cfg.OnPersonaSwitch(spec)
	}
	return nil
}

// ApplyEvent updates the dashboard visual state from an orchestrator activity event.
func (d *LiveDashboard) ApplyEvent(ev engine.ActivityEvent) {
	switch ev.Kind {
	case engine.ActivityThinking:
		d.state = "thinking"
		d.holdTicks = 120 // ~11s safety cap
		if ev.Username != "" && ev.UserMessage != "" {
			d.lastUser = "@" + ev.Username + ": " + ev.UserMessage
		}
	case engine.ActivityReplied:
		d.state = "speaking"
		d.holdTicks = 24 // ~2.2s speaking burst
		if ev.Username != "" && ev.UserMessage != "" {
			d.lastUser = "@" + ev.Username + ": " + ev.UserMessage
		}
		if ev.BotReply != "" {
			d.lastReply = ev.BotReply
		}
	case engine.ActivityError:
		d.state = "idle"
		d.holdTicks = 0
	}
}

// AdvanceTick advances animation frames and speaking/thinking hold timers.
func (d *LiveDashboard) AdvanceTick() {
	d.tick++
	if d.holdTicks > 0 {
		d.holdTicks--
		if d.holdTicks == 0 {
			d.state = "idle"
		}
	}
}

// Run starts the interactive alternate-screen loop until ctx is canceled,
// Back to Menu (Q/Esc) is pressed, or Quit (Ctrl+C) is triggered.
func (d *LiveDashboard) Run(ctx context.Context, onBackToMenu, onQuit func()) {
	keys := StartLiveKeyReader(ctx)

	fmt.Fprint(d.cfg.Out, "\x1b[?1049h\x1b[?25l")
	defer fmt.Fprint(d.cfg.Out, "\x1b[?25h\x1b[?1049l")

	ticker := time.NewTicker(d.cfg.FrameInterval)
	defer ticker.Stop()

	d.drawFrame()

	for {
		select {
		case <-ctx.Done():
			return
		case k, ok := <-keys:
			if !ok {
				keys = nil
				continue
			}
			switch k {
			case LiveKeyToggleLogs:
				d.ToggleLogs()
				d.drawFrame()
			case LiveKeyTogglePersona:
				d.TogglePersonaPicker()
				d.drawFrame()
			case LiveKeyUp:
				if d.showPicker {
					d.MovePersonaCursor(-1)
					d.drawFrame()
				}
			case LiveKeyDown:
				if d.showPicker {
					d.MovePersonaCursor(1)
					d.drawFrame()
				}
			case LiveKeyEnter:
				if d.showPicker {
					_ = d.ConfirmPersonaSelection()
					d.drawFrame()
				}
			case LiveKeyEsc:
				if d.showPicker {
					d.TogglePersonaPicker()
					d.drawFrame()
					continue
				}
				if onBackToMenu != nil {
					onBackToMenu()
				}
				return
			case LiveKeyBackToMenu:
				if onBackToMenu != nil {
					onBackToMenu()
				}
				return
			case LiveKeyQuit:
				if onQuit != nil {
					onQuit()
				}
				return
			}
		case ev, ok := <-d.cfg.Events:
			if !ok {
				d.cfg.Events = nil
				continue
			}
			d.ApplyEvent(ev)
			d.drawFrame()
		case <-ticker.C:
			d.drainEvents()
			d.AdvanceTick()
			d.drawFrame()
		}
	}
}

func (d *LiveDashboard) drainEvents() {
	if d.cfg.Events == nil {
		return
	}
	for {
		select {
		case ev, ok := <-d.cfg.Events:
			if !ok {
				d.cfg.Events = nil
				return
			}
			d.ApplyEvent(ev)
		default:
			return
		}
	}
}

func (d *LiveDashboard) drawFrame() {
	fmt.Fprint(d.cfg.Out, "\x1b[H\x1b[2J\n"+d.RenderFrame(time.Now()))
}

// RenderFrame renders the current frame string (live desk or logs view) cleanly aligned.
func (d *LiveDashboard) RenderFrame(now time.Time) string {
	isEN := d.cfg.Locale != nil && d.cfg.Locale.ActiveID() == "en"

	var spec *domain.PersonaSpec
	if d.cfg.Persona != nil {
		spec = d.cfg.Persona.Current()
	}
	accent := ThemeColorANSI(spec)
	if accent == "" {
		accent = cGreen
	}

	personaName := "Bot"
	personaRole := "-"
	if spec != nil {
		if spec.BotName != "" {
			personaName = spec.BotName
		}
		if spec.Name != "" {
			personaRole = spec.Name
		}
	}

	var frame string
	switch {
	case d.showPicker:
		frame = d.renderPersonaPickerFrame(spec, isEN)
	case d.showLogs:
		frame = d.renderLogsFrame(isEN)
	default:
		frame = d.renderLiveFrame(now, spec, accent, personaName, personaRole, isEN)
	}

	var footer string
	if d.showPicker {
		viewLabel, selHint, actHint, cancelHint := "Vista", "Elegir", "Activar", "Cancelar"
		if isEN {
			viewLabel, selHint, actHint, cancelHint = "View", "Select", "Activate", "Cancel"
		}
		footer = fmt.Sprintf("\n  %s%s:%s %sPERSONA%s  %s[Arriba/Abajo]%s %s  %s[Enter]%s %s  %s[Esc/P]%s %s\n",
			cDim, viewLabel, cReset, cBold+cWhite, cReset,
			cCyan, cReset, selHint,
			cMint, cReset, actHint,
			cGray, cReset, cancelHint,
		)
	} else {
		viewName := "LIVE (" + personaName + ")"
		toggleHint := "Logs"
		menuHint := "Menu"
		quitHint := "Salir"
		viewLabel := "Vista"
		if d.showLogs {
			viewName = "LOGS"
			toggleHint = "Live"
		}
		if isEN {
			viewLabel = "View"
			quitHint = "Quit"
		}
		footer = fmt.Sprintf("\n  %s%s:%s %s%s%s  %s[P]%s Persona  %s[L]%s %s  %s[Q/Esc]%s %s  %s[Ctrl+C]%s %s\n",
			cDim, viewLabel, cReset, cBold+cWhite, viewName, cReset,
			cPurple, cReset,
			cYellow, cReset, toggleHint,
			cCyan, cReset, menuHint,
			cGray, cReset, quitHint,
		)
	}
	return frame + footer
}

func (d *LiveDashboard) renderLiveFrame(now time.Time, spec *domain.PersonaSpec, accent, personaName, personaRole string, isEN bool) string {
	var b strings.Builder

	up := now.Sub(d.cfg.StartTime)
	if up < 0 {
		up = 0
	}
	up = up.Truncate(time.Second)

	ver := strings.TrimSpace(d.cfg.Version)
	if ver == "" {
		ver = "dev"
	}

	paused := false
	var procMsgs, genReplies int64
	if d.cfg.Orchestrator != nil {
		snap := d.cfg.Orchestrator.StatusSnapshot(context.Background())
		paused = snap.Paused
		procMsgs = snap.ProcessedMessages
		genReplies = snap.GeneratedReplies
	}

	b.WriteString(liveBoxTop(cPurple))
	header := fmt.Sprintf(" %sTWITCH SOCIAL BOT%s %s%s%s   %s   %sUP %s%s",
		cBold+cWhite, cReset, cDim, ver, cReset, d.statusBadge(accent, paused, isEN), cGray, up, cReset)
	b.WriteString(liveBoxRow(cPurple, header))
	b.WriteString(liveBoxSep(cPurple))

	ringRows := renderSpeakingRing(AvatarForPersona(spec), accent, d.state, d.tick)

	// Braille audio visualizer (32 cols x 4 rows = 64x16 virtual dots)
	eq := newLiveBraille(32, 4)
	energy := 0.30
	if paused {
		energy = 0.08
	} else if d.state == "thinking" {
		energy = 0.68
	} else if d.state == "speaking" {
		energy = 1.0
	}
	for px := 0; px < 64; px++ {
		t := float64(d.tick) * 0.28
		h := math.Sin(float64(px)*0.20+t)*0.5 + math.Cos(float64(px)*0.11-t*1.3)*0.5
		norm := (h + 1.0) * 0.5 * energy
		barH := int(norm*13) + 2
		col := cPurple
		if px%4 < 2 {
			col = accent
		}
		if d.state == "thinking" {
			col = cYellow
		}
		for py := 15; py >= 16-barH; py-- {
			eq.set(px, py, col)
		}
	}
	eqLines := eq.renderLines()

	channelStr := "#local"
	if strings.TrimSpace(d.cfg.Broadcaster) != "" {
		channelStr = "#" + strings.TrimSpace(d.cfg.Broadcaster)
	}
	connStr := cCyan + "Local Sim" + cReset
	if d.cfg.HasTwitch {
		if d.cfg.EventSubReady != nil && d.cfg.EventSubReady.Load() {
			connStr = cMint + "EventSub OK" + cReset
		} else {
			connStr = cYellow + "Conectando..." + cReset
			if isEN {
				connStr = cYellow + "Connecting..." + cReset
			}
		}
	}

	channelLabel := "Canal:"
	neuralLabel := "Actividad Neural / Audio:"
	if isEN {
		channelLabel = "Channel:"
		neuralLabel = "Neural Activity / Audio:"
	}

	right := []string{
		"",
		fmt.Sprintf("%sPersona:%s  %s%s%s %s(%s)%s", cGray, cReset, cBold+accent, personaName, cReset, cDim, personaRole, cReset),
		fmt.Sprintf("%s%-8s%s  %s%s%s  %s|%s  %s", cGray, channelLabel, cReset, cWhite, channelStr, cReset, cGray, cReset, connStr),
		fmt.Sprintf("%sStats:%s    %s%d msgs%s  ·  %s%d replies%s", cGray, cReset, cCyan, procMsgs, cReset, accent, genReplies, cReset),
		"",
		cDim + neuralLabel + cReset,
		"",
		eqLines[0],
		eqLines[1],
		eqLines[2],
		eqLines[3],
		"",
		"",
		"",
	}

	for i := 0; i < len(ringRows); i++ {
		rText := ""
		if i < len(right) {
			rText = right[i]
		}
		b.WriteString(liveBoxRow(cPurple, ringRows[i]+"  "+rText))
	}

	waitingUser := "Esperando trigger (!bot / @mencion) en el chat..."
	waitingReply := "Sin respuestas enviadas aun en esta sesion"
	if isEN {
		waitingUser = "Waiting for chat trigger (!bot / @mention)..."
		waitingReply = "No replies sent yet in this session"
	}

	inLine := cDim + waitingUser + cReset
	if d.lastUser != "" {
		inLine = d.lastUser
	}
	botLine := cDim + waitingReply + cReset
	if d.lastReply != "" {
		botLine = d.lastReply
	}

	b.WriteString(liveBoxSep(cPurple))
	b.WriteString(liveBoxRow(cPurple, fmt.Sprintf("%s[IN] %s %s", cCyan, cReset, inLine)))
	b.WriteString(liveBoxRow(cPurple, fmt.Sprintf("%s[BOT]%s %s", accent, cReset, botLine)))
	b.WriteString(liveBoxBot(cPurple))
	return b.String()
}

func (d *LiveDashboard) renderLogsFrame(isEN bool) string {
	var b strings.Builder
	title := "[LOGS] MODO DEPURACION EN VIVO"
	emptyMsg := "Sin logs registrados aun."
	if isEN {
		title = "[LOGS] LIVE DEBUG STREAM"
		emptyMsg = "No logs recorded yet."
	}

	b.WriteString(liveBoxTopTitle(cYellow, title))
	var lines []string
	if d.cfg.Logs != nil {
		lines = d.cfg.Logs.Lines(14)
	}
	if len(lines) == 0 {
		b.WriteString(liveBoxRow(cYellow, cDim+emptyMsg+cReset))
		for i := 1; i < 14; i++ {
			b.WriteString(liveBoxRow(cYellow, ""))
		}
	} else {
		for _, line := range lines {
			b.WriteString(liveBoxRow(cYellow, styleLogLine(line)))
		}
		for i := len(lines); i < 14; i++ {
			b.WriteString(liveBoxRow(cYellow, ""))
		}
	}
	b.WriteString(liveBoxBot(cYellow))
	return b.String()
}

func (d *LiveDashboard) renderPersonaPickerFrame(current *domain.PersonaSpec, isEN bool) string {
	var b strings.Builder
	title := "[PERSONA] CAMBIAR PERSONALIDAD EN VIVO"
	activeTag := "[ACTIVA]"
	subHint := "Cambia la voz y el avatar al instante sin desconectar el bot:"
	if isEN {
		title = "[PERSONA] LIVE PERSONA SWITCHER"
		activeTag = "[ACTIVE]"
		subHint = "Switch voice and avatar instantly without disconnecting:"
	}

	b.WriteString(liveBoxTopTitle(cPurple, title))
	b.WriteString(liveBoxRow(cPurple, cDim+subHint+cReset))
	b.WriteString(liveBoxRow(cPurple, ""))

	const maxVisible = 11
	total := len(d.pickerIDs)
	start := 0
	if total > maxVisible && d.pickerIdx >= maxVisible {
		start = d.pickerIdx - maxVisible + 1
	}
	end := start + maxVisible
	if end > total {
		end = total
	}

	curID := ""
	if current != nil {
		curID = current.ID
	}

	for i := start; i < end; i++ {
		id := d.pickerIDs[i]
		pCol := ThemeColorANSI(&domain.PersonaSpec{ID: id})
		if pCol == "" {
			pCol = cMint
		}
		mark := ""
		if id == curID {
			mark = " " + cDim + activeTag + cReset
		}
		if i == d.pickerIdx {
			b.WriteString(liveBoxRow(cPurple, fmt.Sprintf("  %s> %s%s%s", cBold+pCol, id, cReset, mark)))
		} else {
			b.WriteString(liveBoxRow(cPurple, fmt.Sprintf("    %s%s%s%s", cGray, id, cReset, mark)))
		}
	}

	for rows := end - start; rows < maxVisible; rows++ {
		b.WriteString(liveBoxRow(cPurple, ""))
	}

	if d.pickerErr != "" {
		b.WriteString(liveBoxRow(cPurple, "\x1b[38;2;248;113;113mError: "+d.pickerErr+cReset))
	} else {
		b.WriteString(liveBoxRow(cPurple, ""))
	}
	b.WriteString(liveBoxBot(cPurple))
	return b.String()
}

func (d *LiveDashboard) statusBadge(accent string, paused, isEN bool) string {
	if paused {
		label := "PAUSADO"
		if isEN {
			label = "PAUSED"
		}
		return cYellow + cBold + "|| " + label + cReset
	}
	if d.state == "thinking" {
		spin := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		label := " PENSANDO..."
		if isEN {
			label = " THINKING..."
		}
		return cYellow + cBold + spin[d.tick%len(spin)] + label + cReset
	}
	if d.state == "speaking" {
		label := "» RESPONDIENDO"
		if isEN {
			label = "» SPEAKING"
		}
		return accent + cBold + label + cReset
	}
	dot := "●"
	if (d.tick/6)%2 == 1 {
		dot = "○"
	}
	listen := "Escuchando chat"
	if isEN {
		listen = "Listening"
	}
	return "\x1b[38;2;248;113;113m" + cBold + dot + " LIVE" + cReset + cGray + " · " + listen + cReset
}

func renderSpeakingRing(artRows []string, accent, state string, tick int) []string {
	ringCol := cDim + cGray
	ringTag := "  IDLE  "
	cornerTL, cornerTR, cornerBL, cornerBR := "┌", "┐", "└", "┘"
	hBar, vBar := "─", "│"

	switch state {
	case "thinking":
		ringCol = cBold + cYellow
		ringTag = " THINK  "
		cornerTL, cornerTR, cornerBL, cornerBR = "╭", "╮", "╰", "╯"
	case "speaking":
		ringCol = cBold + accent
		if tick%4 < 2 {
			ringTag = " ON AIR "
		} else {
			ringTag = " SPEAK  "
		}
		cornerTL, cornerTR, cornerBL, cornerBR = "┏", "┓", "┗", "┛"
		hBar, vBar = "━", "┃"
	}

	avatarW := avatarWidth
	for _, row := range artRows {
		if w := visibleWidth(row); w > avatarW {
			avatarW = w
		}
	}

	innerSpan := avatarW + 2
	tagBox := "[" + ringTag + "]"
	barsTotal := innerSpan - visibleWidth(tagBox)
	if barsTotal < 2 {
		barsTotal = 2
	}
	leftBars := barsTotal / 2
	rightBars := barsTotal - leftBars

	top := fmt.Sprintf("%s%s%s%s%s%s",
		ringCol, cornerTL, strings.Repeat(hBar, leftBars), tagBox, strings.Repeat(hBar, rightBars), cornerTR+cReset)
	bot := fmt.Sprintf("%s%s%s%s",
		ringCol, cornerBL, strings.Repeat(hBar, innerSpan), cornerBR+cReset)

	out := make([]string, 0, len(artRows)+2)
	out = append(out, top)
	for _, row := range artRows {
		out = append(out, fmt.Sprintf("%s%s%s %s %s%s%s", ringCol, vBar, cReset, padVisible(row, avatarW), ringCol, vBar, cReset))
	}
	out = append(out, bot)
	return out
}

func styleLogLine(line string) string {
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, "error") || strings.Contains(lower, "failed"):
		return "\x1b[38;2;248;113;113m" + line + cReset
	case strings.Contains(lower, "confirmada") || strings.Contains(lower, "exitosa") || strings.Contains(lower, "[chat out]"):
		return cMint + line + cReset
	case strings.Contains(lower, "suscribiendo") || strings.Contains(lower, "conectado") || strings.Contains(lower, "servidor http"):
		return cCyan + line + cReset
	default:
		return cWhite + line + cReset
	}
}

func visibleWidth(s string) int {
	w := 0
	inEsc := false
	for _, r := range s {
		if inEsc {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEsc = false
			}
			continue
		}
		if r == '\x1b' {
			inEsc = true
			continue
		}
		w++
	}
	return w
}

func truncateANSI(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if visibleWidth(s) <= max {
		return s
	}
	var b strings.Builder
	w := 0
	inEsc := false
	hasANSI := false
	for _, r := range s {
		if inEsc {
			b.WriteRune(r)
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEsc = false
			}
			continue
		}
		if r == '\x1b' {
			inEsc = true
			hasANSI = true
			b.WriteRune(r)
			continue
		}
		if w >= max-1 {
			b.WriteRune('…')
			break
		}
		b.WriteRune(r)
		w++
	}
	if hasANSI {
		b.WriteString(cReset)
	}
	return b.String()
}

func padVisible(s string, width int) string {
	t := truncateANSI(s, width)
	w := visibleWidth(t)
	if w >= width {
		return t
	}
	return t + strings.Repeat(" ", width-w)
}

func liveBoxTop(col string) string {
	return fmt.Sprintf("  %s╭%s╮%s\n", col, strings.Repeat("─", liveBoxInner), cReset)
}

func liveBoxTopTitle(col, title string) string {
	label := "─ " + title + " "
	rem := liveBoxInner - visibleWidth(label)
	if rem < 0 {
		rem = 0
	}
	return fmt.Sprintf("  %s╭─ %s%s%s%s╮%s\n", col, cBold+cWhite, title, col, " "+strings.Repeat("─", rem), cReset)
}

func liveBoxSep(col string) string {
	return fmt.Sprintf("  %s├%s┤%s\n", col, strings.Repeat("─", liveBoxInner), cReset)
}

func liveBoxBot(col string) string {
	return fmt.Sprintf("  %s╰%s╯%s\n", col, strings.Repeat("─", liveBoxInner), cReset)
}

func liveBoxRow(col, content string) string {
	return fmt.Sprintf("  %s│%s %s %s│%s\n", col, cReset, padVisible(content, liveBoxInner-2), col, cReset)
}

type liveBrailleCanvas struct {
	cols, rows int
	bits       []uint8
	colors     []string
}

func newLiveBraille(cols, rows int) *liveBrailleCanvas {
	return &liveBrailleCanvas{
		cols:   cols,
		rows:   rows,
		bits:   make([]uint8, cols*rows),
		colors: make([]string, cols*rows),
	}
}

var liveBrailleMap = [4][2]uint8{
	{0x01, 0x08},
	{0x02, 0x10},
	{0x04, 0x20},
	{0x40, 0x80},
}

func (c *liveBrailleCanvas) set(px, py int, color string) {
	if px < 0 || py < 0 || px >= c.cols*2 || py >= c.rows*4 {
		return
	}
	cx, cy := px/2, py/4
	idx := cy*c.cols + cx
	c.bits[idx] |= liveBrailleMap[py%4][px%2]
	if color != "" {
		c.colors[idx] = color
	}
}

func (c *liveBrailleCanvas) renderLines() []string {
	lines := make([]string, c.rows)
	for y := 0; y < c.rows; y++ {
		var sb strings.Builder
		lastCol := ""
		for x := 0; x < c.cols; x++ {
			idx := y*c.cols + x
			b := c.bits[idx]
			col := c.colors[idx]
			if b == 0 {
				if lastCol != "" {
					sb.WriteString(cReset)
					lastCol = ""
				}
				sb.WriteRune(' ')
				continue
			}
			if col != lastCol {
				if col == "" {
					sb.WriteString(cReset)
				} else {
					sb.WriteString(col)
				}
				lastCol = col
			}
			sb.WriteRune(rune(0x2800 + int(b)))
		}
		if lastCol != "" {
			sb.WriteString(cReset)
		}
		lines[y] = sb.String()
	}
	return lines
}
