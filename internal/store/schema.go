package store

// The schema version is stored in PRAGMA user_version.
const schemaVersion = 3

// v3: ISBN for search. Populated on web upload and lazily —
// on the first view of the book card (from the file's publish-info).
const schemaV3 = `
ALTER TABLE books ADD COLUMN isbn TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_books_isbn ON books (isbn) WHERE isbn != '';
`

// v2 catches up databases created before duplicate hashes existed.
// Fresh databases get these columns right away in schemaV1.
const schemaV2 = `
ALTER TABLE books ADD COLUMN file_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE books ADD COLUMN content_hash TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_books_file_hash ON books (file_hash) WHERE file_hash != '';
CREATE INDEX IF NOT EXISTS idx_books_content_hash ON books (content_hash) WHERE content_hash != '';
`

const schemaV1 = `
CREATE TABLE meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
) WITHOUT ROWID;

CREATE TABLE folders (
	id   INTEGER PRIMARY KEY,
	name TEXT NOT NULL UNIQUE
);

CREATE TABLE series (
	id    INTEGER PRIMARY KEY,
	title TEXT NOT NULL UNIQUE
);

CREATE TABLE authors (
	id          INTEGER PRIMARY KEY,
	last_name   TEXT NOT NULL DEFAULT '',
	first_name  TEXT NOT NULL DEFAULT '',
	middle_name TEXT NOT NULL DEFAULT '',
	UNIQUE (last_name, first_name, middle_name)
);

CREATE TABLE genres (
	id   INTEGER PRIMARY KEY,
	code TEXT NOT NULL UNIQUE,
	name TEXT NOT NULL DEFAULT ''
);

CREATE TABLE keywords (
	id   INTEGER PRIMARY KEY,
	name TEXT NOT NULL UNIQUE
);

CREATE TABLE books (
	id         INTEGER PRIMARY KEY,
	lib_id     TEXT NOT NULL DEFAULT '',
	title      TEXT NOT NULL,
	series_id  INTEGER,
	series_num INTEGER,
	folder_id  INTEGER NOT NULL REFERENCES folders (id),
	file       TEXT NOT NULL,
	ext        TEXT NOT NULL DEFAULT 'fb2',
	size       INTEGER NOT NULL DEFAULT 0,
	lang       TEXT NOT NULL DEFAULT '',
	year       INTEGER,
	added      TEXT NOT NULL DEFAULT '',
	lib_rate   REAL NOT NULL DEFAULT 0,
	deleted    INTEGER NOT NULL DEFAULT 0,
	file_hash    TEXT NOT NULL DEFAULT '',
	content_hash TEXT NOT NULL DEFAULT '',
	isbn         TEXT NOT NULL DEFAULT ''
);

CREATE TABLE book_authors (
	book_id   INTEGER NOT NULL,
	author_id INTEGER NOT NULL,
	PRIMARY KEY (book_id, author_id)
) WITHOUT ROWID;

CREATE TABLE book_genres (
	book_id  INTEGER NOT NULL,
	genre_id INTEGER NOT NULL,
	PRIMARY KEY (book_id, genre_id)
) WITHOUT ROWID;

CREATE TABLE book_keywords (
	book_id    INTEGER NOT NULL,
	keyword_id INTEGER NOT NULL,
	PRIMARY KEY (book_id, keyword_id)
) WITHOUT ROWID;

CREATE VIRTUAL TABLE book_search USING fts5(
	title,
	authors,
	series,
	tokenize = 'unicode61 remove_diacritics 2'
);

CREATE VIRTUAL TABLE authors_search USING fts5(
	name,
	tokenize = 'unicode61 remove_diacritics 2'
);

CREATE VIRTUAL TABLE series_search USING fts5(
	title,
	tokenize = 'unicode61 remove_diacritics 2'
);
`

// Indexes are kept separate: during bulk import it is cheaper to
// build them after the data is loaded than to maintain them on every insert.
var schemaIndexes = []string{
	`CREATE INDEX IF NOT EXISTS idx_books_series ON books (series_id) WHERE series_id IS NOT NULL`,
	`CREATE INDEX IF NOT EXISTS idx_books_folder ON books (folder_id)`,
	// (added, id) and (lib_rate, id) serve shelf ORDER BYs: a reverse index
	// scan yields a DESC selection with LIMIT without a full sort.
	`CREATE INDEX IF NOT EXISTS idx_books_added ON books (added, id) WHERE deleted = 0`,
	`CREATE INDEX IF NOT EXISTS idx_books_rate ON books (lib_rate, id) WHERE deleted = 0`,
	`CREATE INDEX IF NOT EXISTS idx_book_authors_author ON book_authors (author_id, book_id)`,
	`CREATE INDEX IF NOT EXISTS idx_book_genres_genre ON book_genres (genre_id, book_id)`,
	`CREATE INDEX IF NOT EXISTS idx_book_keywords_keyword ON book_keywords (keyword_id, book_id)`,
	`CREATE INDEX IF NOT EXISTS idx_authors_name ON authors (last_name, first_name)`,
	`CREATE INDEX IF NOT EXISTS idx_books_file_hash ON books (file_hash) WHERE file_hash != ''`,
	`CREATE INDEX IF NOT EXISTS idx_books_content_hash ON books (content_hash) WHERE content_hash != ''`,
	`CREATE INDEX IF NOT EXISTS idx_books_isbn ON books (isbn) WHERE isbn != ''`,
}
