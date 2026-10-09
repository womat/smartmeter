# <img src="docs/images/logo.svg" alt="" width="40" align="top"> smartmeter – Deutsche Kurzfassung

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
Werte bleiben stehen. Liefert die Quelle länger als `staleTimeout` (30 s) keine gültigen Werte,
antworten die Unit-IDs nicht mehr: Der Wechselrichter erkennt einen Zählerausfall, statt mit
eingefrorenen Werten zu regeln.

Unter `https://<dein-pi>:8443/` zeigt eine Web-Seite Energiefluss, Leistung, Phasen, Zählerstände
und die ausgelieferten Register; sie ist im Programm enthalten und braucht kein Internet.

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
   9600 Baud einstellen. Am Raspberry Pi die UART `/dev/ttyS0` verwenden und
   `interFrameDelay: 20ms` aus der Beispiel-Config übernehmen, sonst kommen einzelne Anfragen
   zerteilt an.
5. **Starten:** als systemd-Dienst, dann `https://<dein-pi>:8443/ready` prüfen oder die Web-Seite
   `https://<dein-pi>:8443/` öffnen.

Die genauen Befehle stehen im [Quick start](README.md#quick-start), alle Einstellungen unter
[Configuration](README.md#configuration), die Herkunft der Registertabellen unter
[Register maps](README.md#register-maps).

## Hinweis

smartmeter ist ein unabhängiges Projekt und steht in keiner Verbindung zur Fronius International
GmbH. Der Wechselrichter regelt mit den Werten des Zählers, etwa die dynamische
Einspeisebegrenzung. Falsch konfigurierte Werte führen zu falschen Regelentscheidungen – vor dem
Einsatz die Werte mit dem Quellzähler und dem Webinterface des Wechselrichters vergleichen.

## Lizenz

MIT, siehe [`LICENSE`](LICENSE).
