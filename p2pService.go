package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/grandcat/zeroconf"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
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
	Sender    string `json:"sender"`
	Content   string `json:"content"`
	MediaType string `json:"media_type"` // "text" | "image"
	Time      string `json:"time"`
}

type incomingFileState struct {
	file       *os.File
	filename   string
	totalBytes int64
	recvBytes  int64
	isFolder   bool
	startTime  time.Time
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
	peerWindows     map[string]application.Window
	incomingFiles   map[uint64]*incomingFileState
	tcpListener     net.Listener
	notchWindow     *application.NotchWindow
	mainWindow      application.Window
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
				Sender:    remoteAddr,
				Content:   content,
				MediaType: "text",
				Time:      time.Now().Format("15:04:05"),
			}
			log.Printf("📩 [收到即焚剪贴板] 来源: %s, 长度: %d 字节", msg.Sender, len(msg.Content))
			if app := application.Get(); app != nil {
				app.Event.Emit("p2p:clipboard:received", msg)
			}
			if p.notchWindow != nil {
				p.notchWindow.Show()
			}
		case MsgTypeImage:
			content := string(frame.Payload)
			msg := ClipboardMessage{
				Sender:    remoteAddr,
				Content:   content,
				MediaType: "image",
				Time:      time.Now().Format("15:04:05"),
			}
			log.Printf("🖼️ [收到即焚图片] 来源: %s, 字节数: %d", msg.Sender, len(msg.Content))
			if app := application.Get(); app != nil {
				app.Event.Emit("p2p:clipboard:received", msg)
			}
			if p.notchWindow != nil {
				p.notchWindow.Show()
			}
		case MsgTypeFileMeta:
			var meta FileMeta
			if err := json.Unmarshal(frame.Payload, &meta); err != nil {
				log.Printf("❌ 解析文件元数据失败: %v", err)
				continue
			}

			// 保存到系统标准 Downloads/GhostTunnel 目录
			homeDir, _ := os.UserHomeDir()
			downloadDir := filepath.Join(homeDir, "Downloads", "GhostTunnel")
			_ = os.MkdirAll(downloadDir, 0755)

			savePath := filepath.Join(downloadDir, meta.Filename)
			dstFile, err := os.Create(savePath)
			if err != nil {
				log.Printf("❌ 创建接收文件失败: %v", err)
				continue
			}

			p.mu.Lock()
			if p.incomingFiles == nil {
				p.incomingFiles = make(map[uint64]*incomingFileState)
			}
			p.incomingFiles[meta.StreamID] = &incomingFileState{
				file:       dstFile,
				filename:   meta.Filename,
				totalBytes: meta.FileSize,
				recvBytes:  0,
				isFolder:   meta.IsFolder,
				startTime:  time.Now(),
			}
			p.mu.Unlock()

			log.Printf("📥 [开始接收文件/文件夹压缩包] 来源: %s, 文件名: %s, 大小: %d 字节", remoteAddr, meta.Filename, meta.FileSize)
			if app := application.Get(); app != nil {
				app.Event.Emit("p2p:transfer:start", meta)
			}
			if p.notchWindow != nil {
				p.notchWindow.Show()
			}

		case MsgTypeFileChunk:
			p.mu.Lock()
			st, exists := p.incomingFiles[frame.StreamID]
			p.mu.Unlock()

			if exists && st != nil && st.file != nil {
				_, _ = st.file.Write(frame.Payload)
				st.recvBytes += int64(len(frame.Payload))

				elapsed := time.Since(st.startTime).Seconds()
				if elapsed <= 0 {
					elapsed = 0.001
				}
				speedBps := float64(st.recvBytes) / elapsed
				percent := float64(st.recvBytes) / float64(st.totalBytes) * 100

				prog := TransferProgress{
					StreamID:   frame.StreamID,
					Filename:   st.filename,
					TotalBytes: st.totalBytes,
					SentBytes:  st.recvBytes,
					SpeedBps:   speedBps,
					Percent:    percent,
					IsComplete: st.recvBytes >= st.totalBytes,
				}
				if app := application.Get(); app != nil {
					app.Event.Emit("p2p:transfer:progress", prog)
				}

				if st.recvBytes >= st.totalBytes {
					_ = st.file.Close()
					p.mu.Lock()
					delete(p.incomingFiles, frame.StreamID)
					p.mu.Unlock()
					log.Printf("✅ [文件接收完毕] 文件名: %s", st.filename)
					if app := application.Get(); app != nil {
						app.Event.Emit("p2p:transfer:completed", prog)
					}
				}
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

// 供前端调用的 RPC：向指定节点即时发送图片数据 (Base64 DataURL 或二进制载荷)
func (p *P2PService) SendImage(targetIP string, targetPort int, imageDataURL string) error {
	if targetIP == "" {
		targetIP = "127.0.0.1"
	}
	if targetPort <= 0 {
		targetPort = 9988
	}

	addr := fmt.Sprintf("%s:%d", targetIP, targetPort)
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("建立点对点连接失败(%s): %w", addr, err)
	}
	defer conn.Close()

	frame := Frame{
		MsgType:  MsgTypeImage,
		StreamID: uint64(time.Now().UnixNano()),
		Payload:  []byte(imageDataURL),
	}

	if err := EncodeFrame(conn, frame); err != nil {
		return fmt.Errorf("发送图片帧失败: %w", err)
	}

	log.Printf("🖼️ [即焚图片已直传] 目标: %s, 大小: %d 字节", addr, len(frame.Payload))
	return nil
}

