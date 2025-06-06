package google

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strings"
	"syncal/internal/models"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

const (
	credentialsFile = "credentials.json"
)

// CalendarClient provides a client for interacting with the Google Calendar API.
type CalendarClient struct {
	service *calendar.Service
	logger  *slog.Logger
}

// NewClient creates a new Google Calendar client.
// It handles loading credentials and setting up an authenticated HTTP client.
// It supports multiple accounts by looking for token files like token-user1.json, token-user2.json, etc.
// The accountName is used to find the correct token file.
func NewClient(ctx context.Context, logger *slog.Logger, clientID, clientSecret, accountName string) (*CalendarClient, error) {
	config, err := getOAuthConfig(clientID, clientSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to get OAuth config: %w", err)
	}

	tokenFile := fmt.Sprintf("token-%s.json", accountName)
	token, err := tokenFromFile(tokenFile)
	if err != nil {
		return nil, fmt.Errorf("could not load token for account %s: %w. Please run the 'auth' command first", accountName, err)
	}

	client := config.Client(ctx, token)
	service, err := calendar.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("failed to create calendar service: %w", err)
	}

	return &CalendarClient{service: service, logger: logger}, nil
}

// GetUpcomingEvents fetches upcoming events from the specified calendar.
func (c *CalendarClient) GetUpcomingEvents(calendarID string, days int) ([]*models.Event, error) {
	c.logger.Debug("Fetching upcoming events", "calendarID", calendarID, "days", days)
	now := time.Now().UTC()
	tmax := now.Add(time.Duration(days) * 24 * time.Hour).Format(time.RFC3339)
	tmin := now.Format(time.RFC3339)

	events, err := c.service.Events.List(calendarID).
		ShowDeleted(false).
		SingleEvents(true).
		TimeMin(tmin).
		TimeMax(tmax).
		OrderBy("startTime").
		Do()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve events: %w", err)
	}

	c.logger.Info("Successfully fetched events from Google Calendar", "count", len(events.Items), "calendarID", calendarID)
	return c.toInternalEvents(events.Items, calendarID), nil
}

// toInternalEvents converts Google Calendar events to the internal Event model.
func (c *CalendarClient) toInternalEvents(googleEvents []*calendar.Event, source string) []*models.Event {
	var internalEvents []*models.Event
	for _, item := range googleEvents {
		// Skip events without a start time (e.g., all-day events without a specific time)
		if item.Start == nil || item.Start.DateTime == "" {
			continue
		}

		startTime, _ := time.Parse(time.RFC3339, item.Start.DateTime)
		endTime, _ := time.Parse(time.RFC3339, item.End.DateTime)

		var attendees []string
		for _, a := range item.Attendees {
			attendees = append(attendees, a.Email)
		}

		event := &models.Event{
			ID:          item.Id,
			Title:       item.Summary,
			Description: item.Description,
			StartTime:   startTime,
			EndTime:     endTime,
			Location:    item.Location,
			Organizer:   item.Organizer.Email,
			Attendees:   attendees,
			UID:         item.ICalUID, // Use the iCalendar UID for syncing
			Source:      fmt.Sprintf("google-%s", source),
		}
		internalEvents = append(internalEvents, event)
	}
	return internalEvents
}

// GetOAuthConfigForAuthFlow is used by the auth command to get the config for the web flow.
func GetOAuthConfigForAuthFlow(clientID, clientSecret string) (*oauth2.Config, error) {
	return getOAuthConfig(clientID, clientSecret)
}

// getOAuthConfig reads credentials and returns an OAuth2 config.
// It prioritizes environment variables over a local credentials.json file.
func getOAuthConfig(clientID, clientSecret string) (*oauth2.Config, error) {
	if clientID != "" && clientSecret != "" {
		return &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  "urn:ietf:wg:oauth:2.0:oob",
			Scopes:       []string{calendar.CalendarReadonlyScope},
			Endpoint:     google.Endpoint,
		}, nil
	}

	b, err := os.ReadFile(credentialsFile)
	if err != nil {
		if _, ok := err.(*fs.PathError); ok {
			return nil, fmt.Errorf("credentials.json not found. Please provide GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET env vars or place credentials.json in the root directory")
		}
		return nil, fmt.Errorf("unable to read client secret file: %w", err)
	}

	config, err := google.ConfigFromJSON(b, calendar.CalendarReadonlyScope)
	if err != nil {
		return nil, fmt.Errorf("unable to parse client secret file to config: %w", err)
	}
	config.RedirectURL = "urn:ietf:wg:oauth:2.0:oob" // For desktop app flow
	return config, nil
}

// TokenFromWeb is called by the auth flow to retrieve a token.
func TokenFromWeb(config *oauth2.Config, authCode string) (*oauth2.Token, error) {
	return config.Exchange(context.Background(), authCode)
}

// SaveToken saves a token to a file path.
func SaveToken(path string, token *oauth2.Token) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("unable to create token file: %w", err)
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(token)
}

// tokenFromFile retrieves a token from a local file.
func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tok := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(tok)
	return tok, err
}

// DiscoverGoogleCalendars finds all calendars associated with the authenticated account.
func (c *CalendarClient) DiscoverGoogleCalendars() ([]string, error) {
	list, err := c.service.CalendarList.List().Do()
	if err != nil {
		return nil, fmt.Errorf("failed to list calendars: %w", err)
	}

	var calendarIDs []string
	for _, item := range list.Items {
		calendarIDs = append(calendarIDs, item.Id)
	}
	return calendarIDs, nil
}

// Helper function to get all token accounts
func GetTokenAccounts() ([]string, error) {
	files, err := os.ReadDir(".")
	if err != nil {
		return nil, err
	}

	var accounts []string
	for _, file := range files {
		if strings.HasPrefix(file.Name(), "token-") && strings.HasSuffix(file.Name(), ".json") {
			accountName := strings.TrimSuffix(strings.TrimPrefix(file.Name(), "token-"), ".json")
			accounts = append(accounts, accountName)
		}
	}
	return accounts, nil
}
