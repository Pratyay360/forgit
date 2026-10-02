package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/pratyay360/forgit/operations"
	"github.com/spf13/cobra"
)

const (
	tabRepos = iota
	tabIssues
	tabPRs
)

var tabNames = []string{"Repos", "Issues", "PRs"}

var browseCmd = &cobra.Command{
	Use:   "browse",
	Short: "Interactively browse repositories, issues and pull requests",
	Long: `Open an interactive terminal UI to browse repositories, issues and
pull requests across all configured forges.

Navigation:
  ↑/k  move up       ←/h  previous tab     /      filter by typing
  ↓/j  move down     →/l  next tab         s/S    sort by column & order
  g/G  top/bottom    enter  open in browser
  q    quit`,
	RunE: runBrowse,
}

func init() {
	rootCmd.AddCommand(browseCmd)
	browseCmd.Flags().Bool("demo", false, "show the UI with sample data (no credentials needed)")
	_ = browseCmd.Flags().MarkHidden("demo")
}

func demoTabs() [3][]browseEntry {
	return [3][]browseEntry{
		{
			{forge: "github", title: "charmbracelet/bubbletea", url: "https://github.com/charmbracelet/bubbletea"},
			{forge: "github", title: "pratyay360/forge", url: "https://github.com/pratyay360/forgit", state: "private"},
			{forge: "gitlab", title: "gitlab-org/gitlab", url: "https://gitlab.com/gitlab-org/gitlab"},
			{forge: "forgejo", title: "codeberg/forgejo", url: "https://codeberg.org/codeberg/forgejo"},
			{forge: "sourcehut", title: "~sircmpwn/hare", url: "https://git.sr.ht/~sircmpwn/hare"},
			{forge: "bitbucket", title: "atlassian/python-bitbucket", url: "https://bitbucket.org/atlassian/python-bitbucket"},
			{forge: "bitbucket", title: "atlassian/bitbucket-linguist", url: "https://bitbucket.org/atlassian/bitbucket-linguist"},
			{forge: "gitlab", title: "gitlab-org/gitlab-runner", url: "https://gitlab.com/gitlab-org/gitlab-runner"},
		},
		{
			{forge: "github", title: "#42 TUI glitches on resize", url: "https://github.com/charmbracelet/bubbletea/issues/42", state: "open"},
			{forge: "github", title: "#7 add --json output", url: "https://github.com/pratyay360/forgit/issues/7", state: "open"},
			{forge: "gitlab", title: "#999 docs: fix typo", url: "https://gitlab.com/gitlab-org/gitlab/-/issues/999", state: "opened"},
			{forge: "sourcehut", title: "~sircmpwn/todo ~T0 draft replies", url: "https://todo.sr.ht/~sircmpwn/todo/0", state: "open"},
			{forge: "bitbucket", title: "#88 plan builds", url: "https://bitbucket.org/atlassian/python-bitbucket/issues/88", state: "new"},
		},
		{
			{forge: "github", title: "#101 feat: focus list", url: "https://github.com/charmbracelet/bubbletea/pull/101", state: "open"},
			{forge: "gitlab", title: "!88 ci: speed up pipeline", url: "https://gitlab.com/gitlab-org/gitlab/-/merge_requests/88", state: "opened"},
			{forge: "forgejo", title: "#99 add git lfs", url: "https://codeberg.org/forgejo/forgejo/pulls/99", state: "open"},
			{forge: "bitbucket", title: "#45 fix auth", url: "https://bitbucket.org/atlassian/python-bitbucket/pull-requests/45", state: "open"},
		},
	}
}

// browseEntry is a single selectable row in the TUI.
type browseEntry struct {
	forge string
	title string
	url   string
	state string
}

