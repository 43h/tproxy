package main

import (
	"context"
	"net"
	"time"
	. "tproxy/common"
)

type VpnMonitor struct {
	webhookURL string
	interval   int
	vpnOnline  bool
}

func NewVpnMonitor(webhookURL string, interval int) *VpnMonitor {
	if interval <= 0 {
		interval = 30
	}
	return &VpnMonitor{
		webhookURL: webhookURL,
		interval:   interval,
		vpnOnline:  false,
	}
}

func (m *VpnMonitor) Start(ctx context.Context) {
	LOGI("[vpn-monitor] Starting, check interval: ", m.interval, "s")

	// 首次检测确定初始状态
	m.vpnOnline = checkVpnIP()
	if m.vpnOnline {
		LOGI("[vpn-monitor] VPN is online")
	} else {
		LOGI("[vpn-monitor] VPN is offline")
	}

	for {
		select {
		case <-ctx.Done():
			LOGI("[vpn-monitor] Stopped")
			return
		case <-time.After(time.Duration(m.interval) * time.Second):
			online := checkVpnIP()
			if m.vpnOnline && !online {
				LOGI("[vpn-monitor] VPN disconnected!")
				SendWechatNotify(m.webhookURL, "提示: qax超时")
			} else if !m.vpnOnline && online {
				LOGI("[vpn-monitor] VPN reconnected")
			}
			m.vpnOnline = online
		}
	}
}

func checkVpnIP() bool {
	ifaces, err := net.Interfaces()
	if err != nil {
		LOGE("[vpn-monitor] Get interfaces failed: ", err)
		return false
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			if ip4 := ip.To4(); ip4 != nil && ip4[0] == 10 {
				return true
			}
		}
	}
	return false
}
