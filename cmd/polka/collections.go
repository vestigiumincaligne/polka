package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/vestigiumincaligne/polka/internal/collections"
	"github.com/vestigiumincaligne/polka/internal/config"
	"github.com/vestigiumincaligne/polka/internal/store"
)

const collectionsUsage = `использование:
  polka collections import <файл.json>…   добавить или обновить подборки
  polka collections match [slug]          пересчитать сопоставление с библиотекой
  polka collections list                  показать подборки
  polka collections remove <slug>         удалить подборку`

// runCollections manages collections from the console. A collection file is
// JSON (see collections.File and the collections/ directory in the repository).
func runCollections(log *slog.Logger, args []string) error {
	if len(args) == 0 {
		return errors.New(collectionsUsage)
	}
	sub, args := args[0], args[1:]
	cfg, rest, err := config.Load(args, nil)
	if err != nil {
		return err
	}
	cs, err := collections.Open(filepath.Join(cfg.DataDir, "collections.db"))
	if err != nil {
		return fmt.Errorf("open collections database: %w", err)
	}
	defer cs.Close()
	ctx := context.Background()

	openStore := func() (*store.Store, error) {
		st, err := store.Open(cfg.DBPath())
		if err != nil {
			return nil, fmt.Errorf("open database: %w", err)
		}
		return st, nil
	}
	printStats := func(st collections.MatchStats) {
		fmt.Printf("%-32s найдено %d из %d\n", st.Slug, st.Matched, st.Total)
	}

	switch sub {
	case "import":
		if len(rest) == 0 {
			return errors.New("укажите хотя бы один JSON-файл подборки")
		}
		st, err := openStore()
		if err != nil {
			return err
		}
		defer st.Close()
		for _, path := range rest {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			parsed, perr := collections.Parse(f)
			f.Close()
			if perr != nil {
				return fmt.Errorf("%s: %w", path, perr)
			}
			c, err := cs.Import(ctx, parsed)
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			stats, err := cs.Match(ctx, st, c.Slug)
			if err != nil {
				return err
			}
			fmt.Printf("%s: «%s» — ", path, c.Title)
			printStats(*stats)
		}
		return nil

	case "match":
		st, err := openStore()
		if err != nil {
			return err
		}
		defer st.Close()
		if len(rest) == 1 {
			stats, err := cs.Match(ctx, st, rest[0])
			if err != nil {
				return err
			}
			printStats(*stats)
			return nil
		}
		all, err := cs.MatchAll(ctx, st)
		for _, s := range all {
			printStats(s)
		}
		return err

	case "list":
		list, err := cs.List(ctx)
		if err != nil {
			return err
		}
		if len(list) == 0 {
			fmt.Println("подборок нет — добавьте: polka collections import <файл.json>")
			return nil
		}
		for _, c := range list {
			fmt.Printf("%-32s %-40s найдено %d из %d\n", c.Slug, c.Title, c.Matched, c.Total)
		}
		return nil

	case "remove":
		if len(rest) != 1 {
			return errors.New("укажите slug подборки")
		}
		if err := cs.Delete(ctx, rest[0]); err != nil {
			return err
		}
		fmt.Printf("подборка %s удалена\n", rest[0])
		return nil
	}
	return errors.New(collectionsUsage)
}
