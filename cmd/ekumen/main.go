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
	"github.com/Yashswarnkar/ekumen/internal/cli"
	"github.com/Yashswarnkar/ekumen/internal/server"
	"github.com/Yashswarnkar/ekumen/web"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run handles version and serve here and every C-cli-json command in
// internal/cli.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "version":
			fmt.Fprintf(stdout, "ekumen %s (commit %s, built %s)\n", buildinfo.Version, buildinfo.Commit, buildinfo.Date)
			return 0
		case "serve":
			return serve(args[1:], stderr)
		}
	}
	env := cli.DefaultEnv()
	env.Stdout, env.Stderr = stdout, stderr
	return cli.Run(args, env)
}

func serve(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addr := fs.String("addr", server.DefaultAddr, "listen address")
	if err := fs.Parse(args); err != nil {
		return cli.ExitUsage
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
