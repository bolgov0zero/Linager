package server

import (
	"reflect"
	"strings"
	"testing"
)

func TestCleanDNS(t *testing.T) {
	cases := []struct {
		name       string
		ns, search []string
		wantNS     []string
		wantSearch []string
		bad        bool
	}{
		{"ok", []string{"1.1.1.1", "2606:4700:4700::1111"}, []string{"Example.com."}, []string{"1.1.1.1", "2606:4700:4700::1111"}, []string{"example.com"}, false},
		{"blank rows dropped", []string{"", "8.8.8.8"}, []string{" "}, []string{"8.8.8.8"}, nil, false},
		{"none", nil, nil, nil, nil, true},
		{"four", []string{"1.1.1.1", "1.0.0.1", "8.8.8.8", "8.8.4.4"}, nil, nil, nil, true},
		{"not an ip", []string{"dns.google"}, nil, nil, nil, true},
		{"bad domain", []string{"1.1.1.1"}, []string{"bad_name.com"}, nil, nil, true},
		{"seven domains", []string{"1.1.1.1"}, strings.Fields("a.x b.x c.x d.x e.x f.x g.x"), nil, nil, true},
	}
	for _, c := range cases {
		ns, search, ru, en := cleanDNS(c.ns, c.search)
		if (ru != "") != c.bad || (ru == "") != (en == "") {
			t.Errorf("%s: ru=%q en=%q, want bad=%v", c.name, ru, en, c.bad)
			continue
		}
		if !c.bad && (!reflect.DeepEqual(ns, c.wantNS) || !reflect.DeepEqual(search, c.wantSearch)) {
			t.Errorf("%s: got %v %v", c.name, ns, search)
		}
	}
}
