// Command simple-file-browser serves a directory over HTTP with browse, download,
// upload, delete and create-directory support.
//
// It is a standalone replacement for the previous Caddy plugin variant and
// is configured entirely via environment variables:
//
//	PORT            listen port (default: 8080)
//	HOME_DIR        root directory to serve (default: current working dir)
//	ADMIN_PASSWORD  if set, DELETE requests must present this value in the
//	                X-Admin-Password header. If unset, deletes are open.
package main

import (
	_ "embed"
	"html/template"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

// init registers MIME type mappings for extensions that browsers can
// render inline (mostly text formats) so http.ServeFile sets a
// Content-Type that browsers display instead of downloading. Without
// these, less-common extensions fall through to content sniffing and may
// be treated as a binary download.
func init() {
	// 纯文本类，浏览器可直接渲染
	mime.AddExtensionType(".md", "text/plain; charset=utf-8")
	mime.AddExtensionType(".markdown", "text/plain; charset=utf-8")
	mime.AddExtensionType(".log", "text/plain; charset=utf-8")
	mime.AddExtensionType(".yaml", "text/plain; charset=utf-8")
	mime.AddExtensionType(".yml", "text/plain; charset=utf-8")
	mime.AddExtensionType(".toml", "text/plain; charset=utf-8")
	mime.AddExtensionType(".ini", "text/plain; charset=utf-8")
	mime.AddExtensionType(".conf", "text/plain; charset=utf-8")
	mime.AddExtensionType(".sql", "text/plain; charset=utf-8")
	mime.AddExtensionType(".csv", "text/csv; charset=utf-8")
	mime.AddExtensionType(".tsv", "text/tab-separated-values; charset=utf-8")
	// 富文本/代码（部分浏览器有原生预览能力）
	mime.AddExtensionType(".json", "application/json; charset=utf-8")
	mime.AddExtensionType(".xml", "text/xml; charset=utf-8")
	mime.AddExtensionType(".rss", "application/rss+xml; charset=utf-8")
	mime.AddExtensionType(".atom", "application/atom+xml; charset=utf-8")
	// 图片类
	mime.AddExtensionType(".webp", "image/webp")
	mime.AddExtensionType(".avif", "image/avif")
	mime.AddExtensionType(".heic", "image/heic")
	mime.AddExtensionType(".svgz", "image/svg+xml")
	// 视频类（浏览器内联播放）
	mime.AddExtensionType(".m3u8", "application/vnd.apple.mpegurl")
	mime.AddExtensionType(".mpd", "application/dash+xml")
	// 编程语言源码：统一按纯文本预览，浏览器直接显示而不下载
	// Go / Rust / Zig / Nim / V / D / Crystal / Julia
	mime.AddExtensionType(".go", "text/plain; charset=utf-8")
	mime.AddExtensionType(".rs", "text/plain; charset=utf-8")
	mime.AddExtensionType(".zig", "text/plain; charset=utf-8")
	mime.AddExtensionType(".nim", "text/plain; charset=utf-8")
	mime.AddExtensionType(".v", "text/plain; charset=utf-8")
	mime.AddExtensionType(".d", "text/plain; charset=utf-8")
	mime.AddExtensionType(".cr", "text/plain; charset=utf-8")
	mime.AddExtensionType(".jl", "text/plain; charset=utf-8")
	// JavaScript / TypeScript / Dart / CoffeeScript
	mime.AddExtensionType(".js", "text/plain; charset=utf-8")
	mime.AddExtensionType(".mjs", "text/plain; charset=utf-8")
	mime.AddExtensionType(".cjs", "text/plain; charset=utf-8")
	mime.AddExtensionType(".ts", "text/plain; charset=utf-8")
	mime.AddExtensionType(".dart", "text/plain; charset=utf-8")
	mime.AddExtensionType(".coffee", "text/plain; charset=utf-8")
	// PHP / Ruby / Python / Perl / Lua / R / Tcl
	mime.AddExtensionType(".php", "text/plain; charset=utf-8")
	mime.AddExtensionType(".rb", "text/plain; charset=utf-8")
	mime.AddExtensionType(".rake", "text/plain; charset=utf-8")
	mime.AddExtensionType(".gemspec", "text/plain; charset=utf-8")
	mime.AddExtensionType(".py", "text/plain; charset=utf-8")
	mime.AddExtensionType(".pyw", "text/plain; charset=utf-8")
	mime.AddExtensionType(".pyi", "text/plain; charset=utf-8")
	mime.AddExtensionType(".pl", "text/plain; charset=utf-8")
	mime.AddExtensionType(".pm", "text/plain; charset=utf-8")
	mime.AddExtensionType(".lua", "text/plain; charset=utf-8")
	mime.AddExtensionType(".r", "text/plain; charset=utf-8")
	mime.AddExtensionType(".tcl", "text/plain; charset=utf-8")
	// Shell 脚本 / 批处理 / PowerShell / AWK
	mime.AddExtensionType(".sh", "text/plain; charset=utf-8")
	mime.AddExtensionType(".bash", "text/plain; charset=utf-8")
	mime.AddExtensionType(".zsh", "text/plain; charset=utf-8")
	mime.AddExtensionType(".ksh", "text/plain; charset=utf-8")
	mime.AddExtensionType(".fish", "text/plain; charset=utf-8")
	mime.AddExtensionType(".csh", "text/plain; charset=utf-8")
	mime.AddExtensionType(".tcsh", "text/plain; charset=utf-8")
	mime.AddExtensionType(".bat", "text/plain; charset=utf-8")
	mime.AddExtensionType(".cmd", "text/plain; charset=utf-8")
	mime.AddExtensionType(".ps1", "text/plain; charset=utf-8")
	mime.AddExtensionType(".psm1", "text/plain; charset=utf-8")
	mime.AddExtensionType(".psd1", "text/plain; charset=utf-8")
	mime.AddExtensionType(".awk", "text/plain; charset=utf-8")
	// C / C++ / Objective-C / C#
	mime.AddExtensionType(".c", "text/plain; charset=utf-8")
	mime.AddExtensionType(".h", "text/plain; charset=utf-8")
	mime.AddExtensionType(".cpp", "text/plain; charset=utf-8")
	mime.AddExtensionType(".cc", "text/plain; charset=utf-8")
	mime.AddExtensionType(".cxx", "text/plain; charset=utf-8")
	mime.AddExtensionType(".hpp", "text/plain; charset=utf-8")
	mime.AddExtensionType(".hh", "text/plain; charset=utf-8")
	mime.AddExtensionType(".hxx", "text/plain; charset=utf-8")
	mime.AddExtensionType(".ino", "text/plain; charset=utf-8")
	mime.AddExtensionType(".m", "text/plain; charset=utf-8")
	mime.AddExtensionType(".mm", "text/plain; charset=utf-8")
	mime.AddExtensionType(".cs", "text/plain; charset=utf-8")
	// Java / Kotlin / Scala / Groovy / Clojure
	mime.AddExtensionType(".java", "text/plain; charset=utf-8")
	mime.AddExtensionType(".kt", "text/plain; charset=utf-8")
	mime.AddExtensionType(".kts", "text/plain; charset=utf-8")
	mime.AddExtensionType(".scala", "text/plain; charset=utf-8")
	mime.AddExtensionType(".sc", "text/plain; charset=utf-8")
	mime.AddExtensionType(".groovy", "text/plain; charset=utf-8")
	mime.AddExtensionType(".gradle", "text/plain; charset=utf-8")
	mime.AddExtensionType(".clj", "text/plain; charset=utf-8")
	mime.AddExtensionType(".cljs", "text/plain; charset=utf-8")
	mime.AddExtensionType(".cljc", "text/plain; charset=utf-8")
	mime.AddExtensionType(".edn", "text/plain; charset=utf-8")
	// Swift / Haskell / Erlang / Elixir / F# / OCaml
	mime.AddExtensionType(".swift", "text/plain; charset=utf-8")
	mime.AddExtensionType(".hs", "text/plain; charset=utf-8")
	mime.AddExtensionType(".lhs", "text/plain; charset=utf-8")
	mime.AddExtensionType(".erl", "text/plain; charset=utf-8")
	mime.AddExtensionType(".ex", "text/plain; charset=utf-8")
	mime.AddExtensionType(".exs", "text/plain; charset=utf-8")
	mime.AddExtensionType(".fs", "text/plain; charset=utf-8")
	mime.AddExtensionType(".fsx", "text/plain; charset=utf-8")
	mime.AddExtensionType(".ml", "text/plain; charset=utf-8")
	mime.AddExtensionType(".mli", "text/plain; charset=utf-8")
	// Lisp / Scheme / Fortran / Pascal / Ada / COBOL / Prolog
	mime.AddExtensionType(".lisp", "text/plain; charset=utf-8")
	mime.AddExtensionType(".lsp", "text/plain; charset=utf-8")
	mime.AddExtensionType(".scm", "text/plain; charset=utf-8")
	mime.AddExtensionType(".ss", "text/plain; charset=utf-8")
	mime.AddExtensionType(".f", "text/plain; charset=utf-8")
	mime.AddExtensionType(".f90", "text/plain; charset=utf-8")
	mime.AddExtensionType(".f95", "text/plain; charset=utf-8")
	mime.AddExtensionType(".for", "text/plain; charset=utf-8")
	mime.AddExtensionType(".pas", "text/plain; charset=utf-8")
	mime.AddExtensionType(".adb", "text/plain; charset=utf-8")
	mime.AddExtensionType(".ads", "text/plain; charset=utf-8")
	mime.AddExtensionType(".cob", "text/plain; charset=utf-8")
	mime.AddExtensionType(".cbl", "text/plain; charset=utf-8")
	mime.AddExtensionType(".pro", "text/plain; charset=utf-8")
	// Vim / 汇编 / 构建 / 接口描述
	mime.AddExtensionType(".vim", "text/plain; charset=utf-8")
	mime.AddExtensionType(".asm", "text/plain; charset=utf-8")
	mime.AddExtensionType(".s", "text/plain; charset=utf-8")
	mime.AddExtensionType(".mk", "text/plain; charset=utf-8")
	mime.AddExtensionType(".mak", "text/plain; charset=utf-8")
	mime.AddExtensionType(".cmake", "text/plain; charset=utf-8")
	mime.AddExtensionType(".proto", "text/plain; charset=utf-8")
	mime.AddExtensionType(".thrift", "text/plain; charset=utf-8")
	mime.AddExtensionType(".graphql", "text/plain; charset=utf-8")
	mime.AddExtensionType(".gql", "text/plain; charset=utf-8")
	mime.AddExtensionType(".sol", "text/plain; charset=utf-8")
}

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
	root := getEnv("HOME_DIR", ".")

	absRoot, err := filepath.Abs(root)
	if err != nil {
		log.Fatalf("resolve HOME_DIR %q: %v", root, err)
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

	log.Printf("simple-file-browser listening on %s, serving %s (admin_password set: %t)",
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
