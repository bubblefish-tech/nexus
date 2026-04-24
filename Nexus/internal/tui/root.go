// Copyright © 2026 BubbleFish Technologies, Inc.
//
// This file is part of BubbleFish Nexus.
//
// BubbleFish Nexus is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// BubbleFish Nexus is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with BubbleFish Nexus. If not, see <https://www.gnu.org/licenses/>.

package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/bubblefish-tech/nexus/internal/tui/api"
	"github.com/bubblefish-tech/nexus/internal/tui/components"
	"github.com/bubblefish-tech/nexus/internal/tui/screens"
	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	headerHeight      = 1
	tabBarHeight      = 1
	featureFlagsHeight = 1
	commandBarHeight  = 1
	chromeHeight      = headerHeight + tabBarHeight + featureFlagsHeight + commandBarHeight
	minWidth          = 100
	minHeight         = 30
)

// screenNames maps each page state to its tab bar label.
var screenNames = []struct {
	state AppState
	label string
}{
	{StateDashboard, "Dashboard"},
	{StateMemoryBrowser, "Memory"},
	{StateRetrievalTheater, "Retrieval"},
	{StateAuditWalker, "Audit"},
	{StateAgentCanvas, "Agents"},
	{StateCryptoVault, "Crypto"},
	{StateGovernance, "Gov"},
	{StateImmuneTheater, "Immune"},
}

// authState tracks the authentication status determined by probing /api/status on startup.
type authState int

const (
	authNone     authState = iota // no token configured
	authOK                        // /api/status returned 200 with valid token
	authRejected                  // /api/status returned 401/403
)

// RootModel is the top-level model for the running TUI dashboard.
// It owns the state machine, all screen sub-models, and the global chrome
// (header bar, tab bar, feature flags bar, command bar).
type RootModel struct {
	state       AppState
	screens     map[AppState]screens.Screen
	client      *api.Client
	width       int
	height      int
	statusCache *api.StatusResponse
	dotFrame    int
	paused      bool
	daemonUp    bool
	authStatus  authState
	instanceName string
	showHelp    bool
	retryCount  int
	screenInited map[AppState]bool
	keys        GlobalKeyMap
	prefs       *TUIPrefs
	slashCmd    components.SlashCommandModel
	palette     PaletteModel
	splash      SplashModel
	bubbleField *components.BubbleField
}

// NewRootModel creates the root model with the dashboard screen.
// Additional screens are registered as they are implemented.
func NewRootModel(client *api.Client, prefs *TUIPrefs) *RootModel {
	if prefs == nil {
		prefs = DefaultPrefs()
	}
	scr := map[AppState]screens.Screen{
		StateDashboard:     screens.NewDashboardScreen(),
		StateMemoryBrowser: screens.NewMemoryBrowserScreen(),
		StateRetrievalTheater: screens.NewRetrievalTheaterScreen(),
		StateAuditWalker:   screens.NewAuditWalkerScreen(),
		StateAgentCanvas:   screens.NewAgentCanvasScreen(),
		StateCryptoVault:   screens.NewCryptoVaultScreen(),
		StateGovernance:    screens.NewGovernanceScreen(),
		StateImmuneTheater: screens.NewImmuneTheaterScreen(),
	}
	return &RootModel{
		state:        StateSplash,
		screens:      scr,
		client:       client,
		daemonUp:     true,
		screenInited: make(map[AppState]bool),
		keys:         DefaultGlobalKeyMap(),
		prefs:        prefs,
		slashCmd: components.NewSlashCommandModel(allSlashCommands()),
		palette: NewPaletteModel([]PaletteCommand{
			{"/search", "Search memories"},
			{"/write", "Write a memory"},
			{"/verify", "Verify provenance"},
			{"/backup", "Create encrypted backup"},
			{"/theme deepocean", "Switch to DeepOcean theme"},
			{"/theme phosphor", "Switch to Phosphor theme"},
			{"/theme amber", "Switch to Amber theme"},
			{"/theme midnight", "Switch to Midnight theme"},
			{"/doctor", "Run health checks"},
			{"/logs", "View recent logs"},
			{"/test", "Run test suite"},
			{"/quit", "Quit Nexus"},
		}),
		splash: NewSplashModel(),
		bubbleField:  components.NewBubbleField(120, 40, 12),
	}
}

