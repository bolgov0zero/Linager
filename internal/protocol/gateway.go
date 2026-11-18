package protocol

// The gateway module: an agent looks for devices in the networks it was told to scan and reports what it found.
// The server keeps the configuration and sends it to the agent at every connection; the agent keeps its own copy
// and goes on scanning when the link to the panel is lost.

// TypeGatewayPass is sent by the agent after it has scanned one network (a pass is a round over all the networks).
const TypeGatewayPass = "gateway.pass"

// GatewayModule is the name of the module in hello and in commands: gateway.configure, gateway.scan, gateway.state.
const GatewayModule = "gateway"

// GatewayNetCfg is one network of the configuration.
type GatewayNetCfg struct {
	CIDR  string `json:"cidr"`
	On    bool   `json:"on"`
	Ports bool   `json:"ports"` // probe a short list of TCP ports
	Names bool   `json:"names"` // look up the names (reverse DNS)
	// Public is set when the administrator confirmed that this public network may be scanned. The agent refuses a
	// public network without it.
	Public bool `json:"public,omitempty"`
}

// GatewayConfig is what the server sends with gateway.configure.
type GatewayConfig struct {
	Enabled  bool            `json:"enabled"`
	Interval int             `json:"interval_s"` // seconds between the end of a pass and the start of the next
	Nets     []GatewayNetCfg `json:"nets"`
}

// GatewayOwnNet is a network the agent is connected to directly (an interface address and its prefix).
type GatewayOwnNet struct {
	CIDR  string `json:"cidr"` // the /24 around the address of the interface
	Iface string `json:"iface"`
	VLAN  string `json:"vlan,omitempty"` // "VLAN 20" for eth0.20
}

// GatewayLive is the answer of gateway.state.
type GatewayLive struct {
	Enabled    bool            `json:"enabled"`
	Running    bool            `json:"running"`
	Index      int             `json:"index"` // the network being scanned, from 1
	Total      int             `json:"total"`
	CIDR       string          `json:"cidr,omitempty"`
	Phase      string          `json:"phase,omitempty"` // arp, ping, ports, names
	LastPassAt int64           `json:"last_pass_at,omitempty"`
	NextPassAt int64           `json:"next_pass_at,omitempty"`
	PingOK     bool            `json:"ping_ok"` // false: raw ICMP is not available on this host
	OwnNets    []GatewayOwnNet `json:"own_nets"`
}

// GatewayName is a name found for a device and where it came from (ptr, mdns, netbios, ssdp).
type GatewayName struct {
	Src  string `json:"src"`
	Name string `json:"name"`
}

// GatewayPort is an open TCP port with what the service said (an SSH banner, an HTTP title), if anything.
type GatewayPort struct {
	Port   int    `json:"port"`
	Banner string `json:"banner,omitempty"`
}

// GatewayUPnP is what a device says about itself in its UPnP description.
type GatewayUPnP struct {
	Type  string `json:"type,omitempty"` // MediaRenderer, InternetGatewayDevice, Printer, ZonePlayer
	Model string `json:"model,omitempty"`
	Maker string `json:"maker,omitempty"`
}

// GatewayDevice is a device that answered.
type GatewayDevice struct {
	IP    string        `json:"ip"`
	MAC   string        `json:"mac,omitempty"` // only in a network the agent is connected to directly
	Names []GatewayName `json:"names,omitempty"`
	Ports []GatewayPort `json:"ports,omitempty"`
	Via   []string      `json:"via"` // arp, ping, ports
	// Svc are the service types the device announces over mDNS ("_ipp._tcp"), UPnP is its UPnP description, Router
	// is set for the default gateway of the network. They tell what kind of device it is.
	Svc    []string     `json:"svc,omitempty"`
	UPnP   *GatewayUPnP `json:"upnp,omitempty"`
	Router bool         `json:"router,omitempty"`
}

// GatewayPass is the result of one network of a pass.
type GatewayPass struct {
	PassID     string          `json:"pass_id"`
	CIDR       string          `json:"cidr"`
	Own        bool            `json:"own"`             // connected directly: ARP worked and MAC addresses are real
	Iface      string          `json:"iface,omitempty"` // the interface that leads to the network
	Via        string          `json:"via,omitempty"`   // the router of a network that is not own: "192.168.1.1 (eth0)"
	StartedAt  int64           `json:"started_at"`
	FinishedAt int64           `json:"finished_at"`
	Probed     int             `json:"probed"`   // addresses looked at
	Answered   int             `json:"answered"` // addresses that answered
	Err        string          `json:"err,omitempty"`
	Devices    []GatewayDevice `json:"devices"`
	Last       bool            `json:"last"` // the last network of the pass
}

// Limits the server enforces on what an agent sends.
const (
	GatewayMaxDevices  = 1024 // per network (a /24 has at most 254, the rest is a bug or an attack)
	GatewayMinInterval = 60
	GatewayMaxInterval = 86400
)
