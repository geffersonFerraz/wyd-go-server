// Command admin serves the account management web panel.
//
//	admin -dsn postgres://dbsrv:dbsrv@localhost:5432/dbsrv
//
// Login: user from -user / ADMIN_USER (default "admin"); password hash from
// ADMIN_PASSWORD_HASH. Without a hash, a random password is generated once
// and kept in -password-file (default .admin-password, git-ignored).
//
//	admin hash-password      # reads a password from stdin, prints its hash
//	admin serverlist [-config tmsrv.yaml] [-o serverlist.bin]
//	                         # writes the client's serverlist.bin
//
// The panel only queues changes; dbsrv must be running to apply them.
//
// With -public-listen (e.g. ":80"), a second, unauthenticated listener
// serves what the game client downloads over plain HTTP:
//
//   - /servNN.htm: online count per channel for the server list (the URL in
//     serverlist.bin, entry [group][0], must point at this host);
//   - /serverlist.bin: the client's server list, built from the channels of
//     -config (tmsrv.yaml) on every request;
//   - /itemlist.bin: the client's item list, the file given by -itemlist
//     as it is on disk;
//   - /guild/img_guilds/b0100000<id>.bmp: guild marks uploaded in the panel
//     and saved to -guildmark-dir (GameURLMark in the client's TMPaths.h
//     must point at this host).
//
// -guildmark-listen is the old name of -public-listen.
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"dbsrv/internal/admin"
	"dbsrv/internal/auth"
	"dbsrv/internal/game"
	"dbsrv/internal/logfile"
	"dbsrv/internal/store"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "hash-password" {
		fmt.Fprint(os.Stderr, "senha: ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		h, err := auth.Hash(strings.TrimRight(line, "\r\n"))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(h)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "serverlist" {
		if err := writeServerList(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	listen := flag.String("listen", "127.0.0.1:8080", "address to serve the panel on")
	dsn := flag.String("dsn", os.Getenv("DATABASE_URL"), "Postgres connection string (or DATABASE_URL)")
	user := flag.String("user", envOr("ADMIN_USER", "admin"), "admin login (or ADMIN_USER)")
	pwFile := flag.String("password-file", ".admin-password", "where a generated password is kept when ADMIN_PASSWORD_HASH is unset")
	secure := flag.Bool("secure-cookie", false, "mark the session cookie Secure (use behind HTTPS)")
	markDir := flag.String("guildmark-dir", "guildmarks", `where guild marks are saved ("" disables the upload)`)
	tmsrvDir := flag.String("tmsrv-dir", "data/tmsrv", `TMSRV folder (its Cache\LockAccount holds the in-game account locks; "" hides the lock tools)`)
	publicListen := flag.String("public-listen", "", `public address serving the game client servNN.htm, `+admin.ServerListPath+` and the guild marks at `+admin.GuildMarkPath+` (e.g. ":80"; "" = don't serve)`)
	config := flag.String("config", "tmsrv.yaml", `TMSRV settings, source of `+admin.ServerListPath+` ("" = don't serve it)`)
	markListen := flag.String("guildmark-listen", "", "old name of -public-listen")
	itemList := flag.String("itemlist", "data/client/ItemList.bin", `the game client's ItemList.bin, served at `+admin.ItemListPath+` ("" = don't serve it)`)
	logDir := flag.String("log-dir", envOr("LOG_DIR", "logs"), `folder of the daily log files (or LOG_DIR; "" = console only)`)
	logDays := flag.Int("log-days", 90, "days of log files to keep (0 = all)")
	flag.Parse()
	if *publicListen == "" {
		publicListen = markListen
	}

	log, closeLog, err := logfile.Logger(*logDir, "admin", *logDays, slog.LevelInfo)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	opts := options{listen: *listen, dsn: *dsn, user: *user, pwFile: *pwFile, secure: *secure, markDir: *markDir, publicListen: *publicListen, tmsrvDir: *tmsrvDir, config: *config, itemList: *itemList}
	if err := run(log, opts); err != nil {
		log.Error("fatal", "err", err)
		closeLog()
		os.Exit(1)
	}
	closeLog()
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

type options struct {
	listen, dsn, user, pwFile string
	secure                    bool
	markDir, publicListen     string
	itemList                  string
	tmsrvDir                  string
	config                    string
}

func run(log *slog.Logger, o options) error {
	listen, dsn, user, pwFile, secure := o.listen, o.dsn, o.user, o.pwFile, o.secure
	if dsn == "" {
		return errors.New("no -dsn or DATABASE_URL")
	}
	hash := os.Getenv("ADMIN_PASSWORD_HASH")
	if hash == "" {
		var err error
		if hash, err = generatedPasswordHash(log, pwFile); err != nil {
			return err
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Unprivileged: the panel must not slip past the online-account guard.
	st, err := store.OpenUnprivileged(ctx, dsn, time.Local)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	var channels []int
	if settings, err := game.LoadSettings(o.config); err == nil {
		for _, c := range settings.Channels {
			channels = append(channels, c.Number)
		}
	} else if o.config != "" {
		log.Warn("channels not read from the settings", "err", err)
	}

	// City owners: the database is the only source; the first start copies
	// the owners of guild_zone.txt for the cities not set yet.
	if o.tmsrvDir != "" {
		n, err := admin.SeedCityOwners(ctx, st, filepath.Join(o.tmsrvDir, "guild_zone.txt"))
		if err != nil {
			log.Warn("city owners not seeded from guild_zone.txt", "err", err)
		} else if n > 0 {
			log.Info("city owners copied from guild_zone.txt", "cities", n)
		}
	}

	// Drops by chance: the first start copies the monsters' inventories.
	if o.tmsrvDir != "" {
		if n, err := admin.SeedRateDrops(ctx, st, o.tmsrvDir); err != nil {
			log.Warn("drops by chance not imported", "err", err)
		} else if n > 0 {
			log.Info("drops by chance imported", "lines", n)
		}
	}

	if o.markDir != "" {
		if err := os.MkdirAll(o.markDir, 0o755); err != nil {
			return fmt.Errorf("guildmark dir: %w", err)
		}
	}
	panel, err := admin.New(admin.Config{Users: map[string]string{user: hash}, SecureCookie: secure, GuildMarkDir: o.markDir, LockDir: lockDir(o.tmsrvDir), Channels: channels,
		SettingsPath: o.config, DataDir: dataDirOf(o.tmsrvDir), StateDir: filepath.Dir(pwFile)}, st, log)
	if err != nil {
		return err
	}
	// What the game client downloads goes on its own listener: the client
	// fetches it over plain HTTP from anywhere, while the panel stays private.
	if o.publicListen != "" {
		mux := http.NewServeMux()
		mux.Handle("/", admin.ServerStatusHandler(st, log))
		if o.config != "" {
			if _, err := admin.BuildServerList(o.config); err != nil {
				return fmt.Errorf("serverlist.bin: %w", err)
			}
			mux.Handle("GET "+admin.ServerListPath, admin.ServerListHandler(o.config, log))
		}
		if o.itemList != "" {
			if _, err := os.Stat(o.itemList); err != nil {
				return fmt.Errorf("itemlist.bin: %w", err)
			}
			mux.Handle("GET "+admin.ItemListPath, admin.ItemListHandler(o.itemList, log))
		}
		if o.markDir != "" {
			mux.Handle(admin.GuildMarkPath, admin.GuildMarkHandler(o.markDir))
		}
		public := &http.Server{
			Addr:              o.publicListen,
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      10 * time.Second,
		}
		go func() {
			<-ctx.Done()
			_ = public.Close()
		}()
		go func() {
			log.Info("public files", "status", "http://"+o.publicListen+"/serv00.htm", "serverlist", "http://"+o.publicListen+admin.ServerListPath,
				"itemlist", "http://"+o.publicListen+admin.ItemListPath,
				"guild_marks", "http://"+o.publicListen+admin.GuildMarkPath, "dir", o.markDir)
			if err := public.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
				log.Error("public server", "err", err)
			}
		}()
	}
	srv := &http.Server{
		Addr:              listen,
		Handler:           panel.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	log.Info("admin panel", "url", "http://"+listen, "user", user)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// writeServerList is "admin serverlist": serverlist.bin from tmsrv.yaml.
func writeServerList(args []string) error {
	fs := flag.NewFlagSet("serverlist", flag.ExitOnError)
	config := fs.String("config", "tmsrv.yaml", "TMSRV settings (serverlist and channels sections)")
	out := fs.String("o", "serverlist.bin", "output file (the client reads ./serverlist.bin)")
	_ = fs.Parse(args)
	b, err := admin.BuildServerList(*config)
	if err != nil {
		return err
	}
	if err := os.WriteFile(*out, b, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%s: %d bytes\n", *out, len(b))
	return nil
}

// lockDir is <TMSRV>/Cache/LockAccount ("" when no TMSRV folder).
func lockDir(tmsrv string) string {
	if tmsrv == "" {
		return ""
	}
	return filepath.Join(tmsrv, "Cache", "LockAccount")
}

// generatedPasswordHash reads (or creates) a random admin password in pwFile.
func generatedPasswordHash(log *slog.Logger, pwFile string) (string, error) {
	raw, err := os.ReadFile(pwFile)
	if errors.Is(err, os.ErrNotExist) {
		const alphabet = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKMNPQRSTUVWXYZ23456789"
		b := make([]byte, 16)
		for i := range b {
			n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
			if err != nil {
				return "", err
			}
			b[i] = alphabet[n.Int64()]
		}
		raw = b
		if err := os.WriteFile(pwFile, raw, 0o600); err != nil {
			return "", fmt.Errorf("write %s: %w", pwFile, err)
		}
		log.Warn("ADMIN_PASSWORD_HASH not set: generated a password", "file", pwFile)
	} else if err != nil {
		return "", err
	} else {
		log.Info("using the password in the password file", "file", pwFile)
	}
	pw := strings.TrimSpace(string(raw))
	if pw == "" {
		return "", fmt.Errorf("%s is empty", pwFile)
	}
	return auth.Hash(pw)
}

// dataDirOf is the data folder that holds the TMSRV folder.
func dataDirOf(tmsrv string) string {
	if tmsrv == "" {
		return ""
	}
	return filepath.Dir(tmsrv)
}
