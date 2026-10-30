package main

import (
    "embed"

    "log"
    "time"

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
    app := application.New(application.Options{
        Name:        "desktop App",
        Description: "A demo of using raw HTML & CSS",
        Services: []application.Service{
            application.NewService(&P2PService{}),
        },
        Assets: application.AssetOptions{
            Handler: application.AssetFileServerFS(assets),
        },
        Mac: application.MacOptions{
            ApplicationShouldTerminateAfterLastWindowClosed: true,
        },
    })

    // Create a new window with the necessary options.
    // 'Title' is the title of the window.
    // 'Mac' options tailor the window when running on macOS.
    // 'BackgroundColour' is the background colour of the window.
    // 'URL' is the URL that will be loaded into the webview.
    app.Window.NewWithOptions(application.WebviewWindowOptions{
        Title: "Window 1",
        // Window sized to the golden ratio (1000 / 618 ≈ 1.618).
        Width:  1000,
        Height: 618,
        Mac: application.MacWindow{
            InvisibleTitleBarHeight: 50,
            Backdrop:                application.MacBackdropTranslucent,
            TitleBar:                application.MacTitleBarHiddenInset,
        },
        BackgroundColour: application.NewRGB(6, 7, 15),
        URL:              "/",
        Frameless:        true,
    })

    // Create a goroutine that emits an event containing the current time every second.
    // The frontend can listen to this event and update the UI accordingly.
    go func() {
        for {
            now := time.Now().Format(time.RFC1123)
            app.Event.Emit("time", now)
            time.Sleep(time.Second)
        }
    }()

    // Run the application. This blocks until the application has been exited.
    err := app.Run()

    // If an error occurred while running the application, log it and exit.
    if err != nil {
        log.Fatal(err)
    }
}
