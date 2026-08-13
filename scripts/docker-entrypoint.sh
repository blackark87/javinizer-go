#!/bin/sh
set -e

DEFAULT_UID="${JAVINIZER_IMAGE_DEFAULT_UID:-1000}"
DEFAULT_GID="${JAVINIZER_IMAGE_DEFAULT_GID:-1000}"
container_uid="$(id -u)"
container_gid="$(id -g)"
requested_uid="${PUID:-${USER_ID:-${DEFAULT_UID}}}"
requested_gid="${PGID:-${GROUP_ID:-${DEFAULT_GID}}}"
requested_supplementary_gids="${SUPPLEMENTARY_GIDS:-}"

is_numeric_id() {
    case "$1" in
        ''|*[!0-9]*)
            return 1
            ;;
        *)
            return 0
            ;;
    esac
}

ensure_group_entry() {
    target_gid="$1"

    if awk -F: -v gid="${target_gid}" '$3 == gid { found=1; exit } END { exit !found }' /etc/group; then
        return 0
    fi

    group_name="javinizer"
    if awk -F: -v name="${group_name}" '$1 == name { found=1; exit } END { exit !found }' /etc/group; then
        group_name="javinizer-${target_gid}"
    fi

    addgroup -g "${target_gid}" -S "${group_name}" >/dev/null
}

ensure_user_entry() {
    target_uid="$1"
    target_gid="$2"

    if awk -F: -v uid="${target_uid}" '$3 == uid { found=1; exit } END { exit !found }' /etc/passwd; then
        return 0
    fi

    group_name="$(awk -F: -v gid="${target_gid}" '$3 == gid { print $1; exit }' /etc/group)"
    if [ -z "${group_name}" ]; then
        echo "ERROR: No group entry exists for gid=${target_gid}."
        exit 1
    fi

    user_name="javinizer"
    if awk -F: -v name="${user_name}" '$1 == name { found=1; exit } END { exit !found }' /etc/passwd; then
        user_name="javinizer-${target_uid}"
    fi

    adduser -u "${target_uid}" -G "${group_name}" -s /bin/sh -D "${user_name}" >/dev/null
}

ensure_supplementary_groups() {
    user_name="$1"
    primary_gid="$2"
    group_ids="$3"

    [ -n "${group_ids}" ] || return 0

    old_ifs="${IFS}"
    IFS=','
    for group_id in ${group_ids}; do
        if ! is_numeric_id "${group_id}"; then
            echo "ERROR: Invalid supplementary GID '${group_id}'. Use comma-separated numeric values."
            exit 1
        fi
        [ "${group_id}" = "${primary_gid}" ] && continue

        ensure_group_entry "${group_id}"
        group_name="$(awk -F: -v gid="${group_id}" '$3 == gid { print $1; exit }' /etc/group)"
        if ! awk -F: -v group_name="${group_name}" -v user_name="${user_name}" '
            $1 == group_name {
                count = split($4, members, ",")
                for (i = 1; i <= count; i++) {
                    if (members[i] == user_name) {
                        found = 1
                        exit
                    }
                }
            }
            END { exit !found }
        ' /etc/group; then
            addgroup "${user_name}" "${group_name}" >/dev/null
        fi
    done
    IFS="${old_ifs}"
}

prepare_internal_path() {
    target_path="$1"
    target_uid="$2"
    target_gid="$3"
    recursive="${4:-no}"
    chown_mount_root="${5:-yes}"

    mkdir -p "${target_path}"
    if [ "${target_uid}" = "0" ] && [ "${target_gid}" = "0" ]; then
        return 0
    fi
    if [ "${recursive}" = "yes" ]; then
        if ! chown -R "${target_uid}:${target_gid}" "${target_path}" 2>/dev/null; then
            echo "WARNING: chown -R on ${target_path} partially failed (some files may retain prior ownership)"
        fi
    elif ! awk -v path="${target_path}" '$2 == path { found=1; exit } END { exit found ? 0 : 1 }' /proc/mounts; then
        if ! chown -R "${target_uid}:${target_gid}" "${target_path}" 2>/dev/null; then
            echo "WARNING: chown -R on ${target_path} failed (mount may be read-only)"
        fi
    elif [ "${chown_mount_root}" = "yes" ]; then
        if ! chown "${target_uid}:${target_gid}" "${target_path}" 2>/dev/null; then
            echo "WARNING: chown on ${target_path} failed (mount may be read-only)"
        fi
    fi
}

if ! is_numeric_id "${requested_uid}"; then
    echo "ERROR: Invalid runtime UID '${requested_uid}'. Use PUID or USER_ID with a numeric value."
    exit 1
fi
if ! is_numeric_id "${requested_gid}"; then
    echo "ERROR: Invalid runtime GID '${requested_gid}'. Use PGID or GROUP_ID with a numeric value."
    exit 1
