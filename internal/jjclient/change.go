package jjclient

import "fmt"

// Change is one file's state in the worktree.
type Change struct {
	Path      string
	Staging   StatusCode // index state
	Worktree  StatusCode // working tree state
	Untracked bool
}

// String renders the change the way git status --short does.
func (c Change) String() string {
	if c.Untracked {
		return fmt.Sprintf("?? %s", c.Path)
	}
	return fmt.Sprintf("%c%c %s", byte(c.Staging), byte(c.Worktree), c.Path)
}