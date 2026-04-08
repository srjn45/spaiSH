package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"spaios/internal/config"
	"spaios/internal/fusefs"
)

func configPath() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "spaios", "spaid.toml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "spaios", "spaid.toml")
}

func sockPath() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "spaios", "spaid.sock")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "spaios", "spaid.sock")
}

func main() {
	mountpointFlag := flag.String("mountpoint", "", "FUSE mount point (default: from config or /ai)")
	flag.Parse()

	cfg, err := config.Load(configPath())
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	// Resolve mountpoint: flag > config > /ai
	mountpoint := "/ai"
	if cfg.Fuse.Mountpoint != "" {
		mountpoint = cfg.Fuse.Mountpoint
	}
	if *mountpointFlag != "" {
		mountpoint = *mountpointFlag
	}

	// Resolve default timeout: config > 60s
	defaultTimeout := time.Duration(cfg.Fuse.TimeoutSeconds) * time.Second
	if cfg.Fuse.TimeoutSeconds == 0 {
		defaultTimeout = 60 * time.Second
	}

	h := &fusefs.Handler{
		SockPath:       sockPath(),
		DefaultTimeout: defaultTimeout,
	}

	srv, err := fusefs.Mount(mountpoint, h)
	if err != nil {
		log.Fatalf("mount error: %v", err)
	}

	fmt.Printf("spaiOS FUSE mounted at %s\n", mountpoint)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigCh
		srv.Unmount()
	}()

	srv.Wait()
}