fi

case "${requested_supplementary_gids}" in
    ,*|*,|*,,*)
        echo "ERROR: Invalid supplementary GID list '${requested_supplementary_gids}'. Use comma-separated numeric values."
        exit 1
        ;;
esac

if [ "${container_uid}" = "0" ] && [ "${JAVINIZER_RUNTIME_DROPPED:-0}" != "1" ]; then
    ensure_group_entry "${requested_gid}"
    ensure_user_entry "${requested_uid}" "${requested_gid}"
    runtime_user="$(awk -F: -v uid="${requested_uid}" '$3 == uid { print $1; exit }' /etc/passwd)"
    drop_identity="${requested_uid}:${requested_gid}"
    if [ -n "${requested_supplementary_gids}" ]; then
        runtime_gid="$(awk -F: -v uid="${requested_uid}" '$3 == uid { print $4; exit }' /etc/passwd)"
        if [ "${runtime_gid}" != "${requested_gid}" ]; then
            echo "ERROR: uid=${requested_uid} already exists with gid=${runtime_gid}, not requested gid=${requested_gid}."
            echo "       Supplementary groups require an account whose primary GID matches PGID."
            exit 1
        fi
        ensure_supplementary_groups "${runtime_user}" "${requested_gid}" "${requested_supplementary_gids}"
        drop_identity="${runtime_user}"
    fi
    prepare_internal_path /javinizer "${requested_uid}" "${requested_gid}" yes
    # Preserve ownership on bind/NFS media mounts. Access is granted through
    # SUPPLEMENTARY_GIDS instead of changing the NAS-side numeric owner/group.
    prepare_internal_path /media "${requested_uid}" "${requested_gid}" no no
    export JAVINIZER_RUNTIME_DROPPED=1
    # When supplementary groups are configured, drop_identity is the user name
    # so su-exec initializes memberships from /etc/group instead of discarding
    # them. The numeric uid:gid path retains the legacy behavior otherwise.
    exec su-exec "${drop_identity}" /usr/local/bin/docker-entrypoint.sh "$@"
fi

container_uid="$(id -u)"
container_gid="$(id -g)"

if [ -n "${requested_uid}" ] && [ "${requested_uid}" != "${container_uid}" ]; then
    echo "WARNING: Requested UID (${requested_uid}) does not match runtime UID (${container_uid})."
    echo "         Ensure container is started with matching user mapping."
fi
if [ -n "${requested_gid}" ] && [ "${requested_gid}" != "${container_gid}" ]; then
    echo "WARNING: Requested GID (${requested_gid}) does not match runtime GID (${container_gid})."
    echo "         Ensure container is started with matching user mapping."
fi

# Preflight write checks for mounted state directory.
if [ ! -d "/javinizer" ]; then
    echo "ERROR: /javinizer does not exist. Check your volume mapping."
    exit 1
fi

if ! mkdir -p /javinizer/logs /javinizer/cache /javinizer/temp; then
    echo "ERROR: Unable to create /javinizer/logs or /javinizer/cache."
    echo "       Container is running as uid=${container_uid} gid=${container_gid}."
    echo "       On Unraid, set PUID/PGID (or USER_ID/GROUP_ID) to match share ownership."
    exit 1
fi

javinizer_probe="/javinizer/.javinizer-write-test.$$"
if ! (umask 077 && : > "${javinizer_probe}") 2>/dev/null; then
    echo "ERROR: /javinizer is not writable by uid=${container_uid} gid=${container_gid}."
    echo "       Fix directory ownership/permissions or adjust PUID/PGID."
    exit 1
fi
rm -f "${javinizer_probe}" 2>/dev/null || true

# /media may be intentionally read-only for scan-only usage. Warn instead of failing.
if [ -d "/media" ]; then
    media_writable=0
    media_probe="/media/.javinizer-write-test.$$"
    if (umask 077 && : > "${media_probe}") 2>/dev/null; then
        rm -f "${media_probe}" 2>/dev/null || true
        media_writable=1
    else
        # Some environments keep /media root read-only while allowing writes
        # in specific subdirectories. Check existing children before warning.
        for media_dir in /media/*; do
            [ -d "${media_dir}" ] || continue
            media_probe="${media_dir}/.javinizer-write-test.$$"
            if (umask 077 && : > "${media_probe}") 2>/dev/null; then
                rm -f "${media_probe}" 2>/dev/null || true
                media_writable=1
                break
            fi
        done
    fi

    if [ "${media_writable}" -eq 0 ]; then
        echo "WARNING: /media is not writable by uid=${container_uid} gid=${container_gid}."
        echo "         Scan/review works, but organize/move/copy operations may fail."
    fi
fi

# Config initialization is handled by the app (config.LoadOrCreate) so Docker
# and non-Docker flows share the same default generation path.

# Execute the main command
exec "$@"
