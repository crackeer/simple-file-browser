# simple-file-browser

A small standalone HTTP file server that exposes a single directory over
the network with browse / download / upload / delete / create-directory
support. Built on [Gin](https://github.com/gin-gonic/gin) with a custom
browse page embedded into the binary at build time.

The browse page is embedded into the binary at build time, so
`template.html` no longer needs to be shipped alongside the executable.

## Features

- Directory browse UI with sortable columns (name / size / time), a
  client-side filter, and breadcrumb navigation
- `PUT` file upload (multipart `file` field) with progress modal
- `POST` create directory
- `DELETE` file or directory (recursive)
- Range/`If-Modified-Since` aware file downloads via `http.ServeFile`
- Optional admin password for `DELETE` (constant-time compare)
- Path-traversal protection: every request resolves inside the configured
  root before being touched

## Requirements

- Go 1.22 or newer to build

## Configuration

The binary is configured entirely via environment variables:

| Variable         | Default | Purpose                                                                       |
|------------------|---------|-------------------------------------------------------------------------------|
| `PORT`           | `8080`  | TCP port to listen on                                                          |
| `HOME`           | `.`     | Root directory to serve. Files and directories under this path are exposed.    |
| `ADMIN_PASSWORD` | *empty* | If set, `DELETE` requests must include `X-Admin-Password: <value>`. If unset, deletes are unauthenticated (matches the original behaviour). |

> ⚠️ `HOME` is intentionally reused as the served-root variable so it
> plays nicely with systemd. If the process inherits a real user's
> `HOME` (for example when run from a shell), every file in that home
> directory becomes writable/deletable over HTTP. Set `HOME` explicitly
> to a dedicated data directory.

## Build & Run

```sh
go build -o simple-file-browser ./...
HOME=/srv/simple-file-browser PORT=8080 ADMIN_PASSWORD='replace-me' ./simple-file-browser
```

Then browse to <http://localhost:8080/>.

## Uploading a file with an existing name

If a file with the same name already exists, the upload is stored
alongside it under `{name}-{unix_nano}` so the existing file is never
overwritten.

## Install as a systemd service

`build.sh` compiles the binary and produces `simple-file-browser.zip` that
extracts into a single top-level `simple-file-browser/` directory containing the
binary, a systemd unit, a sample env file, and the installer:

```sh
./build.sh                           # build for the current OS/arch
GOOS=linux GOARCH=amd64 ./build.sh   # cross-compile a Linux/amd64 build
```

Copy the archive to the target host, extract it, and run:

```sh
sudo ./install.sh
```

`install.sh`:

1. installs the `simple-file-browser` binary to `/usr/local/bin/`
2. installs the systemd unit as `/etc/systemd/system/simple-file-browser.service`
3. installs (or backs up + refreshes) `/etc/simple-file-browser.env`
4. creates `/srv/simple-file-browser` if missing
5. reloads systemd and enables + starts the service

Edit `/etc/simple-file-browser.env` to change the port, served root, or admin
password, then `systemctl restart simple-file-browser.service`.

Useful commands after install:

```sh
systemctl status simple-file-browser.service
journalctl -u simple-file-browser.service -f
```

## HTTP surface

| Method   | Path            | Description                                                             |
|----------|-----------------|-------------------------------------------------------------------------|
| `GET`    | `/dir/`         | Render the browse page for `dir`                                        |
| `GET`    | `/path/to/file` | Download the file                                                       |
| `POST`   | `/dir/name`     | Create directory `name` under `dir` (idempotent, recursive)             |
| `PUT`    | `/dir/name`     | Upload a file to `dir/name`; multipart form field `file`                |
| `DELETE` | `/path`         | Remove file or directory. Requires `X-Admin-Password` when configured.  |

Serve this endpoint over HTTPS whenever the admin password is set —
`X-Admin-Password` is otherwise transmitted in cleartext.
