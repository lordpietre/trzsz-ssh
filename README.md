## tssh — fork de trzsz-ssh

Cliente SSH con interfaz TUI ncurses. Fork de [trzsz-ssh](https://github.com/trzsz/trzsz-ssh) con gestión visual de hosts, túneles y edición inline.

### Navegación

| Tecla | Acción |
|---|---|
| `↑/↓` | Navegar lista de hosts |
| `←` | Menú contextual del host (Editar / Túneles / Eliminar) |
| `→` | Barra de acciones (New / Search / Select / Enter / Quit) |
| `Enter` | Conectar / Confirmar selección |
| `/` | Buscar / filtrar hosts |
| `?` | Ayuda de teclas |
| `n` | Nuevo host |
| `S` | Mostrar/ocultar hosts del sistema |
| `q` / `Ctrl+C` | Salir |

### Túneles SSH

- `←` sobre un host → **Tunnels** → menú Manual o Automático
- **Manual**: puerto remoto y local, lanza `ssh -NL` en segundo plano
- **Automático**: escanea puertos abiertos vía `ss`, seleccionas y creas túnel
- `d` sobre un túnel → modo borrado, `Enter` confirma
- Persistencia en `~/.tssh_tunnels`

### Edición inline

`←` → **Edit** abre formulario ncurses. Tab/Enter entre campos, Esc cierra. Guarda en `~/.ssh/config` y `~/.ssh/password`.

### Compilar

```sh
CGO_ENABLED=0 go build -o ./bin/ ./cmd/tssh
```
