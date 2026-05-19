package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tiankongzhise/config-center-by-codex/internal/config"
	"github.com/tiankongzhise/config-center-by-codex/internal/db"
	"github.com/tiankongzhise/config-center-by-codex/internal/server"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("config-center failed", "error", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printHelp()
		return nil
	}

	switch args[0] {
	case "serve":
		return serve(args[1:])
	case "init-db":
		return initDB(args[1:])
	case "migrate":
		return migrateDB(args[1:])
	case "register-service":
		return fmt.Errorf("%s command is not implemented yet", args[0])
	case "-h", "--help", "help":
		printHelp()
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func initDB(args []string) error {
	fs := flag.NewFlagSet("init-db", flag.ContinueOnError)
	envPath := fs.String("env", ".env", "path to dotenv file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*envPath)
	if err != nil {
		return err
	}

	updated, err := db.EnsureDatabaseAndUser(context.Background(), cfg)
	if err != nil {
		return err
	}

	if err := config.UpdateDotEnv(*envPath, map[string]string{
		"CONFIG_CENTER_DB_NAME":     updated.ConfigDBName,
		"CONFIG_CENTER_DB_USER":     updated.ConfigDBUser,
		"CONFIG_CENTER_DB_PASSWORD": updated.ConfigDBPassword,
	}); err != nil {
		return err
	}

	slog.Info("database and dedicated user are ready", "database", updated.ConfigDBName, "user", updated.ConfigDBUser)
	return nil
}

func migrateDB(args []string) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	envPath := fs.String("env", ".env", "path to dotenv file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*envPath)
	if err != nil {
		return err
	}
	if err := db.Migrate(context.Background(), cfg.DatabaseURL); err != nil {
		return err
	}

	slog.Info("database migrations completed")
	return nil
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	envPath := fs.String("env", ".env", "path to dotenv file")
	addr := fs.String("addr", "", "listen address, overrides CONFIG_CENTER_ADDR")
	migrate := fs.Bool("migrate", false, "run database migrations before serving")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*envPath)
	if err != nil {
		return err
	}
	if *migrate {
		if err := db.Migrate(context.Background(), cfg.DatabaseURL); err != nil {
			return err
		}
	}
	if *addr != "" {
		cfg.Addr = *addr
	}

	app := server.New(cfg)
	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           app.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("config-center listening", "addr", cfg.Addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-errCh:
		return err
	case <-stop:
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpServer.Shutdown(ctx)
	}
}

func printHelp() {
	fmt.Print(`config-center commands:
  serve             start the HTTP server
  init-db           create the dedicated PostgreSQL database and user
  migrate           run database migrations
  register-service  register this service in auth-limit

Examples:
  config-center serve --env .env --addr :8080
  config-center init-db --env .env
  config-center migrate --env .env
`)
}
