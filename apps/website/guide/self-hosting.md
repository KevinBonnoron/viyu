# Running a server

The commands, the compose file and the full table of environment variables live in the repository README so they exist in one
place:

- [Quick start with Docker](https://github.com/KevinBonnoron/viyu#docker)
- [Configuration](https://github.com/KevinBonnoron/viyu#configuration)
- [Data and backups](https://github.com/KevinBonnoron/viyu#data-and-backups)
- [Development setup](https://github.com/KevinBonnoron/viyu#development-nix-devshell)

This page covers what to know once it is running.

## What you need

One container (or one `viyu` process) with yt-dlp, FFmpeg and Deno available and a persistent data directory. Put HTTPS in
front of it when it is reachable beyond your network: the apps accept `http://` addresses too. The official image includes everything
and runs as a non-root user.

## First administrator

Register a normal account in the web UI, then promote it with the `viyu admin promote EMAIL` command, run against the data
directory while the server is stopped. The README shows the Docker form. As an alternative, a PocketBase superuser can change
the account's `role` from `user` to `admin` in the dashboard at `/_/`.

The dashboard superuser is separate from Viyu accounts. It is created from `PB_SUPERUSER_EMAIL` and `PB_SUPERUSER_PASSWORD`
on first start, or through the link printed in the startup log.

## Registration and users

Public registration is on by default. If the server is for a few people, turn it off in Administration → Settings and create
accounts from Administration → Users.

## Sources and yt-dlp

All extraction goes through yt-dlp, and the server limits how many yt-dlp processes run at once (default 6,
`VIYU_YTDLP_CONCURRENCY`). Raise it on a strong host, lower it on a small one. Searches and page views queue instead of
overloading the machine.

YouTube changes often. If videos fail to play while their metadata still loads, update yt-dlp: the published image is
rebuilt every week with the latest release, so pulling the image is usually enough.

PeerTube only contacts the instances in `VIYU_PEERTUBE_INSTANCES`; add the ones your users follow.

## Connected-source key

Connected-source cookies are encrypted with `source_connections.key` in the data directory, or the file named by
`VIYU_SOURCE_KEY_FILE`. Back it up with the data; if it is lost, users must reconnect their sources.

## Backups and upgrades

Copy the whole data directory (and the key file if it is stored elsewhere) while the server is stopped, or use the
dashboard's backup feature. Upgrade by pulling the new image and restarting.

Viyu is pre-alpha: the database schema is a single snapshot, and a release that changes it requires an **empty data
directory**. Check the repository for schema changes before upgrading, and keep the old directory until the new instance works.

## Health

`/api/health` answers when the server is up. The Docker image uses `viyu healthcheck` (set `VIYU_HEALTHCHECK_URL` if you
change the listen address).