// runBrowse loads data from every configured forge and opens the TUI.
func runBrowse(cmd *cobra.Command, args []string) error {
	if !isTerminal(os.Stdout) {
		return fmt.Errorf("browse requires an interactive terminal")
	}

	if demo, _ := cmd.Flags().GetBool("demo"); demo {
		m := newBrowseModel(demoTabs())
		p := tea.NewProgram(m)
		if _, err := p.Run(); err != nil {
			return fmt.Errorf("running TUI: %w", err)
		}
		return nil
	}

	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), 90*time.Second)
	defer cancel()

	cs, err := clients(cfg)
	if err != nil {
		return err
	}

	var repos, issues, prs []browseEntry
	var issuesUnavailable []string // forges whose issue tracker is retired
	for _, c := range cs {
		rs, err := c.ListRepos(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %s: %v\n", c.Name(), err)
			continue
		}
		for _, r := range rs {
			repos = append(repos, browseEntry{forge: c.Name(), title: r.FullName, url: r.URL})
		}

		is, err := c.ListIssues(ctx)
		if err != nil {
			if errors.Is(err, operations.ErrIssuesUnsupported) {
				issuesUnavailable = append(issuesUnavailable, c.Name())
				fmt.Fprintf(os.Stderr, "note: %s: %v\n", c.Name(), err)
			} else {
				fmt.Fprintf(os.Stderr, "warning: %s: %v\n", c.Name(), err)
			}
			continue
		}
		for _, x := range is {
			issues = append(issues, browseEntry{forge: c.Name(), title: fmt.Sprintf("#%d %s", x.Number, x.Title), url: x.URL, state: x.State})
		}

		ps, err := c.ListPRs(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %s: %v\n", c.Name(), err)
			continue
		}
		for _, x := range ps {
			prs = append(prs, browseEntry{forge: c.Name(), title: fmt.Sprintf("#%d %s", x.Number, x.Title), url: x.URL, state: x.State})
		}
	}
	if len(repos)+len(issues)+len(prs) == 0 {
		return fmt.Errorf("no data fetched: every forge failed")
	}

	m := newBrowseModel([3][]browseEntry{repos, issues, prs})
	for _, f := range issuesUnavailable {
		m.addTabNote(tabIssues, f+" · issue tracker retired (2023)")
	}
	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("running TUI: %w", err)
	}
	return nil
}

// clearStatusMsg is sent after a short delay to dismiss the transient
// status line shown after an action like opening a URL. It carries the
// status text it should dismiss so a stale timer can never clear a newer
// status (e.g. two Enter presses within the timeout window).
type clearStatusMsg struct{ status string }

// sortField identifies a column the active tab can be sorted by.
type sortField int

const (
	sortNone sortField = iota
	sortForge
	sortTitle
	sortState
	// numSortFields is the count of sortField values, used for cycling.
	numSortFields
)

// browseModel is the bubbletea model backing the browse UI.
type browseModel struct {
	tabs     [3][]browseEntry
	tabNotes [3][]string // dimmed, non-selectable note rows per tab
	tab      int
	sel      int
	width    int
	height   int
	status   string
	filter   string    // active filter query (empty = no filter)
	filterOn bool      // true while the user is typing a filter query
	sortF    sortField // active sort column (sortNone = original order)
	sortAsc  bool      // sort direction when sortF is set
}

func newBrowseModel(tabs [3][]browseEntry) *browseModel {
	return &browseModel{tabs: tabs}
}

// addTabNote appends a dimmed, non-selectable note row to a tab, e.g. a forge
// whose issue tracker is unavailable. Notes are informational only and never
// participate in selection, filtering or sorting.
func (m *browseModel) addTabNote(tab int, note string) {
	m.tabNotes[tab] = append(m.tabNotes[tab], note)
}

// Init implements tea.Model.
func (m *browseModel) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m *browseModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		return m, m.handleKey(msg)
	case clearStatusMsg:
		if msg.status == m.status {
			m.status = ""
		}
	}
	return m, nil
}

// View implements tea.Model.
func (m *browseModel) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

// handleKey processes one key press and returns an optional command.
func (m *browseModel) handleKey(msg tea.KeyMsg) tea.Cmd {
	key := msg.Key()
	if m.filterOn {
		return m.handleFilterKey(key)
	}
	switch {
	case key.Code == tea.KeyUp || key.Text == "k":
		if m.sel > 0 {
			m.sel--
		}
	case key.Code == tea.KeyDown || key.Text == "j":
		if m.sel < len(m.view())-1 {
			m.sel++
		}
	case key.Code == tea.KeyLeft || key.Text == "h":
		m.prevTab()
	case key.Code == tea.KeyRight || key.Text == "l":
		m.nextTab()
	case key.Code == tea.KeyEnter:
		if e := m.current(); e != nil && e.url != "" {
			m.status = "opened " + e.url
			_ = openURLFn(e.url)
			return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return clearStatusMsg{status: m.status} })
		}
	case key.Text == "/":
		m.filterOn = true
	case key.Text == "s":
		m.cycleSort()
	case key.Text == "S":
		if m.sortF != sortNone {
			prev := m.current()
			m.sortAsc = !m.sortAsc
			m.recalcSel(prev)
		}
	case key.Code == tea.KeyEsc:
		if m.filter != "" {
			prev := m.current()
			m.filter = ""
			m.recalcSel(prev)
		}
	case key.Text == "g":
		m.sel = 0
	case key.Text == "G":
		if v := m.view(); len(v) > 0 {
			m.sel = len(v) - 1
		}
	case key.Text == "q" || key.Text == "Q":
		return tea.Quit
	}
	return nil
}

