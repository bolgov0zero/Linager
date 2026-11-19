package agent

// AssistSession is a Windows session the Assistant can be pointed at.
type AssistSession struct {
	ID      uint32 `json:"id"`
	Type    string `json:"type"`             // console, rdp or other
	User    string `json:"user"`             // DOMAIN\name, empty on the logon screen
	State   string `json:"state"`            // active, connected, disconnected, ...
	Client  string `json:"client,omitempty"` // where an RDP session comes from
	Console bool   `json:"console"`
	Screen  bool   `json:"screen"` // there is a picture to show: a disconnected session has none
}
