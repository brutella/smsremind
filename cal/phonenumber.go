package cal

import (
	"strings"

	"github.com/nyaruka/phonenumbers"
)

// EventPhoneNumber returns the phone number stored in the event.
func EventPhoneNumber(event Event) string {
	for _, str := range []string{event.Summary, event.Description, event.Comment} {
		if pn := textPhoneNumber(str); pn != nil {
			return format(pn)
		}
	}
	return ""
}

func format(num *phonenumbers.PhoneNumber) string {
	return phonenumbers.Format(num, phonenumbers.E164)
}

func textPhoneNumber(text string) *phonenumbers.PhoneNumber {
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		if pn, err := phonenumbers.Parse(line, "AT"); err == nil {
			return pn
		}
	}

	return nil
}
