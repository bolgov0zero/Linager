package installer

import (
	"context"
	"errors"
	"fmt"
	"os"
)

// Options is the full set of `linager install` parameters.
type Options struct {
	Role  string // "", "server" or "agent"
	Lang  string
	NoTUI bool

	Server ServerOpts
	Agent  AgentOpts
}

// Run executes the wizard (interactive TUI on a terminal, plain output otherwise).
func Run(ctx context.Context, o Options) error {
	lang := ParseLang(o.Lang)
	fe := NewPlain(os.Stdout)
	if !o.NoTUI && isTerminal(os.Stdin) && isTerminal(os.Stdout) {
		fe = NewTUI()
	}
	flow := func(ctx context.Context, c Console) (*Summary, error) {
		role := o.Role
		if role == "" {
			if !c.Interactive() {
				return nil, errors.New(tr(c.Lang(), "err.noninteractive", "server|agent"))
			}
			a, err := c.Ask(Form{Title: tr(c.Lang(), "role.title"), Fields: []Field{{
				Key: "role", Label: tr(c.Lang(), "role.label"), Kind: KindChoice, Default: "server",
				Options: []Option{{"server", tr(c.Lang(), "role.server")}, {"agent", tr(c.Lang(), "role.agent")}}}}})
			if err != nil {
				return nil, err
			}
			role = a["role"]
		}
		switch role {
		case "server":
			return ServerFlow(o.Server)(ctx, c)
		case "agent":
			return AgentFlow(o.Agent)(ctx, c)
		}
		return nil, fmt.Errorf("unknown role %q", role)
	}
	_, err := fe.Run(ctx, lang, flow)
	return err
}
