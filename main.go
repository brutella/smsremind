package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/brutella/smsremind/aspsms"
	"github.com/brutella/smsremind/caldav"
	"github.com/brutella/smsremind/idempotency"
	"github.com/brutella/smsremind/internal/app"
)

var stateDir = flag.String("state-dir", ".", "Directory used to store internal states.")
var offset = flag.Int("offset", 1, "Number of days in the future from now for which a reminder should be sent.")

var calendars = flag.String("calendars", "", "Command separates list of calendar names")
var caldavURL = flag.String("caldav", "", "URL of the CalDav server")

var sender = flag.String("sms-sender", "Reminder", "The SMS sender name")
var msg = flag.String("sms-template", "Your next appointment is on {{ .StartDate }} at {{ .StartTime }}", "The SMS template")

var dryRun = flag.Bool("dry-run", false, "Do not send SMS – only print.")
var timezone = flag.String("timezone", "Europe/Vienna", "Timezone location")

func main() {
	if err := run(); err != nil {
		if errors.Is(err, app.ErrAlreadyRunning) {
			os.Exit(0)
		}
		log.Fatal(err)
	}
}

func RequireEnv(key string) (string, error) {
	value, ok := os.LookupEnv(key)
	if !ok || value == "" {
		return "", fmt.Errorf("required environment variable %q is not set", key)
	}
	return value, nil
}

func run() error {
	flag.Parse()

	aspsmsUserkey, err := RequireEnv("ASPSMS_USERKEY")
	if err != nil {
		return err
	}
	aspsmsApiPwd, err := RequireEnv("ASPSMS_PASSWORD")
	if err != nil {
		return err
	}
	appleID, err := RequireEnv("CALDAV_APPLEID")
	if err != nil {
		return err
	}
	appPwd, err := RequireEnv("CALDAV_PASSWORD")
	if err != nil {
		return err
	}

	eventsClient, err := caldav.NewClient(*caldavURL, appleID, appPwd, caldav.NewHTTPClient(30*time.Second))
	if err != nil {
		return err
	}

	runner := app.Runner{
		EventSource: eventsClient,
		SMS:         aspsms.NewClient(aspsmsUserkey, aspsmsApiPwd, *sender, 5*time.Second),
		OpenStore: func(path string) (app.IdempotencyStore, error) {
			return idempotency.Open(path)
		},
		AcquireLock: func(path string, maxAge time.Duration) (app.Releaser, error) {
			lock, err := idempotency.AcquireLock(path, maxAge)
			if err != nil {
				if errors.Is(err, idempotency.ErrLockHeld) {
					return nil, fmt.Errorf("%w: %v", app.ErrLockUnavailable, err)
				}
				return nil, err
			}
			return lock, nil
		},
		Clock:      nil,
		Output:     os.Stdout,
		LockMaxAge: time.Minute,
	}

	cfg := app.Config{
		StateDir:        *stateDir,
		OffsetDays:      *offset,
		Calendars:       parseCalendarNames(*calendars),
		MessageTemplate: *msg,
		DryRun:          *dryRun,
		Timezone:        *timezone,
	}

	return runner.Run(context.Background(), cfg)
}

func parseCalendarNames(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