// handleFilterKey processes keys while the filter prompt is active. Every
// printable character is appended to the query (so letters like q, s and j
// type normally); arrows navigate the live-filtered list; Enter locks the
// filter; Esc clears it and leaves filter mode; backspace edits the query.
func (m *browseModel) handleFilterKey(key tea.Key) tea.Cmd {
	switch key.Code {
	case tea.KeyEsc:
		prev := m.current()
		m.filter = ""
		m.filterOn = false
		m.recalcSel(prev)
		return nil
	case tea.KeyEnter:
		m.filterOn = false
		return nil
	case tea.KeyBackspace, tea.KeyDelete:
		prev := m.current()
		if r := []rune(m.filter); len(r) > 0 {
			m.filter = string(r[:len(r)-1])
			m.recalcSel(prev)
		}
		return nil
	case tea.KeyUp:
		if m.sel > 0 {
			m.sel--
		}
		return nil
	case tea.KeyDown:
		if m.sel < len(m.view())-1 {
			m.sel++
		}
		return nil
	}
	if key.Text != "" {
		prev := m.current()
		m.filter += key.Text
		m.recalcSel(prev)
	}
	return nil
}

// cycleSort advances to the next sort column, restarting at ascending order;
// sortNone -> forge -> title -> state -> sortNone.
func (m *browseModel) cycleSort() {
	prev := m.current()
	m.sortF = (m.sortF + 1) % numSortFields
	m.sortAsc = true
	m.recalcSel(prev)
}

// view returns the entries visible on the active tab after applying the
// current filter and sort. It returns the underlying slice unchanged when
// neither is active, and a freshly built copy otherwise.
func (m *browseModel) view() []browseEntry {
	entries := m.tabs[m.tab]
	if m.filter == "" && m.sortF == sortNone {
		return entries
	}
	q := strings.ToLower(m.filter)
	out := make([]browseEntry, 0, len(entries))
	for _, e := range entries {
		if q == "" || entryMatches(e, q) {
			out = append(out, e)
		}
	}
	if m.sortF != sortNone {
		asc := m.sortAsc
		sort.SliceStable(out, func(i, j int) bool {
			c := compareEntries(out[i], out[j], m.sortF)
			if asc {
				return c < 0
			}
			return c > 0
		})
	}
	return out
}

// entryMatches reports whether the entry matches the lowercased query,
// checking the title, forge and state fields.
func entryMatches(e browseEntry, q string) bool {
	return strings.Contains(strings.ToLower(e.title), q) ||
		strings.Contains(strings.ToLower(e.forge), q) ||
		strings.Contains(strings.ToLower(e.state), q)
}

// compareEntries compares two entries by the given field, returning a value
// <0, >0 or 0. Title is the tie-breaker for forge and state sorts.
func compareEntries(a, b browseEntry, f sortField) int {
	var x, y string
	switch f {
	case sortForge:
		x, y = a.forge, b.forge
	case sortState:
		x, y = a.state, b.state
	default:
		x, y = a.title, b.title
	}
	if c := strings.Compare(x, y); c != 0 {
		return c
	}
	return strings.Compare(a.title, b.title)
}

// sortLabel returns the display name of a sort column.
func sortLabel(f sortField) string {
	switch f {
	case sortForge:
		return "forge"
	case sortTitle:
		return "title"
	case sortState:
		return "state"
	}
	return ""
}

// recalcSel restores the selection after the view changed. When prev is
// non-nil it prefers the entry matching prev, falling back to clamping the
// current index to the new view.
func (m *browseModel) recalcSel(prev *browseEntry) {
	v := m.view()
	if len(v) == 0 {
		m.sel = 0
		return
	}
	if prev != nil {
		for i, e := range v {
			if e.url == prev.url && e.title == prev.title {
				m.sel = i
				return
			}
		}
	}
	if m.sel >= len(v) {
		m.sel = len(v) - 1
	}
}

func (m *browseModel) nextTab() {
	m.tab = (m.tab + 1) % len(m.tabs)
	m.sel = 0
}

func (m *browseModel) prevTab() {
	m.tab = (m.tab - 1 + len(m.tabs)) % len(m.tabs)
	m.sel = 0
}

// current returns the selected entry, or nil when the current tab is empty
// (after filtering).
func (m *browseModel) current() *browseEntry {
	v := m.view()
	if m.sel >= 0 && m.sel < len(v) {
		return &v[m.sel]
	}
	return nil
}