// Init starts the refresh timers, health check, and splash animation.
func (r *RootModel) Init() tea.Cmd {
	return tea.Batch(
		r.splash.Init(),
		dataTickCmd(),
		healthCheckCmd(r.client),
		dotTickCmd(),
	)
}

// Update handles all messages for the root model.
func (r *RootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		r.width = msg.Width
		r.height = msg.Height
		contentH := r.height - chromeHeight
		if contentH < 1 {
			contentH = 1
		}
		for _, scr := range r.screens {
			scr.SetSize(r.width, contentH)
		}
		r.bubbleField.SetSize(r.width, r.height)
		r.splash.bubbleField.SetSize(r.width, r.height)
		r.splash.width = r.width
		r.splash.height = r.height
		return r, nil

	case HealthCheckResultMsg:
		return r.handleHealthCheck(msg)

	case StatusRefreshMsg:
		if msg.Err != nil {
			dbg("STATUS_REFRESH err=%v", msg.Err)
			if api.Classify(msg.Err) == api.ErrKindForbidden {
				if r.client.HasToken() {
					r.authStatus = authRejected
				}
				// If no token was configured, stay at authNone (zero value).
			}
			return r, nil
		}
		if msg.Data != nil {
			r.statusCache = msg.Data
			r.authStatus = authOK
			r.instanceName = msg.Data.InstanceName
			dbg("STATUS_REFRESH ok memories=%d writes_1m=%d reads_1m=%d queue=%d",
				msg.Data.MemoriesTotal, msg.Data.Writes1m, msg.Data.Reads1m, msg.Data.QueueDepth)
		}
		// Forward status to active screen so it can update its display.
		if scr, ok := r.screens[r.state]; ok {
			dbg("STATUS_FORWARD to=%d(%s)", r.state, scr.Name())
			updated, cmd := scr.Update(api.StatusBroadcastMsg{Data: msg.Data})
			r.screens[r.state] = updated
			return r, cmd
		}
		dbg("STATUS_FORWARD skipped: state=%d has no screen", r.state)
		return r, nil

	case DataTickMsg:
		return r.handleDataTick()

	case DotTickMsg:
		r.dotFrame++
		return r, dotTickCmd()

	case splashTickMsg:
		if r.state == StateSplash {
			updated, cmd := r.splash.Update(msg)
			r.splash = updated
			return r, cmd
		}
		return r, nil

	case SplashDoneMsg:
		dbg("SPLASH_DONE → StateDashboard (cached status=%v)", r.statusCache != nil)
		r.state = StateDashboard
		if !r.screenInited[StateDashboard] {
			r.screenInited[StateDashboard] = true
			contentH := r.height - chromeHeight
			if contentH < 1 {
				contentH = 1
			}
			r.screens[StateDashboard].SetSize(r.width, contentH)
		}
		// Forward cached status immediately so dashboard isn't empty.
		if r.statusCache != nil {
			dbg("SPLASH_DONE forwarding cached status to dashboard (memories=%d)", r.statusCache.MemoriesTotal)
			r.screens[StateDashboard].Update(api.StatusBroadcastMsg{Data: r.statusCache})
		}
		// Also fire a fresh fetch + screen-specific refresh.
		return r, tea.Batch(
			r.screens[StateDashboard].FireRefresh(r.client),
			fetchStatusCmd(r.client),
		)

	case PaletteSelectedMsg:
		r.palette.Close()
		return r, nil

	case NavigateMsg:
		return r.switchScreen(msg.To)

	case tea.KeyMsg:
		if r.state == StateSplash {
			updated, cmd := r.splash.Update(msg)
			r.splash = updated
			return r, cmd
		}
		return r.handleKey(msg)
	}

	// Route to active screen.
	if scr, ok := r.screens[r.state]; ok {
		updated, cmd := scr.Update(msg)
		r.screens[r.state] = updated
		return r, cmd
	}

	return r, nil
}

