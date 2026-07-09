#!/bin/bash
# server.sh — start/stop the RFID HTTP server with logging
# Usage:
#   ./server.sh start [--mock]   — start server (real reader by default, --mock for mock mode)
#   ./server.sh stop             — stop server (SIGTERM)
#   ./server.sh status           — check if server is running (exit 0 if alive)

SERVER_PID_FILE="${SERVER_PID_FILE:-/tmp/koha-rfid-server.pid}"
SERVER_LOG="${SERVER_LOG:-/tmp/koha-rfid-server.log}"
LISTEN="${LISTEN:-localhost:9000}"
TLS="${TLS:-1}"  # 1 = enable TLS, 0 = plain HTTP

start_server() {
    local mode="$1"  # --mock or empty
    if [ -f "$SERVER_PID_FILE" ]; then
        local pid
        pid=$(cat "$SERVER_PID_FILE")
        if kill -0 "$pid" 2>/dev/null; then
            echo "Server already running (pid $pid)"
            return 0
        fi
        rm -f "$SERVER_PID_FILE"
    fi

    # Free port if something is still holding it
    fuser -k "$LISTEN" 2>/dev/null || true
    sleep 1

    local cmd="./koha-rfid"
    [ "$mode" = "--mock" ] && cmd="$cmd -mock"
    [ "$TLS" = "1" ] && cmd="$cmd -tls"

    echo "Starting: $cmd"
    echo "Log: $SERVER_LOG"
    nohup $cmd > "$SERVER_LOG" 2>&1 &
    PID=$!
    echo "$PID" > "$SERVER_PID_FILE"

    # Wait for server to become ready
    local proto="http"
    [ "$TLS" = "1" ] && proto="https"
    for i in 1 2 3 4 5; do
        if curl -sk "${proto}://${LISTEN}/ping" >/dev/null 2>&1; then
            echo "Server ready (pid $PID)"
            return 0
        fi
        sleep 1
    done
    echo "ERROR: server did not start (check $SERVER_LOG)"
    return 1
}

stop_server() {
    if [ ! -f "$SERVER_PID_FILE" ]; then
        echo "No pid file found"
        return 0
    fi
    local pid
    pid=$(cat "$SERVER_PID_FILE")
    if kill "$pid" 2>/dev/null; then
        echo "Server stopped (pid $pid)"
    else
        echo "Server not running (pid $pid stale)"
    fi
    rm -f "$SERVER_PID_FILE"
    fuser -k "$LISTEN" 2>/dev/null || true
}

status_server() {
    if [ -f "$SERVER_PID_FILE" ]; then
        local pid
        pid=$(cat "$SERVER_PID_FILE")
        if kill -0 "$pid" 2>/dev/null; then
            local proto="http"
            [ "$TLS" = "1" ] && proto="https"
            if curl -sk "${proto}://${LISTEN}/ping" >/dev/null 2>&1; then
                echo "Server running (pid $pid)"
                return 0
            fi
            echo "Server pid $pid but not responding"
            return 1
        fi
        echo "Stale pid file"
        rm -f "$SERVER_PID_FILE"
    fi
    echo "Server not running"
    return 1
}

case "${1:-}" in
    start)
        shift
        start_server "${1:-}"
        ;;
    stop)
        stop_server
        ;;
    status)
        status_server
        ;;
    *)
        echo "Usage: $0 {start|stop|status} [--mock]"
        exit 1
        ;;
esac
