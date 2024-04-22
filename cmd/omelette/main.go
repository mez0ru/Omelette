package main

import (
	"flag"
	"path/filepath"

	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/mez0ru/omelette/models"
	"github.com/mez0ru/omelette/services"

	"github.com/TwiN/go-color"

	"database/sql"

	"os"

	"github.com/cristalhq/acmd"
	"golang.org/x/net/html"
	"modernc.org/sqlite"

	"github.com/cespare/xxhash"

	"encoding/base64"
	"time"

	"github.com/jaytaylor/html2text"
)

type Env struct {
	log       *log.Logger
	bookmarks interface {
		All(ctx *context.Context) ([]models.TitleHref, error)
		AllUncached(ctx *context.Context) ([]models.TitleHref, error)
		UpdateContentStmt(ctx *context.Context, tx *sql.Tx) (*sql.Stmt, error)
		UpdateContent(ctx *context.Context, id int64, content string, xxh uint64, modified time.Time, stmt *sql.Stmt) (int64, error)
		Init(ctx *context.Context) error
		Insert(ctx *context.Context, entry models.Entry, stmt *sql.Stmt) (int64, error)
		InsertStmt(ctx *context.Context, tx *sql.Tx) (*sql.Stmt, error)
		Transaction() (*sql.Tx, error)
		Search(ctx *context.Context, tokens string) ([]models.SearchResult, error)
	}
}

