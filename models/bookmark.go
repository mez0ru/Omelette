package models

import (
	"github.com/TwiN/go-color"

	"context"
	"database/sql"
	"time"
)

// Schema versioning is good in case there's a better algorithm for x website. like reddit
const SCHEMA_VER = 0

type Bookmark struct {
}

type BookmarkModel struct {
	DB *sql.DB
}

type Entry struct {
	Content string // Needed only on fetching
	Icon    []byte
	Href    string
	Title   string
	Date    int
}

type TitleHref struct {
    Id int64
    Href string
    Title string
    Xxh int64
    LastModified time.Time
    Outdated bool
}

type SearchResult struct {
    Id int64
    Href string
    Title string
    Content string
}

func (m BookmarkModel) Insert(ctx *context.Context, entry Entry, stmt *sql.Stmt) (int64, error) {
	r, err := stmt.ExecContext(*ctx, &entry.Title, &entry.Href, &entry.Date, &entry.Icon, &entry.Content, SCHEMA_VER)

	if err != nil {
		return -1, err
	}
return r.LastInsertId()
}

func (m BookmarkModel) InsertStmt(ctx *context.Context, tx *sql.Tx) (*sql.Stmt, error) {
	return tx.PrepareContext(*ctx, `
        insert into bookmark
        (title, href, date, icon, content, version)
        values
        (?, ?, (select datetime(?, 'unixepoch')), ?, ?, ?);
        `)
}

func (m BookmarkModel) UpdateContent(ctx *context.Context, id int64, content string, xxh uint64, modified time.Time, stmt *sql.Stmt) (int64, error) {
	r, err := stmt.ExecContext(*ctx, content, int64(xxh), modified.Unix(), id)

	if err != nil {
		return -1, err
	}
return r.LastInsertId()
}

func (m BookmarkModel) UpdateContentStmt(ctx *context.Context, tx *sql.Tx) (*sql.Stmt, error) {
	return tx.PrepareContext(*ctx, `
        update bookmark set
        content = ?, xxh = ?, modified = datetime(?, 'unixepoch')
        where id = ?;
        `)
}

func (m BookmarkModel) Init(ctx *context.Context) error {
	_, err := m.DB.ExecContext(*ctx, `
        create table if not exists bookmark(
            id integer not null primary key,
            title text,
            href text unique not null,
            date timestamp not null,
            icon blob,
            content text,
            xxh integer not null default 0,
            modified timestamp not null default (datetime(0, 'unixepoch')),
            version integer not null default ?
            created_at timestamp not null default current_timestamp,
            updated_at timestamp not null default current_timestamp
        );

        CREATE VIRTUAL TABLE IF NOT EXISTS bookmark_fts USING fts5
        (
            title,
            href,
            content,
            content=bookmark,
            tokenize="trigram"
        );

        CREATE TRIGGER IF NOT EXISTS bookmark_fts_insert AFTER INSERT ON bookmark 
        BEGIN
            INSERT INTO bookmark_fts (rowid, title, href, content) VALUES (new.rowid, new.title, new.href, new.content);
        END;

        CREATE TRIGGER IF NOT EXISTS bookmark_fts_delete AFTER DELETE ON bookmark
        BEGIN
            INSERT INTO bookmark_fts (bookmark_fts, rowid, title, href, content) VALUES ('delete', old.rowid, old.title, old.href, old.content);
        END;

        CREATE TRIGGER IF NOT EXISTS bookmark_fts_update AFTER UPDATE ON bookmark
        BEGIN
            INSERT INTO bookmark_fts (bookmark_fts, rowid, title, href, content) VALUES ('delete', old.rowid, old.title, old.href, old.content);
            INSERT INTO bookmark_fts (rowid, title, href, content) VALUES (new.rowid, new.title, new.href, new.content);
        END;

        create trigger if not exists bookmark_updated_trigger
        update of title, href, date, icon, content on bookmark
        begin
            update bookmark set updated_at=current_timestamp
            where id=new.id;
        end;
        `, SCHEMA_VER)

	return err
}

func (m BookmarkModel) Transaction() (*sql.Tx, error) {
    return m.DB.Begin()
}

func (m BookmarkModel) Search(ctx *context.Context, tokens string) ([]SearchResult, error) {
    rows, err := m.DB.QueryContext(*ctx, `
        SELECT rowid, title, href,
        snippet(bookmark_fts, 2, ?, ?, '...', 64)
        FROM bookmark_fts(?) ORDER BY rank;
    `, color.Green, color.Reset, tokens)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

    var ss []SearchResult

	for rows.Next() {
        var s SearchResult

		err := rows.Scan(&s.Id, &s.Title, &s.Href, &s.Content)
		if err != nil {
			return nil, err
		}

		ss = append(ss, s)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	return ss, nil
}

func (m BookmarkModel) AllUncached(ctx *context.Context) ([]TitleHref, error) {
	rows, err := m.DB.QueryContext(*ctx, `SELECT id, title, href, xxh, modified, version
        FROM bookmark where is null order by RANDOM()`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

    var b []TitleHref

	for rows.Next() {
        t := TitleHref{ Outdated: false }
        var ver int

		err := rows.Scan(&t.Id, &t.Title, &t.Href, &t.Xxh, &t.LastModified, &ver)
		if err != nil {
			return nil, err
		}

        if SCHEMA_VER > ver {
            t.Outdated = true
        }

		b = append(b, t)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	return b, nil
}

func (m BookmarkModel) All(ctx *context.Context) ([]TitleHref, error) {
	rows, err := m.DB.QueryContext(*ctx, "SELECT id, title, href, xxh, modified FROM bookmark order by RANDOM()")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

    var b []TitleHref

	for rows.Next() {
        var t TitleHref

		err := rows.Scan(&t.Id, &t.Title, &t.Href, &t.Xxh, &t.LastModified)
		if err != nil {
			return nil, err
		}

		b = append(b, t)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	return b, nil
}
