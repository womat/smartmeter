# smartmeteremu

**smartmeteremu**  is an ...

---

## Usage

```text
smartmeteremu [--config FILE] [--debug] [--version] [--about] [--help]
```

---

## Command-line Flags

| Flag        | Default                         | Description                                                         |
|-------------|---------------------------------|---------------------------------------------------------------------|
| `--config`  | `/opt/smartmeteremu/etc/config.yaml` | Path to the configuration file                                      |
| `--debug`   | `false`                         | Enable debug logging to stdout (overrides log settings from config) |
| `--version` |                                 | Print the application version and exit                              |
| `--about`   |                                 | Print application details and exit                                  |
| `--help`    |                                 | Print this help message and exit                                    |

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

The configuration file is a YAML file. By default it is loaded from `/opt/smartmeteremu/etc/config.yaml`.

Environment variables are expanded inside the file, e.g. `apiKey: ${TADL_API_KEY}`.
