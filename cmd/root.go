package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile string
	rootCmd = &cobra.Command{
		Use:   "notificator",
		Short: "Notificator - Alert management and notification system",
		Long: `Notificator is a comprehensive alert management and notification system
that integrates with Alertmanager to provide real-time monitoring,
notifications, and alert management capabilities.`,
	}
)

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	// Determine the default command based on the binary name or lack of subcommand
	binaryName := filepath.Base(os.Args[0])
	
	// If no subcommand is provided, determine what to run
	if len(os.Args) == 1 || (len(os.Args) > 1 && strings.HasPrefix(os.Args[1], "-")) {
		switch binaryName {
		case "backend":
			os.Args = append([]string{os.Args[0], "backend"}, os.Args[1:]...)
		case "webui":
			os.Args = append([]string{os.Args[0], "webui"}, os.Args[1:]...)
		default:
			// Default to desktop mode for main notificator binary
			os.Args = append([]string{os.Args[0], "desktop"}, os.Args[1:]...)
		}
	}
	
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.config/notificator/config.json)")
	rootCmd.PersistentFlags().String("log-level", "info", "log level (debug, info, warn, error)")
	
	// Bind flags to viper
	viper.BindPFlag("log-level", rootCmd.PersistentFlags().Lookup("log-level"))
}

// initConfig reads in config file and ENV variables.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)

		// Search config in home directory with name ".notificator" (without extension).
		viper.AddConfigPath(home + "/.config/notificator")
		viper.AddConfigPath("./config")
		viper.AddConfigPath(".")
		viper.SetConfigType("json")
		viper.SetConfigName("config")
	}

	// Set environment variable prefix
	viper.SetEnvPrefix("NOTIFICATOR")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		fmt.Println("Using config file:", viper.ConfigFileUsed())
	}
}