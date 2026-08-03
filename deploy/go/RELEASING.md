# Releasing & rolling back production

Production (`@Chevalet_bot`, container `chevalet-go-bot-prod` on the Germany
server) is deployed **only** by pushing a version tag, or by asking for a
specific tag from the Actions tab. Pushing to `main` runs the test suite and
stops there — merging no longer changes what users are talking to.

## Cut a release

```sh
git checkout main && git pull
git tag -a v1.2.0 -m "short summary of what changed"
git push origin v1.2.0
```

CI then runs `build / vet / fmt / test -race` **against that tag**, and only on
success deploys it. Watch the run in the Actions tab; the job is named
`deploy v1.2.0 to production`.

Versioning is plain semver: bump patch for fixes, minor for new behavior. The
tag must match `vX.Y.Z` exactly — the server refuses anything else, including
`v1.2`, `v1.2.3-rc1`, and branch names.

## Roll back (or redeploy an old version)

Actions tab → **CI** → *Run workflow* → enter the tag, e.g. `v1.1.0`.

Rollback does not rebuild: the server keeps the last 5 versioned images
(`chevalet-go-bot:v1.1.0`, …), so it re-points at an image that already exists
and restarts the container. That makes rollback both fast and independent of
whether a build would still succeed right now.

If Actions itself is unavailable, the same thing by hand:

```sh
ssh germany
tail -f /var/log/chevalet-deploy.log        # what past deploys did
cd /opt/chevalet-go-staging && git fetch --tags
git checkout --detach v1.1.0
cd deploy/go && IMAGE_TAG=v1.1.0 docker compose -p go -f docker-compose.cutover.yml up -d
```

## How the deploy is restricted

`.github/workflows/ci.yml` sends exactly one string over SSH: `deploy vX.Y.Z`.
It does **not** get to choose the command that runs. The deploy key's entry in
the server's `/root/.ssh/authorized_keys` is forced-command restricted to:

```
command="/usr/local/bin/deploy-chevalet.sh",no-agent-forwarding,no-X11-forwarding,no-port-forwarding,no-pty
```

so whatever a holder of that key asks for lands in `SSH_ORIGINAL_COMMAND` and is
treated as untrusted input. [`deploy-tag.sh`](deploy-tag.sh) — the source of
truth for that script — extracts a tag only if the payload matches
`^v[0-9]+\.[0-9]+\.[0-9]+$`, never eval's it, and refuses everything else with
exit code 2. A compromised CI token therefore cannot run arbitrary commands on
the box; it can only deploy some tag that already exists in this repo.

> Before this, the forced command was a fixed
> `git pull --ff-only && docker compose up -d --build`. That always deployed the
> tracked *branch*, so it could not honor a requested version at all — a
> "deploy v1.2.0" run would have shipped whatever `main` pointed at.

## What the deploy script does

1. Refuses the request unless it is exactly `deploy vX.Y.Z`.
2. `git fetch --tags` and resolves the tag; aborts if it doesn't exist.
3. Records the current commit and image so it can undo the change.
4. Checks out the tag detached and runs `docker compose up -d --build` with
   `IMAGE_TAG` set, so the image is tagged with the version (and `:prod` is
   re-pointed at it afterwards). Skips `--build` when that version's image is
   already on disk.
5. Waits up to 180s for the container's healthcheck to report `healthy`.
6. Waits up to 90s for the `bot polling` log line from *this* container start,
   then settles 20s and re-checks that the container is still running and its
   restart count hasn't moved.
7. **Rolls back automatically** to the previous commit and image if any of that
   fails, logs the container's last 30 lines, and exits non-zero so CI goes red.
8. Prunes versioned images beyond the newest 5.

### Why healthy alone isn't enough

The Docker healthcheck is a bare TCP connect to the health port, and `main.go`
opens that listener *before* `Run()` calls `StartPolling` — so a container reports
`healthy` before it has ever reached Telegram. If polling then fails, `main`
exits, the restart policy loops the container, and each fresh start re-opens the
health port, so snapshots keep looking fine. A deploy could therefore be reported
green while the bot was dead to users.

Hence the two extra gates: the `bot polling` line is positive proof that
`StartPolling` returned successfully (Telegram accepted `getUpdates`), and the
restart-count comparison across a settle window catches a crash-loop that
instantaneous checks miss.

A single `level=ERROR` after start is **not** a rollback trigger — that is often
ordinary traffic, such as a send to a user who has blocked the bot. Those lines
are counted and logged instead, because auto-reverting a good release on a benign
error would be worse than surfacing it. `panic:` and `level=FATAL` do roll back.

Every run appends to `/var/log/chevalet-deploy.log` on the server.

## Updating the script itself

The script runs from `/usr/local/bin/`, so editing it in the repo is not enough —
after merging a change to `deploy-tag.sh`, reinstall it:

```sh
ssh germany 'install -m 755 /opt/chevalet-go-staging/deploy/go/deploy-tag.sh /usr/local/bin/deploy-chevalet.sh'
```

Note the ordering trap: the running script is what deploys the tag that contains
the new script, so a change to `deploy-tag.sh` takes effect on the deploy
*after* the one that ships it, unless you reinstall by hand as above.
