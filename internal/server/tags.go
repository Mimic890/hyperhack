package server

import "net/url"

// tagPath is the escaped URL path of a tag page. Tags may contain "/".
func tagPath(tag string) string { return (&url.URL{Path: "/tags/" + tag}).EscapedPath() }
