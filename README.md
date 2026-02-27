# rt — reduce tokens

CLI que comprime la salida de comandos antes de que llegue al contexto de un LLM. Útil como hook de [Claude Code](https://docs.anthropic.com/en/docs/claude-code) u otros agentes que ejecutan comandos de terminal.

## El problema

Cuando un agente LLM ejecuta `git status`, `docker compose up` o `npm test`, la salida cruda consume cientos o miles de tokens innecesarios: barras de progreso, hashes, líneas en blanco, metadata repetitiva. `rt` intercepta esa salida y la reduce a lo esencial.

## Instalación

```bash
git clone https://github.com/javicasper/rt.git
cd rt
./install.sh
```

Requiere Go 1.25+. El script compila el binario y crea un symlink en `/usr/local/bin/rt`.

## Uso básico

```bash
# Ejecutar un comando con filtro
rt run git status
rt run docker compose up -d
rt run npm test

# Ver filtros disponibles
rt ls

# Ver el TOML de un filtro
rt show git/status

# Estadísticas de tokens ahorrados
rt gain
rt gain --by-filter
rt gain --log
```

## Integración con Claude Code

`rt` puede instalarse como hook de Claude Code para interceptar automáticamente las llamadas al tool `Bash`:

```bash
# Hook global (se aplica a todos los proyectos)
rt hook install --global

# Hook local (solo el proyecto actual)
rt hook install
```

Esto modifica `settings.json` para que cada comando Bash pase por `rt` automáticamente. Si no hay filtro para un comando, la salida pasa sin cambios.

### Skill para crear filtros

`rt` incluye un skill que enseña a Claude Code a escribir filtros TOML:

```bash
rt skill install
```

Después puedes pedirle a Claude Code: *"crea un filtro rt para kubectl get pods"* y usará la guía integrada para generar un filtro válido.

## Filtros

Los filtros son archivos TOML que definen cómo transformar la salida de un comando. Se buscan en este orden de prioridad:

1. `~/.config/rt/filters/` — filtros de usuario (sobreescriben los built-in)
2. Filtros integrados en el binario

### Filtros incluidos

| Filtro | Comando(s) |
|---|---|
| `cargo/build` | `cargo build` |
| `cargo/install` | `cargo install *` |
| `docker/build` | `docker build`, `docker buildx build` |
| `docker/compose` | `docker compose *` |
| `docker/images` | `docker images` |
| `docker/ps` | `docker ps` |
| `gh/issue/list` | `gh issue list` |
| `gh/issue/view` | `gh issue view *` |
| `gh/pr/checks` | `gh pr checks *` |
| `gh/pr/list` | `gh pr list` |
| `gh/pr/view` | `gh pr view *` |
| `git/commit` | `git commit` |
| `git/diff` | `git diff` |
| `git/log` | `git log` |
| `git/pull` | `git pull` |
| `git/push` | `git push` |
| `git/show` | `git show` |
| `git/stash` | `git stash`, `git stash pop`, etc. |
| `git/status` | `git status` |
| `npm/install` | `npm install`, `npm ci`, `pnpm install`, `yarn install` |
| `npm/run` | `npm run *` |
| `npm/test` | `npm test`, `pnpm test`, `yarn test` |

### Anatomía de un filtro

```toml
command = "git push"

# Short-circuit: si la salida contiene este texto, emitir directamente
match_output = [
  { contains = "Everything up-to-date", output = "ok (up-to-date)" },
]

# Eliminar líneas que matcheen cualquiera de estos patrones
skip = [
  "^remote:",
  "^Enumerating objects:",
  "^Counting objects:",
]

# Reformatear líneas (regex con grupos de captura)
[[replace]]
pattern = '^\[(\S+)\s+(\w+)\]\s+(.+)$'
output = "ok {2} ({1}) {3}"

# Salida en caso de éxito
[on_success]
output = "{output}"

# Salida en caso de error (últimas N líneas)
[on_failure]
tail = 5
```

### Orden de procesamiento

Los pasos se ejecutan en este orden fijo:

1. **`match_output`** — comprobación de la salida completa; si matchea, cortocircuita todo
2. **`skip`** — elimina líneas por regex
3. **`[[replace]]`** — transforma líneas por regex
4. **`[on_success]` / `[on_failure]`** — rama según código de salida

### Crear un filtro personalizado

```bash
# Crear el archivo
mkdir -p ~/.config/rt/filters/kubectl
cat > ~/.config/rt/filters/kubectl/get.toml << 'EOF'
command = "kubectl get *"

skip = [
  "^\\s*$",
]

[on_success]
output = "{output}"

[on_failure]
tail = 20
EOF

# Validar
rt check ~/.config/rt/filters/kubectl/get.toml

# Probar
rt run kubectl get pods
```

O instalar desde un archivo o URL:

```bash
rt add mi-filtro.toml
rt add https://example.com/filters/kubectl/get.toml
```

## Otros comandos

### `rt suggest`

Analiza el historial de comandos ejecutados sin filtro y sugiere cuáles se beneficiarían de uno:

```
$ rt suggest
commands without filters (sorted by total tokens wasted):
  kubectl get pods              runs:   12  avg:   340 tok  total: 4080 tok
  terraform plan                runs:    5  avg:   890 tok  total: 4450 tok
```

### `rt eject`

Copia un filtro built-in a `~/.config/rt/filters/` para personalizarlo:

```bash
rt eject git/status
# ejected to: ~/.config/rt/filters/git/status.toml
```

### `rt cache`

Gestiona la caché de filtros compilados:

```bash
rt cache info    # ver estado de la caché
rt cache clear   # limpiar caché
```

## Campos TOML de referencia

| Campo | Tipo | Descripción |
|---|---|---|
| `command` | string o string[] | Patrón de comando. Soporta `*` wildcard. |
| `run` | string | Comando alternativo a ejecutar. |
| `strip_ansi` | bool | Eliminar secuencias ANSI antes de filtrar. |
| `match_output` | tabla[] | Short-circuit por substring (`contains`) o regex (`matches`). |
| `skip` | string[] | Regex para eliminar líneas. |
| `keep` | string[] | Regex allowlist (solo retener líneas que matcheen). |
| `[[replace]]` | tabla[] | Transformaciones por línea: `pattern` (regex) + `output` (template con `{1}`, `{2}`...). |
| `[on_success]` | tabla | Rama para exit code 0. Campos: `output`, `head`, `tail`, `skip`. |
| `[on_failure]` | tabla | Rama para exit code != 0. Mismos campos. |
| `[[variant]]` | tabla[] | Delegación contextual a filtros especializados. |

## Licencia

MIT
