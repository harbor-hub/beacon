package app

import (
	"strings"

	"github.com/spf13/cobra"
)

func NewCmd(name, version string) *cobra.Command {
	beacon := New()

	rootCmd := NewRootCmd(name, version, func(c *cobra.Command, args []string) error {
		dnsListen, _ := c.Flags().GetStringArray("dns-listen")
		dnsDomains, _ := c.Flags().GetString("dns-domain")
		networks, _ := c.Flags().GetString("network")
		recordLabels, _ := c.Flags().GetString("record-labels")
		apiAddr, _ := c.Flags().GetString("api-listen")
		apiToken, _ := c.Flags().GetString("api-token")
		verbose, _ := c.Flags().GetBool("verbose")

		dnsListeners, err := parseDNSListeners(dnsListen)
		if err != nil {
			return err
		}

		beacon.Version = version
		beacon.DNSListeners = dnsListeners
		beacon.DNSDomains = splitTrimmed(dnsDomains)
		beacon.Networks = splitTrimmed(networks)
		beacon.RecordLabels = recordLabels
		beacon.APIAddr = apiAddr
		beacon.APIToken = apiToken
		beacon.Verbose = verbose

		return beacon.Run()
	})

	rootCmd.Flags().SortFlags = false
	rootCmd.Flags().StringArray("dns-listen", []string{LookupEnv("BEACON_DNS_LISTEN", ":5053").GetString()}, "DNS listen address, addr[?upstream=auto|IP[:port]] (no upstream by default; repeatable or comma-separated)")
	rootCmd.Flags().String("dns-domain", LookupEnv("BEACON_DNS_DOMAIN", "beacon").GetString(), "Comma-separated local domains")
	rootCmd.Flags().String("network", LookupEnv("BEACON_NETWORK", "bridge").GetString(), "Comma-separated Docker networks, in order of preference, whose container IP is used (fallback: first network by name)")
	rootCmd.Flags().String("record-labels", LookupEnv("BEACON_RECORD_LABELS", "").GetString(), "Include container labels with this prefix in records (empty = none)")
	rootCmd.Flags().String("api-listen", LookupEnv("BEACON_API_LISTEN", "").GetString(), "API listen address, HTTP (empty = disabled)")
	rootCmd.Flags().String("api-token", LookupEnv("BEACON_API_TOKEN", "").GetString(), "Bearer token required for /api/* (empty = no auth)")
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
