// Command tmsrv is the Go port of the C++ TMSRV (game server).
//
//	tmsrv -config-dir config -dsn postgres://... -server 1 -channel 2
//
// tmsrv.yaml lists every server and its channels; -server (or WYD_SERVER, the
// server's group) and -channel (or TMSRV_CHANNEL) say which one this process
// is: it listens on that channel's ip:port and connects to the DBSRV of its
// server from that IP (DBSRV identifies the channel by it). With a single
// server or a single channel in the file, the flag may be left out. -dsn is
// the database every server shares; the guilds, ranking and city owners read
// from it are the ones of this TMSRV's server.
//
// Items, skills, drops, teleports, the maps, the NPC templates and the NPC
// generators come from Postgres (schema gamedata, filled by the admin panel;
// -dsn or DATABASE_URL). Its options and the small world tables come from
// -config (tmsrv.yaml), and the big tables (missions, messages) from
// -config-dir.
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
	"strconv"
	"syscall"
	"time"

	"dbsrv/internal/game"
	"dbsrv/internal/logfile"
	"dbsrv/internal/protocol"
	"dbsrv/internal/store"
)

func main() {
	dir := flag.String("config-dir", "config", "folder with the big YAML tables (language, missions)")
	listen := flag.String("listen", "", "override network.listen_ip/listen_port of the settings (host:port)")
	dsn := flag.String("dsn", os.Getenv("DATABASE_URL"), "Postgres with the gamedata tables (or DATABASE_URL)")
	config := flag.String("config", "tmsrv.yaml", "server settings (YAML, required: every option comes from it)")
	server := flag.Int("server", envInt("WYD_SERVER"), "server (group) of this TMSRV in the settings (or WYD_SERVER; 0 = the only one)")
	channel := flag.Int("channel", envInt("TMSRV_CHANNEL"), "channel of this TMSRV in the settings (or TMSRV_CHANNEL; 0 = the only one)")
	debug := flag.Bool("debug", false, "debug logging")
	logDir := flag.String("log-dir", envOr("LOG_DIR", "logs"), `folder of the daily log files (or LOG_DIR; "" = console only)`)
	logDays := flag.Int("log-days", 90, "days of log files to keep (0 = all)")
	flag.Parse()

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	name := "tmsrv"
	if *channel > 0 {
		name = fmt.Sprintf("tmsrv%d", *channel)
	}
	if *server > 0 {
		// Servers may share the log folder.
		name = fmt.Sprintf("srv%d-%s", *server, name)
	}
	log, closeLog, err := logfile.Logger(*logDir, name, *logDays, level)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := run(log, *dir, *listen, *config, *dsn, *server, *channel); err != nil {
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

func run(log *slog.Logger, dir, listen, configPath, dsn string, server, channel int) error {
	cfg, err := game.LoadConfig()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	settings, err := game.LoadSettings(configPath)
	if err != nil {
		return fmt.Errorf("settings: %w", err)
	}
	if err := cfg.ApplySettings(settings, server, channel); err != nil {
		return fmt.Errorf("settings: %w", err)
	}
	// The client link is always encrypted: without the key the channel
	// would accept logins anyone on the network could read.
	if cfg.TransportKey, err = protocol.ParseTransportKey(os.Getenv("TMSRV_TRANSPORT_KEY")); err != nil {
		return fmt.Errorf("TMSRV_TRANSPORT_KEY: %w (create it with: admin transport-key -env .env)", err)
	}
	log.Info("settings", "path", configPath, "listen", net.JoinHostPort(cfg.ListenIP, fmt.Sprint(cfg.ListenPort)),
		"dbsrv", net.JoinHostPort(cfg.DBIP, fmt.Sprint(cfg.DBPort)), "server", cfg.Cluster.Group, "channel", cfg.Channel,
		"newbie_zone", cfg.NewbieZone, "mob_ai", fmt.Sprintf("%+v", cfg.MobAI))
	st, gd, world, err := loadGameData(dsn, cfg.Cluster.Group)
	if err != nil {
		return fmt.Errorf("game data: %w", err)
	}
	defer st.Close()
	cfg.Locks = st
	data, err := game.LoadData(dir, settings, world)
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	data.ApplyCityOwners(cfg.Channel, gd.CityOwners)
	if len(gd.Generators) == 0 {
		// The panel fills the table on its first start after the update; a
		// channel that starts before it uses the old file meanwhile.
		path := filepath.Join(dir, game.GeneratorsFile)
		if gens, err := game.ReadGeneratorsFile(path); err == nil && len(gens) > 0 {
			gd.Generators = gens
			log.Warn("gamedata.npc_generators is empty: generators read from the old file", "path", path, "generators", len(gens))
		}
	}
	if err := data.ApplyGameData(gd); err != nil {
		return fmt.Errorf("game data: %w", err)
	}
	log.Info("game data loaded from database", "items", len(gd.Items), "skills", len(gd.Skills), "drop_mobs", len(gd.Drops), "teleports", len(gd.Teleports), "generators", len(gd.Generators))
	if listen == "" {
		listen = net.JoinHostPort(cfg.ListenIP, fmt.Sprint(cfg.ListenPort))
	}
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	g := game.New(cfg, data, log)
	if err := loadGuilds(ctx, st, g, log); err != nil {
		return fmt.Errorf("guilds: %w", err)
	}
	g.ConnectDB(ctx)
	go refreshRanking(ctx, st, g, log)
	// Live reload: edits to the gamedata tables (admin panel, SQL, a new
	// import) reach the running server about a second later.
	go st.ListenGameData(ctx, time.Second, log, func() {
		lctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		gd, err := st.LoadGameData(lctx)
		if err != nil {
			log.Error("game data reload failed", "err", err)
			return
		}
		g.ReloadGameData(gd)
	})
	go func() {
		if err := g.ServeClients(ctx, ln); err != nil {
			log.Error("listener", "err", err)
		}
	}()
	log.Info("tmsrv listening", "addr", ln.Addr().String(), "server", cfg.Cluster.Group, "channel", cfg.Channel,
		"dbsrv", net.JoinHostPort(cfg.DBIP, fmt.Sprint(cfg.DBPort)))
	g.Run(ctx)
	log.Info("stopped")
	return nil
}

// loadGuilds reads the guild table and alliances/wars (written by DBSRV);
// the C++ TMSRV read ..\DBsrv\guilds.txt instead. Later changes arrive as
// DBSRV broadcasts.
// refreshRanking reads the ranking every channel shares (DBSRV stores it
// from the 0xC14 of all channels).
func refreshRanking(ctx context.Context, st *store.Store, g *game.Game, log *slog.Logger) {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		if list, err := st.Ranking(ctx); err == nil {
			g.SetRanking(list)
		} else if ctx.Err() == nil {
			log.Warn("ranking", "err", err)
		}
		if macs, err := st.MacBans(ctx); err == nil {
			g.SetMacBans(macs)
		} else if ctx.Err() == nil {
			log.Warn("mac bans", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func loadGuilds(ctx context.Context, st *store.Store, g *game.Game, log *slog.Logger) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	guilds, err := st.Guilds(ctx)
	if err != nil {
		return err
	}
	ally, err := st.GuildRelations(ctx, store.RelationAlly)
	if err != nil {
		return err
	}
	war, err := st.GuildRelations(ctx, store.RelationWar)
	if err != nil {
		return err
	}
	g.SetGuilds(guilds, ally, war)
	state, err := st.LoadChannelState(ctx, g.ChannelNumber())
	if err != nil {
		return err
	}
	g.RestoreChannelState(state)
	log.Info("guilds loaded from database", "guilds", len(guilds), "alliances", len(ally), "wars", len(war))
	return nil
}

// loadGameData opens the database and loads the gamedata tables and the
// binary world files; the store stays open for live reloads and account locks.
func loadGameData(dsn string, server int) (*store.Store, *store.GameData, *store.WorldAssets, error) {
	if dsn == "" {
		return nil, nil, nil, fmt.Errorf("no -dsn or DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	st, err := store.OpenServerUnprivileged(ctx, dsn, time.Local, server)
	if err != nil {
		return nil, nil, nil, err
	}
	gd, err := st.LoadGameData(ctx)
	if err != nil {
		st.Close()
		return nil, nil, nil, err
	}
	world, err := st.LoadWorldAssets(ctx)
	if err != nil {
		st.Close()
		return nil, nil, nil, err
	}
	return st, gd, world, nil
}
