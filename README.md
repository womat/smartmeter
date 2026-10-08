# 🚀 smartmeter — bla bla

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
| `--config`  | `/opt/smartmeter/etc/config.yaml` | Path to the configuration file                                      |
| `--debug`   | `false`                         | Enable debug logging to stdout (overrides log settings from config) |
| `--version` | `false`                         | Print the application version and exit                              |
| `--about`   | `false`                         | Print application details and exit                                  |
| `--help`    | `false`                         | Print this help message and exit                                    |

The config file path can also be set via the environment variable `CONFIG_FILE`.

**Examples:**

```bash
smartmeter --config /etc/smartmeter/config.yaml
smartmeter --debug
smartmeter --version
CONFIG_FILE=/etc/smartmeter/config.yaml smartmeter
```

---

## Configuration

Default location: `/opt/smartmeter/etc/config.yaml`
Environment variables are expanded inside the file, e.g. `apiKey: ${TADL_API_KEY}`.

```yaml
# =============================================================================
# smartmeter configuration
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
  keyFile: /opt/smartmeter/etc/key.pem

  # TLS certificate file
  certFile: /opt/smartmeter/etc/cert.pem

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
  -keyout /opt/smartmeter/etc/key.pem \
  -out /opt/smartmeter/etc/cert.pem \
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
sudo groupadd -f smartmeter
sudo useradd -r -s /usr/sbin/nologin -g smartmeter smartmeter
sudo usermod -aG gpio smartmeter

sudo mkdir -p /opt/smartmeter/{bin,etc,data}
sudo chown -R smartmeter:smartmeter /opt/smartmeter
```

### 2. Copy files

```sh
sudo cp smartmeter /opt/smartmeter/bin/
sudo cp config.yaml /opt/smartmeter/etc/
sudo cp cert.pem key.pem /opt/smartmeter/etc/
sudo chown -R smartmeter:smartmeter /opt/smartmeter
```

### 3. Create systemd service

```sh
sudo tee /etc/systemd/system/smartmeter.service > /dev/null <<'EOF'
[Unit]
Description=smartmeter — S0 Pulse Energy Monitor
After=network.target

[Service]
User=smartmeter
Group=smartmeter
Type=simple
ExecStart=/opt/smartmeter/bin/smartmeter 
Restart=on-failure

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable smartmeter
sudo systemctl start smartmeter
sudo systemctl status smartmeter
```

### 4. View logs

```sh
journalctl -u smartmeter -n 50 -f
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
sudo systemctl reload smartmeter
# or
kill -HUP $(pidof smartmeter)
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
sudo tar czf /tmp/smartmeter-backup.tar.gz /opt/smartmeter

# Restore
sudo tar xzf /tmp/smartmeter-backup.tar.gz -C /
sudo chown -R smartmeter:smartmeter /opt/smartmeter
sudo systemctl restart smartmeter
```

---

## License

MIT
