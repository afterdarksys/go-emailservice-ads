package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/afterdarksys/go-emailservice-ads/internal/backup"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: mailhub-backup create|verify|restore --archive FILE [--data-dir DIR] [--max-bytes N]")
		os.Exit(2)
	}
	command := os.Args[1]
	flags := flag.NewFlagSet(command, flag.ExitOnError)
	archive := flags.String("archive", "", "archive path")
	data := flags.String("data-dir", "", "source or nonexistent restore destination")
	max := flags.Int64("max-bytes", 1<<40, "maximum extracted bytes")
	flags.Parse(os.Args[2:])
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	var err error
	if *archive == "" {
		err = fmt.Errorf("archive required")
	} else {
		switch command {
		case "create":
			if *data == "" {
				err = fmt.Errorf("data-dir required")
			} else {
				err = backup.Create(ctx, *data, *archive)
			}
		case "verify":
			err = backup.Verify(ctx, *archive, *max)
		case "restore":
			if *data == "" {
				err = fmt.Errorf("data-dir required")
			} else {
				err = backup.Restore(ctx, *archive, *data, *max)
			}
		default:
			err = fmt.Errorf("unknown command %q", command)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(command + " completed")
}
