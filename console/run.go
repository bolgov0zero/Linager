package console

import (
	"context"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"

	"github.com/bolgov0zero/linager/installer"
)

// Options configure Run.
type Options struct {
	Lang    installer.Lang
	Version string
	Layout  Layout // zero value: DefaultLayout
}

// Interactive reports whether both ends are a terminal, so the menu can be drawn and read.
func Interactive() bool {
	return term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stdout.Fd())
}

// Run shows the menu until the user leaves it.
func Run(ctx context.Context, o Options) error {
	if o.Layout == (Layout{}) {
		o.Layout = DefaultLayout()
	}
	probe := NewProbe(o.Layout, o.Version)
	defer probe.Close()
	_, err := tea.NewProgram(newModel(o, probe), tea.WithAltScreen(), tea.WithContext(ctx)).Run()
	return err
}
