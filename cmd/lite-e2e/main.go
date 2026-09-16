// Command lite-e2e serves the built frontend against a real Mizan Lite graph for the end-to-end journeys (L8 §3.2). Playwright
// starts it; it listens on loopback only and is never part of the shipped application.
//
//	go run ./cmd/lite-e2e -dist apps/lite/frontend/dist -addr 127.0.0.1:34199
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/mizan-erp/mizan/internal/lite/e2e"
)

func main() {
	dist := flag.String("dist", "apps/lite/frontend/dist", "the built frontend")
	addr := flag.String("addr", "127.0.0.1:34199", "a loopback address to listen on")
	fixture := flag.String("fixture", "seeded", "the shop to start with: empty or seeded")
	flag.Parse()

	root, err := os.MkdirTemp("", "mizan-lite-e2e-*")
	if err != nil {
		fail(err)
	}
	defer os.RemoveAll(root)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	bridge, err := e2e.New(e2e.Options{Dist: *dist, Root: root})
	if err != nil {
		fail(err)
	}
	defer bridge.Close()
	if err = bridge.Reset(ctx, *fixture); err != nil {
		fail(err)
	}
	fmt.Printf("lite-e2e: serving %s on http://%s\n", *dist, *addr)
	if err = bridge.Listen(ctx, *addr); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "lite-e2e:", err)
	os.Exit(1)
}
