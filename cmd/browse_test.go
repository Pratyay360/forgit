package cmd

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func fakeTabs() [3][]browseEntry {
	return [3][]browseEntry{
		{
			{forge: "github", title: "alice/alpha", url: "https://github.com/alice/alpha"},
			{forge: "gitlab", title: "alice/beta", url: "https://gitlab.com/alice/beta"},
		},
		{
			{forge: "github", title: "#3 fix the bug", url: "https://github.com/alice/alpha/issues/3", state: "open"},
		},
		nil,
	}
}

func TestBrowseRender(t *testing.T) {
	m := newBrowseModel(fakeTabs())
	m.width, m.height = 100, 24

	out := m.render()

	for _, want := range []string{
		"forge",
		"Repos (2)",
		"Issues (1)",
		"PRs (0)",
		"alice/alpha",
		"alice/beta",
		"↑/k ↓/j move",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render output missing %q:\n%s", want, out)
		}
	}
}

func TestBrowseRenderSelectedTab(t *testing.T) {
	m := newBrowseModel(fakeTabs())
	m.width, m.height = 100, 24
	m.nextTab() // -> issues

	out := m.render()
	if !strings.Contains(out, "#3 fix the bug") {
		t.Errorf("issues tab missing entry:\n%s", out)
	}
	if !strings.Contains(out, "[open]") {
		t.Errorf("issue state not shown:\n%s", out)
	}
}

