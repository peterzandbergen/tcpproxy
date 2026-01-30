package main

import (
	"errors"
	"flag"
	"fmt"
	"strings"
)

// ProxyMapping represents a single "port=remote:port" rule
type ProxyMapping struct {
	LocalPort  string
	RemoteAddr string
}

// Config holds the final application options
type Config struct {
	LogLevel  string
	LogFormat string
	Proxies   []ProxyMapping
}

// ProxyList implements flag.Value to handle multiple --proxy flags
type ProxyList []ProxyMapping

func (p *ProxyList) String() string {
	var result []string
	for _, pm := range *p {
		result = append(result, fmt.Sprintf("%s=%s", pm.LocalPort, pm.RemoteAddr))
	}
	return strings.Join(result, ",")
}

// Set parses a single flag value: "port=remote-host:remote-port"
func (p *ProxyList) Set(value string) error {
	parts := strings.SplitN(value, "=", 2)
	if len(parts) != 2 {
		return errors.New("invalid format, expected port=remote-host:remote-port")
	}

	mapping := ProxyMapping{
		LocalPort:  strings.TrimSpace(parts[0]),
		RemoteAddr: strings.TrimSpace(parts[1]),
	}
	*p = append(*p, mapping)
	return nil
}

// LoadConfig reads configuration from Environment Variables and Command Line Flags.
// args: should be os.Args (including the program name at index 0)
// getenv: dependency injection for reading environment variables (usually os.Getenv)
func LoadConfig(args []string, getenv func(string) string) *Config {
	// 1. Initialize with Defaults
	cfg := &Config{
		LogLevel:  "info",
		LogFormat: "text",
		Proxies:   []ProxyMapping{},
	}

	// 2. Load Environment Variables
	envLogLevel := getenv("TCPPROXY_LOGLEVEL")
	if envLogLevel != "" {
		cfg.LogLevel = envLogLevel
	}

	envLogFormat := getenv("TCPPROXY_LOGFORMAT")
	if envLogFormat != "" {
		cfg.LogFormat = envLogFormat
	}

	var envProxies ProxyList
	if envProxyStr := getenv("TCPPROXY_PROXY"); envProxyStr != "" {
		for entry := range strings.SplitSeq(envProxyStr, ",") {
			if err := envProxies.Set(strings.TrimSpace(entry)); err != nil {
				fmt.Printf("Warning: Ignoring invalid env proxy '%s': %v\n", entry, err)
			}
		}
	}

	// 3. Setup and Parse Flags using a FlagSet
	// We use the first argument (program name) as the FlagSet name
	fsName := "tcpproxy"
	if len(args) > 0 {
		fsName = args[0]
	}

	fs := flag.NewFlagSet(fsName, flag.ExitOnError)

	var flagProxies ProxyList

	fs.StringVar(&cfg.LogLevel, "loglevel", cfg.LogLevel, "Log level [error, warn, info, debug] TCPPROXY_LOGLEVEL")
	fs.StringVar(&cfg.LogFormat, "logformat", cfg.LogFormat, "Log format [json, text] TCPPROXY_LOGFORMAT")
	fs.Var(&flagProxies, "proxy", "Proxy spec: port=remote-host:remote-port (can be repeated) TCPPROXY_PROXY")

	// Parse args. We assume args comes from os.Args, so args[0] is the program path.
	// We must parse starting from args[1:].
	// If args is empty or only has the program name, we parse an empty slice.
	parseArgs := []string{}
	if len(args) > 1 {
		parseArgs = args[1:]
	}
	fs.Parse(parseArgs)

	// 4. Reconciliation (Flag Priority > Env Priority)
	if len(flagProxies) > 0 {
		cfg.Proxies = flagProxies
	} else if len(envProxies) > 0 {
		cfg.Proxies = envProxies
	}

	return cfg
}
