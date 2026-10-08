# Security

smartmeter runs on a Raspberry Pi in a home network, reads an energy meter over Modbus and serves it as a Fronius Smart Meter over Modbus TCP/RTU and through an HTTPS API. Reports of security issues are taken seriously.

## Reporting a vulnerability

Please **do not open a public issue**. Report it privately through GitHub instead:
**Security → Report a vulnerability** ([direct link](https://github.com/womat/smartmeter/security/advisories/new)).

Helpful details:

- the affected version (`smartmeter --version`, or `GET /version` on the device)
- steps to reproduce
- what an attacker could achieve with it

You will usually get an answer within a week. smartmeter is a spare-time project, so no fixed response time can be
promised.

## Supported versions

Security fixes are made for the latest release only.
