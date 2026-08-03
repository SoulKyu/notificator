//go:build webui
// +build webui

package cmd

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"notificator/internal/webui"
)

// webuiCmd represents the webui command
var webuiCmd = &cobra.Command{
	Use:   "webui",
	Short: "Start the Notificator WebUI server",
	Long: `Start the Notificator WebUI server which provides:
- Web-based dashboard for alert management
- User authentication and session management
- Real-time alert updates
- Alert acknowledgment and commenting interface`,
	Run: runWebUI,
}

func init() {
	rootCmd.AddCommand(webuiCmd)

	// WebUI-specific flags
	webuiCmd.Flags().String("listen", ":8081", "WebUI server listen address")
	webuiCmd.Flags().String("backend", "localhost:50051", "Backend gRPC server address")

	// Bind flags to viper
	viper.BindPFlag("webui.listen", webuiCmd.Flags().Lookup("listen"))
	viper.BindPFlag("webui.backend", webuiCmd.Flags().Lookup("backend"))
}

func runWebUI(cmd *cobra.Command, args []string) {
	// Get configuration from Viper
	listenAddr := viper.GetString("webui.listen")
	backendAddr := viper.GetString("webui.backend")

	// Override with environment variable if set
	if envBackend := viper.GetString("backend_address"); envBackend != "" {
		backendAddr = envBackend
	}

	fmt.Println("🌐 Starting Notificator WebUI Server...")
	fmt.Printf("   Config file: %s\n", viper.ConfigFileUsed())
	fmt.Printf("   Listen: %s\n", listenAddr)
	fmt.Printf("   Backend: %s\n", backendAddr)

	router, alertCache := webui.SetupRouter(backendAddr)

	fmt.Printf("Visit http://localhost%s to view the WebUI\n", listenAddr)

	srv := &http.Server{
		Addr:    listenAddr,
		Handler: router,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("Failed to start WebUI server:", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	fmt.Println("🛑 Shutting down WebUI server...")

	alertCache.Stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("WebUI server shutdown error: %v", err)
	}

	fmt.Println("✅ WebUI server shut down gracefully")
}
