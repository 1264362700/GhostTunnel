import { useState, useEffect, useRef } from 'react';
import { Events } from '@wailsio/runtime';
import {
  GetConfig,
  SaveConfig,
  GetStatus,
  GetPeers,
  SendClipboard,
  SendImage,
  SelectAndSendFile,
  SelectAndSendFolder,
  ShowMainWindow,
  ToggleNotch,
  HideNotch,
  OpenPeerWindow,
  OpenDropzoneWindow,
  TilePeerWindows,
  StartBroadcast,
  StopBroadcast,
  StartDiscovery,
  StopDiscovery,
} from '../bindings/GhostTunnel/p2pservice';
import type { Config, PeerInfo, ServiceStatus, ClipboardMessage, TransferProgress } from '../bindings/GhostTunnel/models';

type TabType = 'radar' | 'transfers' | 'settings';

function App() {
  // 解析多窗口路由参数
  const urlParams = new URLSearchParams(window.location.search);
  const viewMode = urlParams.get('view'); // 'notch' | 'peer' | 'dropzone' | null
  const peerInstance = urlParams.get('instance') || '未知节点';
  const peerIP = urlParams.get('ip') || '127.0.0.1';
  const peerPort = parseInt(urlParams.get('port') || '9988');

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

  // 剪贴板消息流与图片捕获
  const [sendText, setSendText] = useState<string>('');
  const [pastedImage, setPastedImage] = useState<string | null>(null);
  const [receivedMessages, setReceivedMessages] = useState<ClipboardMessage[]>([]);

  // 阶段三：文件与文件夹分块传输实时进度流
  const [currentTransfer, setCurrentTransfer] = useState<TransferProgress | null>(null);

  // 刘海智能交互状态 (鼠标悬停驻留 & 倒计时自隐)
  const [notchHovered, setNotchHovered] = useState(false);
  const notchTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const [toast, setToast] = useState<string | null>(null);
  const toastTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  const showToast = (message: string) => {
    setToast(message);
    clearTimeout(toastTimer.current);
    toastTimer.current = setTimeout(() => {
      setToast(null);
    }, 4000);
  };

  // 全局剪贴板粘贴监听 (支持截图/图片直接 Cmd+V 捕获)
  useEffect(() => {
    const handleGlobalPaste = (e: ClipboardEvent) => {
      const items = e.clipboardData?.items;
      if (!items) return;
      for (let i = 0; i < items.length; i++) {
        if (items[i].type.indexOf('image') !== -1) {
          const file = items[i].getAsFile();
          if (file) {
            const reader = new FileReader();
            reader.onload = (event) => {
              const base64 = event.target?.result as string;
              if (base64) {
                setPastedImage(base64);
                showToast('🖼️ 已捕获剪贴板图片！可在直传区预览并瞬移发送！');
              }
            };
            reader.readAsDataURL(file);
            break;
          }
        }
      }
    };
    window.addEventListener('paste', handleGlobalPaste);
    return () => window.removeEventListener('paste', handleGlobalPaste);
  }, []);

  // 刘海窗口自动隐藏控制：若鼠标未悬停，5秒后自动隐回刘海
  useEffect(() => {
    if (viewMode !== 'notch') return;
    if (!notchHovered) {
      if (notchTimerRef.current) clearTimeout(notchTimerRef.current);
      notchTimerRef.current = setTimeout(() => {
        HideNotch();
      }, 5000);
    } else {
      if (notchTimerRef.current) {
        clearTimeout(notchTimerRef.current);
        notchTimerRef.current = null;
      }
    }
    return () => {
      if (notchTimerRef.current) clearTimeout(notchTimerRef.current);
    };
  }, [viewMode, notchHovered, receivedMessages]);

  useEffect(() => {
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

    const unsubClip = Events.On('p2p:clipboard:received', (ev: any) => {
      if (!ev || !ev.data) return;
      const msg: ClipboardMessage = ev.data;
      setReceivedMessages((prev) => [msg, ...prev]);
      showToast(`📩 收到来自 ${msg.sender} 的即焚剪贴板！`);
    });

    const unsubProg = Events.On('p2p:transfer:progress', (ev: any) => {
      if (!ev || !ev.data) return;
      const prog: TransferProgress = ev.data;
      setCurrentTransfer(prog);
      if (prog.is_complete) {
        setTimeout(() => setCurrentTransfer(null), 3000);
      }
    });

    const unsubComp = Events.On('p2p:transfer:completed', (ev: any) => {
      if (!ev || !ev.data) return;
      const prog: TransferProgress = ev.data;
      showToast(`🎉 文件 [${prog.filename}] 接收完成！已保存至 Downloads/GhostTunnel`);
    });

    return () => {
      unsubStatus();
      unsubPeer();
      unsubClip();
      unsubProg();
      unsubComp();
    };
  }, []);

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

  const handleSendClipboard = (overrideIP?: string, overridePort?: number) => {
    if (!sendText.trim()) {
      showToast('请输入要直传的剪贴板内容');
      return;
    }

    const targetIP = overrideIP || selectedPeer?.ip || '127.0.0.1';
    const targetPort = overridePort || selectedPeer?.port || formConfig.port || 9988;

    SendClipboard(targetIP, targetPort, sendText)
      .then(() => {
        showToast(`🚀 剪贴板已直达目标`);
        setSendText('');
      })
      .catch((err) => {
        showToast(`❌ 发送失败: ${err}`);
      });
  };

  // Base64 DataURL 转 Blob 对象
  const dataURLtoBlob = (dataurl: string): Blob => {
    const arr = dataurl.split(',');
    const match = arr[0].match(/:(.*?);/);
    const mime = match ? match[1] : 'image/png';
    const bstr = atob(arr[1] || arr[0]);
    let n = bstr.length;
    const u8arr = new Uint8Array(n);
    while (n--) {
      u8arr[n] = bstr.charCodeAt(n);
    }
    return new Blob([u8arr], { type: mime });
  };

  // 一键将图片写入操作系统原生剪贴板 (让用户直接在微信/飞书/文档里 Cmd+V 粘贴)
  const handleCopyImage = (imageDataURL: string) => {
    try {
      const blob = dataURLtoBlob(imageDataURL);
      navigator.clipboard.write([
        new ClipboardItem({ [blob.type]: blob })
      ]).then(() => {
        showToast('🖼️ 图片已成功写入系统剪贴板！可直接在任意软件 Cmd+V 粘贴！');
      }).catch((err) => {
        showToast(`复制图片失败: ${err}`);
      });
    } catch (e) {
      showToast(`处理图片失败: ${e}`);
    }
  };

  // 直传图片 RPC
  const handleSendImage = (overrideIP?: string, overridePort?: number) => {
    if (!pastedImage) {
      showToast('当前未捕获到剪贴板图片');
      return;
    }
    const targetIP = overrideIP || selectedPeer?.ip || '127.0.0.1';
    const targetPort = overridePort || selectedPeer?.port || formConfig.port || 9988;

    SendImage(targetIP, targetPort, pastedImage)
      .then(() => {
        showToast('🚀 图片已瞬移直传目标！');
        setPastedImage(null);
      })
      .catch((err) => {
        showToast(`❌ 发送图片失败: ${err}`);
      });
  };

  const handleCopy = (content: string) => {
    navigator.clipboard.writeText(content).then(() => {
      showToast('📋 已成功复制到系统剪贴板！');
    }).catch(() => {
      showToast('复制失败，请手动选取');
    });
  };

  const handleSaveSettings = () => {
    SaveConfig(formConfig)
      .then(() => {
        showToast('✅ 配置已成功保存至 config.json，将在下次启动时生效！');
      })
      .catch((err) => {
        showToast(`❌ 保存配置失败: ${err}`);
      });
  };

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
    showToast('已重置为默认参数');
  };

  // ==================== 1. 专属刘海灵动胶囊窗口视图 ====================
  if (viewMode === 'notch') {
    const latestMsg = receivedMessages[0];
    const isImage = latestMsg?.media_type === 'image';

    return (
      <div
        onMouseEnter={() => setNotchHovered(true)}
        onMouseLeave={() => {
          setNotchHovered(false);
          if (notchTimerRef.current) clearTimeout(notchTimerRef.current);
          notchTimerRef.current = setTimeout(() => HideNotch(), 1800);
        }}
        style={{
          width: '100vw',
          height: '100vh',
          background: '#000000',
          color: '#f8fafc',
          display: 'flex',
          flexDirection: 'column',
          justifyContent: 'space-between',
          padding: '12px 24px 10px',
          boxSizing: 'border-box',
          borderBottomLeftRadius: '28px',
          borderBottomRightRadius: '28px',
          borderBottom: '1px solid rgba(0, 242, 254, 0.4)',
          boxShadow: '0 25px 50px rgba(0,0,0,0.95), 0 0 25px rgba(0, 242, 254, 0.25)',
          overflow: 'hidden',
          transition: 'all 0.3s cubic-bezier(0.16, 1, 0.3, 1)',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          {/* 左侧：胶囊标识与脉冲呼吸灯 */}
          <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
            <div style={{
              width: '8px',
              height: '8px',
              borderRadius: '50%',
              background: '#00f2fe',
              boxShadow: '0 0 10px #00f2fe',
              animation: 'pulse 2s infinite'
            }} />
            <img src="/appicon.png" style={{ width: '22px', height: '22px', borderRadius: '6px' }} alt="" />
            <div>
              <div style={{ fontSize: '11px', fontWeight: 800, color: '#00f2fe', letterSpacing: '0.5px' }}>
                GHOSTTUNNEL 灵动岛
              </div>
              <div style={{ fontSize: '9px', color: '#64748b' }}>
                {notchHovered ? '⚡ 鼠标已锁定 · 保持展开' : '⏳ 鼠标离开或无操作将自动收起'}
              </div>
            </div>
          </div>

          {/* 中间：直传内容/图片预览 */}
          <div style={{
            flex: 1,
            margin: '0 16px',
            background: 'rgba(255, 255, 255, 0.05)',
            padding: '6px 12px',
            borderRadius: '10px',
            border: '1px solid rgba(255,255,255,0.08)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            overflow: 'hidden',
            maxHeight: '48px',
          }}>
            {latestMsg ? (
              isImage ? (
                <div style={{ display: 'flex', alignItems: 'center', gap: '8px', overflow: 'hidden' }}>
                  <img
                    src={latestMsg.content}
                    alt="收到截图"
                    style={{ height: '36px', borderRadius: '4px', border: '1px solid rgba(0,242,254,0.4)', objectFit: 'contain' }}
                  />
                  <span style={{ fontSize: '11px', color: '#e2e8f0', whiteSpace: 'nowrap' }}>
                    🖼️ 来自 <strong style={{ color: '#00f2fe' }}>{latestMsg.sender}</strong> 的图片直传
                  </span>
                </div>
              ) : (
                <span style={{ fontSize: '11px', color: '#e2e8f0', textOverflow: 'ellipsis', overflow: 'hidden', whiteSpace: 'nowrap' }}>
                  📩 <strong style={{ color: '#00f2fe' }}>{latestMsg.sender}:</strong> {latestMsg.content}
                </span>
              )
            ) : (
              <span style={{ fontSize: '11px', color: '#64748b' }}>暂未收到新的直传消息，对端发送时此处将灵动唤醒</span>
            )}

            {latestMsg && (
              <button
                className="btn btn-primary"
                style={{ padding: '3px 10px', fontSize: '10px', marginLeft: '10px', flexShrink: 0, borderRadius: '6px' }}
                onClick={() => isImage ? handleCopyImage(latestMsg.content) : handleCopy(latestMsg.content)}
              >
                {isImage ? '复制图片' : '复制文本'}
              </button>
            )}
          </div>

          {/* 右侧：手动收起 */}
          <button
            className="btn btn-secondary"
            style={{ padding: '4px 10px', fontSize: '10px', borderRadius: '6px' }}
            onClick={() => HideNotch()}
            title="立即收起刘海窗口"
          >
            ✕
          </button>
        </div>

        {/* 底部：流光倒计时进度条 (悬停时暂停全满，移开后5秒平滑清零并隐退) */}
        <div style={{
          width: '100%',
          height: '2px',
          background: 'rgba(255,255,255,0.06)',
          borderRadius: '1px',
          overflow: 'hidden',
          marginTop: '6px'
        }}>
          <div style={{
            height: '100%',
            background: 'linear-gradient(90deg, #00f2fe, #4facfe)',
            width: notchHovered ? '100%' : '0%',
            transition: notchHovered ? 'none' : 'width 5s linear'
          }} />
        </div>
      </div>
    );
  }

  // ==================== 2. 节点专属独立悬浮直传窗口视图 ====================
  if (viewMode === 'peer') {
    return (
      <div style={{
        width: '100vw',
        height: '100vh',
        background: '#0c0e14',
        color: '#f8fafc',
        padding: '18px',
        boxSizing: 'border-box',
        display: 'flex',
        flexDirection: 'column',
        gap: '12px',
      }}>
        {/* 顶部标题栏 */}
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', borderBottom: '1px solid rgba(255,255,255,0.08)', paddingBottom: '10px' }}>
          <div>
            <div style={{ fontSize: '13px', fontWeight: 700, color: '#00f2fe' }}>💻 {peerInstance}</div>
            <div style={{ fontSize: '10px', color: '#64748b' }}>端对端直传专用隧道 · {peerIP}:{peerPort}</div>
          </div>
          <span className="pill-badge active">
            <span className="dot"></span>
            <span>置顶悬浮</span>
          </span>
        </div>

        {/* 剪贴板图片暂存与快捷直传 */}
        {pastedImage && (
          <div style={{
            background: 'rgba(0, 242, 254, 0.08)',
            border: '1px dashed rgba(0, 242, 254, 0.4)',
            borderRadius: '8px',
            padding: '8px 12px',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between'
          }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
              <img src={pastedImage} style={{ height: '32px', borderRadius: '4px' }} alt="" />
              <span style={{ fontSize: '11px', color: '#00f2fe' }}>已捕获剪贴板图片</span>
            </div>
            <div style={{ display: 'flex', gap: '6px' }}>
              <button className="btn btn-primary" style={{ padding: '2px 8px', fontSize: '10px' }} onClick={() => handleSendImage(peerIP, peerPort)}>
                ⚡ 瞬移发送
              </button>
              <button className="btn btn-secondary" style={{ padding: '2px 6px', fontSize: '10px' }} onClick={() => setPastedImage(null)}>
                ✕
              </button>
            </div>
          </div>
        )}

        {/* 文本输入框 */}
        <textarea
          style={{
            width: '100%',
            height: '80px',
            background: 'rgba(10, 13, 20, 0.8)',
            border: '1px solid rgba(255,255,255,0.1)',
            borderRadius: '8px',
            padding: '10px',
            color: '#fff',
            fontSize: '12px',
            outline: 'none',
            resize: 'none',
            boxSizing: 'border-box',
          }}
          placeholder={`输入文本，或在此直接按 Cmd+V 粘贴截图直传给 ${peerInstance}...`}
          value={sendText}
          onChange={(e) => setSendText(e.target.value)}
        />

        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '8px' }}>
          <button className="btn btn-primary" onClick={() => handleSendClipboard(peerIP, peerPort)}>
            ⚡ 直传文本
          </button>
        </div>

        {/* 消息历史 */}
        <div style={{ flex: 1, overflowY: 'auto', display: 'flex', flexDirection: 'column', gap: '8px' }}>
          <span style={{ fontSize: '11px', color: '#64748b' }}>收到的剪贴流:</span>
          {receivedMessages.map((msg, i) => {
            const isImg = msg.media_type === 'image';
            return (
              <div key={i} style={{ background: 'rgba(255,255,255,0.03)', padding: '10px', borderRadius: '6px', fontSize: '12px' }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', color: '#64748b', fontSize: '10px', marginBottom: '4px' }}>
                  <span>{msg.sender}</span>
                  <span>{msg.time}</span>
                </div>
                {isImg ? (
                  <div style={{ marginTop: '4px' }}>
                    <img src={msg.content} style={{ maxWidth: '100%', maxHeight: '140px', borderRadius: '6px', border: '1px solid rgba(255,255,255,0.1)' }} alt="" />
                    <div style={{ textAlign: 'right', marginTop: '6px' }}>
                      <button className="btn btn-primary" style={{ padding: '2px 8px', fontSize: '10px' }} onClick={() => handleCopyImage(msg.content)}>
                        📋 复制图片
                      </button>
                    </div>
                  </div>
                ) : (
                  <>
                    <div style={{ wordBreak: 'break-all' }}>{msg.content}</div>
                    <div style={{ textAlign: 'right', marginTop: '6px' }}>
                      <button className="btn btn-secondary" style={{ padding: '2px 8px', fontSize: '10px' }} onClick={() => handleCopy(msg.content)}>
                        复制
                      </button>
                    </div>
                  </>
                )}
              </div>
            );
          })}
        </div>
      </div>
    );
  }

  // ==================== 3. 桌面边缘吸附瞬移虫洞挂件 (Dropzone Bubble - 满血修复) ====================
  if (viewMode === 'dropzone') {
    return (
      <div
        data-file-drop-target="true"
        onDoubleClick={() => ShowMainWindow()}
        style={{
          width: '100vw',
          height: '100vh',
          background: 'transparent',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          overflow: 'hidden',
          padding: '10px',
          boxSizing: 'border-box',
          // @ts-ignore
          WebkitAppRegion: 'drag',
        }}
      >
        <div
          data-file-drop-target="true"
          className="wormhole-bubble"
          title="按住鼠标拖动位置 · 双击唤醒控制台 · 拖入文件/文件夹直接瞬移"
          style={{
            width: '128px',
            height: '128px',
            borderRadius: '50%',
            background: currentTransfer
              ? 'radial-gradient(circle at 35% 35%, rgba(0, 242, 254, 0.6), rgba(7, 9, 14, 0.98))'
              : 'radial-gradient(circle at 35% 35%, rgba(0, 242, 254, 0.35), rgba(7, 9, 14, 0.96))',
            border: currentTransfer
              ? '2px solid #00f2fe'
              : '1.5px solid rgba(0, 242, 254, 0.55)',
            boxShadow: currentTransfer
              ? '0 0 40px rgba(0, 242, 254, 0.8), inset 0 0 25px rgba(0,0,0,0.9)'
              : '0 0 30px rgba(0, 242, 254, 0.35), inset 0 0 25px rgba(0,0,0,0.9)',
            backdropFilter: 'blur(20px)',
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'center',
            justifyContent: 'center',
            color: '#fff',
            textAlign: 'center',
            transition: 'all 0.3s cubic-bezier(0.16, 1, 0.3, 1)',
            cursor: 'grab',
          }}
        >
          {currentTransfer ? (
            <div style={{ pointerEvents: 'none' }}>
              <div style={{ fontSize: '18px', fontWeight: 800, color: '#00f2fe' }}>
                {Math.round(currentTransfer.percent)}%
              </div>
              <div style={{ fontSize: '9px', color: '#94a3b8', marginTop: '2px' }}>
                {(currentTransfer.speed_bps / 1024 / 1024).toFixed(1)} MB/s
              </div>
            </div>
          ) : (
            <div style={{ pointerEvents: 'none' }}>
              <div style={{ fontSize: '26px', filter: 'drop-shadow(0 0 8px #00f2fe)' }}>🌀</div>
              <div style={{ fontSize: '11px', fontWeight: 800, color: '#00f2fe', letterSpacing: '0.5px', marginTop: '2px' }}>瞬移虫洞</div>
              <div style={{ fontSize: '9px', color: 'rgba(255,255,255,0.6)' }}>拖入即焚</div>
            </div>
          )}

          {/* 快捷选文件微型把手 (禁止窗口拖拽以便点击) */}
          <div
            style={{
              // @ts-ignore
              WebkitAppRegion: 'no-drag',
              display: 'flex',
              gap: '6px',
              marginTop: '4px',
            }}
          >
            <button
              onClick={(e) => {
                e.stopPropagation();
                SelectAndSendFile('127.0.0.1', 9988);
              }}
              style={{
                background: 'rgba(255,255,255,0.08)',
                border: 'none',
                borderRadius: '4px',
                color: '#00f2fe',
                fontSize: '9px',
                padding: '2px 5px',
                cursor: 'pointer',
              }}
              title="选择单个文件发送"
            >
              📄
            </button>
            <button
              onClick={(e) => {
                e.stopPropagation();
                SelectAndSendFolder('127.0.0.1', 9988);
              }}
              style={{
                background: 'rgba(255,255,255,0.08)',
                border: 'none',
                borderRadius: '4px',
                color: '#00f2fe',
                fontSize: '9px',
                padding: '2px 5px',
                cursor: 'pointer',
              }}
              title="选择文件夹自动压缩并发送"
            >
              📂
            </button>
          </div>
        </div>
      </div>
    );
  }

  // ==================== 3. 主控制台窗口 (Main Dashboard) ====================
  return (
    <div className="app-layout">
      {/* 侧边栏 */}
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

      {/* 主展示区 */}
      <main className="main-content">
        <header className="top-header">
          <div className="page-title">
            {activeTab === 'radar' && '局域网对等节点雷达'}
            {activeTab === 'transfers' && '端到端即焚传输通道'}
            {activeTab === 'settings' && '系统偏好设置'}
          </div>

          <div className="status-badges">
            {/* 多窗口操作工具栏 */}
            <button
              className="btn btn-secondary"
              style={{ padding: '3px 10px', fontSize: '11px', display: 'flex', alignItems: 'center', gap: '4px' }}
              onClick={() => ToggleNotch()}
              title="贴合 MacBook 物理刘海 (快捷键 Option+Space)"
            >
              <span>🍎 灵动刘海</span>
            </button>

            <button
              className="btn btn-secondary"
              style={{ padding: '3px 10px', fontSize: '11px', display: 'flex', alignItems: 'center', gap: '4px' }}
              onClick={() => OpenDropzoneWindow()}
              title="打开桌面角落极简吸附悬浮球"
            >
              <span>🔮 桌面虫洞</span>
            </button>

            <button
              className="btn btn-secondary"
              style={{ padding: '3px 10px', fontSize: '11px', display: 'flex', alignItems: 'center', gap: '4px' }}
              onClick={() => TilePeerWindows()}
              title="将所有打开的对端独立窗口在屏幕上网格排列"
            >
              <span>📐 窗口平铺</span>
            </button>

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

        <div className="scroll-view">
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
                          <div style={{ display: 'flex', gap: '6px', alignItems: 'center' }}>
                            <button
                              className="btn btn-secondary"
                              style={{ padding: '4px 8px', fontSize: '11px' }}
                              onClick={() => {
                                setSelectedPeer(p);
                                setActiveTab('transfers');
                              }}
                            >
                              ⚡ 直传
                            </button>
                            {/* 多窗口独立开窗按钮 */}
                            <button
                              className="btn btn-secondary"
                              style={{ padding: '4px 8px', fontSize: '11px' }}
                              onClick={() => OpenPeerWindow(p.instance, p.ip || '127.0.0.1', p.port)}
                              title="为该节点开辟独立的置顶悬浮直传窗口"
                            >
                              ⧉ 独立开窗
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

          {activeTab === 'transfers' && (
            <div style={{ maxWidth: '780px', margin: '0 auto', display: 'flex', flexDirection: 'column', gap: '20px' }}>
              {/* 大文件/文件夹分块实时传输进度面板 */}
              {currentTransfer && (
                <div className="card-panel" style={{ border: '1px solid rgba(0, 242, 254, 0.45)', background: 'rgba(0, 242, 254, 0.05)' }}>
                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                      <span style={{ fontSize: '16px' }}>⚡</span>
                      <div>
                        <div style={{ fontSize: '13px', fontWeight: 700, color: '#00f2fe' }}>
                          正在传输: {currentTransfer.filename}
                        </div>
                        <div style={{ fontSize: '11px', color: 'var(--text-muted)' }}>
                          {(currentTransfer.sent_bytes / 1024 / 1024).toFixed(2)} MB / {(currentTransfer.total_bytes / 1024 / 1024).toFixed(2)} MB
                        </div>
                      </div>
                    </div>
                    <div style={{ textAlign: 'right' }}>
                      <div style={{ fontSize: '15px', fontWeight: 800, color: '#fff' }}>
                        {Math.round(currentTransfer.percent)}%
                      </div>
                      <div style={{ fontSize: '10px', color: '#00f2fe' }}>
                        {(currentTransfer.speed_bps / 1024 / 1024).toFixed(1)} MB/s
                      </div>
                    </div>
                  </div>

                  <div style={{ width: '100%', height: '6px', background: 'rgba(255,255,255,0.08)', borderRadius: '3px', overflow: 'hidden', marginTop: '12px' }}>
                    <div style={{
                      height: '100%',
                      background: 'linear-gradient(90deg, #00f2fe, #4facfe)',
                      width: `${currentTransfer.percent}%`,
                      transition: 'width 0.15s ease-out'
                    }} />
                  </div>
                </div>
              )}

              <div className="card-panel">
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                  <span style={{ fontSize: '13px', fontWeight: 600 }}>即焚传输 (支持文本、截图、大文件与文件夹)</span>
                  <div style={{ fontSize: '12px', color: 'var(--text-secondary)' }}>
                    目标节点: <span style={{ color: 'var(--accent)', fontWeight: 600 }}>{selectedPeer?.instance || '未选择 (默认 127.0.0.1)'}</span>
                  </div>
                </div>

                {/* 剪贴板图片捕获横幅 */}
                {pastedImage && (
                  <div style={{
                    background: 'rgba(0, 242, 254, 0.08)',
                    border: '1px dashed rgba(0, 242, 254, 0.45)',
                    borderRadius: '8px',
                    padding: '10px 14px',
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                  }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
                      <img
                        src={pastedImage}
                        alt="待发图片"
                        style={{ height: '42px', borderRadius: '4px', border: '1px solid rgba(0, 242, 254, 0.5)' }}
                      />
                      <div>
                        <div style={{ fontSize: '12px', fontWeight: 600, color: '#00f2fe' }}>🖼️ 剪贴板图片已捕获就绪</div>
                        <div style={{ fontSize: '10px', color: 'var(--text-muted)' }}>可以直接瞬移发送给对端</div>
                      </div>
                    </div>
                    <div style={{ display: 'flex', gap: '8px' }}>
                      <button className="btn btn-primary" onClick={() => handleSendImage()}>
                        ⚡ 瞬移直传图片
                      </button>
                      <button className="btn btn-secondary" onClick={() => setPastedImage(null)}>
                        ✕ 清除
                      </button>
                    </div>
                  </div>
                )}

                <textarea
                  style={{
                    width: '100%',
                    height: '100px',
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
                  placeholder="在此输入文本，或直接按 Cmd+V 粘贴系统截图/图片，瞬间推送对端..."
                  value={sendText}
                  onChange={(e) => setSendText(e.target.value)}
                />

                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                  {/* 原生文件与文件夹选择按钮 */}
                  <div style={{ display: 'flex', gap: '8px' }}>
                    <button
                      className="btn btn-secondary"
                      style={{ padding: '6px 12px', fontSize: '11px', display: 'flex', alignItems: 'center', gap: '4px' }}
                      onClick={() => SelectAndSendFile(selectedPeer?.ip || '127.0.0.1', selectedPeer?.port || formConfig.port || 9988)}
                      title="打开系统原生文件选择器，支持任意格式大文件零拷贝分块直传"
                    >
                      <span>📄 选文件直传</span>
                    </button>
                    <button
                      className="btn btn-secondary"
                      style={{ padding: '6px 12px', fontSize: '11px', display: 'flex', alignItems: 'center', gap: '4px' }}
                      onClick={() => SelectAndSendFolder(selectedPeer?.ip || '127.0.0.1', selectedPeer?.port || formConfig.port || 9988)}
                      title="选择本地任意文件夹，后端自动递归压缩为 ZIP 并流式直传"
                    >
                      <span>📂 选文件夹(自动ZIP)</span>
                    </button>
                  </div>

                  <div style={{ display: 'flex', gap: '10px' }}>
                    <button className="btn btn-primary" onClick={() => handleSendClipboard()}>
                      ⚡ 瞬移发送文本
                    </button>
                  </div>
                </div>
              </div>

              <div className="card-panel">
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                  <span style={{ fontSize: '13px', fontWeight: 600 }}>已接收剪贴流 ({receivedMessages.length})</span>
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
                    {receivedMessages.map((msg, idx) => {
                      const isImg = msg.media_type === 'image';
                      return (
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

                          {isImg ? (
                            <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
                              <img
                                src={msg.content}
                                alt="收到图片"
                                style={{
                                  maxWidth: '320px',
                                  maxHeight: '220px',
                                  borderRadius: '6px',
                                  border: '1px solid rgba(0, 242, 254, 0.3)',
                                  objectFit: 'contain'
                                }}
                              />
                              <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
                                <button
                                  className="btn btn-primary"
                                  style={{ padding: '3px 12px', fontSize: '11px' }}
                                  onClick={() => handleCopyImage(msg.content)}
                                >
                                  📋 复制图片到剪贴板
                                </button>
                              </div>
                            </div>
                          ) : (
                            <>
                              <div style={{ fontSize: '13px', color: 'var(--text-primary)', wordBreak: 'break-all', whiteSpace: 'pre-wrap' }}>
                                {msg.content}
                              </div>
                              <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
                                <button
                                  className="btn btn-secondary"
                                  style={{ padding: '3px 10px', fontSize: '11px' }}
                                  onClick={() => handleCopy(msg.content)}
                                >
                                  📋 复制文本
                                </button>
                              </div>
                            </>
                          )}
                        </div>
                      );
                    })}
                  </div>
                )}
              </div>
            </div>
          )}

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

      {toast && (
        <div className="toast-bar">
          <span>{toast}</span>
        </div>
      )}
    </div>
  );
}

export default App;
