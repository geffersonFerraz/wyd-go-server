// Command tmsrv is the Go port of the C++ TMSRV (game server).
//
//	tmsrv -dir data/tmsrv -dsn postgres://...
//
// Items, skills, drops and teleports come from Postgres (schema gamedata,
// filled once by cmd/gamedata-import; -dsn or DATABASE_URL). From -dir it
// still reads serverip.txt, dataserver.txt, guild_zone.txt, Language.txt,
// NPCGener.txt (+ npc/) and the height/attribute maps. Its own options come
// from -config (tmsrv.yaml: channel, newbie zone, mob AI); the C++
// gameconfig.txt is not read.
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

	"dbsrv/internal/game"
	"dbsrv/internal/store"
)

func main() {
	dir := flag.String("dir", "data/tmsrv", "game data folder (maps, NPCs, guild_zone.txt, Data/, Cache/)")
	listen := flag.String("listen", "", "override network.listen_ip/listen_port of the settings (host:port)")
	dsn := flag.String("dsn", os.Getenv("DATABASE_URL"), "Postgres with the gamedata tables (or DATABASE_URL)")
	config := flag.String("config", "tmsrv.yaml", "server settings (YAML, required: every option comes from it)")
	debug := flag.Bool("debug", false, "debug logging")
	flag.Parse()

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	if err := run(log, *dir, *listen, *config, *dsn); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger, dir, listen, configPath, dsn string) error {
	cfg, err := game.LoadConfig(dir)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	settings, err := game.LoadSettings(configPath)
	if err != nil {
		return fmt.Errorf("settings: %w", err)
	}
	cfg.ApplySettings(settings)
	log.Info("settings", "path", configPath, "listen", net.JoinHostPort(cfg.ListenIP, fmt.Sprint(cfg.ListenPort)),
		"dbsrv", net.JoinHostPort(cfg.DBIP, fmt.Sprint(cfg.DBPort)), "channel", cfg.Channel,
		"newbie_zone", cfg.NewbieZone, "mob_ai", fmt.Sprintf("%+v", cfg.MobAI))
	data, err := game.LoadData(dir)
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	st, gd, err := loadGameData(dsn)
	if err != nil {
		return fmt.Errorf("game data: %w", err)
	}
	defer st.Close()
	if err := data.ApplyGameData(gd); err != nil {
		return fmt.Errorf("game data: %w", err)
	}
	log.Info("game data loaded from database", "items", len(gd.Items), "skills", len(gd.Skills), "drop_mobs", len(gd.Drops), "teleports", len(gd.Teleports))
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
	log.Info("tmsrv listening", "addr", ln.Addr().String(), "channel", cfg.Channel,
		"dbsrv", net.JoinHostPort(cfg.DBIP, fmt.Sprint(cfg.DBPort)))
	g.Run(ctx)
	log.Info("stopped")
	return nil
}

// loadGuilds reads the guild table and alliances/wars (written by DBSRV);
// the C++ TMSRV read ..\DBsrv\guilds.txt instead. Later changes arrive as
// DBSRV broadcasts.
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
	log.Info("guilds loaded from database", "guilds", len(guilds), "alliances", len(ally), "wars", len(war))
	return nil
}

// loadGameData opens the database (read only use) and loads the gamedata
// tables; the store stays open for live reloads.
func loadGameData(dsn string) (*store.Store, *store.GameData, error) {
	if dsn == "" {
		return nil, nil, fmt.Errorf("no -dsn or DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	st, err := store.OpenUnprivileged(ctx, dsn, time.Local)
	if err != nil {
		return nil, nil, err
	}
	gd, err := st.LoadGameData(ctx)
	if err != nil {
		st.Close()
		return nil, nil, err
	}
	return st, gd, nil
}
