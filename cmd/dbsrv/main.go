// Command dbsrv is the Go + PostgreSQL replacement for the C++ DBSRV.
//
//	dbsrv -dsn postgres://dbsrv:dbsrv@localhost:5432/dbsrv \
//	      -config tmsrv.yaml -server 1
//
// tmsrv.yaml lists every server; -server (or WYD_SERVER, the server's group)
// says which one this DBSRV is: it listens on that server's dbsrv address and
// only accepts its channels. With a single server in the file, -server may be
// left out. Every server uses the same database (-dsn): the accounts are
// shared and each server has its world in its own schema (s<group>).
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
	"strconv"
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
	listen := flag.String("listen", "", "address TMSRV connects to (default: the dbsrv ip:port of this server in the settings)")
	dsn := flag.String("dsn", os.Getenv("DATABASE_URL"), "Postgres connection string (or DATABASE_URL)")
	config := flag.String("config", "tmsrv.yaml", "TMSRV settings: their servers section lists the channel IPs (formerly serverlist.txt)")
	serverGroup := flag.Int("server", envInt("WYD_SERVER"), "server (group) of this DBSRV in the settings (or WYD_SERVER; 0 = the only one)")
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
	name := "dbsrv"
	if *serverGroup > 0 {
		// Servers may share the log folder.
		name = fmt.Sprintf("srv%d-dbsrv", *serverGroup)
	}
	log, closeLog, err := logfile.Logger(*logDir, name, *logDays, level)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := run(log, *listen, *dsn, *config, *tz, *statusFile, *serverGroup); err != nil {
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

func envInt(k string) int {
	n, _ := strconv.Atoi(os.Getenv(k))
	return n
}

func run(log *slog.Logger, listen, dsn, configPath, tz, statusFile string, group int) error {
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
	this, err := layout.Server(group)
	if err != nil {
		return err
	}
	channels := this.ChannelIPs()
	if listen == "" {
		listen = this.DBSRV.Addr()
	}
	log.Info("settings", "path", configPath, "server", this.Group, "listen", listen)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Every server uses the same database; this DBSRV works on the world of
	// its own (schema s<group>) and on the shared accounts.
	st, err := store.OpenServer(ctx, dsn, loc, this.Group)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	unlock, err := st.LockServer(ctx, func(err error) { log.Error("server lock lost", "server", this.Group, "err", err) })
	if err != nil {
		return err
	}
	defer unlock()
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
		Group:              this.Group,
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
