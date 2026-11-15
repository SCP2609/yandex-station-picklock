#!/system/bin/sh
# Station 1: автономный Dropbear с входом только по публичному ключу.
set -eu
BB=/system/bin/busybox-smp
BASE=/data/dropbear
RUNTIME=/dev/ssh
[ "$(id -u)" = 0 ]
[ -f "$BASE/.ssh/authorized_keys" ]
[ -f "$BASE/hostkey" ]
[ -x "$BASE/dropbear" ]
[ -x "$BASE/ld-musl-aarch64.so.1" ]
umask 077
$BB mkdir -p "$RUNTIME"
$BB chmod 700 "$RUNTIME"
printf 'root:x:0:0:root:/data/dropbear:/system/bin/sh\n' > "$RUNTIME/pw.new"
printf 'root:x:0:root\n' > "$RUNTIME/g.new"
printf '/system/bin/sh\n' > "$RUNTIME/sh.new"
for name in pw g sh; do
    $BB chmod 644 "$RUNTIME/$name.new"
    $BB mv "$RUNTIME/$name.new" "$RUNTIME/$name"
done
if [ -f "$BASE/dropbear.pid" ]; then
    pid=$($BB cat "$BASE/dropbear.pid")
    case "$pid" in
        ''|*[!0-9]*|0|1) : ;;
        *) if $BB kill -0 "$pid" 2>/dev/null; then
            cmd=$($BB tr '\000' ' ' < "/proc/$pid/cmdline" 2>/dev/null || true)
            case "$cmd" in *"$BASE/dropbear"*"-s -g"*) echo ALREADY_RUNNING; exit 0;; esac
        fi ;;
    esac
    $BB rm -f "$BASE/dropbear.pid"
fi
$BB setsid "$BASE/ld-musl-aarch64.so.1" --library-path "$BASE" "$BASE/dropbear" \
    -E -F -s -g -p 22 -P "$BASE/dropbear.pid" -r "$BASE/hostkey" \
    >> "$BASE/dropbear.log" 2>&1 < /dev/null &
$BB sleep 1
pid=$($BB cat "$BASE/dropbear.pid")
$BB kill -0 "$pid"
echo "SSH_STARTED pid=$pid port=22 key_only=yes"
