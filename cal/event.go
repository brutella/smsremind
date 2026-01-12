package cal

import (
	"fmt"
	"strings"
	"time"
)

type Event struct {
	UID         string
	Start       time.Time
	End         time.Time
	Summary     string
	Description string
	Comment     string
}

func (event Event) String() string {
	var properties = []string{}
	if len(event.Summary) > 0 {
		properties = append(properties, fmt.Sprintf("summary: %s", event.Summary))
	}

	if len(event.Description) > 0 {
		properties = append(properties, fmt.Sprintf("description: %s", event.Description))
	}

	if len(event.Comment) > 0 {
		properties = append(properties, fmt.Sprintf("comment: %s", event.Comment))
	}

	return fmt.Sprintf("%s %s – %s (%s)", event.Start.Format(time.DateOnly), event.Start.Format(time.Kitchen), event.End.Format(time.Kitchen), strings.Join(properties, ", "))
}

func (e Event) StartDate() string {
	return e.Start.Format(time.DateOnly)
}

func (e Event) StartTime() string {
	return fmt.Sprintf("%02d:%02d", e.Start.Hour(), e.Start.Minute())
}

func (e Event) EndTime() string {
	return fmt.Sprintf("%02d:%02d", e.End.Hour(), e.End.Minute())
}
