# Umbauplan: Fronius Smart Meter Emulator

## Problem und Zielbild

Die aktuelle Codebasis ist ein allgemeiner Smart-Meter-Adapter mit mehreren Quelltypen. Ziel des Umbaus ist ein fokussierter Fronius-Smart-Meter-Emulator, der:

- sich nach außen wie ein Fronius Smart Meter verhält
- nur noch Modbus TCP und Modbus RTU als Upstream-Quellen unterstützt
- Quellregister auf kanonische Messwerte mappt
- diese Messwerte auf feste Fronius-Zielregister schreibt
- Werte zyklisch pollt und Anfragen aus einem internen Cache beantwortet
- Quellregister blockweise statt einzeln liest

## Vorgehensweise

Der Umbau sollte nicht als kleiner Patch erfolgen, sondern in klar getrennten Schritten:

1. Zielmodell festziehen: neue Konfiguration, kanonische Felder und feste Fronius-Register definieren.
2. Datenpfad entkoppeln: Upstream-Lesen, Dekodieren, Mapping und Emulation sauber trennen.
3. Altlasten entfernen: HTTP-Gateway, `fritz!powerline` und generische Sonderpfade ausbauen.
4. Verifikation: bestehende Tests anpassen, neue Tests für Mapping, Blockbildung und Registerschreiben ergänzen.

## Konkrete Arbeitspakete

### 1. Zielarchitektur im Code definieren

- Neues internes Domänenmodell für kanonische Felder einführen.
- Feste Fronius-Registertabelle im Code anlegen.
- Entscheiden, welche Felder Pflichtfelder und welche optional sind.
- Eine zentrale Schreibfunktion bauen: `canonical field -> Fronius register`.

Ergebnis:
Eine stabile Zielseite, die nicht mehr von frei konfigurierbaren Server-Registern abhängt.

### 2. Neues Konfigurationsschema einführen

- Altes `clients`/`register`-Modell durch ein neues Schema mit `listen`, `meters`, `source` und `map` ersetzen.
- Pro Meter definieren:
  - Name
  - Protokolltyp `tcp` oder `rtu`
  - Verbindungsparameter
  - Slave-ID
  - Timeout
- Pro Mapping definieren:
  - kanonisches Feld
  - Startregister
  - Registertyp/Funktionscode
  - Datentyp
  - Byte-/Word-Order
  - Scale
  - Offset
- Defaults und Validierung einbauen, damit ungültige Konfigurationen früh scheitern.
- Beispielkonfiguration dokumentieren.

Ergebnis:
Eine lesbare, auf den echten Anwendungsfall reduzierte Konfiguration.

### 3. Upstream-Abstraktion für Modbus TCP und RTU bauen

- Einheitliches Interface für Quellzähler definieren, z. B. `ReadHoldingRegisters(start, quantity)`.
- Gemeinsame Logik für Polling, Retries und Timeouts bereitstellen.
- Zwei konkrete Adapter implementieren:
  - Modbus TCP
  - Modbus RTU
- Bestehende Spezialpfade aus `mbgw` und alten Client-Typen nicht weiterführen.

Ergebnis:
Eine saubere Trennung zwischen Quellprotokoll und Emulatorlogik.

### 4. Mapping-Engine implementieren

- Mappingdefinitionen beim Start analysieren.
- Datentypen dekodieren:
  - `u16`, `i16`
  - `u32`, `i32`
  - `u64`, `i64`
  - optional `f32`, falls benötigt
- Byte- und Word-Order korrekt anwenden.
- `scale` und `offset` auf Rohwerte anwenden.
- Ergebnis als kanonischen Messwert in einen zentralen Snapshot schreiben.

Ergebnis:
Ein generischer, testbarer Decoder von Quellregistern zu fachlichen Messwerten.

### 5. Blockbildung für effizientes Polling implementieren

- Aus allen Felddefinitionen pro Zähler die benötigten Registerbereiche ermitteln.
- Nach Adresse sortieren.
- Zusammenhängende oder nahe Register zu Blöcken zusammenfassen.
- Konfigurierbare Grenzen vorsehen:
  - maximaler Gap zwischen Feldern
  - maximale Blockgröße
  - Registertyp/Funktionscode darf innerhalb eines Blocks nicht gemischt werden
