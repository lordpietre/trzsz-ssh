## tssh — fork de trzsz-ssh v0.1.25

Cliente SSH con interfaz TUI ncurses, reemplazo directo de OpenSSH. Fork de [trzsz-ssh](https://github.com/trzsz/trzsz-ssh) con gestión visual de hosts, túneles y edición inline.

### Características principales

| Categoría | Features |
|---|---|
| **TUI Login** | Interfaz interactiva con búsqueda, filtro, selección múltiple, paginación, 3 temas (tiny/simple/table), colores personalizables, iconos, etiquetas de grupo |
| **Gestión de hosts** | Menu contextual (`←`): Editar, Túneles, Copiar, Favoritos, Forzar IPv4/IPv6, ver historial. Diálogo inline para editar alias/host/puerto/user/password |
| **Túneles SSH** | `←` → Tunnels → Manual o Automático. Escanea puertos (`ss` / `netstat` / Docker). Persistencia en `~/.tssh_tunnels`. Atajo `d` para borrar |
| **Batch Login** | Multi-select + `Ctrl+P` (paneles), `Ctrl+W` (ventanas), `Ctrl+T` (tabs) en tmux/iTerm2/Windows Terminal |
| **Modo UDP** | QUIC o KCP. Reconexión automática, sesiones adjuntables, roaming entre redes. Requiere [tsshd](https://github.com/trzsz/tsshd) en el servidor |
| **Transferencia archivos** | [trzsz](https://trzsz.github.io/) (trz/tsz), Zmodem (rz/sz), drag & drop, upload pre-login (`--upload-file`), instalación remota (`--install-trzsz`) |
| **Recordar contraseñas** | Cifrado AES via `--enc-secret`, soporte de `encPassword`/`encPassphrase`, respuestas para keyboard-interactive, TOTP, OTP command |
| **Password managers externos** | `PasswordCommand`, `PassphraseCommand`, etc. Compatible con gopass, pass, 1Password, Bitwarden, Vault, macOS Keychain |
| **Automated Interaction** | Expect-like: `ExpectPattern`, `ExpectSendPass`, `ExpectSendText`, TOTP, OTP. `CtrlExpect*` para ControlMaster |
| **Port Forwarding** | `-L`/`-R`/`-D` TCP y UDP, X11 forwarding, Agent forwarding, Gateway ports, stdio forward (`-W`) |
| **ProxyJump** | Multihop, también sobre UDP. Relay mode para trzsz en jump servers |
| **Keepalive** | ServerAliveInterval default 30s, configurable via `~/.tssh.conf` `defaultServerAliveInterval` |
| **Wayland integración** | `EnableWaypipe yes` — waypipe automático en background |
| **Clipboard OSC52** | `EnableOSC52 yes` — servidor escribe al portapapeles local |
| **Consola SSH** | Escape `~`: `.` kill, `^Z` suspend, `#` list forwards, `v`/`V` verbosity, `R` rekey, `C` redirect |
| **DNS SRV** | `DnsSrvName myhost.mydomain.com` para resolución via SRV records |
| **Reconnect** | `--reconnect` para reinicio automático tras caída. `-f` modo background. Adjuntar sesión con `--attach` |
| **Herramientas** | `--new-host` (alta GUI), `--list-hosts` (JSON), `--enc-secret` (cifrar), `--install-trzsz`/`--install-tsshd` (remote install) |
| **Temas** | `PromptThemeLayout: tiny/simple/table`, `PromptThemeColors` personalizable, `PromptCursorIcon`/`PromptSelectedIcon` emoji |
| **Autenticación** | Public key, password, keyboard-interactive, SSH agent, PKCS#11, Kerberos/GSSAPI, certificados OpenSSH |
| **i18n** | Español, inglés, chino. `language = english | chinese` en `~/.tssh.conf` |
| **Compatibilidad** | Drop-in de OpenSSH. Soporta `-o`, `-G`, `Include`, `Match`, `ControlMaster`, todos los flags estándar. `#!!` prefix para opciones tssh-only en `~/.ssh/config` |
| **Multiplataforma** | Linux, macOS, Windows (+ Win7), Android/Termux, FreeBSD. Binarios para 386/amd64/arm/arm64/loong64 |

### Navegación

| Tecla | Acción |
|---|---|
| `↑/↓` | Navegar lista de hosts |
| `←` | Menú contextual del host (Editar / Túneles / Eliminar) |
| `→` | Barra de acciones (New / Search / Select / Enter / Quit) |
| `Enter` | Conectar / Confirmar selección |
| `/` | Buscar / filtrar hosts |
| `?` | Ayuda de teclas |
| `Espacio` / `x` | Seleccionar/deseleccionar host (multi-select) |
| `a` / `o` | Seleccionar todos / invertir selección |
| `p` / `w` / `t` | Batch login: paneles / ventanas / tabs |
| `E` | Limpiar filtro de grupo |
| `n` | Nuevo host |
| `S` | Mostrar/ocultar hosts del sistema |
| `q` / `Ctrl+C` | Salir |

### Túneles SSH

- `←` sobre un host → **Tunnels** → menú Manual o Automático
- **Manual**: puerto remoto y local, lanza `ssh -NL` en segundo plano
- **Automático**: escanea puertos abiertos vía `ss`, seleccionas y creas túnel
- `d` sobre un túnel → modo borrado, `Enter` confirma
- Persistencia en `~/.ssh_tunnels`

### Edición inline

`←` → **Edit** abre formulario ncurses. Tab/Enter entre campos, Esc cierra. Guarda en `~/.ssh/config` y `~/.ssh/password`.

### Configuración rápida

```ini
# ~/.tssh.conf
ConfigPath = ~/.ssh/config
ExConfigPath = ~/.ssh/password
PromptThemeLayout = simple
PromptDefaultMode = search
PromptPageSize = 10
DefaultServerAliveInterval = 30
SetTerminalTitle = yes
DragFileUploadCommand = trz -y
DefaultDownloadPath = ~/Downloads
```

### Compilar

```sh
CGO_ENABLED=0 go build -o ./bin/ ./cmd/tssh
```

### Documentación completa

Para documentación detallada de todas las features (trzsz, zmodem, UDP, expect, scp/sftp, remember passwords, external password managers, Wayland, clipboard, themes, etc.), visita [trzsz.github.io/tssh](https://trzsz.github.io/tssh) o lee [README.en.md](README.en.md).
