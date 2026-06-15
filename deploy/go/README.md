# Staging the Go bot

Run the Go port against a throwaway Postgres, isolated from production, to
live-test it with a **staging** bot token. Telegram is unreachable without a
proxy, so a working `PROXY` is required.

## Steps (on a server/host that has Docker + proxy access)

1. Copy the example env next to this compose file:

   ```sh
   cp ../../.env.example deploy/go/.env   # or create deploy/go/.env
   ```

2. Edit `deploy/go/.env`:
   - `BOT_TOKEN` = your **staging** bot token (a second bot from BotFather)
   - `PROXY` = your proxy URL, e.g. `socks5://127.0.0.1:1080` or `http://…`
   - `DB_NAME`, `DB_USER`, `DB_PASS` = anything (a fresh test DB is created)
   - the rest can stay at the example defaults for now

3. Bring it up:

   ```sh
   cd deploy/go
   docker compose up --build
   ```

   The bot runs `CREATE TABLE IF NOT EXISTS` on the fresh DB, connects through
   the proxy, and starts long polling.

4. In Telegram, message the staging bot. The full bot is ported, so you can
   exercise everything end-to-end:
   - **Commands:** `/help`, `/privacy`, `/donate`, `/myuid`, `/bug`,
     `/settings`, `/my_links`, `/cancel`, and `/admin help` (from an `ADMINS`
     uid).
   - **Sending:** open the bot via one of your `/my_links` links
     (`t.me/<bot>?start=<cid>`) in a second account and send a message; check
     the **reply / seen / block / report** buttons on the delivered message,
     the "sent with link N" indicator, and the deletion warning + its delete
     button.
   - **Replies:** use the **⌨️ ارسال جواب** button, then also just *reply* to a
     delivered message (auto-reply); reply to a channel post whose bio/pin holds
     a `?start=` link (reply-to-channel).
   - **Settings:** change name, custom/audio tags, toggle wpp / seen / warning,
     unblock-me / unblock-all.
   - **my_links:** add / remove / rename a link (limit + uniqueness checks).
   - **Media groups** (album), **quotes**, and (if `GM_GROUP_ID` is set + an
     `AI_URL` reachable) the **AI reply** in the GM group.

   Each first message also exercises the `prep` middleware + `init_user` (the
   user row, a cid and a chevaletid are created in the test DB).

`.env` is git-ignored — the token never lands in the repo.

## Notes

- This stack is for staging/verification only; the production cutover (Go bot
  pointed at the **real** shared Postgres) is described in `../../MIGRATION.md`.
- To verify just the database layer against this Postgres, without Telegram:

  ```sh
  DB_HOST=localhost DB_PORT=5432 DB_NAME=… DB_USER=… DB_PASS=… \
    go test ./internal/db/...
  ```
