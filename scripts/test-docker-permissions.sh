#!/bin/sh
set -eu

image="${1:-javinizer:latest}"

docker run --rm \
    -e PUID=1000 \
    -e PGID=1000 \
    -e SUPPLEMENTARY_GIDS=100 \
    --tmpfs /media:rw,uid=1026,gid=100,mode=0770 \
    "${image}" \
    sh -ec '
        test "$(id -u)" = "1000"
        test "$(id -g)" = "1000"
        id -G | tr " " "\n" | grep -qx "100"
        test "$(stat -c "%u:%g" /media)" = "1026:100"
        test -w /media
    '

echo "Docker permission mapping verified: runtime=1000:1000 media=1026:100 supplementary_gid=100"
