#!/bin/bash
# Convenience script to run the email service

set -e

PIDFILE="/tmp/goemailservices.pid"
LOGFILE="service.log"
CONFIG="config.yaml"

case "$1" in
  start)
    if [ -f "$PIDFILE" ]; then
      PID=$(cat "$PIDFILE")
      if ps -p "$PID" > /dev/null 2>&1; then
        echo "Service already running (PID: $PID)"
        exit 1
      else
        rm "$PIDFILE"
      fi
    fi

    echo "Starting email service..."
    ./bin/goemailservices --config "$CONFIG" > "$LOGFILE" 2>&1 &
    PID=$!
    echo $PID > "$PIDFILE"
    echo "Service started (PID: $PID)"
    echo "Logs: tail -f $LOGFILE"
    sleep 2

    if ps -p "$PID" > /dev/null 2>&1; then
      echo "✓ Service is running"
    else
      echo "✗ Service failed to start, check $LOGFILE"
      rm "$PIDFILE"
      exit 1
    fi
    ;;

  stop)
    if [ ! -f "$PIDFILE" ]; then
      echo "Service not running (no PID file)"
      exit 1
    fi

    PID=$(cat "$PIDFILE")
    if ps -p "$PID" > /dev/null 2>&1; then
      echo "Stopping service (PID: $PID)..."
      kill "$PID"
      sleep 2

      if ps -p "$PID" > /dev/null 2>&1; then
        echo "Force killing..."
        kill -9 "$PID"
      fi

      rm "$PIDFILE"
      echo "✓ Service stopped"
    else
      echo "Service not running"
      rm "$PIDFILE"
    fi
    ;;

  restart)
    $0 stop
    sleep 1
    $0 start
    ;;

  status)
    if [ ! -f "$PIDFILE" ]; then
      echo "Service not running (no PID file)"
      exit 1
    fi

    PID=$(cat "$PIDFILE")
    if ps -p "$PID" > /dev/null 2>&1; then
      echo "✓ Service is running (PID: $PID)"

      # Get uptime from API
      if command -v curl > /dev/null 2>&1; then
        HEALTH=$(curl -s http://localhost:8080/health 2>/dev/null)
        if [ $? -eq 0 ]; then
          echo "$HEALTH" | grep -o '"uptime":"[^"]*"' | cut -d'"' -f4 | sed 's/^/  Uptime: /'
        fi
      fi

      # Show queue stats
      if [ -f "./bin/mailctl" ]; then
        echo ""
        ./bin/mailctl --username admin --password changeme queue stats 2>/dev/null || true
      fi
    else
      echo "✗ Service not running (stale PID file)"
      rm "$PIDFILE"
      exit 1
    fi
    ;;

  logs)
    if [ -f "$LOGFILE" ]; then
      tail -f "$LOGFILE"
    else
      echo "No log file found: $LOGFILE"
      exit 1
    fi
    ;;

  test)
    echo "Running test suite..."
    if [ -f "test-suite.py" ]; then
      python3 test-suite.py
      echo ""
      echo "Checking results..."
      sleep 2
      ./bin/mailctl --username admin --password changeme queue stats
    else
      echo "Test suite not found"
      exit 1
    fi
    ;;

  *)
    echo "Usage: $0 {start|stop|restart|status|logs|test}"
    echo ""
    echo "Commands:"
    echo "  start    - Start the email service"
    echo "  stop     - Stop the email service"
    echo "  restart  - Restart the email service"
    echo "  status   - Check service status and show stats"
    echo "  logs     - Tail service logs"
    echo "  test     - Run test suite"
    exit 1
    ;;
esac
