package main

import (
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// Crumb is one segment of the breadcrumb trail displayed at the top of
// each browse page. Link is a relative URL that navigates to that
// ancestor directory.
type Crumb struct {
	Link string
	Text string
}

// BrowseItem is one row in the browse listing. Field names/methods
// match the caddy file_server browse template so template.html can be
// reused unchanged.
type BrowseItem struct {
	Name      string
	URL       string
	IsDir     bool
	IsSymlink bool
	Size      int64

	modTime time.Time
}

// HumanSize is invoked by the browse template as `{{.HumanSize}}`.
func (b BrowseItem) HumanSize() string {
	return humanBytes(b.Size)
}

// HumanModTime is invoked by the browse template as
// `{{.HumanModTime "layout"}}` — it accepts a Go time layout string.
func (b BrowseItem) HumanModTime(layout string) string {
	return b.modTime.Format(strings.TrimSpace(layout))
}

// BrowseData is the template context for template.html.
type BrowseData struct {
	Name        string
	Path        string
	Breadcrumbs []Crumb
	NumDirs     int
	NumFiles    int
	CanGoUp     bool
	Items       []BrowseItem
	Sort        string
	Order       string
	Limit       int
	Offset      int
}

// renderBrowse reads dirPath, applies sort/pagination from query params
// and executes the embedded template into the response.
func (s *Server) renderBrowse(c *gin.Context, dirPath, urlPath string) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		c.String(http.StatusInternalServerError, "read dir: %v", err)
		return
	}

	items := make([]BrowseItem, 0, len(entries))
	var numDirs, numFiles int
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			// Skip entries we can't stat (e.g. broken symlink races).
			continue
		}
		isSymlink := info.Mode()&fs.ModeSymlink != 0
		isDir := info.IsDir()
		// A symlink target may itself be a directory. Follow it so the
		// UI can show a folder icon and append a trailing slash.
		if isSymlink {
			if target, err := os.Stat(path.Join(dirPath, e.Name())); err == nil {
				isDir = target.IsDir()
			}
		}
		it := BrowseItem{
			Name:      e.Name(),
			IsDir:     isDir,
			IsSymlink: isSymlink,
			Size:      info.Size(),
			modTime:   info.ModTime(),
		}
		// URL is relative to the current directory; the browse page URL
		// always ends with "/" (see handleGet redirect) so this produces
		// the correct absolute path client-side.
		u := url.PathEscape(e.Name())
		if it.IsDir {
			u += "/"
		}
		it.URL = u

		if it.IsDir {
			numDirs++
		} else {
			numFiles++
		}
		items = append(items, it)
	}

	sortKey := strings.ToLower(c.DefaultQuery("sort", "namedirfirst"))
	order := strings.ToLower(c.DefaultQuery("order", "asc"))
	sortItems(items, sortKey, order)

	limit, _ := strconv.Atoi(c.Query("limit"))
	offset, _ := strconv.Atoi(c.Query("offset"))
	if limit < 0 {
		limit = 0
	}
	if offset < 0 {
		offset = 0
	}
	displayed := items
	if offset > 0 {
		if offset >= len(displayed) {
			displayed = nil
		} else {
			displayed = displayed[offset:]
		}
	}
	if limit > 0 && limit < len(displayed) {
		displayed = displayed[:limit]
	}

	data := BrowseData{
		Name:        pathBase(urlPath),
		Path:        urlPath,
		Breadcrumbs: buildBreadcrumbs(urlPath),
		NumDirs:     numDirs,
		NumFiles:    numFiles,
		CanGoUp:     strings.Trim(urlPath, "/") != "",
		Items:       displayed,
		Sort:        sortKey,
		Order:       order,
		Limit:       limit,
		Offset:      offset,
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := s.Template.Execute(c.Writer, data); err != nil {
		// The headers/body have likely already been flushed at this
		// point, so we can only log.
		fmt.Fprintf(os.Stderr, "template execute error: %v\n", err)
	}
}

// buildBreadcrumbs mirrors caddy's browse breadcrumb layout: the first
// crumb is the root ("/") linking back to the served root, followed by
// one crumb per path segment, each linking to its ancestor via "../"
// jumps relative to the current directory.
func buildBreadcrumbs(urlPath string) []Crumb {
	trimmed := strings.Trim(urlPath, "/")
	if trimmed == "" {
		return []Crumb{{Link: "", Text: "/"}}
	}
	parts := strings.Split(trimmed, "/")
	out := make([]Crumb, 0, len(parts)+1)
	out = append(out, Crumb{
		Link: strings.Repeat("../", len(parts)),
		Text: "/",
	})
	for i, p := range parts {
		out = append(out, Crumb{
			Link: strings.Repeat("../", len(parts)-i-1),
			Text: p,
		})
	}
	return out
}

func pathBase(p string) string {
	trimmed := strings.TrimRight(p, "/")
	if trimmed == "" {
		return "/"
	}
	return path.Base(trimmed)
}

func sortItems(items []BrowseItem, key, order string) {
	less := func(i, j int) bool {
		switch key {
		case "name":
			return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
		case "size":
			return items[i].Size < items[j].Size
		case "time":
			return items[i].modTime.Before(items[j].modTime)
		default: // "namedirfirst"
			if items[i].IsDir != items[j].IsDir {
				return items[i].IsDir
			}
			return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
		}
	}
	if order == "desc" {
		sort.SliceStable(items, func(i, j int) bool { return less(j, i) })
	} else {
		sort.SliceStable(items, less)
	}
}

// humanBytes formats a byte count using binary (KiB/MiB/...) units, which
// matches what caddy's file_server browse page produces.
func humanBytes(n int64) string {
	const (
		KB int64 = 1024
		MB       = 1024 * KB
		GB       = 1024 * MB
		TB       = 1024 * GB
	)
	f := float64(n)
	switch {
	case n >= TB:
		return fmt.Sprintf("%.2f TiB", f/float64(TB))
	case n >= GB:
		return fmt.Sprintf("%.2f GiB", f/float64(GB))
	case n >= MB:
		return fmt.Sprintf("%.2f MiB", f/float64(MB))
	case n >= KB:
		return fmt.Sprintf("%.2f KiB", f/float64(KB))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
