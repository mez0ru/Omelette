package main

import (
	"database/sql"
	"github.com/mez0ru/omelette/models"
	"testing"
	"time"
)


func TestImportBookmarks(t *testing.T) {
    db, err := sql.Open("sqlite", ":memory:")
	// db, err := sql.Open("sqlite", ":memory:")
	defer db.Close()
	if err != nil {
        t.Fatalf("ImportBookmarks error creating sqlite memory database, %+v", err)
	}

	e := &Env{
		bookmarks: models.BookmarkModel{DB: db},
	}

    err = e.importBookmarks(nil, []string{"../bookmarks-test.html"})
    if err != nil {
        t.Fatalf("ImportBookmarks error, %+v", err)
    }

    res := db.QueryRow(`
        select title, href, date from bookmark
        where id = ?;
    `, 1)

    var title, href string
    var date time.Time
    err = res.Scan(&title, &href, &date)
    if err != nil {
        t.Fatalf("ImportBookmarks Error parsing sql results %+v", err)
    }

    wantTitle := "How to send HTTP request GET/POST in Java – Mkyong.com"
    if title != wantTitle {
        t.Fatalf("ImportBookmarks returned with title \"%s\", but want \"%s\"", title, wantTitle)
    }

    wantHref := "https://www.mkyong.com/java/how-to-send-http-request-getpost-in-java/"
    if href != wantHref {
        t.Fatalf("ImportBookmarks returned with href \"%s\", but want \"%s\"", href, wantHref)
    }

    // wantDate, _ := time.Parse(time.RFC3339, "2018-08-09T19:08:38Z00:00")
    // if date != wantDate {
    //     t.Fatalf("ImportBookmarks returned with date \"%v\", but want \"%v\"", date, wantDate)
    // }

    res = db.QueryRow("select count(*) from bookmark")
    var count int
    err = res.Scan(&count)
    if err != nil {
        t.Fatalf("ImportBookmarks Error parsing sql results %+v", err)
    }

    wantCount := 59
    if count != wantCount {
        t.Fatalf("ImportBookmarks returned with count \"%d\", but want \"%d\"", count, wantCount)
    }
}
