package notify

import (
	"strings"
	"testing"
)

func TestConfigValidate(t *testing.T) {
	c := Defaults()
	c.Name = "  Fallos 🙂 "
	c.Environments = []string{"prod", "prod", " stage "}
	c.Recipients = []Recipient{{Type: "email", Destination: " admin@example.com "}, {Type: "email", Destination: "admin@example.com"}, {Type: "team_email"}}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.Name != "Fallos 🙂" || len(c.Environments) != 2 || c.Environments[1] != " stage " || len(c.Recipients) != 2 || c.Recipients[0].Destination != "admin@example.com" {
		t.Fatalf("normalization/deduplication: %#v", c)
	}
	c.Environments = nil
	if err := c.Validate(); err != nil || c.Environments == nil {
		t.Fatalf("empty environments: %#v %v", c.Environments, err)
	}
	cases := []struct {
		name string
		edit func(*Config)
	}{
		{"name empty", func(c *Config) { c.Name = " " }},
		{"name long unicode", func(c *Config) { c.Name = strings.Repeat("🙂", 201) }},
		{"kind", func(c *Config) { c.Kind = "error" }},
		{"quantity zero", func(c *Config) { c.Quantity = 0 }},
		{"quantity large", func(c *Config) { c.Quantity = 1000001 }},
		{"window zero", func(c *Config) { c.WindowMinutes = 0 }},
		{"window large", func(c *Config) { c.WindowMinutes = 10081 }},
		{"cooldown negative", func(c *Config) { c.CooldownSeconds = -1 }},
		{"cooldown large", func(c *Config) { c.CooldownSeconds = 604801 }},
		{"environment empty", func(c *Config) { c.Environments = []string{""} }},
		{"environment long", func(c *Config) { c.Environments = []string{strings.Repeat("🙂", 129)} }},
		{"environment count", func(c *Config) {
			for i := 0; i < 101; i++ {
				c.Environments = append(c.Environments, string(rune(1000+i)))
			}
		}},
		{"environment nul", func(c *Config) { c.Environments = []string{"a\x00b"} }},
		{"invalid utf8", func(c *Config) { c.Name = string([]byte{255}) }},
		{"name nul", func(c *Config) { c.Name = "a\x00b" }},
		{"recipient empty", func(c *Config) { c.Recipients = nil }},
		{"recipient count", func(c *Config) {
			c.Recipients = nil
			for i := 0; i < 21; i++ {
				c.Recipients = append(c.Recipients, Recipient{Type: "email", Destination: string(rune('a'+i)) + "@example.com"})
			}
		}},
		{"recipient type", func(c *Config) { c.Recipients = []Recipient{{Type: "slack", Destination: "https://example.com"}} }},
		{"team destination", func(c *Config) { c.Recipients = []Recipient{{Type: "team_email", Destination: "a@example.com"}} }},
		{"email display name", func(c *Config) { c.Recipients = []Recipient{{Type: "email", Destination: "Name <a@example.com>"}} }},
		{"email header injection", func(c *Config) {
			c.Recipients = []Recipient{{Type: "email", Destination: "a@example.com\r\nBcc: b@example.com"}}
		}},
		{"email invalid", func(c *Config) { c.Recipients = []Recipient{{Type: "email", Destination: "not-an-address"}} }},
		{"webhook credentials", func(c *Config) {
			c.Recipients = []Recipient{{Type: "webhook", Destination: "https://user:password@example.com"}}
		}},
		{"webhook relative", func(c *Config) { c.Recipients = []Recipient{{Type: "webhook", Destination: "/hook"}} }},
		{"webhook scheme", func(c *Config) { c.Recipients = []Recipient{{Type: "webhook", Destination: "ftp://example.com"}} }},
		{"webhook fragment", func(c *Config) { c.Recipients = []Recipient{{Type: "webhook", Destination: "https://example.com/#"}} }},
		{"webhook length", func(c *Config) {
			c.Recipients = []Recipient{{Type: "webhook", Destination: "https://example.com/" + strings.Repeat("x", 2000)}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			config := Defaults()
			config.Name = "alert"
			tc.edit(&config)
			if config.Validate() == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
	max := Defaults()
	max.Name = strings.Repeat("🙂", 200)
	max.Quantity = 1000000
	max.WindowMinutes = 10080
	max.CooldownSeconds = 604800
	if err := max.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateMail(t *testing.T) {
	for _, raw := range []string{"consolemail://", "smtp://localhost", "smtp://user:secret@localhost:2525", "smtp+starttls://user:secret@example.invalid", "smtps://example.invalid:465"} {
		if err := ValidateMail(raw, "goodsentry@localhost"); err != nil {
			t.Errorf("%s: %v", strings.Split(raw, ":")[0], err)
		}
	}
	for _, tc := range []struct{ raw, from string }{
		{"", "a@example.com"}, {"consolemail://host", "a@example.com"}, {"https://user:very-secret@example.com", "a@example.com"},
		{"smtp://user:very-secret@example.com:0", "a@example.com"}, {"smtp://example.com/path", "a@example.com"}, {"smtp://example.com?x=1", "a@example.com"},
		{"smtp://", "a@example.com"}, {"smtp://example.com#", "a@example.com"}, {"smtp://example.com", "Name <a@example.com>"},
		{"smtp://example.com", "a@example.com\r\nBcc: b@example.com"}, {"smtp://example.com", "not-an-email"}, {"smtp://example.com", ""},
	} {
		err := ValidateMail(tc.raw, tc.from)
		if err == nil {
			t.Errorf("invalid mail configuration accepted: %q", strings.Split(tc.raw, ":")[0])
			continue
		}
		if strings.Contains(err.Error(), "very-secret") || strings.Contains(err.Error(), "user:") || strings.Contains(err.Error(), tc.raw) && tc.raw != "" {
			t.Errorf("sensitive configuration leaked: %v", err)
		}
	}
}
