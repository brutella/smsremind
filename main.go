package main

import (
	"bytes"
	"flag"
	"fmt"
	"html/template"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	ics "github.com/arran4/golang-ical"
	"github.com/brutella/smsremind/aspsms"
	"github.com/brutella/smsremind/idempotency"
	"github.com/nyaruka/phonenumbers"
)

var ical = flag.String("ical", "", "URL of an ical calendar")
var offset = flag.Int("offset", 0, "The number of days before an event when an SMS is sent.")
var stateDir = flag.String("state-dir", ".", "Directory used to store internal states.")
var msg = flag.String("msg", "Your next appointment is on {{ .StartDate }} at {{ .StartTime }}", "The SMS template")
var sender = flag.String("sender", "SMS", "The SMS originator name.")
var dryRun = flag.Bool("dry-run", true, "Do not send SMS – only print.")
var aspsmsUserkey = flag.String("aspsms-userkey", "", "The ASPSMS Userkey")
var aspsmsApiPwd = flag.String("aspsms-password", "", "The ASPSMS API password")

type MsgProps struct {
	Start time.Time
	End   time.Time
}

func (p MsgProps) StartDate() string {
	return p.Start.Format(time.DateOnly)
}

func (p MsgProps) StartTime() string {
	return fmt.Sprintf("%02d:%02d", p.Start.Hour(), p.Start.Minute())
}

func (p MsgProps) EndTime() string {
	return fmt.Sprintf("%02d:%02d", p.End.Hour(), p.End.Minute())
}

func main() {
	flag.Parse()
	if len(*stateDir) == 0 ||
		len(*ical) == 0 ||
		(*dryRun == false && (len(*aspsmsUserkey) == 0 || len(*aspsmsApiPwd) == 0)) {
		flag.PrintDefaults()
		return
	}

	cal, err := ics.ParseCalendarFromUrl(*ical)
	if err != nil {
		log.Fatal(err)
	}

	msgTmpl, err := template.New("output").Parse(*msg)
	if err != nil {
		log.Fatal(err)
	}

	lockPath := filepath.Join(*stateDir, "simremind.lock")
	lock, err := idempotency.AcquireLock(lockPath, 1*time.Minute)
	if err != nil {
		// Another instance is running or lock is valid → exit quietly
		os.Exit(0)
	}
	defer lock.Release()

	statePath := filepath.Join(*stateDir, "sent.json")
	store, err := idempotency.Open(statePath)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	client := aspsms.NewClient(*aspsmsUserkey, *aspsmsApiPwd, *sender, 5*time.Second)

	for _, c := range cal.Components {
		switch event := c.(type) {
		case *ics.VEvent:
			start, err := event.GetStartAt()
			if err != nil {
				fmt.Fprintf(os.Stdout, "warning: getting start date failed (%s)\n", err)
				continue
			}
			end, err := event.GetEndAt()
			if err != nil {
				fmt.Fprintf(os.Stdout, "warning: getting end date failed (%s)\n", err)
				continue
			}

			if !shouldSendEventMsgOnDate(event, time.Now()) {
				continue
			}

			num := eventPhoneNumber(event)
			if num == nil {
				continue
			}

			props := MsgProps{Start: start, End: end}

			// Generate a new message
			var buf bytes.Buffer
			if err := msgTmpl.Execute(&buf, props); err != nil {
				log.Fatal(err)
			}
			msg := buf.String()

			// Get the formatted phone number
			e164 := phonenumbers.Format(num, phonenumbers.E164)

			key := eventMessageKey(event)
			if store.Exists(key) {
				// Skip messages which where already sent.
				continue
			}
			err = store.Mark(key)

			if *dryRun {
				fmt.Fprintf(os.Stdout, "Sending message...\n\tEvent: %s\n\tMessage: %s\n", eventString(event), msg)
			} else {
				if err := client.SendSimpleTextSMS(e164, msg); err != nil {
					fmt.Fprintf(os.Stdout, "sending failed: %s\n", err)
				} else {
					fmt.Fprintf(os.Stdout, "sent SMS to %s", e164)
				}
			}
		}
	}
}

// The UUID of a message related to an event.
func eventMessageKey(event *ics.VEvent) string {
	start, _ := event.GetStartAt()
	return event.Id() + "|" + start.Format(time.RFC3339) + fmt.Sprintf("|T-%dday|D-%t", *offset, *dryRun)
}

func eventString(event *ics.VEvent) string {
	start, _ := event.GetStartAt()
	end, _ := event.GetEndAt()

	var properties = []string{}
	if summary := eventPropertyValue(event, ics.ComponentPropertySummary); len(summary) > 0 {
		properties = append(properties, fmt.Sprintf("summary: %s", summary))
	}

	if description := eventPropertyValue(event, ics.ComponentPropertyDescription); len(description) > 0 {
		properties = append(properties, fmt.Sprintf("description: %s", description))
	}

	if comment := eventPropertyValue(event, ics.ComponentPropertyComment); len(comment) > 0 {
		properties = append(properties, fmt.Sprintf("comment: %s", comment))
	}

	return fmt.Sprintf("%s %s – %s (%s)", start.Format(time.DateOnly), start.Format(time.Kitchen), end.Format(time.Kitchen), strings.Join(properties, ", "))
}

func eventPropertyValue(event *ics.VEvent, ptype ics.ComponentProperty) string {
	if property := event.GetProperty(ptype); property != nil {
		return property.Value
	}

	return ""
}

// / Returns true if a message should be sent on a specific date.
func shouldSendEventMsgOnDate(event *ics.VEvent, date time.Time) bool {
	start, _ := event.GetStartAt()
	offsettedTime := date.AddDate(0, 0, *offset)
	return start.Year() == offsettedTime.Year() && start.YearDay() == offsettedTime.YearDay()
}

func eventPhoneNumber(event *ics.VEvent) *phonenumbers.PhoneNumber {
	var pTypes = []ics.ComponentProperty{ics.ComponentPropertySummary, ics.ComponentPropertyDescription, ics.ComponentPropertyComment}
	for _, ptype := range pTypes {
		if property := event.GetProperty(ptype); property != nil {
			if pn := textPhoneNumber(property.Value); pn != nil {
				return pn
			}
		}
	}
	return nil
}

func textPhoneNumber(text string) *phonenumbers.PhoneNumber {
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		if pn, err := phonenumbers.Parse(line, ""); err == nil {
			return pn
		}
	}

	return nil
}
