package icloud

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"syncal/internal/models"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"
	"github.com/google/uuid"
)

const (
	iCloudCalDAVEndpoint = "https://caldav.icloud.com/"
)

// customTransport handles adding Basic Auth and custom headers to requests.
type customTransport struct {
	Username  string
	Password  string
	Transport http.RoundTripper
}

// RoundTrip adds required headers and authentication to each request.
func (t *customTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.SetBasicAuth(t.Username, t.Password)
	req.Header.Set("User-Agent", "syncal/1.0")
	return t.Transport.RoundTrip(req)
}

// CalDAVClient is a client for interacting with a CalDAV server (iCloud).
type CalDAVClient struct {
	caldavClient *caldav.Client
	webdavClient *webdav.Client
	logger       *slog.Logger
	calendarURL  string
	username     string
}

// NewClient creates and initializes a new CalDAVClient for iCloud.
func NewClient(logger *slog.Logger, username, password, calendarName string) (*CalDAVClient, error) {
	transport := &customTransport{
		Username:  username,
		Password:  password,
		Transport: http.DefaultTransport,
	}
	httpClient := &http.Client{Transport: transport}

	caldavClient, err := caldav.NewClient(httpClient, iCloudCalDAVEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to create caldav client: %w", err)
	}

	webdavClient, err := webdav.NewClient(httpClient, iCloudCalDAVEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to create webdav client: %w", err)
	}

	c := &CalDAVClient{
		caldavClient: caldavClient,
		webdavClient: webdavClient,
		logger:       logger,
		username:     username,
	}

	logger.Info("Finding iCloud calendar", "calendarName", calendarName)
	calendarURL, err := c.findCalendar(context.Background(), calendarName)
	if err != nil {
		return nil, fmt.Errorf("could not find calendar '%s': %w", calendarName, err)
	}
	c.calendarURL = calendarURL
	logger.Info("Successfully found iCloud calendar", "url", calendarURL)

	return c, nil
}

// SyncEvent creates or updates an event in the iCloud calendar.
func (c *CalDAVClient) SyncEvent(ctx context.Context, event *models.Event) error {
	c.logger.Debug("Syncing event to iCloud", "eventTitle", event.Title, "uid", event.UID)

	vevent := c.toICal(event)
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropVersion, "2.0")
	cal.Props.SetText(ical.PropProductID, "-//syncal//EN")
	cal.Children = append(cal.Children, vevent)

	// The event path must be relative to the endpoint for the webdav client.
	eventPath := path.Join(strings.TrimPrefix(c.calendarURL, iCloudCalDAVEndpoint), fmt.Sprintf("%s.ics", event.UID))

	writer, err := c.webdavClient.Create(ctx, eventPath)
	if err != nil {
		return fmt.Errorf("failed to create event on CalDAV server: %w", err)
	}
	defer writer.Close()

	if err := ical.NewEncoder(writer).Encode(cal); err != nil {
		return fmt.Errorf("failed to encode event to iCal format: %w", err)
	}

	c.logger.Info("Successfully synced event to iCloud", "eventTitle", event.Title)
	return nil
}

// toICal converts an internal Event model to an ical.Component (VEvent).
func (c *CalDAVClient) toICal(event *models.Event) *ical.Component {
	ve := ical.NewComponent(ical.CompEvent)
	ve.Props.SetText(ical.PropUID, event.UID)
	ve.Props.SetText(ical.PropSummary, event.Title)
	ve.Props.SetDateTime(ical.PropDateTimeStamp, time.Now().UTC())
	ve.Props.SetDateTime(ical.PropDateTimeStart, event.StartTime)
	ve.Props.SetDateTime(ical.PropDateTimeEnd, event.EndTime)

	if event.Description != "" {
		ve.Props.SetText(ical.PropDescription, event.Description)
	}
	if event.Location != "" {
		ve.Props.SetText(ical.PropLocation, event.Location)
	}
	if event.Organizer != "" {
		p := ical.NewProp(ical.PropOrganizer)
		p.SetText(fmt.Sprintf("mailto:%s", event.Organizer))
		ve.Props.Add(p)
	}
	for _, attendee := range event.Attendees {
		p := ical.NewProp(ical.PropAttendee)
		p.SetText(fmt.Sprintf("mailto:%s", attendee))
		ve.Props.Add(p)
	}
	return ve
}

// findCalendar discovers the user's calendars and returns the URL for the one with the matching name.
func (c *CalDAVClient) findCalendar(ctx context.Context, name string) (string, error) {
	principalPath, err := c.caldavClient.FindCurrentUserPrincipal(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to find principal path: %w", err)
	}

	homeSetPath, err := c.caldavClient.FindCalendarHomeSet(ctx, principalPath)
	if err != nil {
		return "", fmt.Errorf("failed to find calendar home set: %w", err)
	}

	calendars, err := c.caldavClient.FindCalendars(ctx, homeSetPath)
	if err != nil {
		return "", fmt.Errorf("failed to find calendars: %w", err)
	}

	for _, cal := range calendars {
		if cal.Name == name {
			// Return the full URL for the calendar
			return fmt.Sprintf("%s%s", strings.TrimSuffix(iCloudCalDAVEndpoint, "/"), cal.Path), nil
		}
	}

	return "", fmt.Errorf("no calendar found with name '%s'", name)
}

// GenerateUID creates a new unique identifier for an event.
func GenerateUID() string {
	return uuid.New().String()
}
