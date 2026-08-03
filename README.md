# ⚕️ Premium Card — Telegram Refer & Unlock Bot

**Premium Card** is a Telegram bot built around a simple growth funnel:

1. **Force-join** — users must join your channels (supports **2+ channels**) before they can use the bot.
2. **Human verification** — new users solve a quick one-tap captcha (4 rotating challenge types) before registering, so scripted join-farms can't farm your referrals.
3. **Refer** — each user gets a personal referral link; every **5 friends** they refer (each friend also has to pass the channel check to count) unlock **one reward card**.
4. **Unlock, repeatedly** — there is no lifetime cap: `n` referrals = 1 card, `2n` = 2 cards, `5n` = 5 cards (e.g. 25 referrals at target 5 → 5 **different** cards). Users tap **🎁 Claim Reward** to collect each unlocked card, one per tap.
5. **You stock the rewards** — the owner adds cards with `/addcard`; the bot issues every physical card **exactly once, system-wide, atomically**, so no card code is ever given to two users. If the stock runs out, unlocked rewards stay saved for after the restock.

> ⚠️ Only distribute cards you are legally allowed to share (gift cards, vouchers, promo codes, etc.). The authors take no responsibility for misuse.

---

## 🚀 One-Click Deploy

Tap a button, fill in `TOKEN` + `OWNER_ID`, done:

