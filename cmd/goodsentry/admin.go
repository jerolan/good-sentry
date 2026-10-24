package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/mail"
	"os"
	"strings"
	"time"

	"goodsentry/internal/config"
	"goodsentry/internal/store"
)

func runAdmin(args []string) error {
	if len(args) == 0 || args[0] != "grant" {
		return errors.New("usage: goodsentry admin grant --email <address>")
	}
	flags := flag.NewFlagSet("goodsentry admin grant", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	email := flags.String("email", "", "existing active account to promote")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: goodsentry admin grant --email <address>")
	}
	normalized := strings.ToLower(strings.TrimSpace(*email))
	address, err := mail.ParseAddress(normalized)
	if err != nil || address.Address != normalized || len(normalized) > 254 {
		return errors.New("--email must be one valid email address")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	granted, err := db.GrantInstanceAdmin(ctx, normalized)
	if err != nil {
		return err
	}
	if granted {
		fmt.Printf("Granted instance administrator to %s\n", normalized)
	} else {
		fmt.Printf("%s is already an instance administrator\n", normalized)
	}
	return nil
}
