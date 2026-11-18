// Package oui tells the manufacturer of a network card by the first three bytes of its MAC address. The table is the
// MA-L registry of the IEEE (https://standards-oui.ieee.org/oui/oui.csv), compressed into the binary by gen.go; a
// new copy of it comes with a new release.
package oui

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed" // the table
	"strconv"
	"strings"
	"sync"
)

//go:embed oui.tsv.gz
var data []byte

var (
	once  sync.Once
	table map[uint32]string
)

func load() {
	table = make(map[uint32]string, 41000)
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return
	}
	sc := bufio.NewScanner(zr)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "\t")
		if !ok || len(k) != 6 {
			continue
		}
		n, err := strconv.ParseUint(k, 16, 32)
		if err != nil {
			continue
		}
		table[uint32(n)] = v
	}
}

// prefix reads the first three bytes of a MAC address written with colons, dashes or dots.
func prefix(mac string) (uint32, byte, bool) {
	var hex []byte
	for i := 0; i < len(mac) && len(hex) < 6; i++ {
		c := mac[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
			hex = append(hex, c)
		case c == ':' || c == '-' || c == '.':
		default:
			return 0, 0, false
		}
	}
	if len(hex) < 6 {
		return 0, 0, false
	}
	n, err := strconv.ParseUint(string(hex), 16, 32)
	if err != nil {
		return 0, 0, false
	}
	return uint32(n), byte((n >> 16) & 0xff), true
}

// Random tells whether a MAC address is a locally administered one (the second bit of the first byte): phones and
// laptops use such an address to hide who made them, and it belongs to no manufacturer.
func Random(mac string) bool {
	_, first, ok := prefix(mac)
	return ok && first&0x02 != 0
}

// Lookup returns the manufacturer of a MAC address, or "" when it is unknown or the address is a random one.
func Lookup(mac string) string {
	p, first, ok := prefix(mac)
	if !ok || first&0x02 != 0 {
		return ""
	}
	once.Do(load)
	return table[p]
}
