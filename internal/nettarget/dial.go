// Package nettarget applies outbound network policy at connection time.
package nettarget

import (
	"context"
	"errors"
	"net"
)

var (
	ErrBlocked = errors.New("destination_blocked")
	ErrDNS     = errors.New("dns_error")
)

type Dialer struct{ AllowPrivate bool }

// DialContext validates every resolved address and pins the connection to one
// of those addresses, so another resolution cannot change the checked target.
func (d Dialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addresses) == 0 {
		return nil, ErrDNS
	}
	for _, address := range addresses {
		ip := address.Unmap()
		if !d.AllowPrivate && (!ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()) {
			return nil, ErrBlocked
		}
	}
	var dialer net.Dialer
	for _, address := range addresses {
		conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(address.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		err = dialErr
		if ctx.Err() != nil {
			break
		}
	}
	return nil, err
}
