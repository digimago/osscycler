# Security

osscycler's core can change your trainer's resistance, holds your rides
(heart rate included) and, in release builds, the ANT+ network key. If
you find a way to get at any of those that you shouldn't, please tell
the maintainer privately first.

## Reporting a vulnerability

Use GitHub's private reporting: the **Security** tab of this repository,
then **Report a vulnerability**. Please don't open a public issue for
it. Say what you found, how to reproduce it, and which version or
commit you used.

osscycler is maintained in spare time: expect an answer within a couple
of weeks, sooner for anything that lets someone else control a trainer.

## What is in scope

- The core's API (gRPC): every call needs the API token, and the core
  refuses connections from beyond this computer without TLS. A way
  around either is a vulnerability.
- Files the core writes in `~/osscycler`: recordings, results and the
  profile are readable by you alone, and folder contents are never used
  to build paths outside it.
- Release archives and packages: what they install (a udev rule, a
  group, a systemd unit) and the scripts that run as root.
- The ANT+ network key must never appear in the source or in build
  logs.

Only the latest release is supported; fixes go into the next one.
