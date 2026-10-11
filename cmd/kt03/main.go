package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"transport-monitoring/internal/kt03"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := kt03.Run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
