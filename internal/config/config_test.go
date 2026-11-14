package config

import "testing"

func valid() *Server {
	c := &Server{}
	c.Database.DSN = "postgres://x"
	c.Admin.Listen, c.Agents.Listen = "0.0.0.0:8080", "0.0.0.0:9443"
	c.applyDefaults()
	return c
}

func TestPortsAreMandatory(t *testing.T) {
	for _, mut := range []func(*Server){
		func(c *Server) { c.Admin.Listen = "" },
		func(c *Server) { c.Agents.Listen = "" },
		func(c *Server) { c.Admin.Listen = "0.0.0.0" },
		func(c *Server) { c.Agents.Listen = "0.0.0.0:99999" },
		func(c *Server) { c.Agents.Listen = c.Admin.Listen },
	} {
		c := valid()
		mut(c)
		if c.Validate() == nil {
			t.Errorf("expected validation error for %+v", c)
		}
	}
	if err := valid().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestDefaults(t *testing.T) {
	c := valid()
	if len(c.Admin.TrustedProxies) != 2 || c.Admin.CookieSecure != "auto" || c.DataDir != "/var/lib/linager" {
		t.Fatalf("unexpected defaults: %+v", c)
	}
}
