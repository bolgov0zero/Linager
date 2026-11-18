package server

import (
	"slices"
	"sort"
	"strconv"
	"strings"
)

// The kind of a device ("printer", "camera") is worked out when it is shown, from what is known about it, so that
// better rules apply to the data that has been collected already. Every rule adds a score to a kind together with its
// reason; the kind with the highest score wins if it reaches gwClassMin, and the reasons of the winner are what the
// panel shows ("port 554, manufacturer Hikvision").

// gwClassIn is what the rules look at.
type gwClassIn struct {
	Vendor    string
	RandomMAC bool
	Ports     []int
	Banners   string // the banners of all ports, lower case
	Svc       []string
	UPnPType  string
	NameSrc   string
	Router    bool
}

// gwWhy is one reason for a kind: Kind is gateway, port, vendor, service, upnp, banner, random_mac or name, and Value
// the port, the manufacturer, the service type, the UPnP type or the word found in a banner.
type gwWhy struct {
	Kind  string `json:"kind"`
	Value string `json:"value,omitempty"`
}

const gwClassMin = 3

// gwKindOrder settles a tie: the first one wins.
var gwKindOrder = []string{"router", "hypervisor", "nas", "printer", "camera", "tv", "speaker", "windows", "phone", "iot", "server"}

type gwHit struct {
	score int
	why   gwWhy
}

func classifyDevice(in gwClassIn) (string, []gwWhy) {
	hits := map[string][]gwHit{}
	add := func(kind string, score int, why gwWhy) { hits[kind] = append(hits[kind], gwHit{score, why}) }
	port := func(kind string, score int, ports ...int) {
		for _, p := range ports {
			if slices.Contains(in.Ports, p) {
				add(kind, score, gwWhy{"port", strconv.Itoa(p)})
				return
			}
		}
	}
	vendor := func(kind string, score int, words ...string) {
		v := strings.ToLower(in.Vendor)
		for _, w := range words {
			if v != "" && strings.Contains(v, w) {
				add(kind, score, gwWhy{"vendor", in.Vendor})
				return
			}
		}
	}
	service := func(kind string, score int, types ...string) {
		for _, t := range types {
			if slices.Contains(in.Svc, t) {
				add(kind, score, gwWhy{"service", t})
				return
			}
		}
	}
	banner := func(kind string, score int, words ...string) {
		for _, w := range words {
			if strings.Contains(in.Banners, w) {
				add(kind, score, gwWhy{"banner", w})
				return
			}
		}
	}
	upnp := func(kind string, score int, words ...string) {
		t := strings.ToLower(in.UPnPType)
		for _, w := range words {
			if t != "" && strings.Contains(t, w) {
				add(kind, score, gwWhy{"upnp", in.UPnPType})
				return
			}
		}
	}

	// router
	if in.Router {
		add("router", 10, gwWhy{Kind: "gateway"})
	}
	upnp("router", 8, "internetgatewaydevice")
	port("router", 5, 8291)
	vendor("router", 2, "mikrotik", "routerboard", "ubiquiti", "zyxel", "d-link", "keenetic")
	// hypervisor
	port("hypervisor", 8, 8006)
	banner("hypervisor", 8, "proxmox", "vmware esxi")
	// nas
	vendor("nas", 6, "synology", "qnap", "asustor", "terramaster")
	banner("nas", 6, "diskstation", "synology", "qnap")
	// printer
	service("printer", 8, "_ipp._tcp", "_ipps._tcp", "_printer._tcp", "_pdl-datastream._tcp")
	upnp("printer", 8, "printer")
	port("printer", 6, 9100, 631)
	vendor("printer", 2, "hewlett", "hp inc", "canon", "epson", "brother", "xerox", "lexmark", "kyocera", "ricoh", "konica")
	// camera
	port("camera", 7, 554)
	vendor("camera", 6, "hikvision", "dahua", "axis comm", "reolink", "amcrest", "foscam")
	service("camera", 6, "_rtsp._tcp", "_axis-video._tcp")
	// tv
	if strings.Contains(strings.ToLower(in.UPnPType), "mediarenderer") && vendorIs(in.Vendor, "samsung", "lg elec", "sony", "philips", "tcl", "hisense", "vizio") {
		add("tv", 8, gwWhy{"upnp", in.UPnPType})
	}
	port("tv", 6, 8001, 8002)
	service("tv", 4, "_androidtvremote2._tcp", "_googlecast._tcp", "_airplay._tcp")
	// speaker
	port("speaker", 7, 1400)
	upnp("speaker", 7, "zoneplayer")
	vendor("speaker", 6, "sonos", "bose", "denon", "marantz")
	service("speaker", 4, "_sonos._tcp", "_spotify-connect._tcp", "_raop._tcp")
	// windows
	port("windows", 6, 3389)
	if in.NameSrc == "netbios" {
		add("windows", 4, gwWhy{Kind: "name"})
	}
	port("windows", 2, 445) // Samba, a NAS and Windows all have it: not enough alone
	// phone
	service("phone", 6, "_companion-link._tcp", "_apple-mobdev2._tcp")
	if in.RandomMAC && len(in.Ports) == 0 {
		add("phone", 4, gwWhy{Kind: "random_mac"})
	}
	// smart home
	vendor("iot", 6, "espressif", "tuya", "shelly", "itead", "sonoff", "yeelight", "xiaomi")
	service("iot", 5, "_hap._tcp", "_matter._tcp", "_esphomelib._tcp", "_homekit._tcp")
	port("iot", 4, 1883)
	// a Linux server: only when nothing says more
	banner("server", 3, "debian", "ubuntu")

	best, bestScore := "", 0
	for _, kind := range gwKindOrder {
		total := 0
		for _, h := range hits[kind] {
			total += h.score
		}
		if total > bestScore {
			best, bestScore = kind, total
		}
	}
	if bestScore < gwClassMin {
		return "", nil
	}
	hs := hits[best]
	sort.SliceStable(hs, func(i, j int) bool { return hs[i].score > hs[j].score })
	var why []gwWhy
	for _, h := range hs {
		if !slices.Contains(why, h.why) {
			why = append(why, h.why)
		}
	}
	return best, why
}

func vendorIs(vendor string, words ...string) bool {
	v := strings.ToLower(vendor)
	for _, w := range words {
		if v != "" && strings.Contains(v, w) {
			return true
		}
	}
	return false
}
