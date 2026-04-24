package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/example/magictun/config"
	"github.com/example/magictun/node"
)

func main() {
	// Config file (optional — if not set, all params come from CLI flags)
	configPath := flag.String("config", "", "path to JSON config file (optional)")

	// Node identity
	name := flag.String("name", "", "node name (default: auto-generated)")
	identityFile := flag.String("identity", "", "identity key file (default: /tmp/magictun-<name>.key)")
	listenAddr := flag.String("listen", ":9443", "peer listen address")
	socksAddr := flag.String("socks", ":1080", "SOCKS5 proxy listen address")

	// Bootstrap
	bootstrap := flag.String("peers", "", "bootstrap peers, comma-separated (host:port)")

	// Direct networks
	networks := flag.String("net", "", "directly connected networks, comma-separated CIDRs")

	flag.Parse()

	var cfg *config.Config

	if *configPath != "" {
		// Load from file
		var err error
		cfg, err = config.Load(*configPath)
		if err != nil {
			log.Fatalf("Failed to load config: %v", err)
		}
	} else {
		// Build config from CLI flags
		cfg = config.DefaultConfig()

		if *name != "" {
			cfg.Node.Name = *name
		} else {
			cfg.Node.Name = "magictun"
		}
		if *identityFile != "" {
			cfg.Node.IdentityFile = *identityFile
		} else {
			cfg.Node.IdentityFile = fmt.Sprintf("/tmp/magictun-%s.key", cfg.Node.Name)
		}
		cfg.Node.ListenAddr = *listenAddr
		cfg.Node.Socks5Addr = *socksAddr

		if *bootstrap != "" {
			for _, bp := range strings.Split(*bootstrap, ",") {
				bp = strings.TrimSpace(bp)
				if bp != "" {
					cfg.Gossip.BootstrapPeers = append(cfg.Gossip.BootstrapPeers, bp)
				}
			}
		}

		if *networks != "" {
			for _, nw := range strings.Split(*networks, ",") {
				nw = strings.TrimSpace(nw)
				if nw != "" {
					cfg.Routing.DirectNetworks = append(cfg.Routing.DirectNetworks, nw)
				}
			}
		}

		if err := cfg.Validate(); err != nil {
			log.Fatalf("Invalid config: %v", err)
		}
	}

	n, err := node.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create node: %v", err)
	}

	if err := n.Start(); err != nil {
		log.Fatalf("Failed to start node: %v", err)
	}

	log.Printf("node %s started", cfg.Node.Name)
	log.Printf("  peer listen: %s", cfg.Node.ListenAddr)
	log.Printf("  socks5 proxy: %s", cfg.Node.Socks5Addr)
	if len(cfg.Routing.DirectNetworks) > 0 {
		log.Printf("  direct networks: %v", cfg.Routing.DirectNetworks)
	}
	if len(cfg.Gossip.BootstrapPeers) > 0 {
		log.Printf("  bootstrap peers: %v", cfg.Gossip.BootstrapPeers)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Printf("shutting down...")
	if err := n.Stop(); err != nil {
		log.Printf("error during shutdown: %v", err)
	}
}
