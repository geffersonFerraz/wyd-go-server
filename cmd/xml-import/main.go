// Command xml-import copies the C++ DBSRV's files into Postgres:
// account/*/*.xml, char/*, seal/*.xml, guilds.txt, Donate.txt, Config.txt.
//
// It is idempotent (accounts are upserted by username) and never modifies
// the source files. Run with -dry-run first to read the report.
//
//	xml-import -dir "../Release Servidor/DBSRV" -dry-run
//	xml-import -dir "../Release Servidor/DBSRV" -dsn postgres://dbsrv:pw@localhost/dbsrv
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	"dbsrv/internal/auth"
	"dbsrv/internal/legacy"
	"dbsrv/internal/store"
	"dbsrv/internal/wire"
)

func main() {
	dir := flag.String("dir", "../Release Servidor/DBSRV", "C++ DBSRV folder (contains account/, char/, guilds.txt)")
	dsn := flag.String("dsn", os.Getenv("DATABASE_URL"), "Postgres connection string (or DATABASE_URL)")
	tz := flag.String("tz", "America/Sao_Paulo", "time zone of the game's dates (bans etc.)")
	dryRun := flag.Bool("dry-run", false, "parse and report only; do not touch the database")
	flag.Parse()

	loc, err := time.LoadLocation(*tz)
	if err != nil {
		log.Fatalf("time zone: %v", err)
	}

	src, err := readSource(*dir)
	if err != nil {
		log.Fatal(err)
	}
	src.report()

	if *dryRun {
		fmt.Println("\ndry run: nothing written")
		return
	}
	if *dsn == "" {
		log.Fatal("no -dsn or DATABASE_URL given (use -dry-run to only parse)")
	}
	if len(src.errors) > 0 {
		log.Fatalf("%d files could not be read; fix or remove them first", len(src.errors))
	}

	ctx := context.Background()
	st, err := store.Open(ctx, *dsn, loc)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	if err := src.write(ctx, st); err != nil {
		log.Fatalf("import: %v", err)
	}
	fmt.Println("\nimport finished")
}

type source struct {
	accounts  []*legacy.Account
	nameOwner map[string]string   // lower(name) -> account chosen as owner
	conflicts map[string][]string // lower(name) -> all accounts using it
	charIndex map[string]string
	guilds    []legacy.Guild
	donate    []legacy.DonateSlot
	config    legacy.Config
	seals     []*legacy.Seal
	warnings  []string
	errors    []string
}