// View renders the complete TUI layout.
func (r *RootModel) View() string {
	if r.width < minWidth || r.height < minHeight {
		msg := fmt.Sprintf("Terminal too small (minimum %d×%d). Current: %d×%d.",
			minWidth, minHeight, r.width, r.height)
		return lipgloss.Place(r.width, r.height, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().Foreground(styles.ColorAmber).Bold(true).Render(msg))
	}

	if r.state == StateSplash {
		return r.splash.View()
	}

	if !r.daemonUp {
		return r.viewDaemonDown()
	}

	if r.showHelp {
		return r.viewHelp()
	}

	header := r.viewHeaderBar()
	tabbar := r.viewTabBar()
	flags := r.viewFeatureFlags()
	cmdbar := r.viewCommandBar()

	contentH := r.height - chromeHeight
	if contentH < 1 {
		contentH = 1
	}

	var content string
	if scr, ok := r.screens[r.state]; ok {
		content = scr.View()
	}

	page := lipgloss.NewStyle().Width(r.width).Height(contentH).Render(content)

	base := lipgloss.JoinVertical(lipgloss.Left, header, tabbar, page, flags, cmdbar)

	// Palette overlay.
	if r.palette.Active() {
		overlay := r.palette.View()
		return lipgloss.Place(r.width, r.height, lipgloss.Center, lipgloss.Center, overlay)
	}

	return base
}

// ── Key handling ──

func (r *RootModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Palette overlay consumes all keys when active.
	if r.palette.Active() {
		updated, cmd := r.palette.Update(msg)
		r.palette = updated
		return r, cmd
	}

	// Slash command overlay consumes all keys when active.
	if r.slashCmd.Active() {
		updated, cmd := r.slashCmd.Update(msg)
		r.slashCmd = updated
		return r, cmd
	}

	switch {
	case key.Matches(msg, r.keys.Quit):
		return r, tea.Quit
	case key.Matches(msg, r.keys.HardQuit):
		return r, tea.Quit
	case key.Matches(msg, r.keys.Help):
		r.showHelp = !r.showHelp
		return r, nil
	case key.Matches(msg, r.keys.Escape):
		if r.showHelp {
			r.showHelp = false
			return r, nil
		}
		if r.slashCmd.Active() {
			updated, cmd := r.slashCmd.Update(msg)
			r.slashCmd = updated
			return r, cmd
		}
	case key.Matches(msg, r.keys.Pause):
		r.paused = !r.paused
		return r, nil
	case key.Matches(msg, r.keys.Refresh):
		if !r.daemonUp {
			r.retryCount++
			return r, healthCheckCmd(r.client)
		}
		if scr, ok := r.screens[r.state]; ok {
			return r, scr.FireRefresh(r.client)
		}
		return r, nil
	case key.Matches(msg, r.keys.Palette):
		if !r.palette.Active() {
			r.palette.Open(r.width)
			return r, textinput.Blink
		}
	case key.Matches(msg, r.keys.Slash):
		if !r.slashCmd.Active() {
			r.slashCmd.Activate(r.width)
			return r, nil
		}
	case key.Matches(msg, r.keys.NextPage):
		idx := tabIndexForState(r.state)
		next := (idx + 1) % len(screenNames)
		return r.switchScreen(screenNames[next].state)
	case key.Matches(msg, r.keys.PrevPage):
		idx := tabIndexForState(r.state)
		prev := (idx - 1 + len(screenNames)) % len(screenNames)
		return r.switchScreen(screenNames[prev].state)
	}

	// Tab number keys.
	if !r.showHelp {
		for i, kb := range r.keys.tabBindings() {
			if key.Matches(msg, kb) {
				return r.switchScreen(tabStateForIndex(i))
			}
		}
	}

	// Route to active screen.
	if scr, ok := r.screens[r.state]; ok {
		updated, cmd := scr.Update(msg)
		r.screens[r.state] = updated
		return r, cmd
	}

	return r, nil
}

// ── State transitions ──

func (r *RootModel) switchScreen(target AppState) (tea.Model, tea.Cmd) {
	scr, ok := r.screens[target]
	if !ok {
		dbg("SWITCH rejected: no screen for state %d", target)
		return r, nil
	}
	dbg("SWITCH %d(%s) → %d(%s)", r.state, r.screenName(r.state), target, scr.Name())
	r.state = target
	if !r.daemonUp {
		return r, nil
	}
	if !r.screenInited[target] {
		r.screenInited[target] = true
		contentH := r.height - chromeHeight
		if contentH < 1 {
			contentH = 1
		}
		scr.SetSize(r.width, contentH)
	}
	// Forward cached status so the screen isn't empty while waiting for refresh.
	if r.statusCache != nil {
		scr.Update(api.StatusBroadcastMsg{Data: r.statusCache})
	}
	return r, scr.FireRefresh(r.client)
}

