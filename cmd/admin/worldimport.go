package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"dbsrv/internal/game"
	"dbsrv/internal/store"
)

// maxTemplateSize keeps stray files (archives, notes) out of the templates:
// a STRUCT_MOB image is about 1 KB.
const maxTemplateSize = 4096

// worldImport is "admin world-import": puts binary world files into the
// database. Only what is named is touched, so it also replaces a single file
// (a new ItemList.bin, the templates of an NPC editor). TMSRV reads the maps
// and templates at startup; ItemList.bin is served as soon as it changes.
func worldImport(args []string) error {
	fs := flag.NewFlagSet("world-import", flag.ExitOnError)
	dsn := fs.String("dsn", os.Getenv("DATABASE_URL"), "Postgres connection string (or DATABASE_URL)")
	height := fs.String("heightmap", "", "HeightMap.dat")
	attribute := fs.String("attributemap", "", "AttributeMap.dat")
	itemList := fs.String("itemlist", "", "the client's ItemList.bin")
	npc := fs.String("npc", "", "folder with the NPC templates")
	npcBase := fs.String("npc-base", "", "folder with the mount and summon templates")
	class := fs.String("class", "", "folder with the TK, FM, BM and HT templates")
	_ = fs.Parse(args)
	if *dsn == "" {
		return fmt.Errorf("no -dsn or DATABASE_URL")
	}

	w := &store.WorldAssets{}
	for _, a := range []struct {
		path, name string
		size       int // exact size in bytes
	}{
		{*height, store.AssetHeightMap, 4096 * 4096},
		{*attribute, store.AssetAttributeMap, 1024 * 1024},
		{*itemList, store.AssetItemList, game.MaxItemList * 156},
	} {
		if a.path == "" {
			continue
		}
		raw, err := os.ReadFile(a.path)
		if err != nil {
			return err
		}
		if len(raw) != a.size {
			return fmt.Errorf("%s: %d bytes, want %d", a.path, len(raw), a.size)
		}
		w.Assets = append(w.Assets, store.WorldAsset{Name: a.name, Content: raw})
	}

	if *class != "" {
		files := map[string][]byte{}
		for _, name := range game.ClassNames {
			raw, err := os.ReadFile(filepath.Join(*class, name))
			if err != nil {
				return err
			}
			files[name] = raw
			w.Templates = append(w.Templates, store.MobTemplate{Kind: store.TemplateClass, Name: name, Content: raw})
		}
		if err := game.CheckClassTemplates(files); err != nil {
			return err
		}
	}
	for _, t := range []struct {
		dir, kind string
		wanted    func(string) bool
	}{
		{*npc, store.TemplateNPC, func(string) bool { return true }},
		{*npcBase, store.TemplateNPCBase, game.IsNPCBaseName},
	} {
		if t.dir == "" {
			continue
		}
		entries, err := os.ReadDir(t.dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if !e.Type().IsRegular() || !t.wanted(e.Name()) {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(t.dir, e.Name()))
			if err != nil {
				return err
			}
			if len(raw) == 0 || len(raw) > maxTemplateSize {
				fmt.Fprintf(os.Stderr, "ignorado (não é um modelo): %s (%d bytes)\n", e.Name(), len(raw))
				continue
			}
			w.Templates = append(w.Templates, store.MobTemplate{Kind: t.kind, Name: e.Name(), Content: raw})
		}
	}
	if len(w.Assets)+len(w.Templates) == 0 {
		return fmt.Errorf("nothing to import: name at least one of -heightmap, -attributemap, -itemlist, -npc, -npc-base, -class")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	st, err := store.OpenUnprivileged(ctx, *dsn, time.Local)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if err := st.UpsertWorldAssets(ctx, w); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "importado: %d arquivos, %d modelos\n", len(w.Assets), len(w.Templates))
	return nil
}
