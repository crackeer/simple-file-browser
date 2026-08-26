package main

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// handle dispatches a request to the appropriate handler based on HTTP method.
// The URL path (relative to the root directory) is passed via the "reqpath"
// gin route parameter and always starts with "/".
func (s *Server) handle(c *gin.Context) {
	urlPath := c.Param("reqpath")
	if urlPath == "" {
		urlPath = "/"
	}

	switch c.Request.Method {
	case http.MethodGet, http.MethodHead:
		s.handleGet(c, urlPath)
	case http.MethodPost:
		s.handleCreateDir(c, urlPath)
	case http.MethodPut:
		s.handleUpload(c, urlPath)
	case http.MethodDelete:
		s.handleDelete(c, urlPath)
	case http.MethodOptions:
		c.Header("Allow", "GET, HEAD, POST, PUT, DELETE, OPTIONS")
		c.Status(http.StatusNoContent)
	default:
		c.String(http.StatusMethodNotAllowed, "method not allowed")
	}
}

// resolve maps an incoming URL path to an absolute filesystem path under
// RootDir and refuses any path that would escape the root.
func (s *Server) resolve(urlPath string) (string, error) {
	// path.Clean normalises "..", "./", double slashes, etc.
	cleaned := path.Clean("/" + strings.TrimPrefix(urlPath, "/"))
	rel := strings.TrimPrefix(cleaned, "/")
	full := filepath.Join(s.RootDir, filepath.FromSlash(rel))

	fullClean := filepath.Clean(full)
	rootClean := filepath.Clean(s.RootDir)
	if fullClean != rootClean && !strings.HasPrefix(fullClean, rootClean+string(filepath.Separator)) {
		return "", errors.New("path escapes root")
	}
	return fullClean, nil
}

// handleGet serves either a directory listing (rendered via the embedded
// browse template) or a single file via http.ServeFile which handles
// Range/If-Modified-Since/Content-Type automatically.
func (s *Server) handleGet(c *gin.Context, urlPath string) {
	fsPath, err := s.resolve(urlPath)
	if err != nil {
		c.String(http.StatusBadRequest, "%v", err)
		return
	}
	info, err := os.Stat(fsPath)
	if err != nil {
		if os.IsNotExist(err) {
			c.String(http.StatusNotFound, "not found")
			return
		}
		c.String(http.StatusInternalServerError, "%v", err)
		return
	}
	if info.IsDir() {
		// Ensure directory URLs end with "/" so relative hrefs in the
		// browse page resolve correctly.
		if !strings.HasSuffix(c.Request.URL.Path, "/") {
			target := c.Request.URL.Path + "/"
			if c.Request.URL.RawQuery != "" {
				target += "?" + c.Request.URL.RawQuery
			}
			c.Redirect(http.StatusMovedPermanently, target)
			return
		}
		s.renderBrowse(c, fsPath, urlPath)
		return
	}
	http.ServeFile(c.Writer, c.Request, fsPath)
}

// handleCreateDir is idempotent: creating an existing directory returns 200,
// and any missing parent directories are created too.
func (s *Server) handleCreateDir(c *gin.Context, urlPath string) {
	dest, err := s.resolve(urlPath)
	if err != nil {
		c.String(http.StatusBadRequest, "%v", err)
		return
	}
	if dest == filepath.Clean(s.RootDir) {
		c.Status(http.StatusOK)
		return
	}

	info, err := os.Stat(dest)
	if err == nil {
		if !info.IsDir() {
			c.String(http.StatusConflict, "%s exists and is not a directory", filepath.Base(dest))
			return
		}
		c.Status(http.StatusOK)
		return
	}
	if !os.IsNotExist(err) {
		c.String(http.StatusInternalServerError, "stat error: %v", err)
		return
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		c.String(http.StatusInternalServerError, "mkdir error: %v", err)
		return
	}
	c.Status(http.StatusOK)
}

// handleUpload stores an uploaded file at the URL's target path. When a file
// with the same name already exists, the upload is stored under
// "{name}-{unix_nano}" so the existing file is never overwritten.
func (s *Server) handleUpload(c *gin.Context, urlPath string) {
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.String(http.StatusBadRequest, "form file 'file': %v", err)
		return
	}
	defer file.Close()

	dest, err := s.resolve(urlPath)
	if err != nil {
		c.String(http.StatusBadRequest, "%v", err)
		return
	}

	// If the URL points to an existing directory (e.g. PUT /some/dir/),
	// derive the filename from the multipart header.
	if info, statErr := os.Stat(dest); statErr == nil && info.IsDir() {
		dest = filepath.Join(dest, filepath.Base(header.Filename))
	}

	destDir, fileName := filepath.Split(dest)
	if fileName == "" {
		fileName = filepath.Base(header.Filename)
		dest = filepath.Join(destDir, fileName)
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		c.String(http.StatusInternalServerError, "mkdirall %s: %v", destDir, err)
		return
	}
	if _, err := os.Stat(dest); err == nil {
		// Preserve the existing file and store this upload under a
		// timestamped name.
		dest = filepath.Join(destDir, fmt.Sprintf("%s-%d", fileName, time.Now().UnixNano()))
	}

	// O_EXCL guards against a concurrent request that created the same
	// target after our stat above.
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		c.String(http.StatusInternalServerError, "open %s: %v", dest, err)
		return
	}
	defer out.Close()

	if _, err := io.Copy(out, file); err != nil {
		c.String(http.StatusInternalServerError, "copy: %v", err)
		return
	}
	c.Status(http.StatusOK)
}

// handleDelete removes a file or directory (including non-empty directories).
// When ADMIN_PASSWORD is configured, the client must supply a matching value
// via the X-Admin-Password header; comparison is constant-time.
func (s *Server) handleDelete(c *gin.Context, urlPath string) {
	if s.AdminPassword != "" {
		provided := c.GetHeader("X-Admin-Password")
		if subtle.ConstantTimeCompare([]byte(provided), []byte(s.AdminPassword)) != 1 {
			c.String(http.StatusForbidden, "invalid admin password")
			return
		}
	}
	dest, err := s.resolve(urlPath)
	if err != nil {
		c.String(http.StatusBadRequest, "%v", err)
		return
	}
	// Refuse to delete the served root itself.
	if dest == filepath.Clean(s.RootDir) {
		c.String(http.StatusForbidden, "cannot delete root")
		return
	}
	if err := os.RemoveAll(dest); err != nil {
		c.String(http.StatusInternalServerError, "delete %s: %v", dest, err)
		return
	}
	c.Status(http.StatusOK)
}
