// Command gamedata-import loads the C++ game data files into Postgres
// (schema gamedata), replacing what is there: ItemList.csv (+ ItemEffect.h),
// skilldata.csv, "controle drop/Controle.txt" and Teleport.txt. After that
// the Go TMSRV reads these tables from the database, not from the files.
//
//	gamedata-import -dir data/tmsrv -dsn postgres://...
//
// Environment: DATABASE_URL (instead of -dsn).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"dbsrv/internal/game"
	"dbsrv/internal/store"
)

func main() {
	dir := flag.String("dir", "data/tmsrv", "folder with ItemList.csv, ItemEffect.h, skilldata.csv, controle drop/Controle.txt, Teleport.txt")
	dsn := flag.String("dsn", os.Getenv("DATABASE_URL"), "Postgres connection string (or DATABASE_URL)")
	flag.Parse()
	if err := run(*dir, *dsn); err != nil {
		fmt.Fprintln(os.Stderr, "gamedata-import:", err)
		os.Exit(1)
	}
}

func run(dir, dsn string) error {
	if dsn == "" {
		return fmt.Errorf("no -dsn or DATABASE_URL")
	}
	gd, skipped, err := game.ReadLegacyGameData(dir)
	if err != nil {
		return err
	}
	for _, n := range skipped {
		fmt.Printf("skipped duplicate drop block %q (the C++ used the first one)\n", n)
	}
	ctx := context.Background()
	st, err := store.OpenUnprivileged(ctx, dsn, time.Local)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		return err
	}
	if err := st.ImportGameData(ctx, gd, "gamedata-import"); err != nil {
		return err
	}
	// Read it back and check the server can use it.
	back, err := st.LoadGameData(ctx)
	if err != nil {
		return err
	}
	var check game.Data
	if err := check.ApplyGameData(back); err != nil {
		return err
	}
	if len(back.Items) != len(gd.Items) || len(back.Skills) != len(gd.Skills) || len(back.Drops) != len(gd.Drops) || len(back.Teleports) != len(gd.Teleports) {
		return fmt.Errorf("read back %d items, %d skills, %d drops, %d teleports; wrote %d, %d, %d, %d",
			len(back.Items), len(back.Skills), len(back.Drops), len(back.Teleports), len(gd.Items), len(gd.Skills), len(gd.Drops), len(gd.Teleports))
	}
	items := 0
	for _, d := range gd.Drops {
		items += len(d.Items)
	}
	fmt.Printf("imported: %d effect names, %d items, %d skills, %d drop mobs (%d lines), %d teleports\n",
		len(gd.EffectNames), len(gd.Items), len(gd.Skills), len(gd.Drops), items, len(gd.Teleports))
	return nil
}
