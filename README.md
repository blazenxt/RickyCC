# ⚕️ Premium Card — Telegram Refer & Unlock Bot

**Premium Card** is a Telegram bot built around a simple growth funnel:

1. **Force-join** — users must join your channels (supports **2+ channels**) before they can use the bot.
2. **Refer** — each user gets a personal referral link and must refer **5 friends** (each friend also has to pass the channel check to count).
3. **Unlock** — after completing the target, the user taps **🎁 Claim Reward** and instantly receives **one reward card** from the stock.
4. **You stock the rewards** — the owner adds cards with `/addcard`, and the bot hands out **exactly one per user**, atomically, so no card is ever given twice.

> ⚠️ Only distribute cards you are legally allowed to share (gift cards, vouchers, promo codes, etc.). The authors take no responsibility for misuse.

---

## Features

- **Multi-channel force subscribe** — comma-separated channel list, one join prompt listing every missing channel, referral payload survives the "Try again" flow.
- **Referral tracking with progress bar** — users see `X/5` progress everywhere and get a milestone ping on every successful referral.
- **Atomic reward claims** — `FindOneAndUpdate` guarantees one card per card, one claim per user; stock runs out gracefully ("come back soon").
- **Privacy hardened** — users can only view their own info/progress (owner exempt).
- **Full admin panel (`/admin`)** — interactive inline-keyboard UI:
  - 📊 **Dashboard** — users / banned / claimed / stock at a glance
  - 👥 **User management** — search any user, newest users list, **ban/unban**, **reset claim**, **delete user** (auto-unlinks from referrer)
  - 🎟️ **Card stock** — bulk add with duplicate-skip, recent claims history, purge claimed records
  - 📢 **Broadcast** hub
- **Quick owner commands** — `/addcard`, `/stock`, `/stats`, `/broadcast` also work standalone.
- **Duplicate-safe imports** — `/addcard` skips empty lines, in-batch duplicates and cards already in the DB.

---

## Commands

### Users
- `/start` — start the bot, pass the channel check, get your referral link.
- `/progress` — check your referral progress (`X/5`).
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
- **Go** 1.23+ (or Docker)
- A **MongoDB** URI
- A **Telegram bot token** from [@BotFather](https://t.me/BotFather)
- Your Telegram user ID (use [@userinfobot](https://t.me/userinfobot))
- The channel IDs you want to force-join (**bot must be admin** in each so it can check membership and create invite links)

### Configuration

```bash
cp sample.env .env
```

| Variable | Description |
|---|---|
| `TOKEN` | Bot token from BotFather |
| `OWNER_ID` | Your Telegram user ID (admin commands) |
| `LOGGER_ID` | Chat ID where claim notifications go |
| `FSUB_IDS` | Comma-separated channel IDs, e.g. `-100111,-100222` |
| `MONGO_URI` | MongoDB connection string |
| `SECRET_TOKEN`, `WEBHOOK_URL`, `PORT` | Optional — webhook mode (defaults to long-polling) |

The referral target (default **5**) and brand name are constants at the top of `main.go`.

### Docker (recommended)

```bash
docker build -t premiumcard .
docker run --env-file .env -p 8080:8080 -d premiumcard
```

### Manual

```bash
go build -o premiumcard .
./premiumcard
```

---

## Data model (MongoDB)

**`users`** — telegram ID, referrer, referred user IDs, claim status & claimed card.
**`codes`** — code text, status (`available`/`claimed`), who claimed it and when.

---

## License

MIT — see [LICENSE](LICENSE).