func TestBrowseNavigation(t *testing.T) {
	m := newBrowseModel(fakeTabs())

	m.handleKey(tea.KeyPressMsg{Text: "j"})
	if m.sel != 1 {
		t.Errorf("after j: sel = %d, want 1", m.sel)
	}
	m.handleKey(tea.KeyPressMsg{Text: "j"})
	if m.sel != 1 {
		t.Errorf("selection should clamp at last item, got %d", m.sel)
	}
	m.handleKey(tea.KeyPressMsg{Text: "k"})
	if m.sel != 0 {
		t.Errorf("after k: sel = %d, want 0", m.sel)
	}
	m.handleKey(tea.KeyPressMsg{Text: "k"})
	if m.sel != 0 {
		t.Errorf("selection should clamp at first item, got %d", m.sel)
	}

	m.handleKey(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.tab != tabIssues {
		t.Errorf("after right: tab = %d, want issues", m.tab)
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.tab != tabRepos {
		t.Errorf("after left: tab = %d, want repos", m.tab)
	}
}

func TestBrowseFilterTyping(t *testing.T) {
	m := newBrowseModel(fakeTabs())

	// '/' enters filter mode
	if cmd := m.handleKey(tea.KeyPressMsg{Text: "/"}); cmd != nil {
		t.Fatalf("/ should not return a command, got %v", cmd)
	}
	if !m.filterOn {
		t.Fatal("/ should enter filter mode")
	}

	// typing narrows the view; letters that are bindings type normally
	for _, r := range []string{"a", "l", "i", "c", "e"} {
		m.handleKey(tea.KeyPressMsg{Text: r})
	}
	if m.filter != "alice" {
		t.Fatalf("filter = %q, want alice", m.filter)
	}
	if got := len(m.view()); got != 2 {
		t.Errorf("filtered view has %d entries, want 2 (alpha + beta)", got)
	}
	if !strings.Contains(m.render(), "2 matches") {
		t.Errorf("render missing match count:\n%s", m.render())
	}

	// 'q' and 's' while typing are query characters, not bindings
	m.handleKey(tea.KeyPressMsg{Text: "q"})
	m.handleKey(tea.KeyPressMsg{Text: "s"})
	if m.filter != "aliceqs" {
		t.Errorf("filter = %q, want aliceqs (q/s typed)", m.filter)
	}
	if m.sortF != sortNone {
		t.Errorf("typing s should not sort, got %v", m.sortF)
	}
	if len(m.view()) != 0 {
		t.Errorf("view should be empty for aliceqs, got %d", len(m.view()))
	}
	if !strings.Contains(m.render(), "0 matches") {
		t.Errorf("render missing zero match count:\n%s", m.render())
	}

	// render shows the prompt with a cursor and a match count
	out := m.render()
	if !strings.Contains(out, "filter: aliceqs▏") {
		t.Errorf("render missing filter prompt:\n%s", out)
	}
	if !strings.Contains(out, "no matches") {
		t.Errorf("render missing no-matches message:\n%s", out)
	}

	// backspace edits the query
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.filter != "aliceq" {
		t.Errorf("after backspace filter = %q, want aliceq", m.filter)
	}

	// Enter locks the filter in
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.filterOn {
		t.Fatal("enter should leave filter mode")
	}
	if m.filter != "aliceq" {
		t.Errorf("filter should persist after enter, got %q", m.filter)
	}

	// Esc clears the filter entirely
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.filter != "" || m.filterOn {
		t.Errorf("esc should clear filter, got %q (on=%v)", m.filter, m.filterOn)
	}
	if got := len(m.view()); got != len(fakeTabs()[0]) {
		t.Errorf("view after clear = %d, want %d", got, len(fakeTabs()[0]))
	}
}

func TestBrowseFilterSelectionPreserved(t *testing.T) {
	m := newBrowseModel(fakeTabs())
	m.sel = 1 // alice/beta

	m.filterOn = true
	// "al" keeps both entries; selection stays on beta
	m.handleKey(tea.KeyPressMsg{Text: "a"})
	m.handleKey(tea.KeyPressMsg{Text: "l"})
	if e := m.current(); e == nil || e.title != "alice/beta" {
		t.Errorf("selection not preserved, got %+v", e)
	}

	// narrowing to only beta keeps the selection on beta
	for _, r := range []string{"i", "c", "e", "/", "b", "e", "t", "a"} {
		m.handleKey(tea.KeyPressMsg{Text: r})
	}
	if m.filter != "alice/beta" {
		t.Fatalf("filter = %q, want alice/beta", m.filter)
	}
	if e := m.current(); e == nil || e.title != "alice/beta" {
		t.Errorf("selection should follow beta, got %+v", e)
	}

	// "zzz" removes everything; selection clamps to 0
	for _, r := range []string{"z", "z", "z"} {
		m.handleKey(tea.KeyPressMsg{Text: r})
	}
	if m.current() != nil {
		t.Errorf("current should be nil on empty view, got %+v", m.current())
	}
}

func TestBrowseSort(t *testing.T) {
	tabs := [3][]browseEntry{
		{
			{forge: "gitlab", title: "z-last", url: "https://gitlab.com/z"},
			{forge: "bitbucket", title: "m-mid", url: "https://bitbucket.org/m"},
			{forge: "github", title: "a-first", url: "https://github.com/a"},
		},
		nil,
		nil,
	}
	titles := func(v []browseEntry) []string {
		out := make([]string, len(v))
		for i, e := range v {
			out[i] = e.title
		}
		return out
	}
	eq := func(got, want []string) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}

	m := newBrowseModel(tabs)
	m.handleKey(tea.KeyPressMsg{Text: "s"}) // sort by forge, asc
	if m.sortF != sortForge || !m.sortAsc {
		t.Fatalf("after s: sortF=%v asc=%v", m.sortF, m.sortAsc)
	}
	// alphabetical forge order: bitbucket < github < gitlab
	if got, want := titles(m.view()), []string{"m-mid", "a-first", "z-last"}; !eq(got, want) {
		t.Errorf("forge asc order = %v, want %v", got, want)
	}

	m.handleKey(tea.KeyPressMsg{Text: "S"}) // toggle desc
	if m.sortAsc {
		t.Fatal("S should toggle to descending")
	}
	if got, want := titles(m.view()), []string{"z-last", "a-first", "m-mid"}; !eq(got, want) {
		t.Errorf("forge desc order = %v, want %v", got, want)
	}

	m.handleKey(tea.KeyPressMsg{Text: "s"}) // sort by title, asc
	if m.sortF != sortTitle || !m.sortAsc {
		t.Fatalf("after second s: sortF=%v asc=%v", m.sortF, m.sortAsc)
	}
	if got, want := titles(m.view()), []string{"a-first", "m-mid", "z-last"}; !eq(got, want) {
		t.Errorf("title asc order = %v, want %v", got, want)
	}

	m.handleKey(tea.KeyPressMsg{Text: "s"}) // sort by state (all empty), title tiebreak
	m.handleKey(tea.KeyPressMsg{Text: "s"}) // back to none, original order
	if m.sortF != sortNone {
		t.Fatalf("expected sortNone, got %v", m.sortF)
	}
	if got, want := titles(m.view()), []string{"z-last", "m-mid", "a-first"}; !eq(got, want) {
		t.Errorf("original order = %v, want %v", got, want)
	}
}

func TestBrowseSortIndicator(t *testing.T) {
	m := newBrowseModel(fakeTabs())
	if strings.Contains(m.render(), "▴") {
		t.Error("no sort indicator expected when unsorted")
	}
	m.handleKey(tea.KeyPressMsg{Text: "s"})
	out := m.render()
	if !strings.Contains(out, "▴ forge") {
		t.Errorf("render missing sort indicator:\n%s", out)
	}
	m.handleKey(tea.KeyPressMsg{Text: "S"})
	if !strings.Contains(m.render(), "▾ forge") {
		t.Errorf("render missing descending indicator:\n%s", m.render())
	}
}

func TestBrowseFilterArrowsNavigate(t *testing.T) {
	m := newBrowseModel(fakeTabs())
	m.filterOn = true
	m.handleKey(tea.KeyPressMsg{Text: "a"}) // filter = a, both entries match
	if m.sel != 0 {
		t.Fatalf("sel = %d, want 0", m.sel)
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.sel != 1 {
		t.Errorf("down in filter mode: sel = %d, want 1", m.sel)
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.sel != 1 {
		t.Errorf("down should clamp at end, sel = %d", m.sel)
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.sel != 0 {
		t.Errorf("up in filter mode: sel = %d, want 0", m.sel)
	}
}

func TestBrowseOpenShowsStatus(t *testing.T) {
	orig := openURLFn
	openURLFn = func(string) {}
	t.Cleanup(func() { openURLFn = orig })

	m := newBrowseModel(fakeTabs())

	cmd := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on a URL should return a tick command")
	}
	if m.status != "opened https://github.com/alice/alpha" {
		t.Errorf("status = %q, want opened URL", m.status)
	}
	if !strings.Contains(m.render(), "opened https://github.com/alice/alpha") {
		t.Errorf("render missing status line:\n%s", m.render())
	}

	// a matching clear message dismisses the status
	m.Update(clearStatusMsg{status: m.status})
	if m.status != "" {
		t.Errorf("status not cleared, got %q", m.status)
	}
}

func TestBrowseStaleClearKeepsStatus(t *testing.T) {
	orig := openURLFn
	openURLFn = func(string) {}
	t.Cleanup(func() { openURLFn = orig })

	m := newBrowseModel(fakeTabs())
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter}) // status: opened alice/alpha
	m.handleKey(tea.KeyPressMsg{Text: "j"})          // move to alice/beta
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter}) // status: opened alice/beta

	// a stale timer from the first press must not clear the newer status
	m.Update(clearStatusMsg{status: "opened https://github.com/alice/alpha"})
	if m.status != "opened https://gitlab.com/alice/beta" {
		t.Errorf("stale clear wiped newer status, got %q", m.status)
	}
}

