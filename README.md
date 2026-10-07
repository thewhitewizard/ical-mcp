# ical-mcp

Serveur MCP (transport stdio) en lecture seule qui donne à un assistant IA accès à vos calendriers, à partir de flux iCal (`.ics`) servis en HTTPS, par exemple l'« adresse secrète au format iCal » de Google Agenda.

> En cours de construction : le serveur démarre et valide sa configuration, les outils MCP arrivent dans les tickets suivants.

## Compilation

Binaire statique, sans CGO :

```sh
CGO_ENABLED=0 go build -ldflags "-X main.version=v0.1.0" -o ical-mcp .
```

## Configuration

Le chemin du fichier JSON est passé par `--config` ou par la variable d'environnement `ICAL_MCP_CONFIG` (le flag l'emporte). Voir `config.example.json`.

| Champ | Défaut | Rôle |
|---|---|---|
| `timezone` | obligatoire | fuseau IANA d'affichage (`Europe/Paris`) |
| `default_window_days` | 30 | fenêtre par défaut, en jours |
| `max_range_days` | 366 | étendue maximale d'une requête (≥ `default_window_days`) |
| `max_events` | 100 | nombre maximal d'événements renvoyés |
| `cache_ttl_seconds` | 300 | durée du cache |
| `calendars` | obligatoire | objet alias → adresse du flux, en `https` uniquement |

Les adresses des flux sont des secrets : elles n'apparaissent ni dans les résultats, ni dans les erreurs, ni dans les logs.

Le serveur refuse de démarrer, avec un message sur stderr, si le fichier est absent ou invalide, s'il contient un champ inconnu, une valeur ≤ 0, aucun calendrier, ou une adresse qui n'est pas en `https`.

Codes de sortie : `0` arrêt normal, `1` erreur de configuration ou d'exécution, `2` erreur d'usage.

## Protocole

stdout ne porte que le protocole MCP ; tous les logs vont sur stderr.
