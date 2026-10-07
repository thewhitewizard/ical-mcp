## Agent skills

### Issue tracker

Issues live in GitHub Issues (via the `gh` CLI). See `docs/agents/issue-tracker.md`.

### Domain docs

Single-context: one `CONTEXT.md` + `docs/adr/` at the repo root. See `docs/agents/domain.md`.

## Règles de PR

- **≤ 500 lignes Go par PR**, tests inclus : lignes ajoutées + supprimées dans tous les `*.go`, comparées au merge-base avec `main`. Vérification : `bash scripts/check-pr-size.sh`. La CI et un hook Claude Code (avant `git commit`, `git push`, `gh pr create`) appliquent la limite.
- **≥ 80 % des lignes Go de production modifiées couvertes par les tests** : lignes ajoutées ou modifiées hors `_test.go`, comparées au merge-base avec `main` ; seules les lignes exécutables comptent. Vérification : `bash scripts/check-coverage.sh` (`COVERAGE_MIN` change le seuil). La CI l'applique à chaque PR.
- **Si l'implémentation approche la limite, s'arrêter** : livrer ce qui est cohérent et proposer un ticket de suite plutôt que dépasser.
- **Un ticket = une PR.**
- **Les tests ne dépendent pas de leur ordre** : lancer `go test -shuffle=on ./...` ; rejouer un échec avec `go test -shuffle=<graine> ./...`.
- **Lancer `golangci-lint run ./...` avant de pousser** : il doit passer sans aucun problème (v2.14.0 en local).
- **`go build`, `go vet` et `go test ./...` passent** avant toute PR.
- **README à jour dans la même PR** : toute PR qui ajoute ou modifie un outil MCP, un flag, une variable d'environnement ou un champ de configuration met à jour `README.md`.
- **Pas de code spéculatif** pour des tickets futurs.
- **Les adresses de flux sont des secrets** : jamais dans les résultats, les erreurs ni les logs.
- **Stdout est réservé au protocole MCP** : logs sur stderr via `log`, jamais `fmt.Print*`.

## Skills Go

Charger les skills `cc-skills-golang:<nom>` selon la zone touchée, pas tous.

| Zone | Skills |
|---|---|
| Toujours (tout ticket Go) | `golang-testing`, `golang-error-handling`, `golang-safety` |
| Téléchargement des flux, nettoyage du texte des événements, secrets | `golang-security` |
| Cache partagé, délais, annulation | `golang-concurrency`, `golang-context` |
| Point d'entrée (`--config`, variable d'environnement, codes de sortie, stderr) | `golang-cli` |
| Structures de configuration et d'événements, interfaces (horloge, client HTTP) | `golang-structs-interfaces` |
| Ajout ou mise à jour d'une dépendance | `golang-dependency-management`, `golang-popular-libraries`, `golang-pkg-go-dev` |
| Relecture / qualité | `golang-lint`, `golang-naming`, `golang-code-style`, `golang-documentation`, `golang-modernize` |

**À ne pas utiliser** : `golang-grpc`, `golang-graphql`, `golang-database`, `golang-swagger` ; frameworks d'injection (`golang-dependency-injection`, `golang-google-wire`, `golang-uber-dig`, `golang-uber-fx`, `golang-samber-do`) ; bibliothèques `samber/*` ; `golang-spf13-cobra`, `golang-spf13-viper`, `golang-stretchr-testify` (stdlib seule, aucune dépendance hors `mcp-go` et `gocal`) ; `golang-observability` (les logs se limitent à `log` sur stderr).
