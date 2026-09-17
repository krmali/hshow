# hshow

A tiny dashboard server for an [hledger](https://hledger.org) journal. On
every request it shells out to the `hledger` CLI, computes month-to-date
activity per account compared to the same period last month, and renders a
mobile-friendly HTML page. No database, no build step: Go backend +
[htmx](https://htmx.org) + [Tailwind CSS](https://tailwindcss.com) via CDN.

## Requirements

- Go 1.21+ (to build)
- [`hledger`](https://hledger.org/install.html) installed and on `PATH` (or
  point at it explicitly, see below) on the machine that runs the server
- An hledger journal file

## Running

```sh
go run . -journal /path/to/your.journal
```

Or via environment variables (useful under systemd/containers):

```sh
export HSHOW_JOURNAL=/path/to/your.journal
export HSHOW_ADDR=127.0.0.1:8080      # default
export HSHOW_HLEDGER_BIN=hledger      # default, override if not on PATH
go run .
```

Flags take precedence over environment variables. Then open
`http://127.0.0.1:8080/`.

To try it against the bundled sample journal (fake data, three months of
transactions):

```sh
go run . -journal testdata/sample.journal
```

## Building

```sh
go build -o hshow .
```

Produces a single static binary; the HTML templates are embedded via
`go:embed`, so nothing else needs to ship alongside it except the binary
itself.

## Running with Docker

The image bundles the `hledger` CLI, so you only need to mount your journal
file from the host:

```sh
docker build -t hshow .
docker run -d --name hshow \
  -p 127.0.0.1:8080:8080 \
  -e HSHOW_JOURNAL=/data/journal.hledger \
  -v /path/to/your.journal:/data/journal.hledger:ro \
  hshow
```

The journal file lives outside the container/image — only the mounted path
changes, the image itself never needs rebuilding when your journal changes.

### With Docker Compose

```sh
HSHOW_JOURNAL_HOST_PATH=/path/to/your.journal docker compose up -d --build
```

`HSHOW_JOURNAL_HOST_PATH` points at your real journal file on the host; it
defaults to the bundled `testdata/sample.journal` if unset, so
`docker compose up -d --build` with no environment variable works out of the
box for a quick look. See `docker-compose.yml` to adjust the published port
or add an `HSHOW_HLEDGER_BIN` override if needed.

## Deploying behind nginx

`hshow` binds to `127.0.0.1:8080` by default and has no TLS or auth of its
own — put it behind nginx (or similar) as a reverse proxy, e.g.:

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
}
```

Run the binary under your process supervisor of choice (systemd, etc.),
making sure `hledger` is reachable on `PATH` (or set `HSHOW_HLEDGER_BIN` to
an absolute path) in that service's environment.

## How it works

- `GET /` — full dashboard page
- `GET /dashboard` — just the dashboard content as an HTML fragment, used by
  the page's htmx-powered "Refresh" button

On each of those requests, the server runs `hledger balance -O csv --flat -N`
twice: once for the 1st-of-this-month through today, and once for the same
number of days starting on the 1st of last month. It ranks accounts by the
size of their current-month activity and shows the top 5, alongside the
comparable figure from last month and the difference.

## Known limitations

- Assumes a single currency/commodity in the journal. Amounts are parsed by
  stripping non-numeric characters, so a journal mixing multiple currencies
  in the same accounts may produce incorrect totals.
- If `hledger`'s CSV output shape differs across versions, the parser in
  `internal/hledger` may need adjusting. To sanity-check on a real
  deployment host:

  ```sh
  hledger balance -O csv --flat -N -f /path/to/your.journal \
    -b 2026-09-01 -e 2026-09-17
  ```

  Confirm it prints a header row followed by `"account","balance"` rows, and
  that the balance column parses as a plain (optionally currency-prefixed,
  optionally comma-grouped) number.