// 递归压缩文件夹为 ZIP 格式
func zipFolder(srcDir string, destZip string) error {
	zipFile, err := os.Create(destZip)
	if err != nil {
		return err
	}
	defer zipFile.Close()

	archive := zip.NewWriter(zipFile)
	defer archive.Close()

	srcDir = filepath.Clean(srcDir)
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if relPath == "." {
			return nil
		}

		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relPath)
		if info.IsDir() {
			header.Name += "/"
		} else {
			header.Method = zip.Deflate
		}

		writer, err := archive.CreateHeader(header)
		if err != nil {
			return err
		}

		if !info.IsDir() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()
			_, err = io.Copy(writer, file)
			return err
		}
		return nil
	})
}

// 供前端/拖拽直接调用的文件与文件夹发送核心方法 (大文件流式管道分块直传，避免 OOM)
func (p *P2PService) SendFileOrFolder(targetIP string, targetPort int, localPath string) error {
	fi, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("找不到指定物理文件: %w", err)
	}

	actualPath := localPath
	isFolder := fi.IsDir()
	filename := fi.Name()

	// 若为目录，自动调用 zipFolder 递归打包压缩
	if isFolder {
		tmpZip := filepath.Join(os.TempDir(), fmt.Sprintf("%s_%d.zip", filename, time.Now().Unix()))
		if err := zipFolder(localPath, tmpZip); err != nil {
			return fmt.Errorf("压缩文件夹失败: %w", err)
		}
		defer os.Remove(tmpZip)
		actualPath = tmpZip
		filename = filename + ".zip"
		fi, _ = os.Stat(actualPath)
	}

	fileSize := fi.Size()
	streamID := uint64(time.Now().UnixNano())

	if targetIP == "" {
		targetIP = "127.0.0.1"
	}
	if targetPort <= 0 {
		targetPort = 9988
	}

	addr := fmt.Sprintf("%s:%d", targetIP, targetPort)
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("建立点对点直传失败(%s): %w", addr, err)
	}
	defer conn.Close()

	// 1. 发送元数据帧 MsgTypeFileMeta
	meta := FileMeta{
		StreamID: streamID,
		Filename: filename,
		FileSize: fileSize,
		IsFolder: isFolder,
	}
	metaBytes, _ := json.Marshal(meta)
	metaFrame := Frame{
		MsgType:  MsgTypeFileMeta,
		StreamID: streamID,
		Payload:  metaBytes,
	}
	if err := EncodeFrame(conn, metaFrame); err != nil {
		return fmt.Errorf("发送文件元数据失败: %w", err)
	}

	// 2. 以 2MB 固定环形缓冲区流式分块直传 (零内存拷贝，绝不 OOM)
	file, err := os.Open(actualPath)
	if err != nil {
		return fmt.Errorf("读取物理文件失败: %w", err)
	}
	defer file.Close()

	buf := make([]byte, 2*1024*1024) // 2MB chunk
	var sentBytes int64
	startTime := time.Now()

	for {
		n, rErr := file.Read(buf)
		if n > 0 {
			chunkFrame := Frame{
				MsgType:  MsgTypeFileChunk,
				StreamID: streamID,
				Payload:  buf[:n],
			}
			if err := EncodeFrame(conn, chunkFrame); err != nil {
				return fmt.Errorf("发送文件数据分块失败: %w", err)
			}
			sentBytes += int64(n)

			elapsed := time.Since(startTime).Seconds()
			if elapsed <= 0 {
				elapsed = 0.001
			}
			speedBps := float64(sentBytes) / elapsed
			percent := float64(sentBytes) / float64(fileSize) * 100

			prog := TransferProgress{
				StreamID:   streamID,
				Filename:   filename,
				TotalBytes: fileSize,
				SentBytes:  sentBytes,
				SpeedBps:   speedBps,
				Percent:    percent,
				IsComplete: sentBytes >= fileSize,
			}
			if app := application.Get(); app != nil {
				app.Event.Emit("p2p:transfer:progress", prog)
			}
		}
		if rErr == io.EOF {
			break
		}
		if rErr != nil {
			return fmt.Errorf("流式读取文件分块异常: %w", rErr)
		}
	}

	log.Printf("🚀 [文件/文件夹已传输完成] 目标: %s, 文件名: %s, 总字节: %d", addr, filename, fileSize)
	return nil
}

