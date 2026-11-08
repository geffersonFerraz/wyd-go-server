// Command dbsrv is the Go + PostgreSQL replacement for the C++ DBSRV.
//
//	dbsrv -dsn postgres://dbsrv:dbsrv@localhost:5432/dbsrv \
//	      -config tmsrv.yaml \
//	      -npc data/dbsrv/npc
//
// Environment: DATABASE_URL (instead of -dsn), DBSRV_MASTER_PASSWORD_HASH
// (optional argon2id hash enabling the staff master password).
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
	_ "time/tzdata"

	"dbsrv/internal/cluster"
	"dbsrv/internal/server"
	"dbsrv/internal/store"
	"dbsrv/internal/wire"
)

func main() {
	listen := flag.String("listen", ":7514", "address TMSRV connects to")
	dsn := flag.String("dsn", os.Getenv("DATABASE_URL"), "Postgres connection string (or DATABASE_URL)")
	config := flag.String("config", "tmsrv.yaml", "TMSRV settings: their channels section lists the channel IPs (formerly serverlist.txt)")
	npcDir := flag.String("npc", "data/dbsrv/npc", "folder with the TK, FM, BM, HT class templates")
	tz := flag.String("tz", "America/Sao_Paulo", "game time zone")
	statusFile := flag.String("status-file", "", "write online counts here every 30s (C++: C:\\xampp\\htdocs\\servNN.htm)")
	debug := flag.Bool("debug", false, "debug logging")
	flag.Parse()

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	if err := run(log, *listen, *dsn, *config, *npcDir, *tz, *statusFile); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger, listen, dsn, configPath, npcDir, tz, statusFile string) error {
	if dsn == "" {
		return fmt.Errorf("no -dsn or DATABASE_URL")
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return err
	}
	layout, err := cluster.Load(configPath)
	if err != nil {
		return err
	}
	channels := layout.ChannelIPs()
	base, err := LoadBaseMobs(npcDir)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, dsn, loc)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	sapphire, err := st.Sapphire(ctx)
	if err != nil {
		return err
	}

	cfg := server.Config{
		Listen:             listen,
		ChannelIPs:         channels,
		MasterPasswordHash: os.Getenv("DBSRV_MASTER_PASSWORD_HASH"),
		Loc:                loc,
		StatusFile:         statusFile,
	}
	srv := server.New(cfg, st, base, sapphire, log)
	if err := srv.LoadWorld(ctx); err != nil {
		return err
	}
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}
	err = srv.Serve(ctx, ln)
	log.Info("stopped")
	return err
}

// LoadBaseMobs reads the four class templates (C++ ReadCharBase).
func LoadBaseMobs(dir string) ([4]wire.Mob, error) {
	var base [4]wire.Mob
	for i, name := range []string{"TK", "FM", "BM", "HT"} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return base, fmt.Errorf("class template: %w", err)
		}
		if len(raw) != 848 {
			return base, fmt.Errorf("class template %s: %d bytes, want 848 (sizeof STRUCT_MOB)", name, len(raw))
		}
		if err := wire.Unmarshal(raw, &base[i]); err != nil {
			return base, err
		}
	}
	return base, nil
}
