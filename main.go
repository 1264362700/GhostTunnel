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
    app.Window.NewWithOptions(application.WebviewWindowOptions{
        Title:  "GhostTunnel 控制中心",
        Width:  1000,
        Height: 618,
        Mac: application.MacWindow{
            InvisibleTitleBarHeight: 50,
            Backdrop:                application.MacBackdropTranslucent,
            TitleBar:                application.MacTitleBarHiddenInset,
        },
        BackgroundColour: application.NewRGB(12, 14, 20),
        URL:              "/",
    })

    // 2. 创建贴合 MacBook 顶部刘海的灵动胶囊窗口 (Notch Window)
    notchWindow := app.Window.NewNotchWindow(application.NotchWindowOptions{
        Width:    660,
        Height:   96,
        Animated: true,
        WindowOptions: application.WebviewWindowOptions{
            Name:         "notch-capsule-window",
            URL:          "/?view=notch",
            HideOnEscape: true,
            AlwaysOnTop:  true,
        },
    })
    p2pService.SetNotchWindow(notchWindow)

    // 3. 注册全局快捷键 Option+Space 随时唤出/收起刘海窗口
    _ = app.GlobalShortcut.Register("Option+Space", func() {
        _ = p2pService.ToggleNotch()
    })

    err := app.Run()
    if err != nil {
        log.Fatal(err)
    }
}
