# smsremind

*Sends SMS reminders for calendar events.*

When executed it loads a list of events within a specific range (see `--offset` argument) from a CalDav server.
It can filter by calendar names (see `--calendars`) and inspects the event properties (summary, description and comment) for phone numbers.
If an event includes a phone number, an sms is sent with a customizable message (see `--sms-template`).

## Environment variables

The program expects the following environment variables.

- `ASPSMS_USERKEY`: ASPSMS User Key → www.aspsms.at/
- `ASPSMS_PASSWORD`: ASPSMS API password
- `CALDAV_APPLEID`: The Apple ID for the CalDav server
- `CALDAV_PASSWORD`: The app-specific password for the CalDav server → https://support.apple.com/en-us/102654

## Example

Common use cases is to execute the program everyday at 9AM to check if there are events for tomorrow (`--offset=1`).
If that's the case, send an reminder for the event with a custom message including the start time.

```
ASPSMS_USERKEY=...$ \
ASPSMS_PASSWORD=... \
CALDAV_APPLEID=test@example.com \
CALDAV_PASSWORD=... \
go run main.go \
    --offset=1 \
    --calendars="My Private Calendar" \
    --caldav=https://caldav.icloud.com/ \
    --sms-template="Reminder: Your appointment with me is tomorror at {{.StartTime}}. See you!" \
    --sms-sender="Your Friend"
```

**DISCLAIMER: Some of the code was written by ChatGPT.**

How to configure your Linux server to run.

1. Create a dedicated `smsremind` user.

```
sudo useradd \
  --system \
  --create-home \
  --home-dir /var/lib/smsremind \
  --shell /usr/sbin/nologin \
  smsremind
  ```

2. Store the environmne variables in a file

`sudo vim /var/lib/smsremind/env`

3. Set permissions

```
# User and group "smsremind" owns the /var/lib/smsmremind
sudo chown -R smsremind:smsremind /var/lib/smsremind/
# env file can only be read by the smsremind user.
sudo chmod 600 /var/lib/smsremind/env
```

4. Configure a cron job to run at 9AM.

```
# /etc/cron.d/smsmreind
0 9 * * * /usr/bin/env sh -c 'set -eu . /var/lib/smsremind/.config/smsremind/env; smsremind'
```

5. Set permission
sudo chmod 644 /etc/cron.d/smsremind

