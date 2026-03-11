# Settings and accounts

Open **Settings** from the account menu at the bottom left of the sidebar. On small screens the sidebar becomes a bottom bar
(Feed, Search, Subscriptions, History, More); **More** opens a sheet with likes, sources, profiles, settings, server selection
and logout. The search field in Settings looks through all tabs, ignoring accents.

## Account

- Your display name and avatar (JPEG, PNG, WebP or GIF, up to 5 MiB) appear in the sidebar. The email address is read-only.
- Changing the password asks for the current one.
- **Your data** exports your profiles, subscriptions, settings, reactions and watch history as one JSON file, and imports such
  a file into another Viyu server. Importing merges: profiles match by name, subscriptions by profile and channel, reactions
  and history by profile and video; nothing is deleted. The file contains your viewing data, so import only into a server
  you trust.

## Profiles

An account can have several profiles, for example one per person. Each profile owns its subscriptions, likes and dislikes,
history, resume positions, notifications and home feed. Account settings and connected sources are shared.

The profile switcher at the top left switches profile and links to **Settings → Profiles**, where you create, rename and delete
them. Deleting a profile deletes its activity, and the last remaining profile cannot be deleted. Each profile has an icon and
an optional colour (ten palettes, teal by default) chosen in the profile dialog; the colour tints the whole application while
that profile is active.

## Appearance and language

**Appearance** offers light, dark or system theme. The theme is a per-device preference, whereas the colour belongs to the
profile. **Theme** in the desktop sidebar or in **More** on mobile toggles between light and dark. **Language** switches the interface between
English and French.

## Playback preferences

Settings → Playback has two groups:

- **Player behaviour**: default quality, autoplay, automatic SponsorBlock skipping.
- **Content and display**: hide Shorts, hide watched videos, hide subscriber-only videos (Twitch) and original titles.

Hiding watched videos applies to feeds, subscriptions and search, never to history or likes, and is based on the active
profile's Viyu history, not your full YouTube history.

## Native apps

The desktop (Linux) and Android apps ask for your server's address (`http://` or `https://`) on first launch and then use the
normal login. Choose **Change server** in the account menu to switch; each server keeps its own sign-in.

## Administration

Users with the `admin` role see an **Administration** section with two pages:

- **Settings**: switches for public registration (on by default; turning it off rejects new sign-ups without affecting
  existing or administrator-created accounts) and for thumbnail OCR, when the server includes it.
- **Users**: list, create, edit and delete accounts and roles. Public sign-up and self-service cannot change roles, you
  cannot delete or demote yourself, and the last administrator is protected.

The first administrator is made from the command line (the PocketBase dashboard at `/_/` is a separate superuser account):
see [Running a server](/guide/self-hosting#first-administrator).

## Licenses

**Licenses and credits** in the page footer shows Viyu's MIT license and the attribution for services such as SponsorBlock and
Return YouTube Dislike.
