package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/grandcat/zeroconf"
	"github.com/wailsapp/wails/v3/pkg/application"
)

type Config struct {
	DeviceName    string `json:"device_name"`
	ServiceType   string `json:"service_type"`
	Port          int    `json:"port"`
	Domain        string `json:"domain"`
	AutoBroadcast bool   `json:"auto_broadcast"`
	AutoDiscovery bool   `json:"auto_discovery"`
}

type ServiceStatus struct {
	DeviceName     string `json:"device_name"`
	IsBroadcasting bool   `json:"is_broadcasting"`
	IsDiscovering  bool   `json:"is_discovering"`
}

type PeerInfo struct {
	Instance string   `json:"instance"`
	IP       string   `json:"ip"`
	Port     int      `json:"port"`
	Text     []string `json:"text"`
}

type ClipboardMessage struct {
	Sender  string `json:"sender"`
	Content string `json:"content"`
	Time    string `json:"time"`
}

type P2PService struct {
	mu              sync.Mutex
	ctx             context.Context
	config          Config
	server          *zeroconf.Server
	resolver        *zeroconf.Resolver
	discoveryCancel context.CancelFunc
	isBroadcasting  bool
	isDiscovering   bool
	hostname        string
	peersMap        map[string]PeerInfo
	tcpListener     net.Listener
	notchWindow     *application.NotchWindow
}

// 跨平台智能获取配置文件物理路径：
// 1. 开发模式：优先读写当前工程工作目录下的 config.json
// 2. 生产打包模式：自动存储在系统标准应用数据目录 (macOS: ~/Library/Application Support/GhostTunnel/config.json)
func getConfigPath() string {
	// 优先检查本地开发目录的 config.json
	if _, err := os.Stat("config.json"); err == nil {
		return "config.json"
	}

	// 生产打包环境自动写入操作系统标准配置目录
	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		return "config.json"
	}

	appConfigDir := filepath.Join(userConfigDir, "GhostTunnel")
	_ = os.MkdirAll(appConfigDir, 0755)
	return filepath.Join(appConfigDir, "config.json")
}

func (p *P2PService) loadConfig() {
	p.config = Config{
		DeviceName:    "",
		ServiceType:   "_ghosttunnel._tcp",
		Port:          9988,
		Domain:        "local.",
		AutoBroadcast: true,
		AutoDiscovery: true,
	}

	path := getConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("[P2P] 未找到配置文件或读取失败(%s)，使用默认配置: %v", path, err)
		return
	}
	if err := json.Unmarshal(data, &p.config); err != nil {
		log.Printf("[P2P] 解析配置文件失败(%s)，使用默认配置: %v", path, err)
	} else {
		log.Printf("[P2P] 成功加载配置文件: %s", path)
	}
}

func (p *P2PService) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	p.ctx = ctx
	p.peersMap = make(map[string]PeerInfo)
	p.loadConfig()

	hostname, _ := os.Hostname()
	if p.config.DeviceName != "" {
		p.hostname = p.config.DeviceName
	} else {
		p.hostname = hostname
	}

	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return fmt.Errorf("初始化 mDNS 解析器失败: %w", err)
	}
	p.resolver = resolver

	// 启动本地原生 TCP 传输服务
	listenPort := p.config.Port
	if listenPort <= 0 {
		listenPort = 9988
	}
	if err := p.startTCPListener(listenPort); err != nil {
		log.Printf("[P2P] 启动 TCP 监听失败: %v", err)
	}

	// 根据配置自动启动
	if p.config.AutoBroadcast {
		if err := p.StartBroadcast(); err != nil {
			log.Printf("[P2P] 自动启动广播失败: %v", err)
		}
	}
	if p.config.AutoDiscovery {
		if err := p.StartDiscovery(); err != nil {
			log.Printf("[P2P] 自动启动扫描失败: %v", err)
		}
	}

	log.Printf("[P2PService] 服务就绪，设备名: %s", p.hostname)
	return nil
}

func (p *P2PService) GetConfig() Config {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.config
}

func (p *P2PService) SaveConfig(cfg Config) error {
	p.mu.Lock()
	p.config = cfg
	data, err := json.MarshalIndent(cfg, "", "  ")
	p.mu.Unlock()

	if err != nil {
		return fmt.Errorf("格式化配置失败: %w", err)
	}

	path := getConfigPath()
	// 确保父目录存在
	if dir := filepath.Dir(path); dir != "." {
		_ = os.MkdirAll(dir, 0755)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("写入配置文件失败(%s): %w", path, err)
	}

	log.Printf("[P2P] 配置已更新并持久化到: %s", path)
	return nil
}