[![Deploy to Heroku](https://www.herokucdn.com/deploy/button.svg)](https://heroku.com/deploy?template=https://github.com/blazenxt/RickyCC)
[![Deploy on Railway](https://railway.com/button.svg)](https://railway.com/new/template?template=https%3A%2F%2Fgithub.com%2Fblazenxt%2FRickyCC&envs=TOKEN%2COWNER_ID&TOKENDesc=Bot%20token%20from%20%40BotFather&OWNER_IDDesc=Your%20numeric%20Telegram%20user%20ID)
[![Deploy to Render](https://render.com/images/deploy-to-render-button.svg)](https://render.com/deploy?repo=https://github.com/blazenxt/RickyCC)
[![Deploy to Koyeb](https://www.koyeb.com/static/images/deploy/button.svg)](https://app.koyeb.com/deploy?type=git&repository=github.com/blazenxt/RickyCC&branch=master&builder=dockerfile&env%5BTOKEN%5D=&env%5BOWNER_ID%5D=&ports=8080%3Bhttp%3B%2F)

**Environment variables**

| Variable | Required | Purpose |
|---|:---:|---|
| `TOKEN` | ✅ | Bot token from [@BotFather](https://t.me/BotFather) |
| `OWNER_ID` | ✅ | Your numeric Telegram ID (from [@userinfobot](https://t.me/userinfobot)) |
| `LOGGER_ID` | 💾 | Log chat ID — also the **auto-backup & restore chat** (bot needs pin rights there). Strongly recommended! |
| `FSUB_IDS` | ➖ | Comma-separated force-join channel IDs — seed only, changeable later |
| `DATABASE_URL` | ➖ | PostgreSQL connection string — **Railway injects it automatically** when you add the PostgreSQL plugin. When set, ALL data persists across redeploys (SQLite + TG backups bypassed) |
| `DB_PATH` | ➖ | SQLite file location (default `bot.db`, `/data` in Docker) |
| `PORT` | ➖ | Auto-set by PaaS — enables the built-in health-check server |
| `WEBHOOK_URL`, `SECRET_TOKEN` | ➖ | Switch from polling to webhook mode (advanced) |

**Platform notes**

- 🟣 **Heroku** — uses `app.json` + `Procfile` (Go buildpack, `worker` dyno). ⚠️ Heroku's filesystem is *ephemeral*: `bot.db` resets whenever the dyno restarts (~daily). Great for testing — for living data prefer the options below.
- 🚂 **Railway** — auto-detects the `Dockerfile` (`railway.json` pins builder + start command). **Best:** add the **PostgreSQL** plugin (New → Database → PostgreSQL, link it to the service) — `DATABASE_URL` is injected automatically and the bot stores **everything there, surviving every redeploy** (no volume needed). **Free option:** set `LOGGER_ID` and the bot auto-restores its SQLite DB from the pinned backup in that chat after each redeploy (see *💾 Auto DB backup & restore* below).
- 🎨 **Render** — one-click uses `render.yaml` (free instance, polling + health checks built in). Free instances **sleep after ~15 min without inbound traffic** — ping your service URL every 5 min with a free monitor (e.g. UptimeRobot) to stay awake. Free plan has no persistent disk (DB resets on restart); on a paid plan, enable the commented `disk` block in `render.yaml`.
- 🌊 **Koyeb** — builds from the `Dockerfile`; add a volume mounted at `/data` if you want the DB to persist.
- 🐳 **Any VPS with Docker** — see [manual setup](#-setup) below; one command with a named volume and you're live forever.

---

## Features

- **Branded reward delivery** — claimed cards are delivered as a photo caption on the bundled **Code-Stride** graphic (embedded in the binary, uploaded to Telegram once then reused via file_id): card, validity, and your how-to-use text.
  - 🆘 **Support button** under every delivery — link set from the panel (`https://t.me/...` or `off`)
  - 📖 **How-to-use text** — fully editable from the panel (`default` restores)
- **🎨 Custom emoji icons** — restyle the bot's icons across every message body and the delivery caption: 71 named slots (`card`, `party`, `trophy`, `gift`, `validity`, …). Two ways, set from the panel:
  - **any public emoji** (`card=🔥`) — works instantly, zero restrictions
  - **premium custom emoji IDs** (`card=5402…`) — live-validated with a test-send; works from bot-owned packs, or **any public pack** when the bot has an extra username bought on Fragment
  - **⚡ Premium Set, auto-loaded on boot** — a curated **71-icon premium look** (🎉🥇💎👑📌🔄…) is applied automatically when the bot starts on a fresh deploy (or one tap in the panel anytime) and replaces **every** Unicode icon across all message bodies — including the captcha page (a segment-aware `premiumize()` pass sweeps up even hardcoded literals, with zero double-wrapping). A built-in **live probe** first verifies the bot may send public custom emoji, so it can never break message delivery; an existing hand-made mapping is never overwritten.
  - **Premium icons + colors on buttons too (Bot API 9.4+)** — button labels that begin with a mapped emoji move it into the button's `icon_custom_emoji_id`, so the Home/Claim/Refer/Admin buttons wear the premium look as well. **Every** button is also **color-styled by function** (the `style` JSON field): destructive actions red 🔴 (delete/ban/reset/wipe — even "✅ Yes, delete" stays red), positive CTAs green 🟢 (Claim Reward, Joined — Try Again, unban, and all Add/Create actions), and everything else — navigation, menus, settings, captcha answers, links — blue 🔵. Icon support works when the bot has a Fragment-bought extra username **or** the owner has Telegram Premium; unmapped/plain-emoji labels keep their standard look automatically, and a send-failure safety net downgrades message bodies back to standard icons.
- **Multi-channel force subscribe** — comma-separated channel list, one join prompt listing every missing channel, referral payload survives the "Try again" flow. Membership checks for **all channels run in parallel** (no artificial delays) and invite links are pre-warmed at boot + cached, so /start answers near-instantly even with many channels. **ON/OFF toggle in the panel**: pause the whole gate with one tap (channels are kept, users pass instantly — even old "Try Again" locks open up) and resume anytime; the state persists across redeploys. Join buttons and the admin list show real **channel names** (cached titles, zero extra API calls). **Admin-approval channels supported**: for private channels/groups with "join by request", a *pending join request counts as joined* — the bot listens to `chat_join_request` updates and unlocks the user the moment they tap "Request to Join" (approval is outside the user's control, and getChatMember alone can't see pending requests).
- **Never leaves your chats** — the bot stays put wherever you add it: force-join channels (admin rights needed for invite links + join requests), linked discussion groups, and the log/backup destination. It simply doesn't *work* in groups — a `/start` there gets a one-line "private chats only" redirect, nothing more.
- **Smart "/" menu** — published at boot via setMyCommands: users see `/start`, `/help`, `/progress`, `/info`; the owner + every admin's own chat additionally gets the full toolset (`/admin`, `/addcard`, `/stock`, `/stats`, `/broadcast`, `/backupdb`); groups see no menu at all.
- **Stock-update announcements** — the moment an admin adds fresh cards (panel or `/addcard`), every user gets a branded *✅ Stock Updated!* message (service, batch size, total stock, uploaded-by) with a **"🚀 Open Bot" callback button**: the tap answers with a t.me start-link carrying the configured referral, and because inline keyboards survive forwarding, even strangers tapping a forwarded copy land in the bot with the referral attached. Announcements **also post to channels/groups** — a dedicated **🔔 Stock Alerts** panel section manages the destination list (add/remove/clear, real channel names, bot admin rights verified) plus a one-tap **force-join relay toggle** that forwards every announcement to ALL force-join channels; everything persists across redeploys.
- **Human-verification captcha** — after the channel check, new users solve a one-tap captcha before they can register. Challenges **rotate between 4 types** (🧮 math, 🔢 sequences, 👀 emoji counting, 🕵️ odd-one-out emoji/words) so scripts can't pattern-match a fixed format; a fresh challenge on every wrong tap, 3 tries per challenge, **15-minute lockout after 3 failed challenges**, 30-minute challenge expiry, referral payload held server-side (button data only carries the tapped index). Scripted referral farms are stopped cold.
- **Repeat rewards per N referrals** — every time a user completes the referral target a new card unlocks; pending unlocks never expire and survivors of an out-of-stock wave collect after the restock.
- **Referral tracking with progress bar** — users always see progress toward their *next* card and get a milestone ping every time a new card unlocks.
- **Atomic reward claims** — conditional writes inside a single transaction guarantee: one physical card per issue, never more cards than earned unlocks, nothing spent when the stock is empty.
- **Privacy hardened** — users can only view their own info/progress (owner exempt).
- **Zero external services OR managed PostgreSQL — your choice** — out of the box the bot runs on embedded **SQLite** (single `bot.db` file, nothing to configure). Set `DATABASE_URL` (Railway's PostgreSQL plugin injects it automatically) and the **same code transparently switches to PostgreSQL** — users, cards and settings live in the managed database and survive every redeploy. All queries are portable and placeholder-rebound; the atomic claim gates work identically on both engines.
- **💾 Auto DB backup & restore** (SQLite mode) — survives redeploys even on ephemeral hosts (Railway without a volume, Render free, etc.):
  - every **30 minutes** (+ right after boot, + on demand with `/backupdb`) the bot checkpoints and uploads `bot.db` to the **`LOGGER_ID` chat** and keeps the **latest backup pinned**
  - on boot, if the local DB is missing (fresh container), the **newest pinned backup is downloaded and restored automatically** — users, cards, settings all come back
  - needs: `LOGGER_ID` set to a chat where the bot can post **and pin** (admin rights)
- **Full admin panel (`/admin`)** — interactive inline-keyboard UI:
  - 📊 **Dashboard** — users / banned / claimed / stock at a glance
  - 👥 **User management** — search any user, newest users list, **ban/unban**, **reset claims**, **delete user** (auto-unlinks from referrer)
  - 🎟️ **Card stock** — bulk add with duplicate-skip, recent claims history, purge claimed records
  - 🛠 **Live settings** (persisted in the local DB, no restart needed):
    - 📢 **Force-join setup** — add/remove/clear multiple channels from the panel, bot verifies admin access and prepares invite links automatically
    - 🪵 **Log chat setup** — point claim notifications at any chat the bot can reach
    - 🎯 **Referral target** — change the required referrals anytime
    - 🆘 **Support link** for the reward-delivery button
    - 📖 **How-to-use text** shown under delivered cards
    - 🎨 **Custom emojis** — map 30+ icon slots to your premium custom emoji IDs (live-validated)
    - ⏸️ **Claims pause/resume** — one tap, e.g. while restocking
  - 👑 **Multi-admin** — the owner grants full panel access to extra admins right from the panel (new admins get notified); only the owner (`OWNER_ID`) can add/remove them
  - 📢 **Broadcast** hub
- **Quick owner commands** — `/addcard`, `/stock`, `/stats`, `/broadcast` also work standalone.
- **Duplicate-safe imports** — `/addcard` skips empty lines, in-batch duplicates and cards already in the DB.

---

## Commands

### Users
- `/start` — start the bot, pass the channel check, get your referral link.
- `/progress` — referrals, rewards claimed, progress to your next card, and your latest claimed cards.
- `/info` — view your account details (self only).

### Owner only
- `/admin` — open the full admin panel (dashboard, user management, card stock, broadcast).
- `/addcard` — add reward cards. Paste cards after the command (**one per line**) or **reply** to a message containing the list.
- `/stock` — view available / claimed card counts.
- `/stats` — total users, claimed users, card inventory.
- `/backupdb` — upload & pin a database backup to the `LOGGER_ID` chat right now (auto-backup runs every 30 min anyway).
- `/broadcast` — reply to any message to send it to all users.
- `/userbot` — **Premium channel editor** (see below). Login your Premium account once; from then on every stock announcement the bot posts to your channels is edited in-place so the premium custom emojis render there too.
- `/cancel` — abort an active panel input (find-user / add-cards / userbot login).

---

## 🤖 Premium channel editor (MTProto userbot)

Telegram only lets **bots** use custom emoji in DMs and groups — channel posts with real premium emoji need a paid Fragment collectible username… **or** this workaround:

1. The bot posts the announcement as usual (plain Unicode fallback — always delivered).
2. A logged-in **Premium user account** (an admin with *Edit messages* rights in the channel) sees the post over MTProto and edits it in place, swapping the Unicode emojis for real `custom_emoji` entities.

Net effect: premium emojis **everywhere**, zero purchase.

**Setup:**
1. Grab `API_ID` + `API_HASH` from [my.telegram.org](https://my.telegram.org) → *API development tools*, add them to the env, redeploy.
2. DM the bot `/userbot` and follow the prompts (phone → login code → optional 2FA password). DM-only by design — never type these in a group.
3. Done. The session lives in the `settings` table, so redeploys resume it silently (`BACKUPDB` covers it on SQLite; Postgres persists it natively).

**Requirements:** the account needs Telegram **Premium** and *Edit messages* admin rights in every announcement channel. `/userbot` → **Logout** wipes the stored session instantly.

> ⚠️ The stored session grants **full account access** — treat your database/backups like a password, and never share the session string with anyone. Optional feature: without `API_ID`/`API_HASH` the bot works exactly as before, channels just keep standard emoji.

---

## Setup

### Prerequisites
- **Go** 1.24+ (or Docker) — _no external database needed, SQLite is embedded_
- A **Telegram bot token** from [@BotFather](https://t.me/BotFather)
- Your Telegram user ID (use [@userinfobot](https://t.me/userinfobot))
- Optionally: channel IDs to force-join (can also be set later from the admin panel)

### Configuration

```bash
cp sample.env .env
```

| Variable | Required | Description |
|---|---|---|
| `TOKEN` | ✅ | Bot token from BotFather |
| `OWNER_ID` | ✅ | Your Telegram user ID (super-owner; manages the admin list) |
| `DB_PATH` | ➖ | SQLite file location (default: `bot.db`) |
| `LOGGER_ID` | ➖ | Seeds the log chat on first boot — change later via `/admin` → Settings |
| `FSUB_IDS` | ➖ | Seeds force-join channels (comma-separated) on first boot — manage later via `/admin` → Settings |
| `SECRET_TOKEN`, `WEBHOOK_URL`, `PORT` | ➖ | Webhook mode (defaults to long-polling) |

> ⚙️ All runtime settings (**force-join channels, log chat, referral target, claims pause, admin list**) live in the local SQLite DB and are managed from the **admin panel** — env vars are only used as first-boot seeds.

### Docker (recommended)

```bash
docker build -t premiumcard .
docker run --env-file .env -v premiumcard-data:/data -p 8080:8080 -d premiumcard
```

(The volume keeps your `bot.db` database safe across container rebuilds.)

### Manual

```bash
go build -o premiumcard .
./premiumcard
```

---

## Data model (local SQLite — `bot.db`)

**`users`** — telegram ID, name, referrer, referred user IDs, join date, ban status, rewards-claimed counter (`claims`).
**`cards`** — card text, status (`available`/`claimed`), who claimed it and when.
**`settings`** — runtime config: force-join channels, log chat, referral target, claims pause, admin list.
**`join_requests`** — recorded pending admin-approval join requests (`channel_id`, `user_id`) so force-join can count them as satisfied.

---

## License

MIT — see [LICENSE](LICENSE).
