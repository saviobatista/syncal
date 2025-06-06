package main

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"syncal/internal/google"
	"syncal/internal/icloud"
	"syncal/internal/syncer"
	"time"

	"github.com/joho/godotenv"
	"github.com/urfave/cli/v2"
	"golang.org/x/oauth2"
)

func main() {
	// Load .env file first, but don't error if it doesn't exist.
	_ = godotenv.Load()

	app := &cli.App{
		Name:  "syncal",
		Usage: "Sync Google Calendar events to an iCloud Calendar.",
		Commands: []*cli.Command{
			authCommand(),
			syncCommand(),
		},
	}

	if err := app.Run(os.Args); err != nil {
		slog.Error("Application failed", "error", err)
		os.Exit(1)
	}
}

func authCommand() *cli.Command {
	return &cli.Command{
		Name:  "auth",
		Usage: "Authenticate with a Google account to get an API token.",
		Action: func(c *cli.Context) error {
			logger := setupLogger("info")
			logger.Info("Starting Google authentication flow.")

			config, err := google.GetOAuthConfigForAuthFlow(os.Getenv("GOOGLE_CLIENT_ID"), os.Getenv("GOOGLE_CLIENT_SECRET"))
			if err != nil {
				return fmt.Errorf("failed to get google oauth config: %w", err)
			}

			authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
			fmt.Printf("Go to the following link in your browser then type the "+
				"authorization code: \n%v\n", authURL)

			fmt.Print("Enter Authorization Code: ")
			reader := bufio.NewReader(os.Stdin)
			authCode, _ := reader.ReadString('\n')
			authCode = strings.TrimSpace(authCode)

			token, err := google.TokenFromWeb(config, authCode)
			if err != nil {
				return fmt.Errorf("unable to retrieve token from web: %w", err)
			}

			fmt.Print("Enter a name for this account (e.g., 'personal', 'work'): ")
			accountName, _ := reader.ReadString('\n')
			accountName = strings.TrimSpace(accountName)
			tokenFile := "token-" + accountName + ".json"

			if err := google.SaveToken(tokenFile, token); err != nil {
				return fmt.Errorf("failed to save token: %w", err)
			}

			logger.Info("Successfully authenticated and saved token.", "file", tokenFile)
			return nil
		},
	}
}

func syncCommand() *cli.Command {
	return &cli.Command{
		Name:  "sync",
		Usage: "Run the calendar synchronization process.",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "once", Usage: "Run the sync cycle once and exit."},
			&cli.BoolFlag{Name: "dry-run", Usage: "Log what would be synced without making changes."},
			&cli.IntFlag{Name: "watch", Value: 300, Usage: "Run sync every N seconds. Overrides --once."},
		},
		Action: func(c *cli.Context) error {
			logLevel := os.Getenv("LOG_LEVEL")
			if logLevel == "" {
				logLevel = "info"
			}
			logger := setupLogger(logLevel)

			if c.Bool("dry-run") {
				logger.Info("Performing a dry run. No changes will be made.")
			}

			gClientIDs := os.Getenv("GOOGLE_CALENDAR_IDS")
			if gClientIDs == "" {
				return fmt.Errorf("GOOGLE_CALENDAR_IDS environment variable not set")
			}

			// Load all Google clients for all authenticated accounts
			accounts, err := google.GetTokenAccounts()
			if err != nil {
				return fmt.Errorf("could not find any google accounts, did you run auth command? %w", err)
			}
			if len(accounts) == 0 {
				return fmt.Errorf("no google accounts found. Run the 'auth' command first")
			}

			var gClients []*google.CalendarClient
			for _, acc := range accounts {
				gClient, err := google.NewClient(c.Context, logger, os.Getenv("GOOGLE_CLIENT_ID"), os.Getenv("GOOGLE_CLIENT_SECRET"), acc)
				if err != nil {
					return fmt.Errorf("failed to create google client for account %s: %w", acc, err)
				}
				gClients = append(gClients, gClient)
			}
			logger.Info("Initialized Google clients for all accounts.", "count", len(gClients))

			iClient, err := icloud.NewClient(logger, os.Getenv("ICLOUD_USERNAME"), os.Getenv("ICLOUD_APP_SPECIFIC_PASSWORD"), os.Getenv("ICLOUD_CALENDAR_NAME"))
			if err != nil {
				return fmt.Errorf("failed to create icloud client: %w", err)
			}

			tzStr := os.Getenv("PRIMARY_TIMEZONE")
			if tzStr == "" {
				tzStr = "UTC"
			}
			loc, err := time.LoadLocation(tzStr)
			if err != nil {
				return fmt.Errorf("invalid timezone '%s': %w", tzStr, err)
			}

			s, err := syncer.NewSyncer(logger, gClients, []string{gClientIDs}, iClient, c.Bool("dry-run"), loc)
			if err != nil {
				return fmt.Errorf("failed to create syncer: %w", err)
			}

			// --watch flag takes precedence
			if c.IsSet("watch") {
				interval := time.Duration(c.Int("watch")) * time.Second
				logger.Info("Starting watcher.", "interval", interval)
				ticker := time.NewTicker(interval)
				defer ticker.Stop()
				for ; true; <-ticker.C {
					if err := s.Sync(c.Context); err != nil {
						logger.Error("Sync cycle failed", "error", err)
					}
				}
			} else { // --once is the default behavior if --watch is not set
				logger.Info("Running a single sync cycle.")
				if err := s.Sync(c.Context); err != nil {
					return fmt.Errorf("single sync cycle failed: %w", err)
				}
			}

			return nil
		},
	}
}

func setupLogger(level string) *slog.Logger {
	var logLevel slog.Level
	switch strings.ToLower(level) {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))
}
