package cal

import (
	"log"
	"testing"
)

func TestValidPhoneNumbers(t *testing.T) {
	tests := map[string]string{
		"+436604670967":   "+436604670967",
		"06604670967":     "+436604670967",
		"0660 4670967":    "+436604670967",
		"0660 46 70 967":  "+436604670967",
		"0660 (4670967)":  "+436604670967",
		"43 660 4670967":  "+436604670967",
		"+43 660 4670967": "+436604670967",
		"----( Video Call )----\ntel:06604670967\n---===---": "+436604670967",
	}

	for in, out := range tests {
		num := textPhoneNumber(in)
		if num == nil {
			t.Fatalf("phone number expected for %s", in)
		}

		if is, want := format(num), out; is != want {
			log.Fatalf("%s (from %s) != %s", is, in, want)
		}
	}
}

func TestEventPhoneNumberWithBidiControls(t *testing.T) {
	tests := []struct {
		name  string
		event Event
		want  string
	}{
		{
			name:  "Apple Calendar description",
			event: Event{Description: "\u202a067681429806\u202c"},
			want:  "+4367681429806",
		},
		{
			name:  "summary",
			event: Event{Summary: "\u202a067681429806\u202c"},
			want:  "+4367681429806",
		},
		{
			name:  "comment",
			event: Event{Comment: "\u202a067681429806\u202c"},
			want:  "+4367681429806",
		},
		{
			name:  "directional marks",
			event: Event{Description: "\u061c\u200e067681429806\u200f"},
			want:  "+4367681429806",
		},
		{
			name:  "directional embeddings and overrides",
			event: Event{Description: "\u202a\u202b\u202d\u202e067681429806\u202c"},
			want:  "+4367681429806",
		},
		{
			name:  "directional isolates",
			event: Event{Description: "\u2066\u2067\u2068067681429806\u2069"},
			want:  "+4367681429806",
		},
		{
			name:  "embedded controls in multiline description",
			event: Event{Description: "Appointment\ntel:\u202a0676\u200e81429806\u202c\nNotes"},
			want:  "+4367681429806",
		},
		{
			name:  "controls without a phone number",
			event: Event{Description: "\u202aAppointment\u202c"},
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EventPhoneNumber(tt.event); got != tt.want {
				t.Fatalf("EventPhoneNumber(%+v) = %q, want %q", tt.event, got, tt.want)
			}
		})
	}
}
