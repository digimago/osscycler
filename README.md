# osscycler

Self-hosted indoor cycling for Linux. osscycler drives an ANT+ smart
trainer: ride your own GPX routes with the trainer following the
gradient, race your best time as a ghost, or do structured ERG workouts.
Every ride is recorded as a FIT file on your own disk. No account, no
cloud, no subscription.

What it does today:

- **Course rides** on any GPX route you've ridden or planned: the trainer
  follows the grade, your speed comes from a physics simulation, and the
  clock stops at the line.
- **Ghosts**: every course ride races your personal best on it, with the
  gap shown live.
- **ERG workouts**: the trainer holds each target power. Write workouts
  in the built-in editor, or bring Zwift `.zwo` files.
- **Free riding**, with or without a fixed power, grade or resistance.
- **Recording**: every ride is saved as a FIT file, ready to upload by
  hand to intervals.icu, Garmin Connect or anywhere else.

The screen is a terminal app (the TUI), with big numbers you can read
from the bike. A 3D view is planned.

## What you need

- A computer running Linux: a PC, or a Raspberry Pi next to the trainer.
- An ANT+ USB stick: Garmin/Dynastream ANTUSB2 or ANTUSB-m (Tacx sells
  the same stick). Put it on a USB extension cable near the trainer, away
  from USB 3 ports and Wi-Fi.
- A smart trainer with ANT+ FE-C. osscycler is developed with a Tacx Flux 2.
- Optional: an ANT+ heart rate strap.

## Install