func TestBrowseDimmedIssuesTab(t *testing.T) {
	// A tab whose only content is a note row (issue tracker retired) renders
	// dimmed with a dash count and does not claim to have anything here.
	m := newBrowseModel(fakeTabs())
	m.tabs[tabIssues] = nil
	m.addTabNote(tabIssues, "bitbucket · issue tracker retired (2023)")
	m.width, m.height = 100, 24
	m.tab = tabIssues

	out := m.render()
	for _, want := range []string{
		"Issues (–)",
		"bitbucket · issue tracker retired (2023)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "nothing here") {
		t.Errorf("dimmed tab should not show 'nothing here':\n%s", out)
	}

	// Note rows must not be selectable or affect navigation.
	if e := m.current(); e != nil {
		t.Errorf("dimmed tab current = %+v, want nil", e)
	}
	m.handleKey(tea.KeyPressMsg{Text: "j"})
	if m.sel != 0 {
		t.Errorf("sel = %d, want 0 on a dimmed tab", m.sel)
	}
}

func TestBrowseDimmedNoteWithEntries(t *testing.T) {
	// Real issues plus a dimmed note for a retired tracker: both render, and
	// the pill stays a normal count since entries exist.
	m := newBrowseModel(fakeTabs())
	m.addTabNote(tabIssues, "bitbucket · issue tracker retired (2023)")
	m.width, m.height = 100, 24
	m.tab = tabIssues

	out := m.render()
	if !strings.Contains(out, "#3 fix the bug") {
		t.Errorf("real issues missing:\n%s", out)
	}
	if !strings.Contains(out, "issue tracker retired") {
		t.Errorf("dimmed note missing:\n%s", out)
	}
	if !strings.Contains(out, "Issues (1)") {
		t.Errorf("pill should show a normal count when entries exist:\n%s", out)
	}
}

