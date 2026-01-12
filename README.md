# smsremind

*Sends SMS reminders for calendar events.*

When executed it loads a list of events within a specific range (see `--offset` argument) from a CalDav server.
It can filter by calendar names (see `--calendars`) and inspects the event properties (summary, description and comment) for phone numbers.
If an event includes a phone number, an sms is sent with a customizable message (see `--sms-template`).

Common use cases is to execute the program everyday at 9AM to check if there are events for tomorrow (`--offset=1`).
If that's the case, send an reminder for the event including the time (`--sms-template="Do not forget the appointment for tomorror at {{.StartTime}}."`)