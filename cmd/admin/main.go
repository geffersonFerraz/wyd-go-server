// Command admin serves the account management web panel.
//
//	admin -dsn postgres://dbsrv:dbsrv@localhost:5432/dbsrv
//
// Login: user from -user / ADMIN_USER (default "admin"); password hash from
// ADMIN_PASSWORD_HASH. Without a hash, a random password is generated once
// and kept in -password-file (default .admin-password, git-ignored).
//
//	admin hash-password      # reads a password from stdin, prints its hash
//
// The panel only queues changes; dbsrv must be running to apply them.
//
// Guild marks uploaded in the panel are saved to -guildmark-dir. With
// -guildmark-listen (e.g. ":80"), they are also served read-only at
// /guild/img_guilds/b0100000<id>.bmp, the URL the game client downloads
// (GameURLMark in the client's TMPaths.h must point at that host).
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

	listen := flag.String("listen", "127.0.0.1:8080", "address to serve the panel on")
	dsn := flag.String("dsn", os.Getenv("DATABASE_URL"), "Postgres connection string (or DATABASE_URL)")
	user := flag.String("user", envOr("ADMIN_USER", "admin"), "admin login (or ADMIN_USER)")
	pwFile := flag.String("password-file", ".admin-password", "where a generated password is kept when ADMIN_PASSWORD_HASH is unset")
	secure := flag.Bool("secure-cookie", false, "mark the session cookie Secure (use behind HTTPS)")
	markDir := flag.String("guildmark-dir", "guildmarks", `where guild marks are saved ("" disables the upload)`)
	tmsrvDir := flag.String("tmsrv-dir", "data/tmsrv", `TMSRV folder (its Cache\LockAccount holds the in-game account locks; "" hides the lock tools)`)
	markListen := flag.String("guildmark-listen", "", `public address serving the marks to the game client at `+admin.GuildMarkPath+` (e.g. ":80"; "" = don't serve)`)
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	opts := options{listen: *listen, dsn: *dsn, user: *user, pwFile: *pwFile, secure: *secure, markDir: *markDir, markListen: *markListen, tmsrvDir: *tmsrvDir}
	if err := run(log, opts); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
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
	markDir, markListen       string
	tmsrvDir                  string
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

	if o.markDir != "" {
		if err := os.MkdirAll(o.markDir, 0o755); err != nil {
			return fmt.Errorf("guildmark dir: %w", err)
		}
	}
	panel, err := admin.New(admin.Config{Users: map[string]string{user: hash}, SecureCookie: secure, GuildMarkDir: o.markDir, LockDir: lockDir(o.tmsrvDir)}, st, log)
	if err != nil {
		return err
	}
	// The marks go on their own listener: the game client downloads them
	// over plain HTTP from anywhere, while the panel stays private.
	if o.markListen != "" {
		if o.markDir == "" {
			return errors.New("-guildmark-listen needs -guildmark-dir")
		}
		marks := &http.Server{
			Addr:              o.markListen,
			Handler:           admin.GuildMarkHandler(o.markDir),
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      10 * time.Second,
		}
		go func() {
			<-ctx.Done()
			_ = marks.Close()
		}()
		go func() {
			log.Info("guild marks", "url", "http://"+o.markListen+admin.GuildMarkPath, "dir", o.markDir)
			if err := marks.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
				log.Error("guild mark server", "err", err)
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
