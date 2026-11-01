#!/bin/sh
set -eu

: "${PGHOST:?PGHOST is required}"
: "${PGUSER:?PGUSER is required}"
: "${PGDATABASE:?PGDATABASE is required}"
: "${PGPASSWORD:?PGPASSWORD is required}"

backup_interval=${BACKUP_INTERVAL_SECONDS:-86400}
retention_days=${BACKUP_RETENTION_DAYS:-14}
case "$backup_interval" in
	''|*[!0-9]*) echo "BACKUP_INTERVAL_SECONDS must be a positive integer" >&2; exit 1 ;;
esac
case "$retention_days" in
	''|*[!0-9]*) echo "BACKUP_RETENTION_DAYS must be a positive integer" >&2; exit 1 ;;
esac
if [ "$backup_interval" -lt 60 ] || [ "$retention_days" -lt 1 ]; then
	echo "Backup interval must be at least 60 seconds and retention at least 1 day" >&2
	exit 1
fi

umask 077
mkdir -p /backups
chmod 0700 /backups

while :; do
	timestamp=$(date -u '+%Y%m%dT%H%M%SZ')
	temporary="/backups/.nodulo-${timestamp}.dump.tmp"
	final="/backups/nodulo-${timestamp}.dump"
	if pg_dump --format=custom --no-owner --no-acl --file="$temporary"; then
		mv "$temporary" "$final"
		echo "$(date -u '+%Y-%m-%dT%H:%M:%SZ') backup completed: $(basename "$final")"
	else
		rm -f "$temporary"
		echo "$(date -u '+%Y-%m-%dT%H:%M:%SZ') backup failed" >&2
	fi
	find /backups -type f -name 'nodulo-*.dump' -mtime "+$retention_days" -delete
	sleep "$backup_interval"
done
