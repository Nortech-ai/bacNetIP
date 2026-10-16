package validation

import (
	"fmt"
	"net"
	"regexp"
	"strings"
)

func ValidCIDR(ip string, cidr int) (ok bool) {
	ip = fmt.Sprintf("%s/%d", ip, cidr)
	_, _, err := net.ParseCIDR(ip)
	if err != nil {
		return
	}
	ok = true
	return
}

func ValidPort(port int) bool {
	t := fmt.Sprintf("%d", port)
	re, _ := regexp.Compile(`^((6553[0-5])|(655[0-2][0-9])|(65[0-4][0-9]{2})|(6[0-4][0-9]{3})|([1-5][0-9]{4})|([0-5]{0,5})|([0-9]{1,4}))$`)
	return re.MatchString(t)
}

// ValidIP return true if string ip contains a valid representation of an IPv4 or IPv6 address
func ValidIP(ip string) bool {
	ipaddr := net.ParseIP(NormaliseIPAddr(ip))
	return ipaddr != nil
}

// NormaliseIPAddr return ip address without /32 (IPv4 or /128 (IPv6)
func NormaliseIPAddr(ip string) string {
	if strings.HasSuffix(ip, "/32") && strings.Contains(ip, ".") { // single host (IPv4)
		ip = strings.TrimSuffix(ip, "/32")
	} else {
		if before, ok := strings.CutSuffix(ip, "/128"); ok { // single host (IPv6)
			ip = before
		}
	}

	return ip
}
