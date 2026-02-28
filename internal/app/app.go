package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"text/template"
	"time"

	"github.com/brutella/smsremind/cal"
)

var ErrAlreadyRunning = errors.New("already running")
var ErrLockUnavailable = errors.New("lock unavailable")

type Config struct {
	StateDir        string
	OffsetDays      int
	Calendars       []string
	MessageTemplate string
	DryRun          bool
	Timezone        string
}

type EventSource interface {
	Events(ctx context.Context, start, end time.Time, calendars []string, defaultTZ *time.Location) ([]cal.Event, error)
}

type SMSService interface {
	SendSimpleTextSMS(recipientE164 string, text string) error
}

type IdempotencyStore interface {
	Exists(key string) bool
	Mark(key string) error
	Close() error
}

type Releaser interface {
	Release() error
}

type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now()
}

type Runner struct {
	EventSource EventSource
	SMS         SMSService
	OpenStore   func(path string) (IdempotencyStore, error)
	AcquireLock func(path string, maxAge time.Duration) (Releaser, error)
	Clock       Clock
	Output      io.Writer
	LockMaxAge  time.Duration
}

func (r Runner) Run(ctx context.Context, cfg Config) error {
	if r.EventSource == nil {
		return errors.New("missing event source")
	}
	if r.SMS == nil {
		return errors.New("missing sms service")
	}
	if r.OpenStore == nil {
		return errors.New("missing store opener")
	}
	if r.AcquireLock == nil {
		return errors.New("missing lock manager")
	}
	if r.Clock == nil {
		r.Clock = systemClock{}
	}
	if r.Output == nil {
		r.Output = io.Discard
	}
	if r.LockMaxAge <= 0 {
		r.LockMaxAge = time.Minute
	}

	msgTmpl, err := template.New("output").Parse(cfg.MessageTemplate)
	if err != nil {
		return err
	}
	log.Printf("app: parsed message template")

	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		return fmt.Errorf("timezone: %w", err)
	}
	log.Printf("app: loaded timezone=%q", loc.String())

	lockPath := filepath.Join(cfg.StateDir, "smsremind.lock")
	log.Printf("app: acquiring lock path=%q max_age=%s", lockPath, r.LockMaxAge)
	lock, err := r.AcquireLock(lockPath, r.LockMaxAge)
	if err != nil {
		if errors.Is(err, ErrLockUnavailable) {
			log.Printf("app: lock unavailable path=%q", lockPath)
			return ErrAlreadyRunning
		}
		return err
	}
	log.Printf("app: lock acquired path=%q", lockPath)
	defer lock.Release()

	statePath := filepath.Join(cfg.StateDir, "sent.json")
	store, err := r.OpenStore(statePath)
	if err != nil {
		return err
	}
	log.Printf("app: opened idempotency store path=%q", statePath)
	defer store.Close()

	day := r.Clock.Now().AddDate(0, 0, cfg.OffsetDays)
	start := startOfDay(day, loc)
	end := endOfDay(day, loc)
	log.Printf("app: fetching events start=%s end=%s calendars=%v", start.Format(time.RFC3339), end.Format(time.RFC3339), cfg.Calendars)
	events, err := r.EventSource.Events(ctx, start, end, cfg.Calendars, loc)
	if err != nil {
		return err
	}
	log.Printf("app: fetched events count=%d", len(events))

	for _, event := range events {
		num := cal.EventPhoneNumber(event)
		if num == "" {
			log.Printf("app: skipping event uid=%q summary=%q reason=no phone number", event.UID, event.Summary)
			continue
		}

		key := eventMessageKey(event, cfg.OffsetDays)
		if store.Exists(key) {
			log.Printf("app: skipping event uid=%q summary=%q phone=%q reason=already sent key=%q", event.UID, event.Summary, num, key)
			continue
		}

		var buf bytes.Buffer
		if err := msgTmpl.Execute(&buf, event); err != nil {
			return err
		}

		message := buf.String()
		log.Printf("app: prepared reminder uid=%q summary=%q phone=%q dry_run=%t", event.UID, event.Summary, num, cfg.DryRun)
		fmt.Fprintf(r.Output, "remind %s %s: %s\n", event.Summary, num, message)

		if cfg.DryRun {
			log.Printf("app: dry-run skip send uid=%q phone=%q", event.UID, num)
			continue
		}

		log.Printf("app: sending sms uid=%q phone=%q", event.UID, num)
		if err := r.SMS.SendSimpleTextSMS(num, message); err != nil {
			return err
		}
		log.Printf("app: sms sent uid=%q phone=%q", event.UID, num)
		if err := store.Mark(key); err != nil {
			return err
		}
		log.Printf("app: marked reminder uid=%q key=%q", event.UID, key)
	}

	log.Printf("app: run completed")
	return nil
}

func startOfDay(d time.Time, loc *time.Location) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
}

func endOfDay(d time.Time, loc *time.Location) time.Time {
	start := startOfDay(d, loc)
	return start.AddDate(0, 0, 1)
}

func eventMessageKey(event cal.Event, offsetDays int) string {
	return event.UID + "|" + event.Start.Format(time.RFC3339) + fmt.Sprintf("|T-%dd", offsetDays)
}