func TestBrowseQuit(t *testing.T) {
	m := newBrowseModel(fakeTabs())
	if cmd := m.handleKey(tea.KeyPressMsg{Text: "q"}); cmd == nil {
		t.Fatal("q should return a quit command")
	}
	if cmd := m.handleKey(tea.KeyPressMsg{Text: "j"}); cmd != nil {
		t.Fatal("navigation should return no command")
	}
}

func TestBrowseCurrent(t *testing.T) {
	m := newBrowseModel(fakeTabs())
	e := m.current()
	if e == nil || e.title != "alice/alpha" {
		t.Errorf("current = %+v, want alice/alpha", e)
	}
	m.nextTab()
	if e := m.current(); e == nil || e.title != "#3 fix the bug" {
		t.Errorf("issues current = %+v, want issue #3", e)
	}
	m.nextTab() // prs tab is empty
	if e := m.current(); e != nil {
		t.Errorf("empty tab current = %+v, want nil", e)
	}
}

func TestBrowseVisibleRange(t *testing.T) {
	m := newBrowseModel([3][]browseEntry{
		makeEntries(100),
		nil,
		nil,
	})
	m.sel = 50
	start, end := m.visibleRange(20)
	if start != 40 || end != 60 {
		t.Errorf("visibleRange(20) around sel=50 = [%d,%d), want [40,60)", start, end)
	}
	m.sel = 99
	start, end = m.visibleRange(20)
	if start != 80 || end != 100 {
		t.Errorf("visibleRange near end = [%d,%d), want [80,100)", start, end)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"short string untouched", "hello", 10, "hello"},
		{"exact fit", "hello", 5, "hello"},
		{"truncated with ellipsis", "hello world", 6, "hello…"},
		{"tiny max", "hello", 1, "…"},
		{"zero max", "hello", 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncate(tt.in, tt.max); got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.in, tt.max, got, tt.want)
			}
		})
	}
}

func makeEntries(n int) []browseEntry {
	entries := make([]browseEntry, n)
	for i := range entries {
		entries[i] = browseEntry{forge: "github", title: "repo"}
	}
	return entries
}
