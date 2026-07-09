package app

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/brutella/smsremind/cal"
)

type fakeClock struct {
	now time.Time
}

func (f fakeClock) Now() time.Time { return f.now }

type fakeEventSource struct {
	events    []cal.Event
	err       error
	start     time.Time
	end       time.Time
	calendars []string
}

func (f *fakeEventSource) Events(_ context.Context, start, end time.Time, calendars []string, _ *time.Location) ([]cal.Event, error) {
	f.start = start
	f.end = end
	f.calendars = append([]string{}, calendars...)
	if f.err != nil {
		return nil, f.err
	}
	return f.events, nil
}

type sentSMS struct {
	number string
	text   string
}

type fakeSMS struct {
	err  error
	sent []sentSMS
}

func (f *fakeSMS) SendSimpleTextSMS(recipientE164 string, text string) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, sentSMS{number: recipientE164, text: text})
	return nil
}

type fakeStore struct {
	exists   map[string]bool
	marks    []string
	closed   bool
	markErr  error
	closeErr error
}

func (f *fakeStore) Exists(key string) bool {
	return f.exists[key]
}

func (f *fakeStore) Mark(key string) error {
	if f.markErr != nil {
		return f.markErr
	}
	f.marks = append(f.marks, key)
	f.exists[key] = true
	return nil
}

func (f *fakeStore) Close() error {
	f.closed = true
	return f.closeErr
}

type fakeLock struct{ released bool }

func (f *fakeLock) Release() error {
	f.released = true
	return nil
}

func defaultConfig() Config {
	return Config{
		StateDir:        "/tmp",
		OffsetDays:      1,
		Calendars:       []string{"Private"},
		MessageTemplate: "Reminder at {{ .StartTime }}",
		DryRun:          false,
		Timezone:        "Europe/Vienna",
	}
}

func baseEvent() cal.Event {
	return cal.Event{
		UID:     "abc",
		Start:   time.Date(2026, 2, 27, 12, 30, 0, 0, time.UTC),
		End:     time.Date(2026, 2, 27, 13, 0, 0, 0, time.UTC),
		Summary: "+436604670967",
	}
}

func TestRunnerSendsAndMarks(t *testing.T) {
	events := &fakeEventSource{events: []cal.Event{baseEvent()}}
	sms := &fakeSMS{}
	store := &fakeStore{exists: map[string]bool{}}
	lock := &fakeLock{}
	var out bytes.Buffer

	r := Runner{
		EventSource: events,
		SMS:         sms,
		OpenStore: func(string) (IdempotencyStore, error) {
			return store, nil
		},
		AcquireLock: func(string, time.Duration) (Releaser, error) {
			return lock, nil
		},
		Clock:      fakeClock{now: time.Date(2026, 2, 26, 9, 0, 0, 0, time.UTC)},
		Output:     &out,
		LockMaxAge: time.Minute,
	}

	if err := r.Run(context.Background(), defaultConfig()); err != nil {
		t.Fatalf("Run error = %v", err)
	}

	if len(sms.sent) != 1 {
		t.Fatalf("expected 1 sms, got %d", len(sms.sent))
	}
	if len(store.marks) != 1 {
		t.Fatalf("expected 1 mark, got %d", len(store.marks))
	}
	if !lock.released {
		t.Fatalf("expected lock release")
	}
	if out.Len() == 0 {
		t.Fatalf("expected output")
	}
}

func TestRunnerSendsForPhoneNumberInDescription(t *testing.T) {
	e := baseEvent()
	e.Summary = "Matthias Hochgatterer"
	e.Description = "----( Video Call )----\ntel:06604670967\n---===---"
	events := &fakeEventSource{events: []cal.Event{e}}
	sms := &fakeSMS{}
	store := &fakeStore{exists: map[string]bool{}}

	r := Runner{
		EventSource: events,
		SMS:         sms,
		OpenStore: func(string) (IdempotencyStore, error) {
			return store, nil
		},
		AcquireLock: func(string, time.Duration) (Releaser, error) {
			return &fakeLock{}, nil
		},
		Clock:  fakeClock{now: time.Date(2026, 2, 26, 9, 0, 0, 0, time.UTC)},
		Output: &bytes.Buffer{},
	}

	if err := r.Run(context.Background(), defaultConfig()); err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if len(sms.sent) != 1 {
		t.Fatalf("expected 1 sms, got %d", len(sms.sent))
	}
	if got, want := sms.sent[0].number, "+436604670967"; got != want {
		t.Fatalf("sent number = %q, want %q", got, want)
	}
}

func TestRunnerDryRunSkipsSendAndMark(t *testing.T) {
	events := &fakeEventSource{events: []cal.Event{baseEvent()}}
	sms := &fakeSMS{}
	store := &fakeStore{exists: map[string]bool{}}

	r := Runner{
		EventSource: events,
		SMS:         sms,
		OpenStore: func(string) (IdempotencyStore, error) {
			return store, nil
		},
		AcquireLock: func(string, time.Duration) (Releaser, error) {
			return &fakeLock{}, nil
		},
		Clock:  fakeClock{now: time.Date(2026, 2, 26, 9, 0, 0, 0, time.UTC)},
		Output: &bytes.Buffer{},
	}

	cfg := defaultConfig()
	cfg.DryRun = true
	if err := r.Run(context.Background(), cfg); err != nil {
		t.Fatalf("Run error = %v", err)
	}

	if len(sms.sent) != 0 {
		t.Fatalf("expected no sms in dry-run")
	}
	if len(store.marks) != 0 {
		t.Fatalf("expected no marks in dry-run")
	}
}

func TestRunnerSkipsAlreadyMarked(t *testing.T) {
	e := baseEvent()
	key := eventMessageKey(e, 1)
	events := &fakeEventSource{events: []cal.Event{e}}
	sms := &fakeSMS{}
	store := &fakeStore{exists: map[string]bool{key: true}}

	r := Runner{
		EventSource: events,
		SMS:         sms,
		OpenStore: func(string) (IdempotencyStore, error) {
			return store, nil
		},
		AcquireLock: func(string, time.Duration) (Releaser, error) {
			return &fakeLock{}, nil
		},
		Clock:  fakeClock{now: time.Date(2026, 2, 26, 9, 0, 0, 0, time.UTC)},
		Output: &bytes.Buffer{},
	}

	if err := r.Run(context.Background(), defaultConfig()); err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if len(sms.sent) != 0 {
		t.Fatalf("expected no sms for marked event")
	}
}

func TestRunnerMapsLockUnavailable(t *testing.T) {
	r := Runner{
		EventSource: &fakeEventSource{},
		SMS:         &fakeSMS{},
		OpenStore: func(string) (IdempotencyStore, error) {
			return &fakeStore{exists: map[string]bool{}}, nil
		},
		AcquireLock: func(string, time.Duration) (Releaser, error) {
			return nil, ErrLockUnavailable
		},
	}

	err := r.Run(context.Background(), defaultConfig())
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("expected ErrAlreadyRunning, got %v", err)
	}
}

func TestRunnerTemplateError(t *testing.T) {
	r := Runner{
		EventSource: &fakeEventSource{},
		SMS:         &fakeSMS{},
		OpenStore: func(string) (IdempotencyStore, error) {
			return &fakeStore{exists: map[string]bool{}}, nil
		},
		AcquireLock: func(string, time.Duration) (Releaser, error) {
			return &fakeLock{}, nil
		},
	}

	cfg := defaultConfig()
	cfg.MessageTemplate = "{{"
	if err := r.Run(context.Background(), cfg); err == nil {
		t.Fatalf("expected template parse error")
	}
}
