package protocol

// TypeMetrics carries a MetricsBatch from the agent.
const TypeMetrics = "metrics"

// Disk is one mounted filesystem.
type Disk struct {
	Mount  string  `json:"mount"`
	Device string  `json:"device"`
	FS     string  `json:"fs"`
	Total  uint64  `json:"total"`
	Used   uint64  `json:"used"`
	Pct    float64 `json:"pct"`
}

// NetIface is one network interface with current rates.
type NetIface struct {
	Iface     string  `json:"iface"`
	Up        bool    `json:"up"`
	IP        string  `json:"ip"`
	MAC       string  `json:"mac"`
	SpeedMbps int     `json:"speed_mbps"`
	RxBps     float64 `json:"rx_bps"`
	TxBps     float64 `json:"tx_bps"`
	RxTotal   uint64  `json:"rx_total"`
	TxTotal   uint64  `json:"tx_total"`
}

// Sample is one measurement taken every 5 seconds.
type Sample struct {
	TS              int64      `json:"ts"`
	CPU             float64    `json:"cpu"`
	MemUsed         uint64     `json:"mem_used"`
	MemTotal        uint64     `json:"mem_total"`
	MemPct          float64    `json:"mem_pct"`
	SwapUsed        uint64     `json:"swap_used"`
	SwapTotal       uint64     `json:"swap_total"`
	Disks           []Disk     `json:"disks"`
	DiskPct         float64    `json:"disk_pct"`
	Net             []NetIface `json:"net"`
	RxBps           float64    `json:"rx_bps"`
	TxBps           float64    `json:"tx_bps"`
	Load            [3]float64 `json:"load"`
	Uptime          int64      `json:"uptime"`
	BootTime        int64      `json:"boot_time"`
	FailedServices  int        `json:"failed_services"`
	FailedUnits     []string   `json:"failed_units,omitempty"` // names of the failed units; the server may subtract ignored ones from the count
	RebootRequired  bool       `json:"reboot_required"`
	Updates         int        `json:"updates"` // -1 = unknown
	SecurityUpdates int        `json:"security_updates"`
	Procs           int        `json:"procs"`
}

// SystemInfo is static host information.
type SystemInfo struct {
	Hostname     string `json:"hostname"`
	OS           string `json:"os"`
	Kernel       string `json:"kernel"`
	Arch         string `json:"arch"`
	CPUModel     string `json:"cpu_model"`
	CPUCores     int    `json:"cpu_cores"`
	MemTotal     uint64 `json:"mem_total"`
	AgentVersion string `json:"agent_version"`
}

// MetricsBatch is the payload of a metrics message. Buffered samples from a
// disconnected period are delivered in the same way, oldest first.
type MetricsBatch struct {
	System  *SystemInfo `json:"system,omitempty"`
	Samples []Sample    `json:"samples"`
}

// MaxBatchSamples bounds one batch (one hour at 5 s plus slack).
const MaxBatchSamples = 800
