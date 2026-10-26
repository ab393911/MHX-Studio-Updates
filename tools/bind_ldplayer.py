#!/usr/bin/env python3
"""MHX Studio: discover and bind five LDPlayer ADB devices.

Run: python tools/bind_ldplayer.py --adb "C:\\LDPlayer\\LDPlayer9\\adb.exe"
Optional: --adb adb (if adb is on PATH)
Writes config/device_bindings.json; does not control gameplay.
"""
import argparse
import json
import os
import subprocess
import sys
from pathlib import Path

ROLES = ["合欢", "天音", "青云", "鬼王1", "鬼王2"]


def adb_run(adb, *args, timeout=10):
    return subprocess.run([adb, *args], capture_output=True, text=True,
                          encoding="utf-8", errors="replace", timeout=timeout)


def list_devices(adb):
    result = adb_run(adb, "devices", "-l")
    if result.returncode != 0:
        raise RuntimeError(result.stderr.strip() or "ADB failed")
    devices = []
    for line in result.stdout.splitlines()[1:]:
        cols = line.split()
        if len(cols) >= 2 and cols[1] == "device":
            devices.append(cols[0])
    return sorted(set(devices))


def screen_size(adb, serial):
    result = adb_run(adb, "-s", serial, "shell", "wm", "size")
    if result.returncode:
        return "unknown"
    for line in result.stdout.splitlines():
        if "Override size:" in line:
            return line.split(":", 1)[1].strip()
    for line in result.stdout.splitlines():
        if "Physical size:" in line:
            return line.split(":", 1)[1].strip()
    return "unknown"


def main():
    parser = argparse.ArgumentParser(description="Bind five LDPlayer ADB devices to roles")
    parser.add_argument("--adb", default="adb", help="Path to adb executable")
    parser.add_argument("--output", default="config/device_bindings.json")
    args = parser.parse_args()
    adb = os.path.expandvars(os.path.expanduser(args.adb))
    try:
        devices = list_devices(adb)
    except (OSError, subprocess.TimeoutExpired, RuntimeError) as exc:
        print(f"ADB discovery failed: {exc}", file=sys.stderr)
        return 1
    if len(devices) != 5:
        print(f"Found {len(devices)} online ADB devices, need exactly 5: {devices}", file=sys.stderr)
        return 2
    print("Found devices:")
    for i, serial in enumerate(devices, 1):
        print(f"  {i}. {serial}  size={screen_size(adb, serial)}")
    print("Assign each role to a device index; verify each emulator manually.")
    available = set(range(1, 6))
    bindings = {}
    for role in ROLES:
        while True:
            try:
                raw = input(f"Device index for {role}: ").strip()
            except (EOFError, KeyboardInterrupt):
                print("\nCancelled, no config written")
                return 3
            if raw.isdigit() and int(raw) in available:
                number = int(raw)
                available.remove(number)
                bindings[role] = devices[number - 1]
                break
            print("Please enter an unused index from 1 to 5.")
    destination = Path(args.output)
    destination.parent.mkdir(parents=True, exist_ok=True)
    temp = destination.with_suffix(destination.suffix + ".tmp")
    temp.write_text(json.dumps({"adb": adb, "roles": bindings},
                               ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    temp.replace(destination)
    print(f"Saved bindings: {destination}. No gameplay actions performed.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
