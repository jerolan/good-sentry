package notify

import (
	"errors"
	"net/mail"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Validate normalizes the public rule configuration without network I/O.
func (c *Config) Validate() error {
	if c == nil {
		return errors.New("alert configuration is required")
	}
	c.Name = strings.TrimSpace(c.Name)
	if !validText(c.Name) || utf8.RuneCountInString(c.Name) < 1 || utf8.RuneCountInString(c.Name) > 200 {
		return errors.New("name must contain 1 to 200 valid Unicode characters without NUL")
	}
	if c.Kind != Frequency && c.Kind != Uptime {
		return errors.New("unsupported alert kind")
	}
	if c.Quantity < 1 || c.Quantity > 1000000 {
		return errors.New("quantity must be between 1 and 1000000")
	}
	if c.WindowMinutes < 1 || c.WindowMinutes > 10080 {
		return errors.New("windowMinutes must be between 1 and 10080")
	}
	if c.CooldownSeconds < 0 || c.CooldownSeconds > 604800 {
		return errors.New("cooldownSeconds must be between 0 and 604800")
	}
	environments := make([]string, 0, len(c.Environments))
	seenEnvironments := make(map[string]bool)
	for _, environment := range c.Environments {
		if !validText(environment) || environment == "" || utf8.RuneCountInString(environment) > 128 {
			return errors.New("environments must contain nonempty strings of at most 128 characters without NUL")
		}
		if !seenEnvironments[environment] {
			seenEnvironments[environment] = true
			environments = append(environments, environment)
		}
	}
	if len(environments) > 100 {
		return errors.New("at most 100 distinct environments are allowed")
	}
	recipients := make([]Recipient, 0, len(c.Recipients))
	seenRecipients := make(map[Recipient]bool)
	for _, recipient := range c.Recipients {
		recipient.Destination = strings.TrimSpace(recipient.Destination)
		if err := validateRecipient(recipient, true); err != nil {
			return err
		}
		if !seenRecipients[recipient] {
			seenRecipients[recipient] = true
			recipients = append(recipients, recipient)
		}
	}
	if len(recipients) < 1 || len(recipients) > 20 {
		return errors.New("recipients must contain 1 to 20 distinct entries")
	}
	c.Environments, c.Recipients = environments, recipients
	return nil
}

func validText(value string) bool { return utf8.ValidString(value) && !strings.ContainsRune(value, 0) }

func bareEmail(value string) bool {
	if !validText(value) || strings.ContainsAny(value, "\r\n") {
		return false
	}
	address, err := mail.ParseAddress(value)
	return err == nil && address.Name == "" && address.Address == value
}

func validateRecipient(recipient Recipient, allowTeam bool) error {
	if !validText(recipient.Destination) {
		return errors.New("recipient destination must be valid Unicode without NUL")
	}
	switch recipient.Type {
	case "team_email":
		if !allowTeam || recipient.Destination != "" {
			return errors.New("team email requires an empty destination and resolution before delivery")
		}
	case "email":
		if !bareEmail(recipient.Destination) {
			return errors.New("email destination must be a bare valid address")
		}
	case "webhook":
		if utf8.RuneCountInString(recipient.Destination) > 2000 || !validHTTPAddress(recipient.Destination, false) {
			return errors.New("webhook destination must be an absolute HTTP or HTTPS URL of at most 2000 characters without credentials or fragment")
		}
	default:
		return errors.New("unsupported recipient type")
	}
	return nil
}

func validPort(port string) bool {
	if port == "" {
		return false
	}
	for _, ch := range port {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	n, err := strconv.Atoi(port)
	return err == nil && n > 0 && n <= 65535
}

func validHost(host string) bool {
	if host == "" || strings.ContainsAny(host, "\\/?#@") || strings.IndexFunc(host, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return false
	}
	if strings.Contains(host, ":") {
		_, err := netip.ParseAddr(host)
		return err == nil
	}
	return true
}

func validURLHost(u *url.URL) bool {
	if !validHost(u.Hostname()) || strings.HasSuffix(u.Host, ":") {
		return false
	}
	if strings.HasPrefix(u.Host, "[") {
		if _, err := netip.ParseAddr(u.Hostname()); err != nil {
			return false
		}
	}
	return u.Port() == "" || validPort(u.Port())
}

func validHTTPAddress(raw string, publicDomain bool) bool {
	if !validText(raw) || strings.Contains(raw, "#") {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Opaque == "" && u.User == nil && validURLHost(u) && (!publicDomain || (!u.ForceQuery && u.RawQuery == ""))
}

type mailConfig struct {
	scheme, host, port, username, password string
	credentials                            bool
}

// ValidateMail validates operator configuration without echoing a URL or credentials.
func ValidateMail(emailURL, from string) error { _, err := parseMail(emailURL, from); return err }

func parseMail(raw, from string) (mailConfig, error) {
	var config mailConfig
	if !bareEmail(from) {
		return config, errors.New("DEFAULT_FROM_EMAIL must be a bare valid address")
	}
	if !validText(raw) || strings.ContainsAny(raw, "\r\n#") {
		return config, errors.New("EMAIL_URL must use a supported mail transport")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" || u.ForceQuery || u.RawQuery != "" {
		return config, errors.New("EMAIL_URL must use a supported mail transport")
	}
	config.scheme = u.Scheme
	if u.Scheme == "consolemail" {
		if raw != "consolemail://" {
			return config, errors.New("consolemail transport must not specify a destination")
		}
		return config, nil
	}
	switch u.Scheme {
	case "smtp":
		config.port = "25"
	case "smtp+starttls":
		config.port = "587"
	case "smtps":
		config.port = "465"
	default:
		return config, errors.New("EMAIL_URL must use consolemail, smtp, smtp+starttls or smtps")
	}
	if !validURLHost(u) || (u.Path != "" && u.Path != "/") {
		return config, errors.New("EMAIL_URL must contain a valid SMTP host and optional numeric port")
	}
	config.host = u.Hostname()
	if u.Port() != "" {
		config.port = u.Port()
	}
	if u.User != nil {
		config.credentials = true
		config.username, config.password = u.User.Username(), ""
		config.password, _ = u.User.Password()
		if !validText(config.username) || !validText(config.password) || strings.ContainsAny(config.username+config.password, "\r\n") {
			return mailConfig{}, errors.New("EMAIL_URL credentials contain invalid characters")
		}
	}
	return config, nil
}
