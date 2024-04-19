package services

import (
	"regexp"
	"strings"
)

var youtubeRe = regexp.MustCompile(`^https:\/\/(?:www\.)?(youtube\.com\/watch\?v\=|youtu\.be\/)`)
var redditRe = regexp.MustCompile(`^https://(?:.*\.)?reddit.com/r/.*\/comments\/`)
        var youtubeDescription = regexp.MustCompile(`(?:"shortDescription":")(.*)","isCrawlable"`)
var redditBodies = regexp.MustCompile(`(?:"body": ")(.*?)", "edited"`)

// https://stackoverflow.com/a/42251527/3436434
func StandardizeSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func SpecializedWebsite(href string) string {
    if redditRe.MatchString(href) {
        return href+"/.json"
    }

    return href
}

func SpecializedParser(html string, href string) (string, bool) {
    if youtubeRe.MatchString(href) {
        // Since youtube is uncrawable, it's best to just get the description of the video.
        description := youtubeDescription.FindStringSubmatch(html) 
        if len(description) > 1{
            return StandardizeSpaces(description[1]), true
        }
        return "", false
    }

    if redditRe.MatchString(href) {
        bodies := redditBodies.FindAllStringSubmatch(html, -1)
        all := ""
        for _, body := range(bodies) {
            if len(body) > 1 && body[1] != "[deleted]" {
                all += body[1]
            }
        }

        return all, true
    }

    // ... More parsers will be added when deemed necessary

    return "", false
}
