package hls

import (
	"bufio"
	"io"
	"net/url"
	"regexp"
	"strings"
)

func deleteAfterLastSlash(str string) string {
	return str[0 : strings.LastIndex(str, "/")+1]
}

var reURILinkExtract = regexp.MustCompile(`URI="([^"]*)"`)

func rewriteLinks(rbody *io.ReadCloser, prefix, linkRoot string) string {
	var sb strings.Builder
	scanner := bufio.NewScanner(*rbody)
	// Default Scanner token cap is 64KB; a single very long M3U8 line (e.g. a
	// segment URL with a large signed query string) would otherwise be
	// silently dropped. Allow up to 1MB per line.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	linkRootURL, _ := url.Parse(linkRoot) // It will act as a base URL for full URLs

	modifyLink := func(link string) string {
		var l string

		switch {
		case strings.HasPrefix(link, "//"):
			tmpURL, _ := url.Parse(link)
			tmp2URL, _ := url.Parse(tmpURL.RequestURI())
			link = (linkRootURL.ResolveReference(tmp2URL)).String()
			l = strings.ReplaceAll(link, linkRoot, "")
		case strings.HasPrefix(link, "/"):
			tmp2URL, _ := url.Parse(link)
			link = (linkRootURL.ResolveReference(tmp2URL)).String()
			l = strings.ReplaceAll(link, linkRoot, "")
		default:
			l = link
		}

		newurl, _ := url.Parse(prefix + l)
		return newurl.RequestURI()
	}

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "#") {
			line = modifyLink(line)
		} else if strings.Contains(line, "URI=\"") && !strings.Contains(line, "URI=\"\"") {
			// A line containing `URI="` but no closing quote (malformed
			// playlist) yields no submatch — indexing [1] then panicked.
			if m := reURILinkExtract.FindStringSubmatch(line); m != nil {
				line = reURILinkExtract.ReplaceAllString(line, `URI="`+modifyLink(m[1])+`"`)
			}
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}

	return sb.String()
}