// render builds the full screen content.
func (m *browseModel) render() string {
	width := m.width
	if width <= 0 {
		width = 80
	}

	var b strings.Builder

	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("213")).Render("forgit")
	b.WriteString(title)
	b.WriteString("  ")
	for i, name := range tabNames {
		// A tab whose only content is note rows (e.g. the issue tracker was
		// retired) renders dimmed with a dash instead of a count.
		dimmed := len(m.tabs[i]) == 0 && len(m.tabNotes[i]) > 0
		count := strconv.Itoa(len(m.tabs[i]))
		if dimmed {
			count = "–"
		}
		label := fmt.Sprintf(" %s (%s) ", name, count)
		switch {
		case i == m.tab && dimmed:
			label = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("240")).Render(label)
		case i == m.tab:
			label = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("213")).Render(label)
		case dimmed:
			label = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(label)
		default:
			label = lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render(label)
		}
		b.WriteString(label)
		b.WriteString(" ")
	}
	if m.sortF != sortNone {
		arrow := "▴"
		if !m.sortAsc {
			arrow = "▾"
		}
		b.WriteString(lipgloss.NewStyle().Faint(true).Render(fmt.Sprintf("%s %s", arrow, sortLabel(m.sortF))))
		b.WriteString(" ")
	}
	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Faint(true).Render(strings.Repeat("─", width)))
	b.WriteString("\n")

	entries := m.view()
	if len(entries) == 0 {
		if m.filter != "" || m.filterOn {
			b.WriteString(lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("244")).Render("  no matches"))
			b.WriteString("\n")
		} else if len(m.tabNotes[m.tab]) == 0 {
			b.WriteString(lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("244")).Render("  nothing here"))
			b.WriteString("\n")
		}
	} else {
		reserved := 4
		if m.filter != "" || m.filterOn {
			reserved++
		}
		if m.status != "" {
			reserved++
		}
		reserved += len(m.tabNotes[m.tab])
		height := m.height - reserved
		if height <= 0 {
			height = 20
		}
		start, end := m.visibleRange(height)
		for i := start; i < end; i++ {
			b.WriteString(m.renderEntry(entries[i], i == m.sel, width))
			b.WriteString("\n")
		}
	}

	// Dimmed, non-selectable note rows for the current tab (e.g. forges whose
	// issue tracker was retired).
	for _, n := range m.tabNotes[m.tab] {
		b.WriteString(lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color("240")).Render("  " + truncate(n, width-4)))
		b.WriteString("\n")
	}

	b.WriteString(lipgloss.NewStyle().Faint(true).Render(strings.Repeat("─", width)))
	b.WriteString("\n")
	if m.filter != "" || m.filterOn {
		n := len(m.view())
		plural := ""
		if n != 1 {
			plural = "es" // match -> matches
		}
		// Budget the query so the cursor and match count stay on screen.
		line := "filter: " + truncate(m.filter, width-20)
		if m.filterOn {
			line += "▏"
		}
		line += fmt.Sprintf("  %d match%s", n, plural)
		style := lipgloss.NewStyle().Faint(true)
		if m.filterOn {
			style = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("213"))
		}
		b.WriteString(style.Render("  " + line))
		b.WriteString("\n")
	}
	if m.status != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("78")).Render("  " + truncate(m.status, width-2)))
		b.WriteString("\n")
	}
	b.WriteString(lipgloss.NewStyle().Faint(true).Render("↑/k ↓/j move · ←/h →/l tabs · / filter · s/S sort · enter open · q quit"))
	return b.String()
}

// visibleRange returns the slice of entries to draw around the selection,
// operating on the filtered and sorted view.
func (m *browseModel) visibleRange(height int) (start, end int) {
	entries := m.view()
	if len(entries) == 0 || height <= 0 {
		return 0, 0
	}
	start = m.sel - height/2
	if start < 0 {
		start = 0
	}
	end = start + height
	if end > len(entries) {
		end = len(entries)
		start = end - height
		if start < 0 {
			start = 0
		}
	}
	return start, end
}

// renderEntry renders a single list row, highlighting the selected one.
func (m *browseModel) renderEntry(e browseEntry, selected bool, width int) string {
	badge := lipgloss.NewStyle().Foreground(lipgloss.Color(forgeColor(e.forge))).Render(padRight(e.forge, 10))
	state := ""
	if e.state != "" {
		state = lipgloss.NewStyle().Faint(true).Render("[" + e.state + "] ")
	}
	title := truncate(e.title, width-lipgloss.Width(badge)-lipgloss.Width(state)-4)
	line := badge + " " + state + title
	if selected {
		return "▸ " + lipgloss.NewStyle().Bold(true).Background(lipgloss.Color("62")).Render(padRight(line, width-2))
	}
	return "  " + line
}

func forgeColor(forge string) string {
	if c, ok := forgeColors[forge]; ok {
		return c
	}
	return "245"
}

// padRight pads a styled string to at least width display columns.
func padRight(s string, width int) string {
	if n := lipgloss.Width(s); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}

// truncate cuts a plain string to at most max display runes, adding an
// ellipsis when truncated.
func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 1 {
		return "…"
	}
	return string(runes[:max-1]) + "…"
}

// openURLFn is a test seam; swap it to avoid spawning a browser in tests.
// It is a package-level variable, so tests mutating it must not run with
// t.Parallel.
var openURLFn = openURL

// openURL opens the URL in the default browser, falling back to printing it.
func openURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, url)
		return err
	}
	return nil
}
