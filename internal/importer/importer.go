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
