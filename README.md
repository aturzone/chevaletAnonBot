# Chevalet Anon Bot

A fully featured telegram bot to send messages to other users anonymously

## how does it work

Each telegram user has a telegram ID. The bot doesn't directly use that to minimize
risking user's privacy. it instead generates chevaletid and then encodes that using a
custom encoder defined in `myhelpers.py`. When user A sends a message to user B,
user B receives a message with the encoded chevaletid of user A. when telegram is
processing this message internally, it decodes the chevaletid, fetches the corresponding
uid (telegram userid), does whatever it has to do, and if needed to expose something
again (even to context.chat_data), it re-encodes that.

## How to setup

1. install `docker` and `docker compose`
2. clone the repository
3. run `make up`. if faced some errors you might want to run it again
4. here you can restore a backup. when bot is up, use `make restore` to restore
a specific backup file (backup file path pattern: `backups/backup_*.sql.gz`).  
Tip: you can send a backup file to your server using `scp` command on windows.
5. run `make backup-setup-auto` to add the hourly backup cron job
6. if needed to update the bot: `make update`

### Known issues

- health checker for bot is not working properly

### additional info

- git history might be a bit odd since the conventions has changed a bit. but generally you
can follow these conventions:
    - `feat` for features. anything new and of value
    - `fix` for fixing bugs only
    - `maint` for any maintenance stuff. including refactoring code, updating requirements
        or basically anything