func (r *RootModel) screenName(s AppState) string {
	if scr, ok := r.screens[s]; ok {
		return scr.Name()
	}
	if s == StateSplash {
		return "Splash"
	}
	return "?"
}

func (r *RootModel) handleHealthCheck(msg HealthCheckResultMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil || !msg.OK {
		if r.daemonUp {
			dbg("HEALTH_CHECK daemon went DOWN err=%v", msg.Err)
		}
		r.daemonUp = false
		return r, nil
	}
	wasDown := !r.daemonUp
	r.daemonUp = true

	scr, hasScreen := r.screens[r.state]
	if !hasScreen {
		dbg("HEALTH_CHECK ok but no screen for state=%d, fetching status only", r.state)
		return r, fetchStatusCmd(r.client)
	}

	if !r.screenInited[r.state] {
		dbg("HEALTH_CHECK ok, first init for state=%d(%s)", r.state, scr.Name())
		r.screenInited[r.state] = true
		contentH := r.height - chromeHeight
		if contentH < 1 {
			contentH = 1
		}
		scr.SetSize(r.width, contentH)
		cmd := scr.FireRefresh(r.client)
		return r, tea.Batch(cmd, fetchStatusCmd(r.client))
	}
	if wasDown {
		dbg("HEALTH_CHECK daemon recovered, refreshing %s", scr.Name())
		r.retryCount = 0
		cmd := scr.FireRefresh(r.client)
		return r, tea.Batch(cmd, fetchStatusCmd(r.client))
	}
	return r, nil
}

func (r *RootModel) handleDataTick() (tea.Model, tea.Cmd) {
	if !r.daemonUp {
		dbg("DATA_TICK daemon down, health-check only")
		return r, tea.Batch(healthCheckCmd(r.client), dataTickCmd())
	}
	if r.paused {
		dbg("DATA_TICK paused, health-check only")
		return r, tea.Batch(healthCheckCmd(r.client), dataTickCmd())
	}
	cmds := []tea.Cmd{fetchStatusCmd(r.client), healthCheckCmd(r.client), dataTickCmd()}
	if scr, ok := r.screens[r.state]; ok {
		dbg("DATA_TICK refreshing state=%d(%s)", r.state, scr.Name())
		cmds = append(cmds, scr.FireRefresh(r.client))
	} else {
		dbg("DATA_TICK no screen for state=%d", r.state)
	}
	return r, tea.Batch(cmds...)
}

// ── Chrome renderers ──

func (r *RootModel) viewHeaderBar() string {
	dot := components.StatusDot{Status: components.DotOnline, Frame: r.dotFrame}
	if !r.daemonUp {
		dot.Status = components.DotOffline
	}

	ver := "v0.1.3-public"
	uptime := ""
	statusWord := "READY"
	if r.statusCache != nil {
		ver = "v" + r.statusCache.Version
		h := r.statusCache.UptimeSeconds / 3600
		m := (r.statusCache.UptimeSeconds % 3600) / 60
		s := r.statusCache.UptimeSeconds % 60
		uptime = fmt.Sprintf("↑ %dh%02dm%02ds", h, m, s)
		statusWord = strings.ToUpper(r.statusCache.Status)
	}
	if !r.daemonUp {
		statusWord = "OFFLINE"
	}

	instName := r.instanceName
	if instName == "" {
		instName = "default"
	}
	authInd := r.viewAuthIndicator()
	mini := components.MiniLogo{}
	left := fmt.Sprintf("%s %s NEXUS %s · %s · %s  %s", mini.Inline(), dot.View(), ver, instName, authInd, uptime)
	center := "The Governed AI Cryptographic Substrate Control Plane"
	now := time.Now().Format("15:04:05")
	right := fmt.Sprintf("%s · %s", statusWord, now)

	leftStyle := lipgloss.NewStyle().Foreground(styles.TextPrimary)
	centerStyle := lipgloss.NewStyle().Foreground(styles.TextWhiteDim)
	rightStyle := lipgloss.NewStyle().Foreground(styles.TextSecondary)

	leftW := lipgloss.Width(left) + 2
	rightW := lipgloss.Width(right) + 2
	centerW := r.width - leftW - rightW
	if centerW < 0 {
		centerW = 0
		center = ""
	}
	if centerW < len(center) {
		if centerW > 3 {
			center = center[:centerW-1] + "…"
		} else {
			center = ""
		}
	}

	return lipgloss.JoinHorizontal(lipgloss.Bottom,
		leftStyle.Width(leftW).Render(left),
		centerStyle.Width(centerW).Align(lipgloss.Center).Render(center),
		rightStyle.Width(rightW).Align(lipgloss.Right).Render(right),
	)
}

