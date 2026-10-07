# ical-mcp

Serveur MCP (transport stdio) en **lecture seule** qui donne à un assistant IA accès à vos calendriers, à partir de flux iCal (`.ics`) servis en HTTPS, par exemple l'« adresse secrète au format iCal » de Google Agenda.

Il ne dépend d'aucun agent en particulier : n'importe quel client MCP peut le lancer comme sous-processus. Binaire statique, une seule configuration JSON.

> En cours de construction : l'outil `list_events` est disponible, `get_event` arrive dans un ticket de suite.

## Outils

### `list_events(calendar?, from?, to?)`

Liste les événements entre deux jours (inclus), triés par début.

| Paramètre | Rôle |
|---|---|
| `calendar` | un alias de la configuration ; absent = tous les calendriers, fusionnés |
| `from` | premier jour, `AAAA-MM-JJ` ; défaut : aujourd'hui |
| `to` | dernier jour, `AAAA-MM-JJ` ; défaut : `from` + `default_window_days` |

Erreur si `to` est avant `from` ou si l'étendue (`to` − `from`) dépasse `max_range_days`.

Réponse (JSON compact) :

```json
{"events":[{"uid":"abc","calendar":"perso","title":"Standup","start":"2026-03-10T14:00:00+01:00","end":"2026-03-10T15:00:00+01:00","all_day":false,"location":"Salle A"}],"truncated":true,"stale":["perso"],"errors":["travail: unreachable"]}
```

- Les heures sont en ISO 8601 avec décalage, dans le fuseau `timezone`. Un événement sur la journée entière a des **dates** (`2026-03-05`) et sa fin est **exclusive** : le lendemain de son dernier jour.
- Pas de description dans la liste.
- `truncated` : `true` si `max_events` est atteint (les premiers événements sont gardés).
- `stale` : calendriers servis depuis le cache parce que le téléchargement a échoué.
- `errors` : calendriers injoignables, par alias (jamais l'adresse). Les autres calendriers sont servis quand même ; si tous échouent, c'est une erreur d'outil.
- Les champs `truncated`, `stale` et `errors` sont absents quand ils sont vides.

## Compilation

Binaire statique, sans CGO :

```sh
CGO_ENABLED=0 go build -ldflags "-X main.version=v0.1.0" -o ical-mcp .
```

## Configuration

Le chemin du fichier JSON est passé par `--config` ou par la variable d'environnement `ICAL_MCP_CONFIG` (le flag l'emporte). Voir `config.example.json`.

| Champ | Défaut | Rôle |
|---|---|---|
| `timezone` | obligatoire | fuseau IANA d'affichage (`Europe/Paris`) ; `Local` est refusé |
| `default_window_days` | 30 | fenêtre par défaut, en jours |
| `max_range_days` | 366 | étendue maximale d'une requête (≥ `default_window_days`) |
| `max_events` | 100 | nombre maximal d'événements renvoyés |
| `cache_ttl_seconds` | 300 | durée du cache en mémoire |
| `calendars` | obligatoire | objet alias → adresse du flux, en `https` uniquement |

Le serveur refuse de démarrer, avec un message sur stderr, si le fichier est absent ou invalide, s'il contient un champ inconnu, une valeur ≤ 0, aucun calendrier, ou une adresse qui n'est pas en `https`.

Codes de sortie : `0` arrêt normal, `1` erreur de configuration ou d'exécution, `2` erreur d'usage.

### Conteneur minimal

Le binaire n'embarque **pas** la base des fuseaux horaires. Dans un conteneur sans `/usr/share/zoneinfo` (image `scratch`), `timezone` ne peut pas être chargé et le serveur refuse de démarrer. Deux solutions : copier le dossier `zoneinfo` dans l'image, ou fournir l'archive de Go et la désigner par `ZONEINFO=/chemin/zoneinfo.zip` (le fichier `$(go env GOROOT)/lib/time/zoneinfo.zip`).

## Sécurité

