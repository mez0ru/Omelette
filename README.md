# Omelette
 Bookmark Manager that doesn't suck :/

## Installation
```
go install github.com/mez0ru/omelette@main
```

## Usage
```
$ ./Omelette help
Bookmark manager

Usage:

    Omelette <command> [arguments...]

The commands are:

    fetch             fetch all bookmarks
    help              shows help message
    import            import a bookmark html file
    search            Fuzzy find bookmarks
    version           shows version of the application

Version: 0.1.0

2024/04/19 09:37:41 finished task in 0.001040 seconds!
```

## Goals
The idea behind the app is to store your bookmarks, and after fetching the content,
you could fuzzy find your bookmarks using their url, title, and content.

The app is currently usable and stable (tested on +2000 bookmarks)
Though, it's still pretty slow, and doesn't fetch a lot of websites
as they require authentication to even scrape "surprise", so it's kinda lacking.

Next update hopefully will solve this problem, since it could use the
chromium's cookies file to get the login credentials automatically.

But from what I have tried, this is by far satisfactory.
It does a decent job of searching through my bookmarks quickly.
