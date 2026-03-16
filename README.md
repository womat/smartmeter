# 🚀 smartmeteremu — bla bla

description

---

## Features

- Exposes a secured **HTTPS REST API** (API key authentication)
- **IP allowlist / blocklist** support
- **Hot-reload** of configuration via `SIGHUP`
- Embedded self-signed TLS certificate for development (no setup required)
- Optional **Swagger UI** (build tag `swagger`, dev only)

---

## Where to start

- Runtime, API, build, deploy, and Swagger usage: [`cmd/README.md`](cmd/README.md)
- Example configuration: [`config/config.yaml`](config/config.yaml)
- Swagger generation script: [`docs/generate.sh`](docs/generate.sh)

---

## API Endpoints

| Method | Path       | Auth    | Description                        |
|--------|------------|---------|------------------------------------|
| GET    | `/version` | —       | Application name and version       |
| GET    | `/health`  | API Key | Runtime metrics (memory, uptime …) |

Authentication via the `X-API-Key` header.

### Examples

```sh
# Application version (no auth required)
curl -k https://localhost:8443/version

# Health check
curl -k -H "X-Api-Key: your-api-key" https://localhost:8443/health
```

---

## Command-line Flags

| Flag        | Default                         | Description                                                         |
|-------------|---------------------------------|---------------------------------------------------------------------|
| `--config`  | `/opt/smartmeteremu/etc/config.yaml` | Path to the configuration file                                      |
| `--debug`   | `false`                         | Enable debug logging to stdout (overrides log settings from config) |
| `--version` | `false`                         | Print the application version and exit                              |
| `--about`   | `false`                         | Print application details and exit                                  |
| `--help`    | `false`                         | Print this help message and exit                                    |

The config file path can also be set via the environment variable `CONFIG_FILE`.

**Examples:**

```bash
smartmeteremu --config /etc/smartmeteremu/config.yaml
smartmeteremu --debug
smartmeteremu --version
CONFIG_FILE=/etc/smartmeteremu/config.yaml smartmeteremu
```

---

## Configuration

Default location: `/opt/smartmeteremu/etc/config.yaml`
Environment variables are expanded inside the file, e.g. `apiKey: ${TADL_API_KEY}`.

```yaml
# =============================================================================
# smartmeteremu configuration
# =============================================================================

# logLevel defines the minimum log level.
# Allowed values: debug | info | warn | error
logLevel: info

# logDestination defines where logs are written to.
# Supported values: stdout | stderr | /path/to/logfile
logDestination: stdout

# =============================================================================
# Webserver configuration (HTTPS)
# =============================================================================
webserver:
  # Host address the HTTPS server listens on (0.0.0.0 = all interfaces)
  listenHost: 0.0.0.0

  # Port the HTTPS server listens on (default: 8443)
  listenPort: 8443

  # Global API key for protected endpoints
  apiKey: changeme!

  # TLS private key file
  keyFile: /opt/smartmeteremu/etc/key.pem

  # TLS certificate file
  certFile: /opt/smartmeteremu/etc/cert.pem

  # Blocked IP addresses or networks (empty = none blocked)
  blockedIPs: [ ]
  #  - 192.168.0.1
  #  - 192.168.0.0/16

  # Allowed IP addresses or networks (empty = all allowed)
  allowedIPs: [ ]
  #  - 127.0.0.1
  #  - ::1
  #  - 192.168.0.0/16

```

---

## TLS Certificate

Generate a self-signed certificate for development:

```sh
openssl req -x509 -nodes -newkey rsa:2048 \
  -keyout /opt/smartmeteremu/etc/key.pem \
  -out /opt/smartmeteremu/etc/cert.pem \
  -days 825 \
  -subj "/C=AT/ST=Vienna/L=Vienna/O=MyCompany/OU=DEV/CN=localhost"
```

**Subject fields:**

| Field           | Example             | Description                                  |
|-----------------|---------------------|----------------------------------------------|
| `/C`            | `AT`                | Country code (2 letters)                     |
| `/ST`           | `Vienna`            | State or province (optional)                 |
| `/L`            | `Vienna`            | City (optional)                              |
| `/O`            | `MyCompany`         | Organization (optional)                      |
| `/OU`           | `DEV`               | Organizational unit (optional)               |
| `/CN`           | `localhost`         | **Common Name — your domain or `localhost`** |
| `/emailAddress` | `admin@example.com` | E-mail address (optional)                    |

> **Note:** Browsers enforce a maximum certificate validity of 825 days. Use `-days 365` for production-like setups.

---

## Installation

### 1. Create system user and directories

```sh
sudo groupadd -f smartmeteremu
sudo useradd -r -s /usr/sbin/nologin -g smartmeteremu smartmeteremu
sudo usermod -aG gpio smartmeteremu

sudo mkdir -p /opt/smartmeteremu/{bin,etc,data}
sudo chown -R smartmeteremu:smartmeteremu /opt/smartmeteremu
```

### 2. Copy files

```sh
sudo cp smartmeteremu /opt/smartmeteremu/bin/
sudo cp config.yaml /opt/smartmeteremu/etc/
sudo cp cert.pem key.pem /opt/smartmeteremu/etc/
sudo chown -R smartmeteremu:smartmeteremu /opt/smartmeteremu
```

### 3. Create systemd service

```sh
sudo tee /etc/systemd/system/smartmeteremu.service > /dev/null <<'EOF'
[Unit]
Description=smartmeteremu — S0 Pulse Energy Monitor
After=network.target

[Service]
User=smartmeteremu
Group=smartmeteremu
Type=simple
ExecStart=/opt/smartmeteremu/bin/smartmeteremu 
Restart=on-failure

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable smartmeteremu
sudo systemctl start smartmeteremu
sudo systemctl status smartmeteremu
```

### 4. View logs

```sh
journalctl -u smartmeteremu -n 50 -f
```

---

## Build

```sh
# Raspberry Pi 4/5 (64-bit OS)
make build_arm64

# Raspberry Pi 2/3/4 (32-bit OS)
make build_arm7

# Raspberry Pi 1 / Zero (32-bit OS)
make build_arm6

# Build with Swagger UI (dev only)
make build_arm64_dev

# Build and deploy to Raspberry Pi via SCP
make deploy
```

---

## Hot-Reload

Send `SIGHUP` to reload the configuration without restarting the process:

```sh
sudo systemctl reload smartmeteremu
# or
kill -HUP $(pidof smartmeteremu)
```

---

## Firewall

```sh
# Allow the configured port (default 8443)
sudo ufw allow 8443/tcp
sudo ufw status
```

---

## Backup & Restore

```sh
# Backup
sudo tar czf /tmp/smartmeteremu-backup.tar.gz /opt/smartmeteremu

# Restore
sudo tar xzf /tmp/smartmeteremu-backup.tar.gz -C /
sudo chown -R smartmeteremu:smartmeteremu /opt/smartmeteremu
sudo systemctl restart smartmeteremu
```

---

## License

MIT
