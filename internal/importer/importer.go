// Package importer connects catalog sources (inpx, folder scanning)
// to the storage layer.
package importer

import (
	"context"
	"log/slog"

	"github.com/vestigiumincaligne/polka/internal/inpx"
	"github.com/vestigiumincaligne/polka/internal/store"
)

// Progress reports import progress: phase is "loading" or "indexing".
type Progress func(phase string, processed int)

// ImportInpx loads an inpx catalog into an empty database.
func ImportInpx(ctx context.Context, log *slog.Logger, st *store.Store, path string, onProgress Progress) (store.ImportStats, error) {
	var stats store.ImportStats

	f, err := inpx.Open(path)
	if err != nil {
		return stats, err
	}
	defer f.Close()

	if name := f.CollectionName(); name != "" {
		if err := st.SetMeta(ctx, "collection_name", name); err != nil {
			return stats, err
		}
		log.Info("collection", "name", name)
	}

	session, err := st.NewImport(ctx)
	if err != nil {
		return stats, err
	}
	defer session.Abort()

	processed := 0
	err = f.Records(func(rec *inpx.Record) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := session.Add(toBookInput(rec)); err != nil {
			return err
		}
		if processed++; processed%50_000 == 0 {
			log.Info("importing", "books", processed)
		}
		if onProgress != nil && processed%5_000 == 0 {
			onProgress("loading", processed)
		}
		return nil
	})
	if err != nil {
		return stats, err
	}

	log.Info("building indexes and search index", "books", processed)
	if onProgress != nil {
		onProgress("indexing", processed)
	}
	return session.Finish()
}

// UpdateInpx applies a newer inpx catalog to a non-empty database while
// keeping book ids (see store.UpdateSession). With dryRun the changes are
// computed inside a transaction and rolled back. If hasArchive is set,
// records whose archive is missing are skipped, so an index that is newer
// than the downloaded archives does not add books that cannot be opened;
// books already in the catalog are kept as they are.
func UpdateInpx(ctx context.Context, log *slog.Logger, st *store.Store, path string, dryRun bool,
	hasArchive func(folder string) bool, onProgress Progress) (store.UpdateStats, error) {
	var stats store.UpdateStats

	f, err := inpx.Open(path)
	if err != nil {
		return stats, err
	}
	defer f.Close()

	log.Info("loading current catalog")
	session, err := st.NewUpdate(ctx)
	if err != nil {
		return stats, err
	}
	defer session.Abort()

	processed := 0
	archives := map[string]bool{}
	missing := map[string]int{}
	err = f.Records(func(rec *inpx.Record) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		ok := true
		if hasArchive != nil {
			var seen bool
			if ok, seen = archives[rec.Folder]; !seen {
				ok = hasArchive(rec.Folder)
				archives[rec.Folder] = ok
			}
		}
		if ok {
			if err := session.Add(toBookInput(rec)); err != nil {
				return err
			}
		} else {
			session.Skip()
			missing[rec.Folder]++
		}
		if processed++; processed%50_000 == 0 {
			log.Info("updating", "records", processed)
		}
		if onProgress != nil && processed%5_000 == 0 {
			onProgress("loading", processed)
		}
		return nil
	})
	if err != nil {
		return stats, err
	}
	for folder, n := range missing {
		log.Warn("archive not in library, records skipped", "archive", folder, "records", n)
	}

	if dryRun {
		return session.Rollback()
	}
	log.Info("rebuilding search index", "records", processed)
	if onProgress != nil {
		onProgress("indexing", processed)
	}
	if stats, err = session.Finish(); err != nil {
		return stats, err
	}
	if name := f.CollectionName(); name != "" {
		if err := st.SetMeta(ctx, "collection_name", name); err != nil {
			return stats, err
		}
	}
	return stats, nil
}

func toBookInput(rec *inpx.Record) *store.BookInput {
	b := &store.BookInput{
		LibID:     rec.LibID,
		Title:     rec.Title,
		Series:    rec.Series,
		SeriesNum: rec.SeriesNum,
		Genres:    rec.Genres,
		Keywords:  rec.Keywords,
		Folder:    rec.Folder,
		File:      rec.File,
		Ext:       rec.Ext,
		Size:      rec.Size,
		Lang:      rec.Lang,
		Year:      rec.Year,
		Added:     rec.Date,
		Rate:      rec.Rate,
		Deleted:   rec.Deleted,
	}
	for _, a := range rec.Authors {
		b.Authors = append(b.Authors, store.AuthorName{Last: a.Last, First: a.First, Middle: a.Middle})
	}
	return b
}