- Lecture seule : aucun outil ne modifie quoi que ce soit.
- Aucun outil n'accepte d'adresse. Les adresses des flux sont des secrets : elles n'apparaissent ni dans les résultats, ni dans les erreurs, ni dans les logs. Protégez le fichier de configuration (`chmod 600`).
- Le texte des événements vient de tiers et n'est pas fiable : les caractères de contrôle et les caractères invisibles sont supprimés, `title` et `location` sont tronqués à 200 caractères, `description` à 1000.
- Téléchargement : HTTPS uniquement (redirections comprises), délai de 10 s, 10 Mo au plus, cache en mémoire par calendrier.
- stdout ne porte que le protocole MCP ; tous les logs vont sur stderr.

## Déclarer le serveur dans un agent

Dans les exemples, remplacez `/opt/ical-mcp/ical-mcp` et `/opt/ical-mcp/config.json` par vos chemins.

### Hermes Agent

`~/.hermes/config.yaml`, clé `mcp_servers` ([documentation](https://hermes-agent.nousresearch.com/docs/user-guide/features/mcp)) :

```yaml
mcp_servers:
  ical:
    command: "/opt/ical-mcp/ical-mcp"
    args: ["--config", "/opt/ical-mcp/config.json"]
```

### ZeroClaw

`config.toml`, section `[mcp]` ([documentation](https://github.com/zeroclaw-labs/zeroclaw/blob/master/docs/setup-guides/mcp-setup.md)) :

```toml
[mcp]
enabled = true

[[mcp.servers]]
name = "ical"
transport = "stdio"
command = "/opt/ical-mcp/ical-mcp"
args = ["--config", "/opt/ical-mcp/config.json"]
```

### PicoClaw

`config.json`, clé `tools.mcp.servers` ([dépôt](https://github.com/sipeed/picoclaw)) :

```json
{
  "tools": {
    "mcp": {
      "enabled": true,
      "servers": {
        "ical": {
          "enabled": true,
          "command": "/opt/ical-mcp/ical-mcp",
          "args": ["--config", "/opt/ical-mcp/config.json"]
        }
      }
    }
  }
}
```

Les formats de ces trois agents évoluent : en cas de doute, vérifiez la documentation de votre version. Pour passer le chemin de la configuration par l'environnement plutôt que par `--config`, utilisez la clé `env` de l'agent avec `ICAL_MCP_CONFIG`.

## Test manuel en stdio

Cette commande lance le serveur, fait la poignée de main MCP et liste les outils :

```sh
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' \
  | ./ical-mcp --config config.json
```

Les réponses (sur stdout, une par ligne) peuvent arriver dans n'importe quel ordre : c'est permis par JSON-RPC, repérez-les par leur `id`. Le message de démarrage est sur stderr. Pour appeler un outil, ajoutez par exemple :

```sh
'{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_events","arguments":{"from":"2026-03-01","to":"2026-03-31"}}}'
```

## Limites connues

Le calendrier est lu par la bibliothèque [`gocal`](https://github.com/apognu/gocal), qui ne couvre pas tout iCal :

- Un `TZID` propre à Windows (« W. Europe Standard Time », fréquent dans les flux Outlook) est lu comme de l'UTC : l'heure affichée est fausse.
- `UNTIL` est exclusif : la dernière occurrence d'une série qui se termine à une heure précise est perdue.
- Les règles mensuelles et annuelles peuvent déborder sur le mois suivant (`BYMONTHDAY=31`, 29 février).
- Un trou d'heure d'été (02:30 le jour du passage à l'heure d'été) déforme une série quotidienne.
- `FREQ=HOURLY`, `MINUTELY` et `SECONDLY` ne donnent aucune occurrence.
- Un événement sans `DTSTAMP` fait échouer tout le calendrier ; un événement horodaté sans `DTEND` ni `DURATION` est ignoré.
- Dans une description, une suite « antislash + n » est prise pour un saut de ligne.

Autres limites :

- Deux requêtes simultanées sur un même calendrier peuvent le télécharger deux fois.
- La copie en cache d'un calendrier injoignable est servie sans limite d'âge (signalée par `stale`).
