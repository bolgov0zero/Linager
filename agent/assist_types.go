package agent

import "fmt"

// assistTokenEnv is the environment variable that carries the one-time token to the helper. The environment of a
// process of another account is not readable by an ordinary user, the command line may be.
const assistTokenEnv = "LINAGER_ASSIST_TOKEN" //nolint:gosec // the name of a variable, not a secret

// Exit codes of the helper; the agent turns them into the reason shown to the administrator.
const (
	helperExitConnect = 10 // could not call the agent back
	helperExitScreen  = 11 // could not open the screen
	helperExitLoop    = 12 // the picture failed while running
)

func helperExitText(code uint32) string {
	switch code {
	case helperExitConnect:
		return "the helper could not call the agent back"
	case helperExitScreen:
		return "the helper could not open the screen of the session"
	case helperExitLoop:
		return "the helper stopped: the screen could not be read"
	}
	return fmt.Sprintf("the helper exited with code %d", code)
}

// AssistSession is a session of the computer the Assistant can be pointed at (a Windows session, or the graphical session of a Linux seat).
type AssistSession struct {
	ID      uint32 `json:"id"`
	Type    string `json:"type"`             // console, rdp or other
	User    string `json:"user"`             // DOMAIN\name, empty on the logon screen
	State   string `json:"state"`            // active, connected, disconnected, ...
	Client  string `json:"client,omitempty"` // where an RDP session comes from
	Console bool   `json:"console"`
	Screen  bool   `json:"screen"` // there is a picture to show: a disconnected session has none
}
