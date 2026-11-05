#!/bin/bash
set -euo pipefail

[[ $EUID == 0 ]] || { echo "Run with sudo bash scripts/generate-test-media.sh" >&2; exit 1; }

media_dir=/tmp/media

for channel in red green blue; do
    path=$media_dir/$channel/test.mp4
    [[ ! -e $path && ! -L $path ]] || { echo "Refusing to overwrite $path" >&2; exit 1; }
done

if ! command -v ffmpeg >/dev/null; then
    apt-get update
    apt-get install -y --no-install-recommends ffmpeg
fi

mkdir -p "$media_dir"
for channel in red green blue; do
    mkdir -p "$media_dir/$channel"
    case $channel in
        red) tint='rr=1:gg=0.8:bb=0.8' ;;
        green) tint='rr=0.8:gg=1:bb=0.8' ;;
        blue) tint='rr=0.8:gg=0.8:bb=1' ;;
    esac
    ffmpeg -nostdin -hide_banner -loglevel error -n \
        -f lavfi -i 'testsrc2=size=1280x720:rate=25' \
        -f lavfi -i 'sine=frequency=440:sample_rate=48000' \
        -t 20 -vf "colorchannelmixer=$tint" -af volume=0.03 \
        -c:v libx264 -preset ultrafast -pix_fmt yuv420p -c:a aac \
        "$media_dir/$channel/test.mp4"
    chmod a+r "$media_dir/$channel/test.mp4"
done

echo "Created $media_dir/{red,green,blue}/test.mp4"