Download the release for your system from the
[releases page](https://github.com/digimago/osscycler/releases).

**Debian, Ubuntu, Raspberry Pi OS:**

```sh
sudo apt install ./osscycler_*_arm64.deb    # or _amd64.deb on a PC
```

**Fedora:**

```sh
sudo dnf install ./osscycler-*.aarch64.rpm  # or .x86_64.rpm on a PC
```

The package installs `osscycler-core` and `osscycler-tui`, and sets up
access to the ANT+ stick. If you'll run osscycler from a desktop session,
that's all. Over SSH (a Raspberry Pi, say), also join the `ant` group,
then log out and in again:

```sh
sudo usermod -aG ant $USER
```

**Other Linux:** unpack the `_linux_amd64.tar.gz` or `_linux_arm64.tar.gz`,
put both programs on your `PATH` (e.g. `/usr/local/bin`), and set up the
stick from the unpacked folder:

```sh
sudo ./install-udev.sh
```

Then log out and in again, and replug the stick.

**macOS:** the `_darwin_universal.tar.gz` runs on Intel and Apple
Silicon. On a Mac, osscycler can't drive the ANT+ stick yet: use it to
connect to osscycler on a Linux machine, or to try it out without a
trainer (see below). The programs aren't signed yet, so after unpacking
run `xattr -d com.apple.quarantine osscycler-*`.

## First ride

osscycler is two programs: the **core** talks to the trainer, records
your rides and keeps your settings; the **TUI** is the screen. Start the
core:

```sh
osscycler-core
```

The first time, it makes the `osscycler` folder in your home directory.
Put your GPX routes in `~/osscycler/courses` (and any `.zwo` workouts in
`~/osscycler/workouts`), then restart the core. In a second terminal,
start the screen:

```sh
osscycler-tui
```

The first time, osscycler asks for your weight and your FTP. Your weight
sets how climbs feel and how fast you go on a course; your FTP sets the
targets of ERG workouts. Not sure of your FTP? Take the suggestion and
change it later.

Before the first ride, do a spin-down calibration when the trainer asks
for one (press `c` and follow the screen).

**No trainer at hand?** Start the core with `-fake` instead: a simulated
rider pedals for you, so you can try everything.

## Riding

On the dashboard you see power, heart rate, cadence and speed.

| Key | Does |
| --- | --- |
| `r` | Open the picker: COURSES, WORKOUTS, HISTORY, ACTIVITIES (`tab` switches) |
| `+` / `-` | Trainer difficulty on courses, in 10 % steps (50 % is Zwift's default) |
| `w` / `g` / `l` | Free riding at a fixed power (watts), grade (%) or resistance level (%) |
| `p` | Your profile: weight and FTP |
| `c` / `C` | Spin-down calibration: when the trainer asks / any time |
| `e` | End the ride: save it (`enter`) or discard it (`d d`) |
| `q` | Quit the screen (the core keeps running) |

**Course rides.** Pick a course and press `enter`; the clock starts when
you start pedalling. The tiles show power, grade, time and distance to
go, and the strip at the bottom shows the next 250 m, coloured by
steepness. If you've ridden the course before, you race your best time:
the gap shows under the time, and a marker shows your ghost on the
course. `x` twice aborts.

**Workouts.** Pick one in WORKOUTS and press `enter`; the trainer holds
each target. `+` / `-` adjust the intensity in 1 % steps, `n` skips to
the next part, `x` twice aborts. To write a workout press `n` (new) or
`e` (edit) for the editor, or `E` to edit it as text in your own editor.
To use Zwift workouts, copy your `.zwo` files into the workouts folder.

**History.** HISTORY lists your course rides with your personal bests
(★). A personal best counts for the same course from the same start.

## Your rides and settings

Everything lives in one folder, `~/osscycler`, easy to back up:

| What | Where |
| --- | --- |
| Your GPX routes | `~/osscycler/courses/` |
| Workouts | `~/osscycler/workouts/` |
| Recorded rides (FIT) and course results | `~/osscycler/rides/` |
| Your profile (weight, FTP, difficulty) | `~/osscycler/profile.json` |
| The key the screen uses to talk to the core | `~/osscycler/api-token` (keep it private) |

To keep it somewhere else, set `OSSCYCLER_HOME` to another folder.

Every ride is recorded from the first pedal stroke, pauses when you stop,
and is saved when you end it, after 5 minutes without riding, or when the
core stops. To get a ride's FIT file onto the machine you're using, pick
it in ACTIVITIES and press `s`: it's saved to `~/Downloads`. Upload it
wherever you like. Rides on a course include the route's map position,
so mind your privacy settings if you share them, or start the core with
`-record-gps=false`.

## A Raspberry Pi by the trainer

A Pi with the ANT+ stick can run the core on its own, from boot, without
anyone logged in. With the package installed:

```sh
sudo systemctl enable --now osscycler@$USER
```

It runs as you and uses your `~/osscycler`, and it doesn't need you in
the `ant` group. It waits for the ANT+ stick and picks it up whenever
it's plugged in, also after you've pulled it out. Its log:
`journalctl -u osscycler@$USER -f`. (From the archive, copy
`osscycler@.service` to `/etc/systemd/system/` first, and put the programs
in `/usr/bin`.)

The simplest way to see the screen is over SSH: log in to the Pi and run
`osscycler-tui` there.

To run the TUI on another machine instead, the core must listen on the
network with TLS (it can change your trainer's resistance, so it refuses
to do that unencrypted): start it with
`-addr 0.0.0.0:7420 -tls-cert cert.pem -tls-key key.pem`, copy the Pi's
`~/osscycler/api-token` to the other machine, and start the TUI with
`-addr pi.local:7420 -tls-ca ca.pem -token-file api-token`.

## Good to know

- Zwift is a trademark of Zwift, Inc. osscycler reads and writes Zwift's
  `.zwo` workout files; it is not affiliated with Zwift.
- osscycler is young. On a Tacx Flux 2, calibration and the trainer
  following a grade or an ERG target have been checked; a full course
  ride and a full workout on a real trainer are next, as is pairing a
  real heart rate strap. Other FE-C trainers should work, but haven't
  been tried.

## How osscycler is made

osscycler is developed with AI assistance. Most of the code, tests and
documentation are written by Claude (Anthropic's AI model, working in
Claude Code), directed by the maintainer, who decides what gets built
and how. The working notes behind it (design decisions, protocol notes,
conventions) are in [CLAUDE.md](CLAUDE.md). Every change goes through
the automated checks (`make check`); behaviour on a real trainer is only
claimed where this README says it has been tried.

Want to build osscycler yourself or work on it? See
[DEVELOPMENT.md](DEVELOPMENT.md).