func readSource(dir string) (*source, error) {
	if _, err := os.Stat(filepath.Join(dir, "account")); err != nil {
		return nil, fmt.Errorf("%s does not look like a DBSRV folder: %w", dir, err)
	}
	s := &source{nameOwner: map[string]string{}, conflicts: map[string][]string{}}

	files, _ := filepath.Glob(filepath.Join(dir, "account", "*", "*.xml"))
	sort.Strings(files)
	for _, f := range files {
		if filepath.Base(f) == ".xml" {
			s.warn("skipped %s: empty account written every second by the C++ ProcessSecTimer", f)
			continue
		}
		acc, err := legacy.ReadAccountFile(f)
		if err != nil {
			s.errors = append(s.errors, fmt.Sprintf("%s: %v", f, err))
			continue
		}
		user := wire.CString(acc.Wire.Username[:])
		if want := strings.ToUpper(strings.TrimSuffix(filepath.Base(f), filepath.Ext(f))); user != want {
			s.warn("%s: <username> is %q, file name says %q (using <username>)", f, user, want)
		}
		for _, w := range acc.Warnings {
			s.warn("%s: %s", user, w)
		}
		s.accounts = append(s.accounts, acc)
	}

	idx, err := legacy.ReadCharIndex(filepath.Join(dir, "char"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	s.charIndex = idx
	s.resolveNames()

	if g, w, err := legacy.ReadGuilds(filepath.Join(dir, "guilds.txt")); err == nil {
		s.guilds = g
		s.warnings = append(s.warnings, w...)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if d, w, err := legacy.ReadDonate(filepath.Join(dir, "Donate.txt")); err == nil {
		s.donate = d
		s.warnings = append(s.warnings, w...)
	} else {
		return nil, err
	}
	if c, err := legacy.ReadConfig(filepath.Join(dir, "Config.txt")); err == nil {
		s.config = c
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	sealFiles, _ := filepath.Glob(filepath.Join(dir, "seal", "*.xml"))
	for _, f := range sealFiles {
		id, err := strconv.Atoi(strings.TrimSuffix(filepath.Base(f), ".xml"))
		if err != nil {
			s.warn("skipped %s: not a numeric seal id", f)
			continue
		}
		seal, err := legacy.ReadSealFile(f, id)
		if err != nil {
			s.errors = append(s.errors, fmt.Sprintf("%s: %v", f, err))
			continue
		}
		for _, w := range seal.Warnings {
			s.warn("seal %d: %s", id, w)
		}
		s.seals = append(s.seals, seal)
	}
	return s, nil
}

func (s *source) warn(format string, args ...any) {
	s.warnings = append(s.warnings, fmt.Sprintf(format, args...))
}

// resolveNames decides which account owns each character name. Repeats
// inside one account are normal (Arch keeps the mortal's name). A name used
// by several accounts goes to the one char/<NAME> points at, else the first
// alphabetically; the others keep the character but not the reservation.
func (s *source) resolveNames() {
	users := map[string][]string{}
	for _, acc := range s.accounts {
		user := wire.CString(acc.Wire.Username[:])
		seen := map[string]bool{}
		for i := range acc.Wire.Mob {
			n := strings.ToLower(wire.GameString(acc.Wire.Mob[i].Player.Name[:]))
			if n == "" || seen[n] {
				continue
			}
			seen[n] = true
			users[n] = append(users[n], user)
		}
	}
	for n, us := range users {
		sort.Strings(us)
		owner := us[0]
		if len(us) > 1 {
			s.conflicts[n] = us
			if idxOwner, ok := s.charIndex[strings.ToUpper(n)]; ok {
				for _, u := range us {
					if u == idxOwner {
						owner = u
					}
				}
			}
			s.warn("name %q is used by %v; reserved for %s", n, us, owner)
		} else if idxOwner, ok := s.charIndex[strings.ToUpper(n)]; ok && idxOwner != owner {
			s.warn("name %q: char/ says %s but the character is in %s (reserving for %s)", n, idxOwner, owner, owner)
		}
		s.nameOwner[n] = owner
	}
	for name, acc := range s.charIndex {
		if _, ok := users[strings.ToLower(name)]; !ok {
			s.warn("char/%s (account %s) has no matching character; stale reservation not imported", name, acc)
		}
	}
}

func (s *source) report() {
	chars := 0
	for _, a := range s.accounts {
		for i := range a.Wire.Mob {
			if a.Wire.Mob[i].Player.Name[0] != 0 {
				chars++
			}
		}
	}
	fmt.Printf("accounts:        %d\n", len(s.accounts))
	fmt.Printf("characters:      %d (%d distinct names, %d shared between accounts)\n", chars, len(s.nameOwner), len(s.conflicts))
	fmt.Printf("char/ index:     %d files\n", len(s.charIndex))
	fmt.Printf("guilds:          %d\n", len(s.guilds))
	fmt.Printf("donate slots:    %d\n", len(s.donate))
	fmt.Printf("seals:           %d (LastSeal %d)\n", len(s.seals), s.config.LastSeal)
	fmt.Printf("sapphire:        %d (the C++ ReadConfig never actually loaded this value)\n", s.config.Sapphire)
	if len(s.warnings) > 0 {
		fmt.Printf("\nwarnings (%d):\n", len(s.warnings))
		for _, w := range s.warnings {
			fmt.Println("  -", w)
		}
	}
	if len(s.errors) > 0 {
		fmt.Printf("\nerrors (%d):\n", len(s.errors))
		for _, e := range s.errors {
			fmt.Println("  !", e)
		}
	}
}

func (s *source) write(ctx context.Context, st *store.Store) error {
	ids := map[string]int64{}
	for i, acc := range s.accounts {
		user := wire.CString(acc.Wire.Username[:])
		status, pw := auth.SplitLegacyPassword(acc.Password)
		rec := &store.AccountRecord{Status: status, Account: acc.Wire}
		var err error
		if rec.PasswordHash, err = auth.Hash(pw); err != nil {
			return err
		}
		if acc.NumericPass != "" {
			if rec.NumericPassHash, err = auth.Hash(acc.NumericPass); err != nil {
				return err
			}
		}
		if err := st.SaveAccount(ctx, rec); err != nil {
			return fmt.Errorf("account %s: %w", user, err)
		}
		// Read it back and compare the STRUCT_ACCOUNT byte image with what
		// was parsed, so a mapping bug can never silently lose game data.
		back, err := st.LoadAccount(ctx, user)
		if err != nil {
			return fmt.Errorf("account %s: verify: %w", user, err)
		}
		if !bytes.Equal(wire.MustMarshal(&rec.Account), wire.MustMarshal(&back.Account)) {
			return fmt.Errorf("account %s: verify: data read back differs from the XML", user)
		}
		ids[user] = rec.ID
		fmt.Printf("\r  accounts %d/%d", i+1, len(s.accounts))
	}
	fmt.Println()

	for name, owner := range s.nameOwner {
		if _, err := st.ReserveName(ctx, ids[owner], name, true); err != nil {
			return fmt.Errorf("reserve %q: %w", name, err)
		}
	}
	for _, g := range s.guilds {
		if err := st.UpsertGuild(ctx, store.Guild{ID: g.ID, Name: g.Name, Kingdom: g.Kingdom, Fame: g.Fame,
			Citizen: g.Citizen, Wins: g.Wins, SubGuild: g.SubGuild}); err != nil {
			return fmt.Errorf("guild %d: %w", g.ID, err)
		}
	}
	slots := make([]store.DonateSlot, len(s.donate))
	for i, d := range s.donate {
		slots[i] = store.DonateSlot{Store: d.Store, Slot: d.Slot, Price: d.Price, Loop: d.Loop, Available: d.Available, Item: d.Item}
	}
	if len(slots) > 0 {
		if err := st.ReplaceDonateStore(ctx, slots); err != nil {
			return fmt.Errorf("donate store: %w", err)
		}
	}
	for _, seal := range s.seals {
		if err := st.UpsertSeal(ctx, seal.ID, seal.Info, seal.Char); err != nil {
			return fmt.Errorf("seal %d: %w", seal.ID, err)
		}
	}
	if err := st.AdvanceSealSeq(ctx, s.config.LastSeal); err != nil {
		return err
	}
	return st.SetSapphire(ctx, s.config.Sapphire)
}
