package syncer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"syncal/internal/google"
	"syncal/internal/icloud"
	"syncal/internal/models"
	"time"
)

const stateFile = "sync-state.json"

// SyncState keeps track of which events have been synced.
// The key is the Google Event ID, and the value is the UID of the event in iCloud.
type SyncState map[string]string

// Syncer orchestrates the synchronization from Google Calendar to iCloud.
type Syncer struct {
	logger          *slog.Logger
	googleClients   []*google.CalendarClient
	googleCalIDs    []string
	icloudClient    *icloud.CalDAVClient
	state           SyncState
	dryRun          bool
	primaryTimeZone *time.Location
}

// NewSyncer creates a new Syncer.
func NewSyncer(logger *slog.Logger, gClients []*google.CalendarClient, gCalIDs []string, iClient *icloud.CalDAVClient, dryRun bool, tz *time.Location) (*Syncer, error) {
	state, err := loadState()
	if err != nil {
		// If the file doesn't exist, we can start with an empty state.
		if os.IsNotExist(err) {
			logger.Info("No sync state file found, starting fresh.", "file", stateFile)
			state = make(SyncState)
		} else {
			return nil, fmt.Errorf("failed to load sync state: %w", err)
		}
	}

	return &Syncer{
		logger:          logger,
		googleClients:   gClients,
		googleCalIDs:    gCalIDs,
		icloudClient:    iClient,
		state:           state,
		dryRun:          dryRun,
		primaryTimeZone: tz,
	}, nil
}

// Sync performs a full synchronization cycle.
func (s *Syncer) Sync(ctx context.Context) error {
	s.logger.Info("Starting sync cycle.")

	googleEvents, err := s.fetchAllGoogleEvents(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch google events: %w", err)
	}

	s.logger.Info("Fetched all Google events.", "count", len(googleEvents))

	for _, event := range googleEvents {
		err := s.syncEvent(ctx, event)
		if err != nil {
			s.logger.Error("Failed to sync event", "title", event.Title, "error", err)
			// Continue with the next event even if one fails.
		}
	}

	if !s.dryRun {
		if err := s.saveState(); err != nil {
			s.logger.Error("Failed to save sync state", "error", err)
		}
	}

	s.logger.Info("Sync cycle finished.")
	return nil
}

// fetchAllGoogleEvents retrieves events from all configured Google Calendars.
func (s *Syncer) fetchAllGoogleEvents(ctx context.Context) ([]*models.Event, error) {
	var allEvents []*models.Event
	calendarIDs := strings.Split(s.googleCalIDs[0], ",")

	for _, client := range s.googleClients {
		for _, calID := range calendarIDs {
			events, err := client.GetUpcomingEvents(calID, 7) // Fetch events for the next 7 days
			if err != nil {
				s.logger.Error("Could not fetch events for a google calendar", "calendarID", calID, "error", err)
				continue
			}
			allEvents = append(allEvents, events...)
		}
	}
	return allEvents, nil
}

// syncEvent handles the logic for syncing a single event.
func (s *Syncer) syncEvent(ctx context.Context, event *models.Event) error {
	// Check if this event has already been synced.
	if _, exists := s.state[event.ID]; exists {
		// For now, we don't handle updates. In the future, we could check LastModified.
		s.logger.Debug("Event already synced, skipping.", "title", event.Title, "id", event.ID)
		return nil
	}

	s.logger.Info("New event found, syncing to iCloud.", "title", event.Title)

	// We need to generate a new UID for the iCloud event, but store the mapping.
	// We use the Google iCal UID to ensure consistency if we sync from another client.
	if event.UID == "" {
		s.logger.Warn("Google event has no UID, generating a new one.", "title", event.Title)
		event.UID = icloud.GenerateUID()
	}

	// Adjust times to the primary timezone
	event.StartTime = event.StartTime.In(s.primaryTimeZone)
	event.EndTime = event.EndTime.In(s.primaryTimeZone)

	if s.dryRun {
		s.logger.Info("[DRY RUN] Would create new event in iCloud", "title", event.Title, "startTime", event.StartTime)
		return nil
	}

	err := s.icloudClient.SyncEvent(ctx, event)
	if err != nil {
		return fmt.Errorf("failed to sync event to icloud: %w", err)
	}

	// If successful, update the state.
	s.state[event.ID] = event.UID
	return nil
}

// loadState loads the sync state from the JSON file.
func loadState() (SyncState, error) {
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return nil, err
	}
	var state SyncState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return state, nil
}

// saveState saves the current sync state to the JSON file.
func (s *Syncer) saveState() error {
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal sync state: %w", err)
	}
	return os.WriteFile(stateFile, data, 0644)
}
