package main

import (
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/jedisct1/dlog"
)

type Config struct {
	ServerNames      []string `toml:"server_names"`
	ListenAddresses  []string `toml:"listen_addresses"`
	Daemonize        bool
	ForceTCP         bool `toml:"force_tcp"`
	Timeout          int  `toml:"timeout_ms"`
	CertRefreshDelay int  `toml:"cert_refresh_delay"`
	BlockIPv6        bool `toml:"block_ipv6"`
	Cache            bool
	CacheSize        int                     `toml:"cache_size"`
	CacheNegTTL      uint32                  `toml:"cache_neg_ttl"`
	CacheMinTTL      uint32                  `toml:"cache_min_ttl"`
	CacheMaxTTL      uint32                  `toml:"cache_max_ttl"`
	ServersConfig    map[string]ServerConfig `toml:"servers"`
	SourcesConfig    map[string]SourceConfig `toml:"sources"`
}

func newConfig() Config {
	return Config{
		ListenAddresses:  []string{"127.0.0.1:53"},
		Timeout:          2500,
		CertRefreshDelay: 30,
		Cache:            true,
		CacheSize:        256,
		CacheNegTTL:      60,
		CacheMinTTL:      60,
		CacheMaxTTL:      8600,
	}
}

type ServerConfig struct {
	Stamp        string
	ProviderName string `toml:"provider_name"`
	Address      string
	PublicKey    string `toml:"public_key"`
	NoLog        bool   `toml:"no_log"`
	DNSSEC       bool   `toml:"dnssec"`
}

type SourceConfig struct {
	URL            string
	MinisignKeyStr string `toml:"minisign_key"`
	CacheFile      string `toml:"cache_file"`
	FormatStr      string `toml:"format"`
	RefreshDelay   int    `toml:"refresh_delay"`
}

func ConfigLoad(proxy *Proxy, config_file string) error {
	configFile := flag.String("config", "dnscrypt-proxy.toml", "path to the configuration file")
	flag.Parse()
	config := newConfig()
	if _, err := toml.DecodeFile(*configFile, &config); err != nil {
		return err
	}
	proxy.timeout = time.Duration(config.Timeout) * time.Millisecond
	proxy.mainProto = "udp"
	if config.ForceTCP {
		proxy.mainProto = "tcp"
	}
	proxy.certRefreshDelay = time.Duration(config.CertRefreshDelay) * time.Minute
	if len(config.ListenAddresses) == 0 {
		return errors.New("No local IP/port configured")
	}
	proxy.listenAddresses = config.ListenAddresses
	proxy.daemonize = config.Daemonize
	proxy.pluginBlockIPv6 = config.BlockIPv6
	proxy.cache = config.Cache
	proxy.cacheSize = config.CacheSize
	proxy.cacheNegTTL = config.CacheNegTTL
	proxy.cacheMinTTL = config.CacheMinTTL
	proxy.cacheMaxTTL = config.CacheMaxTTL
	if len(config.ServerNames) == 0 {
		for serverName := range config.ServersConfig {
			config.ServerNames = append(config.ServerNames, serverName)
		}
	}
	for sourceName, source := range config.SourcesConfig {
		if source.URL == "" {
			return fmt.Errorf("Missing URL for source [%s]", sourceName)
		}
		if source.MinisignKeyStr == "" {
			return fmt.Errorf("Missing Minisign key for source [%s]", sourceName)
		}
		if source.CacheFile == "" {
			return fmt.Errorf("Missing cache file for source [%s]", sourceName)
		}
		if source.FormatStr == "" {
			return fmt.Errorf("Missing format for source [%s]", sourceName)
		}
		if source.RefreshDelay <= 0 {
			source.RefreshDelay = 24
		}
		source, err := NewSource(source.URL, source.MinisignKeyStr, source.CacheFile, source.FormatStr, time.Duration(source.RefreshDelay)*time.Hour)
		if err != nil {
			dlog.Criticalf("Unable use source [%s]: [%s]", sourceName, err)
			continue
		}
		registeredServers, err := source.Parse()
		if err != nil {
			dlog.Criticalf("Unable use source [%s]: [%s]", sourceName, err)
			continue
		}
		for _, registeredServer := range registeredServers {
			if !includesName(config.ServerNames, registeredServer.name) {
				continue
			}
			dlog.Infof("Adding [%s] to the set of wanted resolvers", registeredServer.name)
			proxy.registeredServers = append(proxy.registeredServers, registeredServer)
		}
	}
	for _, serverName := range config.ServerNames {
		serverConfig, ok := config.ServersConfig[serverName]
		if !ok {
			continue
		}
		var stamp ServerStamp
		var err error
		if len(serverConfig.Stamp) > 0 {
			dlog.Fatal("Stamps are not implemented yet")
		} else {
			stamp, err = NewServerStampFromLegacy(serverConfig.Address, serverConfig.PublicKey, serverConfig.ProviderName)
			if err != nil {
				return err
			}
		}
		proxy.registeredServers = append(proxy.registeredServers,
			RegisteredServer{name: serverName, stamp: stamp})
	}
	if len(proxy.registeredServers) == 0 {
		return errors.New("No servers configured")
	}
	return nil
}

func includesName(names []string, name string) bool {
	for _, found := range names {
		if strings.EqualFold(found, name) {
			return true
		}
	}
	return false
}
