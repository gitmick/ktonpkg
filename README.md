# ktonpkg

Das kton-Paketformat `kton-package/1`: ein Ordner, in dem die Arbeit unter ihren gewohnten Namen
liegt, und ein Unterordner `kton/`, der sagt, was das ist und was darüber behauptet wird.

```
<paket>/
  kton/package.json      Art (ray | report | verticle | tool), Fassung, Einstieg
  kton/<entry>.json      das Potential — ein Foton oder ein Graph davon (Ray)
  kton/claims/<id>.json  nekton-Claims, DSSE über in-toto
  kton/spectrum.json     optional: das Spektrum eines Werkzeugs (kton §10)
  …                      die Nutzlast, unter ihren gewohnten Namen
```

Hier steht die Identitätsregel für Pakete und Rays **genau einmal**. Wer sie braucht, linkt dieses
Modul, statt sie nachzubauen — das kton-cockpit (ADR-005) ebenso wie improvego. Foton-Identität und
Aktionsschlüssel kommen unverändert aus `kton.dev/plankton/core`.

Dieses Paket kennt improve nicht. Es rechnet, prüft, liest und schreibt — mehr nicht.

| Paket | Inhalt |
|---|---|
| `ktonpkg` | Format, Bundle lesen/schreiben/prüfen, Ray und Spektrum, Identität, Signatur, Anforderungen und Trace |
| `ktonpkg/ktontest` | das kleine Stück, mit dem ein Go-Test aufzeichnet, welche Anforderung er einlöst |

Herkunft: aus `improvego/ktonpkg` und `improvego/ktontest` herausgelöst (Historie von `ktonpkg`
erhalten).
