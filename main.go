// Command upload2dir serves a directory over HTTP with browse, download,
// upload, delete and create-directory support.
//
// It is a standalone replacement for the previous Caddy plugin variant and
// is configured entirely via environment variables:
//
//	PORT            listen port (default: 8080)
//	HOME            root directory to serve (default: current working dir)
//	ADMIN_PASSWORD  if set, DELETE requests must present this value in the
//	                X-Admin-Password header. If unset, deletes are open.
package main

import (
	_ "embed"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

// browseTemplateHTML is the file_server browse page shipped with this repo.
// It is embedded at build time so the compiled binary is fully self-contained.
//
//go:embed template.html
var browseTemplateHTML string

// Server holds the runtime configuration and parsed template.
type Server struct {
	RootDir       string
	AdminPassword string
	Template      *template.Template
}

func main() {
	listen := ":" + getEnv("PORT", "8080")
	adminPassword := os.Getenv("ADMIN_PASSWORD")
	root := getEnv("HOME", ".")

	absRoot, err := filepath.Abs(root)
	if err != nil {
		log.Fatalf("resolve HOME %q: %v", root, err)
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		log.Fatalf("stat root %q: %v", absRoot, err)
	}
	if !info.IsDir() {
		log.Fatalf("root %q is not a directory", absRoot)
	}

	tmpl, err := template.New("browse").Parse(browseTemplateHTML)
	if err != nil {
		log.Fatalf("parse embedded template: %v", err)
	}

	srv := &Server{
		RootDir:       absRoot,
		AdminPassword: adminPassword,
		Template:      tmpl,
	}

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	// Cap in-memory parsed multipart form parts at 32 MiB; the rest spills
	// to a temp file on disk. The actual upload is then streamed to disk.
	r.MaxMultipartMemory = 32 << 20

	// Single wildcard route handles every method/path. We dispatch on
	// method inside handle().
	r.Any("/*reqpath", srv.handle)

	log.Printf("upload2dir listening on %s, serving %s (admin_password set: %t)",
		listen, absRoot, adminPassword != "")

	// http.Server.Handler is set by gin; use ListenAndServe directly via r.Run
	// so we get sensible defaults for signal handling.
	if err := r.Run(listen); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}

func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return v
	}
	return def
}
