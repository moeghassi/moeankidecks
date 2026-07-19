package main

import (
	"context"
	"fmt"
	"os"

	"github.com/moeghassi/moeankidecks/uploader/internal/app"
)

func main() {
	if err := app.Run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "uploader: %v\n", err)
		os.Exit(1)
	}
}
