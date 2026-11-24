package tether

import (
	"net/netip"
)

func nodeFrom(host, dnsName, os string, ips []netip.Addr, online bool, relay, cur string) Node {
	n := Node{Name: host, DNSName: dnsName, OS: os, IPs: []string{}, Online: online, Relay: relay, CurAddr: cur}
	for _, ip := range ips {
		n.IPs = append(n.IPs, ip.String())
	}
	return n
}