func (p *P2PService) GetStatus() ServiceStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	return ServiceStatus{
		DeviceName:     p.hostname,
		IsBroadcasting: p.isBroadcasting,
		IsDiscovering:  p.isDiscovering,
	}
}

// 供前端在组件挂载时主动拉取已捕获的节点列表，避免事件丢失
func (p *P2PService) GetPeers() []PeerInfo {
	p.mu.Lock()
	defer p.mu.Unlock()

	result := make([]PeerInfo, 0, len(p.peersMap))
	for _, peer := range p.peersMap {
		result = append(result, peer)
	}
	return result
}

func (p *P2PService) recordPeerLocked(peer PeerInfo) {
	if p.peersMap == nil {
		p.peersMap = make(map[string]PeerInfo)
	}
	p.peersMap[peer.Instance] = peer

	go func(pr PeerInfo) {
		if app := application.Get(); app != nil {
			app.Event.Emit("p2p:peer:found", pr)
		}
	}(peer)
}

func (p *P2PService) recordPeer(peer PeerInfo) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.recordPeerLocked(peer)
}

func (p *P2PService) StartBroadcast() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.isBroadcasting {
		return nil
	}

	serviceType := p.config.ServiceType
	if serviceType == "" {
		serviceType = "_ghosttunnel._tcp"
	}
	port := p.config.Port
	if port <= 0 {
		port = 9988
	}
	domain := p.config.Domain
	if domain == "" {
		domain = "local."
	}

	server, err := zeroconf.Register(p.hostname, serviceType, domain, port, []string{"version=0.1.0"}, nil)
	if err != nil {
		return fmt.Errorf("启动广播失败: %w", err)
	}

	p.server = server
	p.isBroadcasting = true
	log.Printf("🚀 [P2P] 广播已开启！名称: %s, 服务: %s", p.hostname, serviceType)
	p.notifyStatusChange()

	// 本地回环自感知：广播启动后，记录并主动通知前端雷达捕获到本机 (持有锁状态)
	p.recordPeerLocked(PeerInfo{
		Instance: p.hostname,
		IP:       "127.0.0.1",
		Port:     port,
		Text:     []string{"version=0.1.0", "self=true"},
	})
	return nil
}

func (p *P2PService) StopBroadcast() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.isBroadcasting || p.server == nil {
		return nil
	}

	p.server.Shutdown()
	p.server = nil
	p.isBroadcasting = false
	log.Println("🛑 [P2P] 广播已停止！")
	p.notifyStatusChange()
	return nil
}

func (p *P2PService) StartDiscovery() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.isDiscovering {
		return nil
	}

	if p.resolver == nil {
		resolver, err := zeroconf.NewResolver(nil)
		if err != nil {
			return fmt.Errorf("创建解析器失败: %w", err)
		}
		p.resolver = resolver
	}

	ctx, cancel := context.WithCancel(p.ctx)
	p.discoveryCancel = cancel
	p.isDiscovering = true
	p.notifyStatusChange()

	entries := make(chan *zeroconf.ServiceEntry)

	go func() {
		for entry := range entries {
			ip := ""
			if len(entry.AddrIPv4) > 0 {
				ip = entry.AddrIPv4[0].String()
			}

			peer := PeerInfo{
				Instance: entry.Instance,
				IP:       ip,
				Port:     entry.Port,
				Text:     entry.Text,
			}

			log.Printf("📡 [雷达捕获] 发现节点: %s (%s:%d)", peer.Instance, peer.IP, peer.Port)
			p.recordPeer(peer)
		}
	}()

	serviceType := p.config.ServiceType
	if serviceType == "" {
		serviceType = "_ghosttunnel._tcp"
	}
	domain := p.config.Domain
	if domain == "" {
		domain = "local."
	}

	go func() {
		if err := p.resolver.Browse(ctx, serviceType, domain, entries); err != nil {
			log.Printf("[P2P] 扫描浏览异常: %v", err)
		}
	}()

	log.Printf("🔍 [P2P] 雷达扫描已启动！搜索: %s", serviceType)

	// 若当前自身正处于广播中，主动触发一次本机回环感知并存库 (持有锁状态)
	if p.isBroadcasting {
		p.recordPeerLocked(PeerInfo{
			Instance: p.hostname,
			IP:       "127.0.0.1",
			Port:     p.config.Port,
			Text:     []string{"version=0.1.0", "self=true"},
		})
	}
	return nil
}