func (r *RootModel) viewAuthIndicator() string {
	switch r.authStatus {
	case authOK:
		return lipgloss.NewStyle().Foreground(styles.ColorGreen).Render("\U0001F513 admin")
	case authRejected:
		return lipgloss.NewStyle().Foreground(styles.ColorRed).Render("\U0001F512 rejected")
	default:
		return lipgloss.NewStyle().Foreground(styles.ColorAmber).Render("\U0001F512 no auth")
	}
}

func (r *RootModel) viewTabBar() string {
	activeIdx := tabIndexForState(r.state)
	var tabs []string
	for i, sn := range screenNames {
		label := fmt.Sprintf(" %d %s ", i+1, sn.label)
		if i == activeIdx {
			tabs = append(tabs, lipgloss.NewStyle().
				Foreground(styles.TextWhite).
				Background(styles.ColorTeal).
				Bold(true).
				Render(label))
		} else {
			tabs = append(tabs, lipgloss.NewStyle().
				Foreground(styles.TextWhiteDim).
				Background(styles.BorderStrong).
				Render(label))
		}
	}
	bar := lipgloss.JoinHorizontal(lipgloss.Bottom, tabs...)
	pad := r.width - lipgloss.Width(bar)
	if pad > 0 {
		bar += strings.Repeat(" ", pad)
	}
	return bar
}

func (r *RootModel) viewFeatureFlags() string {
	type flag struct {
		name    string
		enabled bool
	}
	flags := []flag{
		{"AES-256-GCM", true},
		{"AUDIT", r.statusCache != nil && r.statusCache.AuditEnabled},
		{"GOVERNANCE", true},
		{"MCP", true},
		{"IMMUNE", true},
		{"ENTERPRISE", false},
	}

	var pills []string
	for _, f := range flags {
		var pill string
		if f.enabled {
			pill = lipgloss.NewStyle().
				Foreground(styles.ColorGreen).
				Background(styles.BgPanelAlt).
				Render(fmt.Sprintf(" ✓ %s ", f.name))
		} else {
			pill = lipgloss.NewStyle().
				Foreground(styles.TextMuted).
				Background(styles.BgPanelAlt).
				Render(fmt.Sprintf(" ✗ %s ", f.name))
		}
		pills = append(pills, pill)
	}
	bar := strings.Join(pills, " ")
	pad := r.width - lipgloss.Width(bar)
	if pad > 0 {
		bar += strings.Repeat(" ", pad)
	}
	return bar
}

func (r *RootModel) viewCommandBar() string {
	hint := lipgloss.NewStyle().Foreground(styles.TextMuted).
		Render("  ⌘K palette · / commands · ? help · 1-8 navigate · q quit")
	if r.paused {
		hint += lipgloss.NewStyle().Foreground(styles.ColorAmber).Bold(true).Render("  [PAUSED]")
	}
	pad := r.width - lipgloss.Width(hint)
	if pad > 0 {
		hint += strings.Repeat(" ", pad)
	}
	return hint
}

// ── Special views ──

func (r *RootModel) viewDaemonDown() string {
	title := lipgloss.NewStyle().Foreground(styles.ColorRed).Bold(true).
		Render("  DAEMON NOT RUNNING")
	body := lipgloss.NewStyle().Foreground(styles.TextSecondary).
		Render(fmt.Sprintf("\n  Start with: nexus start\n\n  Retry count: %d\n  Press 'r' to retry, 'q' to quit.", r.retryCount))
	content := lipgloss.JoinVertical(lipgloss.Left, title, body)
	return lipgloss.Place(r.width, r.height, lipgloss.Center, lipgloss.Center, content)
}

func (r *RootModel) viewHelp() string {
	var activeScreen screens.Screen
	if scr, ok := r.screens[r.state]; ok {
		activeScreen = scr
	}
	return HelpOverlay{
		Width:  r.width,
		Height: r.height,
		Keys:   r.keys,
		Screen: activeScreen,
	}.View()
}
