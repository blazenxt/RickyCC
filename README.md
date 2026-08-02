# ⚕️ Premium Card — Telegram Refer & Unlock Bot

**Premium Card** is a Telegram bot built around a simple growth funnel:

1. **Force-join** — users must join your channels (supports **2+ channels**) before they can use the bot.
2. **Refer** — each user gets a personal referral link; every **5 friends** they refer (each friend also has to pass the channel check to count) unlock **one reward card**.
3. **Unlock, repeatedly** — there is no lifetime cap: `n` referrals = 1 card, `2n` = 2 cards, `5n` = 5 cards (e.g. 25 referrals at target 5 → 5 **different** cards). Users tap **🎁 Claim Reward** to collect each unlocked card, one per tap.
4. **You stock the rewards** — the owner adds cards with `/addcard`; the bot issues every physical card **exactly once, system-wide, atomically**, so no card code is ever given to two users. If the stock runs out, unlocked rewards stay saved for after the restock.

> ⚠️ Only distribute cards you are legally allowed to share (gift cards, vouchers, promo codes, etc.). The authors take no responsibility for misuse.

---

## Features

- **Branded reward delivery** — claimed cards are delivered as a photo caption on the bundled **Code-Stride** graphic (embedded in the binary, uploaded to Telegram once then reused via file_id): card, validity, and your how-to-use text.
  - 🆘 **Support button** under every delivery — link set from the panel (`https://t.me/...` or `off`)
  - 📖 **How-to-use text** — fully editable from the panel (`default` restores)
- **Multi-channel force subscribe** — comma-separated channel list, one join prompt listing every missing channel, referral payload survives the "Try again" flow.
- **Repeat rewards per N referrals** — every time a user completes the referral target a new card unlocks; pending unlocks never expire and survivors of an out-of-stock wave collect after the restock.
- **Referral tracking with progress bar** — users always see progress toward their *next* card and get a milestone ping every time a new card unlocks.
- **Atomic reward claims** — conditional writes inside a single transaction guarantee: one physical card per issue, never more cards than earned unlocks, nothing spent when the stock is empty.
- **Privacy hardened** — users can only view their own info/progress (owner exempt).
- **Zero external services** — embedded **SQLite** database, just a single `bot.db` file. No MongoDB, no hosted DB to configure.
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
- `/broadcast` — reply to any message to send it to all users.
- `/cancel` — abort an active panel input (find-user / add-cards).

---

## Setup

### Prerequisites
- **Go** 1.23+ (or Docker) — _no external database needed, SQLite is embedded_
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

---

## License

MIT — see [LICENSE](LICENSE).
