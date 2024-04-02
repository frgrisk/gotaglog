package cmd

import (
	"fmt"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

const defaultUnreleasedTag = "unreleased"

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "gotaglog",
	Short: "Generate a changelog from git tags",
	Run: func(_ *cobra.Command, _ []string) {
		getChangeLog()
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	cobra.CheckErr(rootCmd.Execute())
}

func init() {
	cobra.OnInitialize(initConfig)
	log.SetFormatter(&log.TextFormatter{DisableTimestamp: true, DisableLevelTruncation: true})

	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.gotaglog.yaml)")
	rootCmd.PersistentFlags().StringP("repo", "r", cwd, "path to git repository")
	err = rootCmd.MarkPersistentFlagDirname("repo")
	if err != nil {
		panic(err)
	}

	rootCmd.Flags().Bool("unreleased", false, "show only unreleased changes")
	rootCmd.Flags().Bool("inc-major", false, "generate tag for unreleased changes by incrementing the major version")
	rootCmd.Flags().Bool("inc-minor", false, "generate tag for unreleased changes by incrementing the minor version")
	rootCmd.Flags().Bool("inc-patch", false, "generate tag for unreleased changes by incrementing the patch version")
	rootCmd.Flags().StringP("tag", "t", defaultUnreleasedTag, "tag for unreleased changes")
	rootCmd.Flags().StringP("output", "o", "", "output file")
	err = rootCmd.MarkFlagFilename("output", "md")
	if err != nil {
		panic(err)
	}
	err = viper.BindPFlags(rootCmd.Flags())
	if err != nil {
		panic(err)
	}
	err = viper.BindPFlags(rootCmd.PersistentFlags())
	if err != nil {
		panic(err)
	}
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)

		// Search config in home directory with name ".gotaglog" (without extension).
		viper.AddConfigPath(home)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".gotaglog")
	}

	viper.SetEnvPrefix("GOTAGLOG")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
}
