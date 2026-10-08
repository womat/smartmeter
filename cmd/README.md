# smartmeter

**smartmeter** reads an energy meter over Modbus TCP or RTU and serves it as a Fronius Smart Meter
63A-3: on RS485 for the Fronius inverter and on Modbus TCP (SunSpec) for wallboxes and evcc.

----

## Usage

```text
smartmeter [--config FILE] [--debug] [--version] [--about] [--help]
```

---

## Command-line Flags

| Flag        | Default                           | Description                                                         |
|-------------|-----------------------------------|---------------------------------------------------------------------|
| `--config`  | `/opt/smartmeter/etc/config.yaml` | Path to the configuration file                                      |
| `--debug`   | `false`                           | Enable debug logging to stdout (overrides log settings from config) |
| `--version` | `false`                           | Print the application version and exit                              |
| `--about`   | `false`                           | Print application details and exit                                  |
| `--help`    | `false`                           | Print this help message and exit                                    |

The config file path can also be set via the environment variable `CONFIG_FILE`.

`--version` prints the semantic version the binary was built from. It is injected from the Git tag
at build time, so an official release reports a plain `2.0.0`, while a local development build
reports a descriptive fallback such as `2.0.0-5-g0c13781-dirty` or `dev`. `--about` additionally
shows the build date and commit.

**Examples:**

```bash
smartmeter --config /etc/smartmeter/config.yaml
smartmeter --debug
smartmeter --version
CONFIG_FILE=/etc/smartmeter/config.yaml smartmeter
```

---

## Configuration

The configuration file is a YAML file. By default it is loaded from `/opt/smartmeter/etc/config.yaml`.

Environment variables are expanded inside the file in the `${VAR}` form only, e.g.
`apiKey: ${SMARTMETER_API_KEY}`. Unknown keys are rejected.

Every key is documented in the example configuration (`config/config.yaml`) and in the project
README: <https://github.com/womat/smartmeter#configuration>.
