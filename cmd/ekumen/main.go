// Command ekumen is the Ekumen CLI.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Yashswarnkar/ekumen/internal/buildinfo"
	"github.com/Yashswarnkar/ekumen/internal/server"
	"github.com/Yashswarnkar/ekumen/web"
)

const usage = `Usage: ekumen <command> [flags]

Commands:
  version   Print version, commit and build date
  serve     Serve the UI and HTTP API (default --addr ` + server.DefaultAddr + `)
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Fprintf(stdout, "ekumen %s (commit %s, built %s)\n", buildinfo.Version, buildinfo.Commit, buildinfo.Date)
		return 0
	case "serve":
		return serve(args[1:], stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "ekumen: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

func serve(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addr := fs.String("addr", server.DefaultAddr, "listen address")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           server.Handler(web.UI()),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	fmt.Fprintf(stderr, "ekumen %s serving on http://%s (ui embedded: %v)\n", buildinfo.Version, *addr, web.Embedded)

	select {
	case err := <-errc:
		fmt.Fprintf(stderr, "ekumen: %v\n", err)
		return 1
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintf(stderr, "ekumen: shutdown: %v\n", err)
		return 1
	}
	return 0
}