func (e *Env) importBookmarks(ctx context.Context, args []string) error {
	f, err := os.Open(args[0])

	if err != nil {
		return err
	}

	doc, err := html.Parse(f)
	if err != nil {
		return err
	}

	if doc.FirstChild != nil && doc.FirstChild.Data != "netscape-bookmark-file-1" {
		return errors.New("Bookmark file is not a valid netscape bookmark html file.")
	}

	err = e.bookmarks.Init(&ctx)
	if err != nil {
		return err
	}

	tx, err := e.bookmarks.Transaction()
	stmt, err := e.bookmarks.InsertStmt(&ctx, tx)
	if err != nil {
		return err
	}

	var entry models.Entry
	var savedAnchor *html.Node

	var fu func(*html.Node) error
	fu = func(n *html.Node) error {
		if n.Type == html.ElementNode && n.Data == "a" {
			entry = models.Entry{}
			for _, a := range n.Attr {
				switch a.Key {
				case "href":
					if !strings.HasPrefix(a.Val, "http") {
						return nil
					}
					entry.Href = a.Val
				case "add_date":
					d, err := strconv.Atoi(a.Val)
					if err == nil {
						entry.Date = d
					}
				case "icon":
					icon, found := strings.CutPrefix(a.Val, "data:image/png;base64,")
					if !found {
						continue
					}

					decoded, err := base64.StdEncoding.DecodeString(icon)
					if err != nil {
						continue
					}

					entry.Icon = decoded
				}
			}

			savedAnchor = n
		}

		if savedAnchor != nil {
			if n.Type == html.TextNode {
				entry.Title = n.Data
				_, err = e.bookmarks.Insert(&ctx, entry, stmt)

				if sqlerror, ok := err.(*sqlite.Error); ok {
					if sqlerror.Code() != 2067 {
						return err
					}
				}
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			err := fu(c)
			if err != nil {
				return err
			}
		}

		if n == savedAnchor {
			savedAnchor = nil
		}

		return nil
	}

	err = fu(doc)
	if err != nil {
		return err
	}

	err = tx.Commit()
	if err != nil {
		return err
	}

	return nil
}

type fetchFlags struct {
	Threads      int
	Retries      int
	UncachedOnly bool
	Timeout      int
	Overwrite    bool
    Outdated bool
}

func (c *fetchFlags) Flags() *flag.FlagSet {
	fs := flag.NewFlagSet("", flag.ContinueOnError)
	fs.BoolVar(&c.UncachedOnly, "uncached", false, "Fetch Uncached bookmarks only.")
	fs.IntVar(&c.Threads, "threads", 6, "Specify how many concurrent connections")
	fs.IntVar(&c.Retries, "retry", 2, "Retries if failed")
	fs.IntVar(&c.Timeout, "timeout", 10, "timeout in seconds")
    fs.BoolVar(&c.Overwrite, "overwrite", false, "Overwrite identical cached version of the website")
    fs.BoolVar(&c.Outdated, "outdated", true, "Overwrite outdated implementations of these websites only")
	return fs
}

func (e *Env) fetch(ctx context.Context, args []string) error {
	var cfg fetchFlags
	err := cfg.Flags().Parse(args)
	if err != nil {
		return err
	}

	var bookmarks []models.TitleHref
	if cfg.UncachedOnly {
		bookmarks, err = e.bookmarks.AllUncached(&ctx)
	} else {
		bookmarks, err = e.bookmarks.All(&ctx)
	}

	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	guard := make(chan struct{}, cfg.Threads)

	tx, err := e.bookmarks.Transaction()
	if err != nil {
		return err
	}

	stmt, err := e.bookmarks.UpdateContentStmt(&ctx, tx)
	if err != nil {
		return err
	}

	customTransport := http.DefaultTransport.(*http.Transport).Clone()
	customTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	client := &http.Client{
		Timeout:   time.Duration(cfg.Timeout) * time.Second,
		Transport: customTransport,
	}

	for _, bookmark := range bookmarks {
        if cfg.Outdated && !services.IsOutdated(bookmark.Href, bookmark.Version) {
            continue
        }
		guard <- struct{}{} // would block if guard channel is already filled
		wg.Add(1)

        if cfg.Overwrite || cfg.Outdated {
            bookmark.Xxh = 0
            bookmark.LastModified = time.Time{}
        }
		go e.processWebsite(&wg, guard, &ctx, stmt, client, &bookmark, cfg.UncachedOnly, cfg.Retries-1)
	}

	wg.Wait()

	err = tx.Commit()
	if err != nil {
		return err
	}

	return nil
}

func (e *Env) fetchWebsite(client *http.Client, href string, if_modified_since time.Time) ([]byte, time.Time, error) {
    href = services.SpecializedWebsite(href)
	req, err := http.NewRequest("GET", href, nil)
	if err != nil {
		return nil, time.Time{}, err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36")
    services.SpecializedHeaders(req)
	if !if_modified_since.IsZero() {
		req.Header.Set("If-Modified-Since", if_modified_since.Format(time.RFC1123))
	}

	resp, err := client.Do(req)

	if err != nil {
		return nil, time.Time{}, err
	}

	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return nil, if_modified_since, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, time.Time{}, fmt.Errorf("returned Status code %d.", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, time.Time{}, err
	}

	m := resp.Header.Get("Last-Modified")
	modified := time.Now()
	if m != "" {
		modified, _ = time.Parse(time.RFC1123, m)
	}

	return body, modified, nil
}

// Automatic free is about setting the goroutine free from the thread
// limit imposed to lessen the time in case a function failed to fetch from the first time
func (e *Env) processWebsite(wg *sync.WaitGroup, ch <-chan struct{}, ctx *context.Context,
	stmt *sql.Stmt, client *http.Client, th *models.TitleHref, disableAutomaticFree bool,
	retry int) {
	is_free := false
	defer func() {
		if !is_free || disableAutomaticFree {
			<-ch
		}
		wg.Done()
	}()

	var content []byte
	var modified time.Time
	var err error
	for retries, ok := 0, true; ok; retries, ok = retries+1, (err != nil && retries < retry) {
		if retries > 0 {
			time.Sleep(3 * time.Second)
		}

		if retries == 1 && !disableAutomaticFree {
			// Don't sufficate the next queue from executing.
			is_free = true
			<-ch
		}

		content, modified, err = e.fetchWebsite(client, th.Href, th.LastModified)
		if err != nil {
			if strings.Contains(err.Error(), "forcibly closed") {
				e.log.Printf("%s%d. Possibly blocked by a firewall? \"%s\", %+v\n%s", color.Yellow, th.Id, th.Title, err, color.Reset)
			} else {
				e.log.Printf("%s%d. Error fetching \"%s\", retrying again in 3 seconds...\n%s", color.Red, th.Id, th.Title, color.Reset)
			}
		}
	}

	if err != nil {
		e.log.Printf("%s%d. Could not fetch \"%s\", %+v\n%s", color.Red, th.Id, th.Title, err, color.Reset)
		return
	}

	if modified == th.LastModified {
		e.log.Printf("%s%d. Not modified \"%s\"\n%s", color.Blue, th.Id, th.Title, color.Reset)
		return
	}

	xxh := xxhash.Sum64(content)

	e.log.Printf("%s%d. Fetched \"%s\" Successfully.\n%s", color.Green, th.Id, th.Title, color.Reset)

	if xxh == uint64(th.Xxh) {
		e.log.Printf("%s%d. Hash match! \"%s\"\n%s", color.Blue, th.Id, th.Title, color.Reset)
		return
	}

	html := string(content[:])
	parsed, ok := services.SpecializedParser(html, th.Href)
	if !ok {
		parsed, err = html2text.FromString(html, html2text.Options{PrettyTables: false, TextOnly: true, OmitLinks: true})
		if err != nil {
			e.log.Fatalf("%s%d. Error converting website to text \"%s\", %+v\n%s", color.Red, th.Id, th.Title, err, color.Reset)
		}
	}

	_, err = e.bookmarks.UpdateContent(ctx, th.Id, services.StandardizeSpaces(parsed), xxh, modified, stmt)
	if err != nil {
		e.log.Fatalf("%d. Error inserting \"%s\", %+v\n", th.Id, th.Title, err)
	}
}

func (e *Env) search(ctx context.Context, args []string) error {
	query, err := e.bookmarks.Search(&ctx, strings.Join(args, " "))
	if err != nil {
		return err
	}

	fmt.Printf("Found %s%d%s search results!\n\n", color.Yellow, len(query), color.Reset)
	for _, q := range query {
		fmt.Printf("%d. %s\n", q.Id, q.Title)
		fmt.Printf("%s\n", q.Href)
		fmt.Printf("\"%s\"\n\n", q.Content)
	}

	return nil
}

func main() {
	log := log.Default()

	configDir, err := os.UserConfigDir()
	if err != nil {
		panic(err)
	}
	configDir = filepath.Join(configDir, "omelette")

	_, err = os.Stat(configDir)
	if os.IsNotExist(err) {
		err = os.Mkdir(configDir, 0700)
		if err != nil {
			panic(err)
		}
	}

	db, err := sql.Open("sqlite", filepath.Join(configDir, "data.db"))
	// db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		panic(err)
	}

	defer db.Close()

	e := &Env{
		bookmarks: models.BookmarkModel{DB: db},
		log:       log,
	}

	cmds := []acmd.Command{
		{
			Name:        "import",
			Description: "import a bookmark html file",
			ExecFunc:    e.importBookmarks,
		},
		{
			Name:        "search",
			Description: "Fuzzy find bookmarks",
			ExecFunc:    e.search,
		},
		{
			Name:        "fetch",
			Description: "fetch all bookmarks",
			ExecFunc:    e.fetch,
		},
	}

	r := acmd.RunnerOf(cmds, acmd.Config{
		AppName:        "Omelette",
		AppDescription: "Bookmark manager",
		Version:        "0.1.0",
		// For testing purposes:
		// Args:           []string{"omelette", "fetch"},
		// Args: []string{"omelette", "fetch", "-uncached"},
		// Args: []string{"omelette", "search", "subs"},
		// Args:           []string{"omelette", "import", "bookmarks-test.html"},
	})

	start := time.Now()
	if err := r.Run(); err != nil {
		e.log.Fatalf("%+v", err)
	}

	end := time.Now()
	e.log.Printf("finished task in %f seconds!", end.Sub(start).Seconds())
}
