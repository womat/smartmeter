# Fronius Smart Meter Emulator Specification

## Ziel

Das Projekt soll ein Fronius Smart Meter Emulator sein, der sich nach außen wie ein Fronius Smart Meter verhaelt.

## Quellen

Als Upstream-Quellen werden nur folgende Protokolle unterstuetzt:

- Modbus TCP
- Modbus RTU

Andere Quellen oder Adapter werden nicht mehr benoetigt.

## Architektur

Die Zielregister des Fronius Smart Meters werden fest im Code definiert.

Die eingelesenen Werte aus den Quellzaehlern werden nicht direkt frei auf beliebige Zielregister gemappt, sondern zuerst auf kanonische Felder abgebildet. Beispiele fuer solche Felder sind:

- `power_total`
- `power_l1`
- `power_l2`
- `power_l3`
- `voltage_l1`
- `voltage_l2`
- `voltage_l3`
- `current_l1`
- `current_l2`
- `current_l3`
- `energy_import`
- `energy_export`
- `frequency`
- `pf_l1`
- `pf_l2`
- `pf_l3`

Danach werden diese kanonischen Felder auf die festen Fronius-Zielregister geschrieben.

## Konfiguration

Die Konfiguration soll pro Zaehler und pro Feld moeglich sein.

Pro Zaehler sollen mindestens folgende Angaben konfigurierbar sein:

- Name
- Typ der Quelle: TCP oder RTU
- Verbindungsparameter
- Unit-ID
- Timeout

Pro Feld sollen mindestens folgende Angaben konfigurierbar sein:

- Quellregister
- Datentyp
- Byte- und Word-Order
- Skalierung
- Offset

## Lesestrategie

Die Werte der Quellzaehler werden zeitgesteuert gelesen.

Der Emulator beantwortet eingehende Anfragen aus einem internen Cache, damit Modbus TCP und Modbus RTU schnell und stabil antworten koennen.

Register sollen nicht einzeln gelesen werden. Stattdessen muessen zusammenhaengende oder nahe beieinander liegende Quellregister erkannt und zu moeglichst grossen Lese-Bloecken zusammengefasst werden. Dadurch wird die Anzahl der Requests an die Quellzaehler reduziert.

## Loesungsansaetze und technische Entscheidungen

### Kanonische Felder statt freiem Zielregister-Mapping

Die Quellzaehler sollen auf fachliche Messwerte wie `power_total`, `voltage_l1` oder `energy_import` gemappt werden. Diese Zwischenebene vermeidet, dass jede Quelle direkt gegen konkrete Fronius-Register verdrahtet wird. Dadurch wird die Konfiguration einfacher, besser lesbar und fuer unterschiedliche Zaehler leichter wiederverwendbar.

### Feste Fronius-Zielregister im Code

Die Fronius-Registerseite soll nicht frei konfigurierbar sein, sondern fest im Code definiert werden. Das reduziert Komplexitaet, verhindert Fehlkonfigurationen und stellt sicher, dass sich der Emulator immer konsistent wie ein Fronius Smart Meter verhaelt.

### Polling mit internem Cache

Die Daten der Quellzaehler sollen im Hintergrund zyklisch gelesen und in einem Cache abgelegt werden. Eingehende Requests an den Emulator werden aus diesem Cache beantwortet. Dadurch bleiben die Antwortzeiten stabil und die Kommunikation mit dem Fronius-Client haengt nicht direkt von langsamen oder stoeranfaelligen Quellgeraeten ab.

### Blockweises Lesen zusammenhaengender Register

Das Mapping soll beim Start analysiert werden, damit Register, die direkt aufeinander folgen oder nahe beieinander liegen, in gemeinsame Lese-Bloecke zusammengefasst werden. So werden moeglichst wenige Modbus-Requests an die Quellzaehler gesendet. Das reduziert Last, verbessert die Performance und vereinfacht die Polling-Logik.

### Vereinfachung der Codebasis

Nicht benoetigte Quelltypen und Adapter sollen entfernt werden. Das betrifft insbesondere HTTP-basierte Quellen, `fritz!powerline` und sonstige Spezialfaelle. Uebrig bleiben nur die Bausteine, die fuer Modbus TCP, Modbus RTU, Register-Mapping und das Fronius-Verhalten wirklich erforderlich sind.

## Nicht mehr benoetigte Komponenten

Die folgenden Komponenten oder Quelltypen werden nicht mehr benoetigt:

- HTTP-Gateway
- `fritz!powerline`
- sonstige Sonderquellen außerhalb von Modbus TCP und Modbus RTU
