# smartmeter – Deutsche Kurzfassung

🇬🇧 [Full documentation in English](README.md)

**smartmeter macht aus einem beliebigen Modbus-Energiezähler einen Fronius Smart Meter – für den
Wechselrichter über RS485 und für Wallboxen über TCP.**

Ein Fronius-Wechselrichter akzeptiert am Zählereingang nur einen Fronius Smart Meter. Misst bereits
ein anderes Gerät den Netzanschluss – ein Smartfox, ein Energiemanager, ein beliebiger Zähler mit
Modbus –, liest smartmeter dieses Gerät aus und **antwortet als Fronius Smart Meter 63A-3**:

- über **RS485 (Modbus RTU)**, Unit-ID 1, mit der proprietären Registertabelle, die der
  Wechselrichter abfragt,
- über **Modbus TCP**, Unit-ID 200, mit dem **SunSpec**-Zählerblock (Modell 203) für Wallboxen,
  evcc und andere Fronius-kompatible Abnehmer.

Der Quellzähler wird einmal pro Intervall gelesen und unter allen Unit-IDs und auf beiden Wegen
gleichzeitig bereitgestellt. Unplausible Werte (Spikes) werden verworfen, die letzten gültigen
Werte bleiben stehen.

## In fünf Schritten

1. **Herunterladen:** Das Archiv für deinen Pi gibt es unter
   [Releases](https://github.com/womat/smartmeter/releases/latest): `armv6` für Pi 1 und Zero,
   `armv7` für 32-Bit-Systeme, `arm64` für 64-Bit-Systeme.
2. **Installieren:** System-User `smartmeter` (Gruppe `dialout` für die serielle Schnittstelle)
   anlegen, Programm und `config.yaml` nach `/opt/smartmeter` kopieren, Zertifikat erzeugen.
3. **Konfigurieren:** API-Key, serielle Schnittstelle des RS485-Adapters und Adresse des
   Quellzählers samt Registerzuordnung.
4. **Anschließen:** RS485-Adapter an den Zählereingang des Wechselrichters (D+ an D+, D− an D−,
   Abschluss 120 Ω); im Wechselrichter einen Fronius Smart Meter auf Modbus RTU, Adresse 1,
   9600 Baud einstellen.
5. **Starten:** als systemd-Dienst, dann `https://<dein-pi>:8443/ready` prüfen.

Die genauen Befehle stehen im [Quick start](README.md#quick-start), alle Einstellungen unter
[Configuration](README.md#configuration), die Herkunft der Registertabellen unter
[Register maps](README.md#register-maps).

## Lizenz

MIT, siehe [`LICENSE`](LICENSE).
