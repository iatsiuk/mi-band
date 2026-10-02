# miband

Xiaomi Smart Band command-line client. Reads the data Mi Fitness synced to the Xiaomi cloud: devices, steps, sleep, heart rate, SpO2, stress and workouts, including the raw per-second workout record and GPS track. Pure Go, no Bluetooth and no cgo: the band itself is never contacted.

Mi Fitness on the phone uploads band data after it syncs with the band (at most every 5 minutes in the foreground, every 24 hours in the background), so the cloud lags behind the band by that much.

## Installation

### Homebrew

```bash
brew install iatsiuk/tap/miband
```

### From releases

Download the binary from the [GitHub Releases](https://github.com/iatsiuk/mi-band/releases) page.

### From source

```bash
make install
```

## Quick Usage

```bash
# log in once; the session is saved to the user config dir
miband login --user you@example.com

# devices bound to the account
miband devices

# last 7 days of steps, sleep and workouts as markdown tables
miband data

# today only, steps of one device, as JSON
miband data --days 0 --kind steps --did 123456789 -f json

# workouts with decoded per-second record and GPS track
miband data --kind workouts --details -f json

# records as stored in the cloud, including tombstones
miband data --kind heart_rate,spo2 --raw
```

## Commands

| Command | Description |
|---------|-------------|
| `login` | Log into the Xiaomi account and save the cloud session |
| `devices` | List devices bound to the account |
| `data` | Fetch steps, sleep, workouts and other records synced by Mi Fitness |

### login

```bash
# password from a no-echo prompt on a terminal, MIBAND_PASSWORD, or the first line of stdin
miband login --user you@example.com
MIBAND_PASSWORD=secret miband login --user you@example.com

# accounts that require a browser check or captcha
miband login --user-id 123456789 --pass-token 'V1:...'
```

When the account asks for verification, the error prints the URL to open. Finish the check in a browser, then pass the `userId` and `passToken` cookies of `account.xiaomi.com`.

The session is saved with mode 0600 to `~/Library/Application Support/miband/cloud.json` on macOS (`$XDG_CONFIG_HOME/miband/cloud.json` or `~/.config/miband/cloud.json` on Linux). An expired service token is refreshed once with the saved pass token.

### data

| Flag | Default | Description |
|------|---------|-------------|
| `--days` | 7 | Days before today to include, from local midnight; today is always included (0..3650) |
| `--kind` | steps,sleep,workouts | Comma list: `steps`, `sleep`, `workouts`, or any raw record key (`heart_rate`, `spo2`, `stress`, `calories`, `watch_night_sleep`, ...) |
| `--did` | | Only records of this device id (see `miband devices`) |
| `--details` | false | Download and decode raw workout files (per-second record, GPS, recover rate) |
| `--raw` | false | Print cloud records as stored instead of summaries |

Steps are summed per device and local day. Deleted records are skipped unless `--raw` is set. With `--details`, files the cloud does not have (e.g. GPS for indoor sports) are listed as missing; failed downloads mark the result as partial.

## Global Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--format` | `-f` | markdown | Output: markdown, json |
| `--region` | | *(saved login, else China)* | Xiaomi health cloud region (e.g. `sg`, `de`) |
| `--user` | `-u` | | Xiaomi account for password login |
| `--user-id` | | | Numeric `userId` cookie of account.xiaomi.com (with `--pass-token`) |
| `--pass-token` | | | `passToken` cookie of account.xiaomi.com after browser verification |
| `--verbose` | `-v` | false | Log cloud requests (endpoints and sizes only) to stderr |

## Output Formats

- **markdown** (default) - a heading per section and aligned tables; workout detail files are listed by name
- **json** - pretty-printed JSON with every field, including decoded workout detail files

## Environment Variables

| Variable | Description |
|----------|-------------|
| `MIBAND_PASSWORD` | Password for `login --user`, skips the prompt |

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Error |
| 3 | Authentication error: not logged in, unknown account, wrong password, verification required, token rejected |
| 130 | Interrupted (SIGINT/SIGTERM) |
