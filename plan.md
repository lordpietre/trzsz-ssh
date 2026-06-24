# Plan: TUI ncurses completo

## ✅ Ya implementado en el TUI ncurses

### Action bar (botones)
- [New] — formulario nuevo host inline
- [Search] — filtro de hosts
- [Select] — multi-select para batch login
- [Enter] / [Win] [Tab] [Pane] — login individual o batch
- [Config] — editor de `~/.tssh.conf`
- [Quit] — salir

### Context menu (← sobre host)
- **Edit** — formulario inline: Alias, HostName, Port, User, Password
- **Tunnels** — gestor de túneles SSH (manual/auto, scan, Docker, persistencia)
- **Download** — formulario: paths remotos + ruta local, ejecuta `tssh -t --client --download-path ...`
- **Delete** — confirmación y eliminación del host

### Config form (8 opciones)
| Opción | Descripción |
|---|---|
| `ServerAliveInterval` | Keepalive en segundos |
| `SetTerminalTitle` | Auto-set titulo terminal |
| `DragFileUploadCommand` | Comando drag & drop |
| `DefaultDownloadPath` | Ruta descarga por defecto |
| `PromptThemeLayout` | Tema (tiny/simple/table) |
| `PromptDefaultMode` | Modo inicial (search/normal) |
| `PromptPageSize` | Registros por página |
| `Language` | Idioma (english/chinese) |

### Otras features TUI
- Búsqueda/filtro con keywords
- Group labels filtering
- Multi-select + batch login (tmux/iTerm2/WT)
- Temas con colores personalizables
- Paginación (`PromptPageSize`)
- Panel de detalle del host
- Favoritos / last login tracking
- Show/hide system hosts
- Ayuda con tecla `?`
- Túneles: creación manual, auto-scan (ss/netstat), Docker, persistencia JSON
- ScrollOffset + availableHeight seguro

---

## ❌ Pendiente de implementar en el TUI

### 1. Ampliar Edit form — campos por host

Actualmente solo edita: Alias, HostName, Port, User, Password.

| Campo | Prioridad |
|---|---|
| `IdentityFile` | Alta |
| `ProxyJump` | Alta |
| `RemoteCommand` | Alta |
| `GroupLabels` | Alta |
| `UdpMode` (yes/QUIC/KCP/no) | Media |
| `EnableTrzsz` (yes/no) | Media |
| `EnableZmodem` (yes/no) | Media |
| `EnableDragFile` (yes/no) | Media |
| `ForwardAgent` (yes/no) | Media |
| `ForwardX11` (yes/no) | Baja |
| `ConnectTimeout` | Baja |
| `ServerAliveCountMax` | Baja |
| `DnsSrvName` | Baja |
| `HideHost` | Baja |
| `ConsoleEscapeTime` | Baja |
| `EnableWaypipe` | Baja |
| `EnableOSC52` | Baja |
| `Compression` | Baja |

### 2. Ampliar Config form — opciones faltantes de ~/.tssh.conf

Actualmente cubre 8 de 17 opciones.

| Opción | Prioridad |
|---|---|
| `ConfigPath` | Alta |
| `ExConfigPath` | Alta |
| `UseOpenSSHConfig` | Alta |
| `DefaultUploadPath` | Media |
| `ProgressColorPair` | Media |
| `PromptDetailItems` | Media |
| `PromptCursorIcon` | Baja |
| `PromptSelectedIcon` | Baja |

### 3. Upload file — context menu

Similar a **Download** pero para subir archivos al servidor con `--upload-file`.
- Formulario: paths locales + ruta remota de destino
- Ejecuta `tssh --upload-file <local> <alias> '<trz -d /ruta/>'`

### 4. Port forwarding avanzado (L/R/D)

Actualmente solo existen túneles TCP simples con `-NL`. Falta:
- `-L` local forwarding con bind address
- `-R` remote forwarding
- `-D` dynamic forwarding (SOCKS5)
- UDP forwarding (`UdpLocalForward` / `UdpRemoteForward`)

### 5. Password managers externos

Formulario para configurar `PasswordCommand`, `PassphraseCommand`, etc. con
soporte de tokens `%n`, `%h`, `%r`, `%p`.

### 6. TOTP / OTP editor

En Edit form, campos para `TotpSecret1..N` y `OtpCommand1..N`.

### 7. Install tools desde el TUI

Botón o acción que ejecute:
- `--install-trzsz` en el host seleccionado
- `--install-tsshd` en el host seleccionado

### 8. UDP mode global / por host

- Config global: UdpMode, TsshdPath, TsshdPort, UdpAliveTimeout, etc.
- Por host: toggle UDP/KCP/QUIC desde Edit form

### 9. Expect automation editor

Formulario para configurar:
- `ExpectCount`, `ExpectTimeout`
- `ExpectPattern1..N`, `ExpectSendPass1..N`, `ExpectSendText1..N`
- `ExpectSendTotp1..N`, `ExpectSendOtp1..N`
- `CtrlExpect*`

### 10. Reconnect / Debug / TraceLog desde TUI

Opciones globales en Config form:
- `--reconnect` al iniciar sesión
- `--debug` mode
- `--tracelog` mode

### 11. Opciones de ~/.ssh/password (exConfig)

Actualmente el TUI solo escribe `encPassword` al crear/editar hosts.
Falta editor para:
- `encPassphrase` / `Passphrase`
- `encQuestionAnswer1..N` / `QuestionAnswer1..N`
- `encTotpSecret1..N` / `TotpSecret1..N`
- `encOtpCommand1..N` / `OtpCommand1..N`

---

## Resumen archivos

| Archivo | Rol |
|---|---|
| `tssh/host_model.go` | TUI principal (~3077 lines) |
| `tssh/args.go` | CLI flags (180 lines) |
| `tssh/config.go` | Config system (~931 lines) |
| `tssh/tools.go` | Tools: enc-secret, list-hosts |
| `tssh/tools_new_host.go` | New host CLI flow |
| `tssh/tools_upload.go` | trz upload exec |
| `tssh/trzsz.go` | trzsz/zmodem filter |
| `tssh/udp.go` | UDP/KCP connection |
| `tssh/dns.go` | Custom DNS |
| `tssh/waypipe.go` | Wayland integration |
| `tssh/console.go` | SSH escape console |
| `tssh/expect.go` | Expect automation |
| `tssh/otp.go` | TOTP/OTP auth |
| `tssh/forward*.go` | Port forwarding |
| `tssh/agent.go` | SSH agent |
| `tssh/krb5.go` | Kerberos auth |
| `tssh/algos.go` | Algorithm config |
| `tssh/auth_method.go` | Auth methods |
| `tssh/known_hosts.go` | Known hosts |
| `tssh/lang.go` | i18n |
| `tssh/login.go` | SSH login logic |

---

## Prioridades

1. **Alta** — Ampliar Edit form (IdentityFile, ProxyJump, RemoteCommand, GroupLabels)
2. **Alta** — Ampliar Config form (ConfigPath, ExConfigPath, UseOpenSSHConfig)
3. **Alta** — Upload file en context menu
4. **Media** — Port forwarding avanzado (L/R/D)
5. **Media** — Password managers, TOTP/OTP editors
6. **Media** — Install tools desde TUI
7. **Baja** — UDP por host, Expect editor, Debug/Reconnect toggles
8. **Baja** — exConfig editor completo
