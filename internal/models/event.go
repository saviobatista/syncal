package models

import "time"

// Event represents a standard calendar event.
// This is an internal representation, independent of any specific calendar provider.
type Event struct {
	ID          string    // Unique identifier for the event (e.g., from the source calendar)
	Title       string    // Summary or title of the event
	Description string    // Detailed description of the event
	StartTime   time.Time // Start time of the event
	EndTime     time.Time // End time of the event
	Location    string    // Location of the event
	Organizer   string    // Organizer's email
	Attendees   []string  // List of attendee emails
	Source      string    // The source of the event (e.g., "google")
	UID         string    // The iCalendar UID, used for syncing
}