- Nach dem Lesen Werte aus dem Block an die Mapping-Engine übergeben.

Ergebnis:
Wenige, effiziente Requests statt vieler Einzelabfragen.

### 6. Cache- und Polling-Schicht aufbauen

- Pro Zähler zyklischen Poller starten.
- Poller liest vorberechnete Blöcke.
- Ergebnisse werden atomar in einem Snapshot/Cache aktualisiert.
- Emulator beantwortet eingehende Requests ausschließlich aus dem Cache.
- Fehlerzustände festlegen:
  - letzter gültiger Wert bleibt erhalten
  - Zeitstempel der letzten erfolgreichen Aktualisierung speichern
  - Logging bei Timeouts und Dekodierfehlern

Ergebnis:
Stabile Antwortzeiten und entkoppelte Quellkommunikation.

### 7. Fronius-Emulation vereinfachen

- Modbus-Server auf die feste Fronius-Registersicht reduzieren.
- Request-getriebene Upstream-Updates entfernen.
- RTU- und TCP-Ausgabe weiter unterstützen.
- Einheitliche Registerschreiblogik für 16/32/64 Bit und signed/unsigned beibehalten oder vereinfachen.

Ergebnis:
Der Server wird zu einem klaren Fronius-Emulator statt zu einem generischen Proxy.

### 8. Alte Komponenten entfernen

- `mbgw` entfernen oder stilllegen.
- `fritz!powerline`-Pfade entfernen.
- Alte generische Register-Konvertierung bereinigen, soweit sie nur noch historische Altlogik trägt.
- Veraltete Konfigdokumentation und Beispielwerte ersetzen.

Ergebnis:
Weniger Code, weniger Sonderfälle, weniger Wartungsaufwand.

### 9. Tests und Verifikation

- Baseline mit bestehenden Tests erfassen.
- Neue Unit-Tests ergänzen für:
  - Konfigurationsvalidierung
  - Blockbildung
  - Dekodierung mit Byte-/Word-Order
  - Skalierung/Offset
  - Schreiben auf feste Fronius-Register
- Integrationstest für einen kompletten Poll-Zyklus mit simuliertem Quellzähler.
- Danach `go test ./...` laufen lassen.

Ergebnis:
Der Umbau ist reproduzierbar abgesichert.

## Empfohlene Reihenfolge

1. Neue Konfigurationsstrukturen und Validierung
2. Feste Fronius-Felddefinitionen
3. Upstream-Interface für TCP/RTU
4. Mapping-Engine
5. Blockbildung
6. Polling/Cache
7. Serveranbindung
8. Entfernen alter Komponenten
9. Tests bereinigen und ergänzen

## Wichtige Designentscheidungen

- Fronius-Zielregister bleiben fest im Code.
- Konfigurierbar ist nur das Quell-Mapping.
- Requests an den Emulator triggern keine Live-Abfrage beim Quellzähler.
- Polling erfolgt blockweise, nicht feldweise.
- Fehler werden sichtbar geloggt und nicht still verschluckt.

## Risiken und zu prüfende Punkte

- Welche Fronius-Felder sind zwingend notwendig, damit die Gegenseite das Gerät akzeptiert?
- Ob `float32` als Quelltyp wirklich benötigt wird oder zunächst weggelassen werden kann.
- Wie groß Registerblöcke in der Praxis maximal sein dürfen, ohne einzelne Geräte zu überfordern.
- Ob für RTU-Quellen unterschiedliche serielle Parameter pro Zähler unterstützt werden müssen.

## Erste sinnvolle Implementierungsetappe

Als erster Code-Schritt sollte ein vertikaler Slice umgesetzt werden:

1. neues Konfigurationsschema einlesen
2. einen Modbus-TCP-Quellzähler anbinden
3. Blockbildung aus dem Mapping erzeugen
4. `power_total`, `voltage_l1`, `voltage_l2`, `voltage_l3` dekodieren
5. auf feste Fronius-Register schreiben
6. per lokalem Modbus-TCP-Server ausgeben

Damit steht früh ein lauffähiger Kern, bevor RTU und weitere Felder ergänzt werden.
