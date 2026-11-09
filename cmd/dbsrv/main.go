// Command dbsrv is the Go + PostgreSQL replacement for the C++ DBSRV.
//
//	dbsrv -dsn postgres://dbsrv:dbsrv@localhost:5432/dbsrv \
//	      -config tmsrv.yaml
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
	"syscall"
	"time"
	_ "time/tzdata"

	"dbsrv/internal/cluster"
	"dbsrv/internal/game"
	"dbsrv/internal/logfile"
	"dbsrv/internal/server"
	"dbsrv/internal/store"
)

func main() {
	listen := flag.String("listen", ":7514", "address TMSRV connects to")
	dsn := flag.String("dsn", os.Getenv("DATABASE_URL"), "Postgres connection string (or DATABASE_URL)")
	config := flag.String("config", "tmsrv.yaml", "TMSRV settings: their channels section lists the channel IPs (formerly serverlist.txt)")
	tz := flag.String("tz", "America/Sao_Paulo", "game time zone")
	statusFile := flag.String("status-file", "", "write online counts here every 30s (C++: C:\\xampp\\htdocs\\servNN.htm)")
	debug := flag.Bool("debug", false, "debug logging")
	logDir := flag.String("log-dir", envOr("LOG_DIR", "logs"), `folder of the daily log files (or LOG_DIR; "" = console only)`)
	logDays := flag.Int("log-days", 90, "days of log files to keep (0 = all)")
	flag.Parse()

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	log, closeLog, err := logfile.Logger(*logDir, "dbsrv", *logDays, level)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := run(log, *listen, *dsn, *config, *tz, *statusFile); err != nil {
		log.Error("fatal", "err", err)
		closeLog()
		os.Exit(1)
	}
	closeLog()
}

func envOr(k, def string) string {
	if v, ok := os.LookupEnv(k); ok {
		return v
	}
	return def
}

func run(log *slog.Logger, listen, dsn, configPath, tz, statusFile string) error {
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
	classes, err := st.ClassTemplates(ctx)
	if err != nil {
		return fmt.Errorf("class templates: %w", err)
	}
	base, err := game.ClassTemplates(classes)
	if err != nil {
		return err
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
