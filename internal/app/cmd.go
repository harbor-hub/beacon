package app

import (
	"strings"

	"github.com/spf13/cobra"
)

func NewCmd(name, version string) *cobra.Command {
	beacon := New()

	rootCmd := NewRootCmd(name, version, func(c *cobra.Command, args []string) error {
		addr, _ := c.Flags().GetString("addr")
		domainsStr, _ := c.Flags().GetString("domain")
		upstreamStr, _ := c.Flags().GetString("upstream")
		verbose, _ := c.Flags().GetBool("verbose")

		beacon.Version = version
		beacon.Addr = addr
		beacon.Domains = splitTrimmed(domainsStr)
		beacon.Upstream = ResolveUpstream(splitTrimmed(upstreamStr))
		beacon.Verbose = verbose

		return beacon.Run()
	})

	rootCmd.Flags().SortFlags = false
	rootCmd.Flags().String("addr", LookupEnv("BEACON_ADDR", "0.0.0.0:5053").GetString(), "DNS listen address")
	rootCmd.Flags().String("domain", LookupEnv("BEACON_DOMAIN", "beacon").GetString(), "Comma-separated local domains")
	rootCmd.Flags().String("upstream", LookupEnv("BEACON_UPSTREAM", "").GetString(), "Comma-separated upstream DNS servers")
	rootCmd.Flags().BoolP("verbose", "v", LookupEnv("BEACON_VERBOSE", false).GetBool(), "Enable verbose logging")

	ApplyDefaultFlags(rootCmd)

	return rootCmd
}

func splitTrimmed(s string) []string {
	var result []string

	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}

	return result
}