// 供前端一键拉起原生操作系统文件选择器
func (p *P2PService) SelectAndSendFile(targetIP string, targetPort int) error {
	app := application.Get()
	if app == nil {
		return fmt.Errorf("应用未初始化")
	}

	dialog := app.Dialog.OpenFile()
	dialog.SetTitle("选择要瞬移直传的文件")
	dialog.CanChooseFiles(true)
	dialog.CanChooseDirectories(false)

	filePath, err := dialog.PromptForSingleSelection()
	if err != nil || filePath == "" {
		return nil
	}

	return p.SendFileOrFolder(targetIP, targetPort, filePath)
}

// 供前端一键拉起原生操作系统文件夹选择器 (自动 zip 压缩并直传)
func (p *P2PService) SelectAndSendFolder(targetIP string, targetPort int) error {
	app := application.Get()
	if app == nil {
		return fmt.Errorf("应用未初始化")
	}

	dialog := app.Dialog.OpenFile()
	dialog.SetTitle("选择要自动压缩并直传的文件夹")
	dialog.CanChooseFiles(false)
	dialog.CanChooseDirectories(true)

	dirPath, err := dialog.PromptForSingleSelection()
	if err != nil || dirPath == "" {
		return nil
	}

	return p.SendFileOrFolder(targetIP, targetPort, dirPath)
}

func (p *P2PService) setMainWindow(w application.Window) {
	p.mainWindow = w
}

func (p *P2PService) ShowMainWindow() error {
	if p.mainWindow != nil {
		p.mainWindow.Show()
		p.mainWindow.Focus()
	}
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

func (p *P2PService) ShowNotch() error {
	if p.notchWindow != nil {
		p.notchWindow.Show()
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
	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
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

	p.mu.Lock()
	if p.peerWindows == nil {
		p.peerWindows = make(map[string]application.Window)
	}
	p.peerWindows[windowName] = win
	p.mu.Unlock()

	return nil
}

// 打开桌面边缘极简吸附悬浮球 (满血版：支持原生文件/文件夹拖拽 & 鼠标拖动 & 双击呼出)
func (p *P2PService) OpenDropzoneWindow() error {
	app := application.Get()
	if app == nil {
		return fmt.Errorf("应用实例未初始化")
	}

	windowName := "desktop-dropzone-window"
	if win, exists := app.Window.GetByName(windowName); exists && win != nil {
		win.Show()
		win.Focus()
		return nil
	}

	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             windowName,
		Title:            "瞬移虫洞",
		Width:            150,
		Height:           150,
		Frameless:        true,
		DisableResize:    true,
		AlwaysOnTop:      true,
		EnableFileDrop:   true, // 开启原生文件拖入！
		BackgroundType:   application.BackgroundTypeTransparent,
		BackgroundColour: application.NewRGBA(0, 0, 0, 0),
		Mac: application.MacWindow{
			Backdrop: application.MacBackdropTransparent,
		},
		URL: "/?view=dropzone",
	})

	// 监听系统级文件拖入事件：捕获物理绝对路径，自动打包发送！
	win.OnWindowEvent(events.Common.WindowFilesDropped, func(event *application.WindowEvent) {
		files := event.Context().DroppedFiles()
		log.Printf("🌀 [虫洞吸入文件/文件夹] 物理路径列表: %v", files)
		if len(files) == 0 {
			return
		}

		targetIP := "127.0.0.1"
		targetPort := 9988
		p.mu.Lock()
		for _, peer := range p.peersMap {
			if peer.IP != "" {
				targetIP = peer.IP
				targetPort = peer.Port
				break
			}
		}
		p.mu.Unlock()

		for _, f := range files {
			go func(path string) {
				if err := p.SendFileOrFolder(targetIP, targetPort, path); err != nil {
					log.Printf("❌ 虫洞直传失败(%s): %v", path, err)
				}
			}(f)
		}
	})

	return nil
}

// 一键矩阵平铺所有多开窗口 (Tile Peer Windows)
func (p *P2PService) TilePeerWindows() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	startX := 120
	startY := 120
	step := 45
	index := 0

	for name, win := range p.peerWindows {
		if win != nil && win.IsVisible() {
			win.SetPosition(startX+index*step, startY+index*step)
			win.Focus()
			index++
		} else {
			delete(p.peerWindows, name)
		}
	}
	return nil
}
