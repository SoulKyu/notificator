//go:build nogui

package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// desktopCmd represents the desktop command (stub for nogui builds)
var desktopCmd = &cobra.Command{
	Use:   "desktop",
	Short: "Desktop GUI is not available in this build",
	Long: `This build was compiled without GUI support.
The desktop command is not available in container/server builds.
	
To use the desktop GUI, please use a local build without the nogui tag.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("❌ Desktop GUI is not available in this build")
		fmt.Println("This is a server/container build without GUI dependencies.")
		fmt.Println("")
		fmt.Println("Available commands:")
		fmt.Println("  backend - Start the backend server")
		fmt.Println("  webui   - Start the web interface")
		fmt.Println("")
		fmt.Println("For desktop GUI, use a local build without the nogui tag.")
		os.Exit(1)
	},
}

func init() {
	rootCmd.AddCommand(desktopCmd)
}
