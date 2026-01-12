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
