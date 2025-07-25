package cmd

import (
	"fmt"
	"log"

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
	
	router := webui.SetupRouter(backendAddr)
	
	fmt.Printf("Visit http://localhost%s to view the WebUI\n", listenAddr)
	
	if err := router.Run(listenAddr); err != nil {
		log.Fatal("Failed to start WebUI server:", err)
	}
}