func (p *P2PService) StopDiscovery() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.isDiscovering {
		return nil
	}

	if p.discoveryCancel != nil {
		p.discoveryCancel()
		p.discoveryCancel = nil
	}
	p.isDiscovering = false
	log.Println("🛑 [P2P] 雷达扫描已停止！")
	p.notifyStatusChange()
	return nil
}

func (p *P2PService) notifyStatusChange() {
	if app := application.Get(); app != nil {
		status := ServiceStatus{
			DeviceName:     p.hostname,
			IsBroadcasting: p.isBroadcasting,
			IsDiscovering:  p.isDiscovering,
		}
		app.Event.Emit("p2p:status:change", status)
	}
}

// 启动本地原生 TCP 传输服务监听
func (p *P2PService) startTCPListener(port int) error {
	addr := fmt.Sprintf("0.0.0.0:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("绑定端口 %s 失败: %w", addr, err)
	}
	p.tcpListener = ln
	log.Printf("⚡ [P2P-TCP] 原生传输服务器监听在 %s", addr)

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // 监听关闭
			}
			go p.handleTCPConn(conn)
		}
	}()

	return nil
}

// 处理对端传入的 TCP 会话与二进制帧
func (p *P2PService) handleTCPConn(conn net.Conn) {
	defer conn.Close()
	remoteAddr := conn.RemoteAddr().String()

	for {
		frame, err := DecodeFrame(conn)
		if err != nil {
			return
		}

		switch frame.MsgType {
		case MsgTypeText:
			content := string(frame.Payload)
			msg := ClipboardMessage{
				Sender:  remoteAddr,
				Content: content,
				Time:    time.Now().Format("15:04:05"),
			}
			log.Printf("📩 [收到即焚剪贴板] 来源: %s, 长度: %d 字节", msg.Sender, len(msg.Content))
			if app := application.Get(); app != nil {
				app.Event.Emit("p2p:clipboard:received", msg)
			}
			// 收到即焚消息自动滑下刘海窗口呈现
			if p.notchWindow != nil {
				p.notchWindow.Show()
			}
		case MsgTypePing:
			// 回复 pong 心跳帧
			_ = EncodeFrame(conn, Frame{MsgType: MsgTypePing})
		}
	}
}

// 供前端调用的 RPC：向指定节点即时发送剪贴板/文本数据帧
func (p *P2PService) SendClipboard(targetIP string, targetPort int, content string) error {
	if targetIP == "" {
		targetIP = "127.0.0.1"
	}
	if targetPort <= 0 {
		targetPort = 9988
	}

	addr := fmt.Sprintf("%s:%d", targetIP, targetPort)
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return fmt.Errorf("建立点对点连接失败(%s): %w", addr, err)
	}
	defer conn.Close()

	frame := Frame{
		MsgType:  MsgTypeText,
		StreamID: uint64(time.Now().UnixNano()),
		Payload:  []byte(content),
	}

	if err := EncodeFrame(conn, frame); err != nil {
		return fmt.Errorf("发送数据帧失败: %w", err)
	}

	log.Printf("🚀 [剪贴流已直传] 目标: %s, 字节数: %d", addr, len(frame.Payload))
	return nil
}

func (p *P2PService) SetNotchWindow(nw *application.NotchWindow) {
	p.notchWindow = nw
}

// 切换刘海窗口的展示与收起
func (p *P2PService) ToggleNotch() error {
	if p.notchWindow == nil {
		return fmt.Errorf("当前系统环境未支持刘海窗口")
	}
	if p.notchWindow.Visibility() {
		p.notchWindow.Hide()
	} else {
		p.notchWindow.Show()
	}
	return nil
}

func (p *P2PService) HideNotch() error {
	if p.notchWindow != nil {
		p.notchWindow.Hide()
	}
	return nil
}

// 供前端调用的多开窗口 RPC：为指定节点单独开辟一个独立的悬浮小窗口
func (p *P2PService) OpenPeerWindow(instance string, ip string, port int) error {
	app := application.Get()
	if app == nil {
		return fmt.Errorf("应用实例未初始化")
	}

	windowName := fmt.Sprintf("peer-window-%s", instance)
	if win, exists := app.Window.GetByName(windowName); exists && win != nil {
		win.Show()
		win.Focus()
		return nil
	}

	url := fmt.Sprintf("/?view=peer&instance=%s&ip=%s&port=%d", instance, ip, port)
	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             windowName,
		Title:            fmt.Sprintf("💻 %s - 点对点直传", instance),
		Width:            460,
		Height:           520,
		AlwaysOnTop:      true,
		URL:              url,
		BackgroundColour: application.NewRGB(12, 14, 20),
		Mac: application.MacWindow{
			Backdrop: application.MacBackdropTranslucent,
		},
	})

	return nil
}
