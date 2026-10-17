package main

import (
	"archive/zip"
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

// Auth cookie set by the client itself after a successful password check.
const authCookieName = "simple_file_browser_auth"

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
		switch strings.ToLower(c.Query("action")) {
		case "zip":
			s.handleZip(c, urlPath)
		case "unzip":
			s.handleUnzip(c, urlPath)
		default:
			s.handleCreateDir(c, urlPath)
		}
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
	// When RootDir is the filesystem root, rootClean+"/" would be "//",
	// which no real path starts with. Build the prefix accordingly.
	rootPrefix := rootClean
	if rootClean != string(filepath.Separator) {
		rootPrefix = rootClean + string(filepath.Separator)
	}
	if fullClean != rootClean && !strings.HasPrefix(fullClean, rootPrefix) {
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
// When ADMIN_PASSWORD is configured, the client must either present the
// simple_file_browser_auth cookie (set by the client itself after a successful
// password check) or supply the matching value via the X-Admin-Password
// header; comparison is constant-time.
func (s *Server) handleDelete(c *gin.Context, urlPath string) {
	if s.AdminPassword != "" {
		cookie, err := c.Request.Cookie(authCookieName)
		if err != nil || cookie.Value == "" {
			provided := c.GetHeader("X-Admin-Password")
			if subtle.ConstantTimeCompare([]byte(provided), []byte(s.AdminPassword)) != 1 {
				c.String(http.StatusForbidden, "invalid admin password")
				return
			}
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

// nonConflictingPath returns candidate unchanged if nothing exists there,
// otherwise it appends a "-{unix_nano}" suffix so an existing file or
// directory is never overwritten (mirrors handleUpload's behaviour).
func nonConflictingPath(candidate string) string {
	if _, err := os.Stat(candidate); err != nil {
		// Treat any stat error (including not-exist) as "free to use".
		return candidate
	}
	return fmt.Sprintf("%s-%d", candidate, time.Now().UnixNano())
}

// handleZip compresses the item at urlPath into "{name}.zip" placed in the
// same directory. When that archive name already exists, a timestamp suffix
// is appended. Both single files and directories (recursively) are supported;
// archive entry names are relative to the source's parent so the archive
// contains the item under its own name.
func (s *Server) handleZip(c *gin.Context, urlPath string) {
	src, err := s.resolve(urlPath)
	if err != nil {
		c.String(http.StatusBadRequest, "%v", err)
		return
	}
	if src == filepath.Clean(s.RootDir) {
		c.String(http.StatusForbidden, "cannot zip root")
		return
	}
	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			c.String(http.StatusNotFound, "not found")
			return
		}
		c.String(http.StatusInternalServerError, "stat %s: %v", src, err)
		return
	}

	parent := filepath.Dir(src)
	base := filepath.Base(src)
	out := nonConflictingPath(filepath.Join(parent, base+".zip"))

	// O_EXCL guards against a concurrent request creating the same target.
	zf, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		c.String(http.StatusInternalServerError, "create archive %s: %v", out, err)
		return
	}
	zw := zip.NewWriter(zf)

	walkErr := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Never archive the output file itself (it lives in parent, so it
		// won't be visited when zipping a single file, but guard anyway).
		if p == out {
			return nil
		}
		// Entry name relative to the source parent, using forward slashes.
		rel, relErr := filepath.Rel(parent, p)
		if relErr != nil {
			return relErr
		}
		name := filepath.ToSlash(rel)

		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		// Skip anything that isn't a regular file or directory (e.g. symlinks,
		// devices) to keep archives portable and avoid following links.
		if !info.Mode().IsRegular() && !d.IsDir() {
			return nil
		}

		hdr, hdrErr := zip.FileInfoHeader(info)
		if hdrErr != nil {
			return hdrErr
		}
		hdr.Name = name
		if d.IsDir() {
			hdr.Name += "/"
			hdr.Method = zip.Store
		} else {
			hdr.Method = zip.Deflate
		}

		w, createErr := zw.CreateHeader(hdr)
		if createErr != nil {
			return createErr
		}
		if d.IsDir() {
			return nil
		}
		in, openErr := os.Open(p)
		if openErr != nil {
			return openErr
		}
		defer in.Close()
		_, copyErr := io.Copy(w, in)
		return copyErr
	})

	if walkErr != nil {
		zw.Close()
		zf.Close()
		os.Remove(out)
		c.String(http.StatusInternalServerError, "zip %s: %v", base, walkErr)
		return
	}
	if err := zw.Close(); err != nil {
		zf.Close()
		os.Remove(out)
		c.String(http.StatusInternalServerError, "finalize archive: %v", err)
		return
	}
	if err := zf.Close(); err != nil {
		os.Remove(out)
		c.String(http.StatusInternalServerError, "close archive: %v", err)
		return
	}
	c.Status(http.StatusOK)
}

// handleUnzip extracts the .zip file at urlPath into a new folder (named via
// the "dest" query param) created in the same directory as the archive. When
// the destination folder name already exists, a timestamp suffix is appended.
// Every archive entry is validated to resolve inside the destination folder
// (zip-slip protection) before anything is written.
func (s *Server) handleUnzip(c *gin.Context, urlPath string) {
	src, err := s.resolve(urlPath)
	if err != nil {
		c.String(http.StatusBadRequest, "%v", err)
		return
	}
	info, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			c.String(http.StatusNotFound, "not found")
			return
		}
		c.String(http.StatusInternalServerError, "stat %s: %v", src, err)
		return
	}
	if !info.Mode().IsRegular() {
		c.String(http.StatusBadRequest, "not a file")
		return
	}
	if !strings.EqualFold(filepath.Ext(src), ".zip") {
		c.String(http.StatusBadRequest, "not a zip file")
		return
	}

	// dest is a plain folder name; strip any path components a client may
	// have supplied so extraction always lands beside the archive.
	destName := filepath.Base(filepath.FromSlash(strings.TrimSpace(c.Query("dest"))))
	if destName == "" || destName == "." || destName == string(filepath.Separator) {
		c.String(http.StatusBadRequest, "missing dest folder name")
		return
	}

	parent := filepath.Dir(src)
	destDir := nonConflictingPath(filepath.Join(parent, destName))
	// Defence in depth: make sure the destination is still under root.
	if destDir != filepath.Clean(s.RootDir) &&
		!strings.HasPrefix(destDir, filepath.Clean(s.RootDir)+string(filepath.Separator)) {
		c.String(http.StatusBadRequest, "dest escapes root")
		return
	}

	zr, err := zip.OpenReader(src)
	if err != nil {
		c.String(http.StatusBadRequest, "open zip: %v", err)
		return
	}
	defer zr.Close()

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		c.String(http.StatusInternalServerError, "mkdir %s: %v", destDir, err)
		return
	}
	destPrefix := destDir + string(filepath.Separator)

	for _, f := range zr.File {
		name := f.Name
		// Reject absolute paths and any traversal in the raw entry name
		// before we touch the filesystem.
		if strings.HasPrefix(name, "/") || strings.HasPrefix(name, "\\") {
			c.String(http.StatusBadRequest, "zip entry %q has absolute path", f.Name)
			return
		}
		target := filepath.Join(destDir, filepath.FromSlash(name))
		targetClean := filepath.Clean(target)
		if targetClean != filepath.Clean(destDir) &&
			!strings.HasPrefix(targetClean, destPrefix) {
			c.String(http.StatusBadRequest, "zip entry %q escapes destination", f.Name)
			return
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(targetClean, 0o755); err != nil {
				c.String(http.StatusInternalServerError, "mkdir %s: %v", targetClean, err)
				return
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(targetClean), 0o755); err != nil {
			c.String(http.StatusInternalServerError, "mkdir parent of %s: %v", targetClean, err)
			return
		}
		if err := extractZipEntry(f, targetClean); err != nil {
			c.String(http.StatusInternalServerError, "extract %q: %v", f.Name, err)
			return
		}
	}
	c.Status(http.StatusOK)
}

// extractZipEntry streams a single zip entry to the target path on disk.
func extractZipEntry(f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}
