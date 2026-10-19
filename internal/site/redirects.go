package site

import (
	"errors"
	"regexp"
)

var redirectPath = regexp.MustCompile(`^/(en|fi)/((services|projects)/[a-z0-9][a-z0-9-]{0,79}/|(privacy|cookies|terms)/)?$`)

func validateRedirects(rows []Redirect) error {
	if len(rows) > 250 {
		return errors.New("maximum 250 redirects")
	}
	paths := map[string]string{}
	for _, r := range rows {
		if !redirectPath.MatchString(r.From) || !redirectPath.MatchString(r.To) || r.From == r.To || paths[r.From] != "" {
			return errors.New("redirects require unique internal EN/FI paths ending in / and cannot point to themselves")
		}
		paths[r.From] = r.To
	}
	for from := range paths {
		seen := map[string]bool{}
		for current := from; paths[current] != ""; current = paths[current] {
			if seen[current] {
				return errors.New("redirect cycle detected")
			}
			seen[current] = true
		}
	}
	return nil
}
func autoRedirects(old State, next *State) {
	for _, group := range []struct {
		kind          string
		before, after []Item
	}{{"services", old.Services, next.Services}, {"projects", old.Projects, next.Projects}} {
		for _, before := range group.before {
			for _, after := range group.after {
				if before.ID != after.ID || before.Slug == after.Slug || !before.Enabled || !after.Enabled {
					continue
				}
				for _, lang := range []string{"en", "fi"} {
					from := "/" + lang + "/" + group.kind + "/" + before.Slug + "/"
					to := "/" + lang + "/" + group.kind + "/" + after.Slug + "/"
					filtered := []Redirect{}
					for _, r := range next.Redirects {
						if r.From != to {
							filtered = append(filtered, r)
						}
					}
					next.Redirects = filtered
					exists := false
					for i := range next.Redirects {
						if next.Redirects[i].To == from {
							next.Redirects[i].To = to
						}
						if next.Redirects[i].From == from {
							next.Redirects[i].To = to
							exists = true
						}
					}
					if !exists {
						next.Redirects = append(next.Redirects, Redirect{from, to})
					}
				}
			}
		}
	}
}
