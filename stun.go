package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"

	"github.com/pion/stun/v2"
)

var defaultSTUNServers = []string{
	"stun.syncthing.net:3478",
	"stun.qq.com:3478",
	"stun.miwifi.com:3478",
	"stun.l.google.com:19302",
	"stun.cloudflare.com:3478",
}

type STUNManager struct {
	mu       sync.RWMutex
	info     STUNInfo
	stopChan chan struct{}
}

func NewSTUNManager() *STUNManager {
	return &STUNManager{
		info: STUNInfo{
			Status:  "probing",
			NATType: "Detecting",
		},
		stopChan: make(chan struct{}),
	}
}

// 探测自身公网反射 IP 与端口
func (m *STUNManager) Probe(localPort int) STUNInfo {
	m.mu.Lock()
	m.info.Status = "probing"
	m.mu.Unlock()

	var mappedIP net.IP
	var mappedPort int
	var successServer string

	for _, s := range defaultSTUNServers {
		c, err := stun.Dial("udp", s)
		if err != nil {
			continue
		}

		var xorAddr stun.XORMappedAddress
		msg := stun.MustBuild(stun.TransactionID, stun.BindingRequest)
		err = c.Do(msg, func(res stun.Event) {
			if res.Error != nil {
				return
			}
			_ = xorAddr.GetFrom(res.Message)
		})
		c.Close()

		if err == nil && xorAddr.IP != nil {
			mappedIP = xorAddr.IP
			mappedPort = xorAddr.Port
			successServer = s
			break
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if mappedIP != nil {
		m.info.PublicIP = mappedIP.String()
		m.info.PublicPort = mappedPort
		m.info.Status = "online"

		// 判断 NAT 锥形特征
		if mappedPort == localPort {
			m.info.NATType = "Full Cone (NAT-1)"
		} else {
			m.info.NATType = "Restricted Cone (NAT-2/3)"
		}

		// 生成 6 位随机可读短码（例如 GT-8F2B）
		randBytes := make([]byte, 2)
		_, _ = rand.Read(randBytes)
		m.info.ShortCode = fmt.Sprintf("GT-%s", strings.ToUpper(hex.EncodeToString(randBytes)))
		log.Printf("[STUN] 探测成功 via %s: %s:%d, 短码: %s, NAT: %s",
			successServer, m.info.PublicIP, m.info.PublicPort, m.info.ShortCode, m.info.NATType)
	} else {
		m.info.Status = "failed"
		m.info.NATType = "Unknown / Symmetric"
		log.Println("[STUN] 所有公共 STUN 服务器探测均超时或受限")
	}

	return m.info
}

func (m *STUNManager) GetInfo() STUNInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.info
}
