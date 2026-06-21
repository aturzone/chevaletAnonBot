# Production cutover: Python → Go (shared database)

Switch the live ChevaletAnonBot from the **Python** bot to the **Go** bot with
**no data migration** and **instant rollback**.

Why it's safe:

- **Shared DB.** The Go bot connects to the SAME PostgreSQL the Python bot uses
  (`telegram-bot-db`). The schema is byte-for-byte identical, so existing
  users / links / settings / blocks just work. Nothing is copied or transformed.
  The Go bot runs `CREATE TABLE IF NOT EXISTS` on start — a no-op on the live DB.
- **One poller per token.** Telegram allows only one `getUpdates` poller per bot
  token (otherwise HTTP 409). So the cutover is simply: **stop Python → start
  Go**. Updates during the few-second gap are buffered by Telegram (up to 24h)
  and delivered to the Go bot, so nothing is lost.
- **The Python stack is left intact.** We only **stop its container** — its
  compose files, image and DB are untouched — so rollback is just starting it
  again.
- **The cipher is byte-compatible** (golden-tested), so the reply/seen/block/
  report buttons on messages already delivered to users keep working.

> Do NOT run this until the manual testing is signed off and you (the owner) give
> the go-ahead. The Python bot keeps serving until the moment of cutover.

---

## Pre-flight — do BEFORE the cutover window (no downtime)

All commands run on the server (`/opt/chevalet-go-staging`).

**1. Back up the production database (safety net).**
```sh
docker exec telegram-bot-db pg_dump -U <DB_USER> -d <DB_NAME> | gzip > /root/chevalet-prod-$(date +%Y%m%d-%H%M).sql.gz
ls -lh /root/chevalet-prod-*.sql.gz     # confirm it is NOT empty
```
(`<DB_USER>`/`<DB_NAME>` are the production values, from the python `.env`.)

**2. Find the network `telegram-bot-db` is attached to** and put it in
`deploy/go/docker-compose.prod.yml` → `networks.prod.name`:
```sh
docker inspect telegram-bot-db --format '{{range $k,$_ := .NetworkSettings.Networks}}{{println $k}}{{end}}'
```

**3. Create `deploy/go/.env.prod`** = a copy of the **production** `.env`
(`/home/hamid/chevaletbot/.env`). Keep all the real values (BOT_TOKEN,
DB_NAME/DB_USER/DB_PASS for telegram-bot-db, ADMINS, REPORT_CHAT_ID,
ERROR_CHAT_ID, GM_GROUP_ID, AI_URL, DONATION_LINK, HEALTH_PORT, …).
`DB_HOST` is forced to `telegram-bot-db` by the compose, so its value here
doesn't matter. `chmod 600 deploy/go/.env.prod`.

**4. AI dynamic settings continuity** (optional): either
```sh
cp /home/hamid/chevaletbot/dynamic_settings.json deploy/go/prod-dynamic_settings.json
```
or set `AI_URL` / `AI_SESSION_ID` in `.env.prod` (the Go bot falls back to those
when the file is absent) and remove the `volumes:` line in the prod compose.

**5. Pre-build the Go image** so the cutover itself is instant:
```sh
docker compose --env-file deploy/go/.env.prod -p chevalet-go-prod \
  -f deploy/go/docker-compose.prod.yml build
```

**6. (Strongly recommended) Test the Go bot against a COPY of real data** —
restore the step-1 backup into a throwaway Postgres and run the Go bot against it
with the **staging** token, to confirm it reads real production data correctly
(decodes historical chevaletids, real settings/blocks). This never touches prod.
See "Appendix: test on real data" below.

---

## Cutover window (a few seconds of bot downtime)

**1. Stop the Python bot** (releases the Telegram poll lock). Leave
`telegram-bot-db` running.
```sh
docker stop telegram-bot
```

**2. Start the Go bot** on the shared DB:
```sh
docker compose --env-file deploy/go/.env.prod -p chevalet-go-prod \
  -f deploy/go/docker-compose.prod.yml up -d
```

**3. Watch the logs:**
```sh
docker logs -f chevalet-go-bot-prod
```
Expect: `database ready`, `bot polling username=<prod bot>`,
`successfully set the commands`, and NO errors. Confirm health:
```sh
docker inspect chevalet-go-bot-prod --format '{{.State.Health.Status}}'   # -> healthy
```

**4. Smoke test as a real user:**
- `/start`, `/help`, `/settings`, `/my_links`, `/donate`.
- Send an anonymous message via one of your links from a second account.
- **Click reply / seen / block on an OLD, pre-cutover message** — this proves
  the cipher compatibility on historical buttons.
- `/admin help` from an admin uid; check error/report delivery to the group.

---

## Rollback (instant — if anything looks wrong)

```sh
docker compose -p chevalet-go-prod -f deploy/go/docker-compose.prod.yml down
docker start telegram-bot
```
Same database, no data lost — the Python bot resumes polling within seconds.
(If the Go bot wrote any rows in the meantime, they remain valid — the schema is
shared — so even a post-write rollback is clean.)

---

## After it's been stable (days later)

```sh
docker rm telegram-bot          # remove the now-stopped python container
```
Then make the GitHub repo public and archive/delete the old `chevaletAnonBotpython` repo.

---

## Appendix: test on real data (isolated, no prod impact)

```sh
# 1. throwaway pg on its own network
docker network create chev-realtest 2>/dev/null || true
docker run -d --name chev-realtest-db --network chev-realtest \
  -e POSTGRES_DB=realtest -e POSTGRES_USER=test -e POSTGRES_PASSWORD=test postgres:16-alpine
# wait until ready
until docker exec chev-realtest-db pg_isready -U test -d realtest >/dev/null 2>&1; do sleep 1; done

# 2. load the prod backup from pre-flight step 1
gunzip -c /root/chevalet-prod-*.sql.gz | docker exec -i chev-realtest-db psql -U test -d realtest

# 3. run the Go bot against it with the STAGING token (a copy of .env with
#    BOT_TOKEN=<staging>, DB_HOST=chev-realtest-db, DB_NAME=realtest,
#    DB_USER=test, DB_PASS=test), then exercise it from Telegram and verify
#    historical buttons + settings work on the real data.

# 4. teardown
docker rm -f chev-realtest-db && docker network rm chev-realtest
```
