package main

import (
    "embed"
    "log"

    "github.com/wailsapp/wails/v3/pkg/application"
)

// Wails uses Go's `embed` package to embed the frontend files into the binary.
// Any files in the frontend/dist folder will be embedded into the binary and
// made available to the frontend.
// See https://pkg.go.dev/embed for more information.

//go:embed all:frontend/dist
var assets embed.FS

func init() {
    application.RegisterEvent[string]("time")
    application.RegisterEvent[PeerInfo]("p2p:peer:found")
    application.RegisterEvent[ServiceStatus]("p2p:status:change")
    application.RegisterEvent[ClipboardMessage]("p2p:clipboard:received")
    application.RegisterEvent[PendingFileRequest]("p2p:transfer:request")
    application.RegisterEvent[TransferProgress]("p2p:transfer:progress")
    application.RegisterEvent[TransferProgress]("p2p:transfer:completed")
}

// main function serves as the application's entry point. It initializes the application, creates a window,
// and starts a goroutine that emits a time-based event every second. It subsequently runs the application and
// logs any error that might occur.
func main() {
    p2pService := &P2PService{}

    app := application.New(application.Options{
        Name:        "GhostTunnel",
        Description: "P2P Zero-Relay Instant Tunnel",
        Services: []application.Service{
            application.NewService(p2pService),
        },
        Assets: application.AssetOptions{
            Handler: application.AssetFileServerFS(assets),
        },
        Mac: application.MacOptions{
            ApplicationShouldTerminateAfterLastWindowClosed: true,
        },
    })

    // 1. 创建主控制台窗口 (Main Dashboard Window)
    mainWindow := app.Window.NewWithOptions(application.WebviewWindowOptions{
        Name:           "main-dashboard-window",
        Title:          "GhostTunnel 控制中心",
        Width:          1000,
        Height:         618,
        EnableFileDrop: true, // 开启原生拖拽
        Mac: application.MacWindow{
            InvisibleTitleBarHeight: 50,
            Backdrop:                application.MacBackdropTranslucent,
            TitleBar:                application.MacTitleBarHiddenInset,
        },
        BackgroundColour: application.NewRGB(12, 14, 20),
        URL:              "/",
    })
    p2pService.setMainWindow(mainWindow)

    // 2. 创建屏幕右上角的高端灵动通知卡片窗口 (Notification Card Window)
    notificationCard := app.Window.NewWithOptions(application.WebviewWindowOptions{
        Name:             "notification-card-window",
        Title:            "GhostTunnel 传输请求",
        Width:            390,
        Height:           210,
        AlwaysOnTop:      true,
        Frameless:        true,
        DisableResize:    true,
        HideOnEscape:     true,
        BackgroundType:   application.BackgroundTypeTransparent,
        BackgroundColour: application.NewRGBA(0, 0, 0, 0),
        Mac: application.MacWindow{
            Backdrop: application.MacBackdropTransparent,
        },
        URL: "/?view=notification",
    })
    p2pService.setNotificationCard(notificationCard)

    // 3. 注册全局快捷键 Option+Space 随时唤出/收起主控制中心
    _ = app.GlobalShortcut.Register("Option+Space", func() {
        _ = p2pService.ShowMainWindow()
    })

    err := app.Run()
    if err != nil {
        log.Fatal(err)
    }
}
