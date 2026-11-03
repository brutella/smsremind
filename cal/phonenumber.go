package cal

import (
	"strings"
	"unicode"

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
	// Calendar text can wrap phone numbers in invisible direction controls.
	text = strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Bidi_Control, r) {
			return -1
		}
		return r
	}, text)
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		if pn, err := phonenumbers.Parse(line, "AT"); err == nil {
			return pn
		}
	}

	return nil
}
