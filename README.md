## Adblock-DNS

Adblock-DNS is a lightweight, system-level DNS proxy that blocks requests to known ad-serving domains by returning empty or `0.0.0.0` responses. It operates in two modes:

* **CLI Mode**: Run from a terminal for fine-grained control and scriptability.
* **Tray Mode**: Windows system tray application for non-technical users, with Start/Stop, Dry-run toggle, and log access from the tray icon.

---

### Features

* **Blocklists** automatically fetched daily from trusted sources (StevenBlack/hosts, AdAway).
* **Whitelist** support via `whitelist.txt` (one domain per line).
* **Dry-run** mode logs blocked domains without actually blocking them.
* **Real-time Start/Stop** from a system tray icon (Windows).
* **Logging** to `adblock.log` for audit and debugging.

---

## Downloads & Prerequisites

### Binaries

* **Tray App**: Download `adblock-tray.exe` from the [Releases](https://github.com/dmtkfs/adblock-dns/releases) page (Windows users).

### Build from Source (Go 1.24+ required)

```bash
git clone https://github.com/dmtkfs/adblock-dns.git
cd adblock-dns

# Build the CLI tool (optional): produces an executable you can run directly
go build -o adblock-cli.exe ./cmd/cli

# Build the Tray app:
go build -ldflags="-H=windowsgui" -o adblock-tray.exe ./cmd/tray
```

---

## Configuration Files

* **whitelist.txt**: Place in the same folder as the executable (`.exe`). One domain per line. Lines starting with `#` are ignored.

* **adblock.log**: Automatically created next to the executable. Contains timestamped logs for blocklist refreshes, whitelist loads, and blocked/whitelisted queries.

---

## Operational Modes

### 1. CLI Mode

Run in a terminal for full verbosity and control:

### Quick Start (no need to build an .exe):

```bash
go run ./cmd/cli -v
```

### Optional: Build a standalone CLI executable

```bash
go build -o adblock-cli.exe ./cmd/cli
./adblock-cli.exe -v
```

### Flags

* `--listen`    : Address and port for DNS proxy (default: `127.0.0.1:53`)
* `--interval`  : Blocklist refresh interval (default: `24h`)
* `--dry-run`   : Log only; do not block (useful for testing)
* `-v`          : Verbose logging (`[BL] domain`, `[WL] domain`)

### 2. Tray Mode (Windows)

Double-click `adblock-tray.exe` to run a windowless tray app. Control from the tray icon:

1. **Start**   : Launch the DNS proxy on port 53.
2. **Stop**    : Stop the DNS proxy.
3. **Dry-run** : Toggle dry-run mode. Status changes in the menu and logs are only recorded.
4. **Open log file**: Opens `adblock.log` in Notepad.
5. **Quit**    : Exit the tray app and stop the proxy.

The top menu item shows **Status: Stopped**, **Status: Running**, or **Status: Running (Dry-run)** for instant feedback.

**Build with:**

```bash
go build -ldflags="-H=windowsgui" -o adblock-tray.exe ./cmd/tray
```

---

## Logs & Troubleshooting

* **CLI mode:** Output appears in both the terminal and in `adblock.log` in your working directory.
* **Tray mode:** Logs are written only to `adblock.log` (located in the same directory as the tray executable).


* **Log file**: `adblock.log` next to the executable.
* **Entries**:

  * `block-list refreshed — N domains` whenever blocklists update.
  * `whitelist loaded — M entries` whenever whitelist is reloaded.
  * `[BL] domain` when a blocked domain is queried in verbose mode.
  * `[WL] domain` when a whitelisted domain is queried in verbose mode.

**Tip**: If you don’t see logs in tray mode, ensure the app has write permission in its folder.

---

## To Uninstall

* Simply delete the `adblock-tray.exe`, `whitelist.txt`, and `adblock.log` files. No system changes are made.

---

## Acknowledgements

* Thanks to the [StevenBlack/hosts](https://github.com/StevenBlack/hosts) project for their community-maintained hosts blocklist.
* Thanks to [AdAway](https://adaway.org/) for providing a robust mobile hosts file.
* Inspired by many open-source DNS-based adblocking tools and the broader community.

## License

This project is licensed under the [MIT License](LICENSE).
