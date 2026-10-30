import { useState, useEffect, useRef } from 'react';
import { Events } from '@wailsio/runtime';
import {
  GetConfig,
  SaveConfig,
  GetStatus,
  GetPeers,
  SendClipboard,
  StartBroadcast,
  StopBroadcast,
  StartDiscovery,
  StopDiscovery,
} from '../bindings/GhostTunnel/p2pservice';
import type { Config, PeerInfo, ServiceStatus, ClipboardMessage } from '../bindings/GhostTunnel/models';

type TabType = 'radar' | 'transfers' | 'settings';

function App() {
  const [activeTab, setActiveTab] = useState<TabType>('radar');

  const [status, setStatus] = useState<ServiceStatus>({
    device_name: 'LocalNode',
    is_broadcasting: false,
    is_discovering: false,
  });

  const [formConfig, setFormConfig] = useState<Config>({
    device_name: '',
    service_type: '_ghosttunnel._tcp',
    port: 9988,
    domain: 'local.',
    auto_broadcast: true,
    auto_discovery: true,
  });

  const [peers, setPeers] = useState<PeerInfo[]>([]);
  const [selectedPeer, setSelectedPeer] = useState<PeerInfo | null>(null);

  // 剪贴板消息流
  const [sendText, setSendText] = useState<string>('');
  const [receivedMessages, setReceivedMessages] = useState<ClipboardMessage[]>([]);

  const [toast, setToast] = useState<string | null>(null);
  const toastTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  const showToast = (message: string) => {
    setToast(message);
    clearTimeout(toastTimer.current);
    toastTimer.current = setTimeout(() => {
      setToast(null);
    }, 4000);
  };

  useEffect(() => {
    // 获取初始状态、配置与已缓存的节点列表
    GetStatus().then((s) => s && setStatus(s)).catch(console.error);
    GetConfig().then((c) => {
      if (c) {
        setFormConfig(c);
      }
    }).catch(console.error);
    GetPeers().then((list) => {
      if (list && list.length > 0) {
        setPeers(list);
        setSelectedPeer(list[0]);
      }
    }).catch(console.error);

    // 监听后端事件
    const unsubStatus = Events.On('p2p:status:change', (ev: any) => {
      if (ev && ev.data) setStatus(ev.data);
    });

    const unsubPeer = Events.On('p2p:peer:found', (ev: any) => {
      if (!ev || !ev.data) return;
      const newPeer: PeerInfo = ev.data;
      setPeers((prev) => {
        const idx = prev.findIndex((p) => p.instance === newPeer.instance);
        if (idx !== -1) {
          const clone = [...prev];
          clone[idx] = newPeer;
          return clone;
        }
        return [newPeer, ...prev];
      });
      setSelectedPeer((curr) => curr || newPeer);
    });

    // 监听收到对端发送的即焚剪贴板
    const unsubClip = Events.On('p2p:clipboard:received', (ev: any) => {
      if (!ev || !ev.data) return;
      const msg: ClipboardMessage = ev.data;
      setReceivedMessages((prev) => [msg, ...prev]);
      showToast(`📩 收到来自 ${msg.sender} 的即焚剪贴板！`);
    });

    return () => {
      unsubStatus();
      unsubPeer();
      unsubClip();
    };
  }, []);

  // 手动开关广播
  const handleToggleBroadcast = () => {
    if (status.is_broadcasting) {
      StopBroadcast()
        .then(() => {
          setStatus((prev) => ({ ...prev, is_broadcasting: false }));
          GetPeers().then((list) => list && setPeers(list));
        })
        .catch((e) => showToast(`停止广播失败: ${e}`));
    } else {
      StartBroadcast()
        .then(() => {
          setStatus((prev) => ({ ...prev, is_broadcasting: true }));
          GetPeers().then((list) => list && setPeers(list));
        })
        .catch((e) => showToast(`启动广播失败: ${e}`));
    }
  };

  // 手动开关扫描
  const handleToggleDiscovery = () => {
    if (status.is_discovering) {
      StopDiscovery()
        .then(() => {
          setStatus((prev) => ({ ...prev, is_discovering: false }));
          GetPeers().then((list) => list && setPeers(list));
        })
        .catch((e) => showToast(`停止扫描失败: ${e}`));
    } else {
      StartDiscovery()
        .then(() => {
          setStatus((prev) => ({ ...prev, is_discovering: true }));
          GetPeers().then((list) => list && setPeers(list));
        })
        .catch((e) => showToast(`启动扫描失败: ${e}`));
    }
  };

  // 发送即焚剪贴板/文本
  const handleSendClipboard = () => {
    if (!sendText.trim()) {
      showToast('请输入要直传的剪贴板内容');
      return;
    }

    const targetIP = selectedPeer?.ip || '127.0.0.1';
    const targetPort = selectedPeer?.port || formConfig.port || 9988;

    SendClipboard(targetIP, targetPort, sendText)
      .then(() => {
        showToast(`🚀 剪贴板已直达 ${selectedPeer?.instance || '目标节点'}`);
        setSendText('');
      })
      .catch((err) => {
        showToast(`❌ 发送失败: ${err}`);
      });
  };

  // 一键复制内容到系统剪贴板
  const handleCopy = (content: string) => {
    navigator.clipboard.writeText(content).then(() => {
      showToast('📋 已成功复制到系统剪贴板！');
    }).catch(() => {
      showToast('复制失败，请手动选取复制');
    });
  };

  // 保存设置页面表单到 config.json
  const handleSaveSettings = () => {
    SaveConfig(formConfig)
      .then(() => {
        showToast('✅ 配置已成功保存至 config.json，将在下次启动时生效！');
      })
      .catch((err) => {
        showToast(`❌ 保存配置失败: ${err}`);
      });
  };

  // 重置表单为默认值
  const handleResetSettings = () => {
    const defaults: Config = {
      device_name: '',
      service_type: '_ghosttunnel._tcp',
      port: 9988,
      domain: 'local.',
      auto_broadcast: true,
      auto_discovery: true,
    };
    setFormConfig(defaults);
    showToast('已重置为默认参数，记得点击下方保存生效');
  };

  return (
    <div className="app-layout">
      {/* 左侧导航侧边栏 */}
      <aside className="sidebar">
        <div className="sidebar-brand">
          <img src="/appicon.png" style={{ width: '28px', height: '28px', borderRadius: '7px', boxShadow: '0 0 12px rgba(0, 242, 254, 0.35)' }} alt="Logo" />
          <span className="brand-text">GhostTunnel</span>
        </div>

        <nav className="nav-list">
          <div
            className={`nav-item ${activeTab === 'radar' ? 'active' : ''}`}
            onClick={() => setActiveTab('radar')}
          >
            <span className="nav-icon">📡</span>
            <span>局域雷达</span>
          </div>

          <div
            className={`nav-item ${activeTab === 'transfers' ? 'active' : ''}`}
            onClick={() => setActiveTab('transfers')}
          >
            <span className="nav-icon">⚡</span>
            <span>极速传输</span>
          </div>

          <div
            className={`nav-item ${activeTab === 'settings' ? 'active' : ''}`}
            onClick={() => setActiveTab('settings')}
          >
            <span className="nav-icon">⚙️</span>
            <span>系统设置</span>
          </div>
        </nav>

        <div className="sidebar-footer">
          <span>节点: {status.device_name}</span>
          <span>v0.1</span>
        </div>
      </aside>

      {/* 右侧主工作区 */}
      <main className="main-content">
        {/* 顶部指示条 */}
        <header className="top-header">
          <div className="page-title">
            {activeTab === 'radar' && '局域网对等节点雷达'}
            {activeTab === 'transfers' && '端到端即焚传输通道'}
            {activeTab === 'settings' && '系统偏好设置'}
          </div>

          <div className="status-badges">
            <div className={`pill-badge ${status.is_broadcasting ? 'active' : ''}`}>
              <span className="dot"></span>
              <span>{status.is_broadcasting ? '广播就绪' : '广播休眠'}</span>
            </div>

            <div className={`pill-badge ${status.is_discovering ? 'active' : ''}`}>
              <span className="dot"></span>
              <span>{status.is_discovering ? '扫描在线' : '扫描休眠'}</span>
            </div>
          </div>
        </header>

        {/* 滚动工作区 */}
        <div className="scroll-view">
          {/* 1. 雷达感知视图 */}
          {activeTab === 'radar' && (
            <div className="radar-grid">
              <div className="card-panel">
                <div className="radar-scope">
                  <div className={`radar-sweep ${status.is_discovering ? '' : 'paused'}`}></div>
                  <div className="radar-ring r1"></div>
                  <div className="radar-ring r2"></div>
                  <div className="radar-ring r3"></div>
                  <div className="radar-center-blip"></div>
                </div>

                <div className="control-bar">
                  <button
                    className={`control-btn ${status.is_broadcasting ? 'is-stop' : 'is-active'}`}
                    onClick={handleToggleBroadcast}
                  >
                    {status.is_broadcasting ? '⏹ 停止广播' : '▶ 开启广播'}
                  </button>

                  <button
                    className={`control-btn ${status.is_discovering ? 'is-stop' : 'is-active'}`}
                    onClick={handleToggleDiscovery}
                  >
                    {status.is_discovering ? '⏹ 停止扫描' : '🔍 开启雷达'}
                  </button>
                </div>
              </div>

              {/* 在线设备感知列表 */}
              <div className="card-panel">
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                  <span style={{ fontSize: '13px', fontWeight: 600 }}>捕获的对端节点 ({peers.length})</span>
                  {peers.length > 0 && (
                    <span
                      style={{ fontSize: '11px', color: 'var(--text-muted)', cursor: 'pointer' }}
                      onClick={() => setPeers([])}
                    >
                      清空列表
                    </span>
                  )}
                </div>

                <div className="peer-list">
                  {peers.length === 0 ? (
                    <div className="empty-placeholder">
                      {status.is_discovering ? '正在扫描同 WiFi 下的设备...' : '雷达已暂停，点击左侧按钮开启扫描'}
                    </div>
                  ) : (
                    peers.map((p, idx) => {
                      const isSelf = p.instance === status.device_name;
                      return (
                        <div
                          key={idx}
                          className="peer-row"
                          style={isSelf ? { borderColor: 'rgba(0, 242, 254, 0.4)', background: 'rgba(0, 242, 254, 0.05)' } : {}}
                        >
                          <div>
                            <div className="peer-title">
                              <span>💻</span> {p.instance} {isSelf && <span style={{ fontSize: '11px', color: 'var(--accent)', marginLeft: '6px' }}>(本机)</span>}
                            </div>
                            <div className="peer-sub">
                              {p.ip || '127.0.0.1'}:{p.port}
                            </div>
                          </div>
                          <div style={{ display: 'flex', gap: '8px', alignItems: 'center' }}>
                            <button
                              className="btn btn-secondary"
                              style={{ padding: '4px 10px', fontSize: '11px' }}
                              onClick={() => {
                                setSelectedPeer(p);
                                setActiveTab('transfers');
                              }}
                            >
                              ⚡ 直传
                            </button>
                          </div>
                        </div>
                      );
                    })
                  )}
                </div>
              </div>
            </div>
          )}

          {/* 2. 极速传输视图 —— 即焚剪贴流实装 */}
          {activeTab === 'transfers' && (
            <div style={{ maxWidth: '780px', margin: '0 auto', display: 'flex', flexDirection: 'column', gap: '20px' }}>
              {/* 发送卡片 */}
              <div className="card-panel">
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                  <span style={{ fontSize: '13px', fontWeight: 600 }}>即焚剪贴流直传</span>
                  <div style={{ fontSize: '12px', color: 'var(--text-secondary)' }}>
                    目标节点: <span style={{ color: 'var(--accent)', fontWeight: 600 }}>{selectedPeer?.instance || '未选择 (默认 127.0.0.1)'}</span>
                  </div>
                </div>

                <textarea
                  style={{
                    width: '100%',
                    height: '110px',
                    background: 'var(--bg-input)',
                    border: '1px solid var(--border)',
                    borderRadius: '8px',
                    padding: '12px',
                    color: 'var(--text-primary)',
                    fontSize: '13px',
                    outline: 'none',
                    resize: 'none',
                    fontFamily: 'inherit',
                  }}
                  placeholder="在此输入或粘贴密码、Token、代码片段，点击直传瞬间推送对端..."
                  value={sendText}
                  onChange={(e) => setSendText(e.target.value)}
                />

                <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '10px' }}>
                  <button className="btn btn-primary" onClick={handleSendClipboard}>
                    ⚡ 瞬移发送
                  </button>
                </div>
              </div>

              {/* 接收卡片列表 */}
              <div className="card-panel">
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                  <span style={{ fontSize: '13px', fontWeight: 600 }}>已接收文本 ({receivedMessages.length})</span>
                  {receivedMessages.length > 0 && (
                    <span
                      style={{ fontSize: '11px', color: 'var(--text-muted)', cursor: 'pointer' }}
                      onClick={() => setReceivedMessages([])}
                    >
                      清空记录
                    </span>
                  )}
                </div>

                {receivedMessages.length === 0 ? (
                  <div className="empty-placeholder">
                    尚未收到对端传输的剪贴板内容，保持监听中...
                  </div>
                ) : (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: '10px' }}>
                    {receivedMessages.map((msg, idx) => (
                      <div
                        key={idx}
                        style={{
                          background: 'rgba(255, 255, 255, 0.02)',
                          border: '1px solid var(--border)',
                          borderRadius: '8px',
                          padding: '12px 16px',
                          display: 'flex',
                          flexDirection: 'column',
                          gap: '8px',
                        }}
                      >
                        <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '11px', color: 'var(--text-muted)' }}>
                          <span>来源: {msg.sender}</span>
                          <span>时间: {msg.time}</span>
                        </div>
                        <div style={{ fontSize: '13px', color: 'var(--text-primary)', wordBreak: 'break-all', whiteSpace: 'pre-wrap' }}>
                          {msg.content}
                        </div>
                        <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
                          <button
                            className="btn btn-secondary"
                            style={{ padding: '3px 10px', fontSize: '11px' }}
                            onClick={() => handleCopy(msg.content)}
                          >
                            📋 一键复制
                          </button>
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </div>
          )}

          {/* 3. 系统设置 (Settings) 页面 */}
          {activeTab === 'settings' && (
            <div className="settings-container">
              <div className="settings-group">
                <div className="group-header">设备与网络配置 (Device & Protocol)</div>
                <div className="settings-card">
                  <div className="form-row">
                    <div className="form-label-col">
                      <span className="form-title">设备展示名称 (Device Name)</span>
                      <span className="form-desc">局域网中对端设备看到的名称，留空则自动取本机主机名</span>
                    </div>
                    <input
                      className="form-input"
                      placeholder="默认使用电脑 Hostname"
                      value={formConfig.device_name}
                      onChange={(e) => setFormConfig({ ...formConfig, device_name: e.target.value })}
                    />
                  </div>

                  <div className="form-row">
                    <div className="form-label-col">
                      <span className="form-title">服务协议类型 (Service Type)</span>
                      <span className="form-desc">mDNS / DNS-SD 发现类型，同网络设备需保持一致</span>
                    </div>
                    <input
                      className="form-input"
                      value={formConfig.service_type}
                      onChange={(e) => setFormConfig({ ...formConfig, service_type: e.target.value })}
                    />
                  </div>

                  <div className="form-row">
                    <div className="form-label-col">
                      <span className="form-title">广播监听端口 (Port)</span>
                      <span className="form-desc">P2P 传输与广播监听的本地端口</span>
                    </div>
                    <input
                      type="number"
                      className="form-input"
                      value={formConfig.port}
                      onChange={(e) => setFormConfig({ ...formConfig, port: parseInt(e.target.value) || 9988 })}
                    />
                  </div>

                  <div className="form-row">
                    <div className="form-label-col">
                      <span className="form-title">广播域 (Domain)</span>
                      <span className="form-desc">本地多播 DNS 域名，通常保持 local. 即可</span>
                    </div>
                    <input
                      className="form-input"
                      value={formConfig.domain}
                      onChange={(e) => setFormConfig({ ...formConfig, domain: e.target.value })}
                    />
                  </div>
                </div>
              </div>

              <div className="settings-group">
                <div className="group-header">启动行为策略 (Startup Behavior)</div>
                <div className="settings-card">
                  <div className="form-row">
                    <div className="form-label-col">
                      <span className="form-title">应用启动时自动开启广播</span>
                      <span className="form-desc">软件打开时即在后台发布自身节点信息</span>
                    </div>
                    <label className="switch">
                      <input
                        type="checkbox"
                        checked={formConfig.auto_broadcast}
                        onChange={(e) => setFormConfig({ ...formConfig, auto_broadcast: e.target.checked })}
                      />
                      <span className="slider"></span>
                    </label>
                  </div>

                  <div className="form-row">
                    <div className="form-label-col">
                      <span className="form-title">应用启动时自动开启雷达扫描</span>
                      <span className="form-desc">软件打开时自动探测局域网内的其它对等节点</span>
                    </div>
                    <label className="switch">
                      <input
                        type="checkbox"
                        checked={formConfig.auto_discovery}
                        onChange={(e) => setFormConfig({ ...formConfig, auto_discovery: e.target.checked })}
                      />
                      <span className="slider"></span>
                    </label>
                  </div>
                </div>
              </div>

              <div className="settings-actions">
                <button className="btn btn-secondary" onClick={handleResetSettings}>
                  恢复默认
                </button>
                <button className="btn btn-primary" onClick={handleSaveSettings}>
                  保存并写入配置
                </button>
              </div>
            </div>
          )}
        </div>
      </main>

      {/* 浮动 Toast 状态反馈 */}
      {toast && (
        <div className="toast-bar">
          <span>{toast}</span>
        </div>
      )}
    </div>
  );
}

export default App;
