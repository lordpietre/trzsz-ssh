# Plan: Migrar TUI de promptui a Bubble Tea + Estabilizar conexiones

## Fase 1 — Estabilizar conexiones (desconexiones) ← EMPEZANDO AHORA

### Problema raíz
`ServerAliveInterval` por defecto es **0** → no se envían keep-alive → firewalls/NAT cortan la TCP idle.

### Solución
| Cambio | Archivo | Detalle |
|---|---|---|
| Default `ServerAliveInterval=30` | `login.go:698-711` | Si no hay `ServerAliveInterval` en config, usar 30s |
| Añadir `defaultServerAliveInterval` | `config.go` | Opción `~/.tssh.conf` para override |
| Exponer en detalle del host TUI | `theme.go` | Mostrar ServerAliveInterval en panel de detalles |
| Health-check TCP pre-conexión | `tools_new_host.go` | Already exists via `net.DialTimeout` |

## Fase 2 — Reemplazar promptui por Bubble Tea TUI ✓ COMPLETADO

| Antes | Después |
|---|---|
| `prompt.go` (673 lines) + `promptui` + `readline` + pipe hacks | `host_model.go` (483 lines) — `tea.Model` nativo |
| `wrapStdin()` pipe goroutine | `Update()` con `tea.KeyPressMsg` |
| Go templates para render | `View()` con `lipgloss` directo |
| `promptui.Select` + `bellFilter` | `tea.NewProgram()` + `tea.WindowSizeMsg` |

### Layout implementado
```
┌─────────────────────────────────────────────┐
│ 🔍 /filtertext                N hosts       │ ← search bar
├─────────────────────────────────────────────┤
│ 🧨 ✔ alias1    host1        labels          │ ← lista (scrolleable)
│    ✔ alias2    host2        labels          │
│      alias3    host3        labels          │
├─────────────────────────────────────────────┤
│  Alias: alias1  Host: host1  Port: 22 ...   │ ← detalles
├─────────────────────────────────────────────┤
│ ↑↓jk nav | / search | Space sel | Enter go  │ ← status bar
└─────────────────────────────────────────────┘
```

### Temas
- Los temas `tiny`/`simple`/`table` actuales (`theme.go`) quedan como dead code.
- El nuevo estilo usa colores ANSI directos (lipgloss).
- `ncurses` — pendiente de implementar como tema adicional.

## Fase 3 — Nuevas opciones en el TUI

| Opción | Descripción |
|---|---|
| `ServerAliveInterval` | Default 30s, configurable en detalle del host |
| `ServerAliveCountMax` | Default 3, configurable |
| `ConnectTimeout` | Mostrar valor actual (default 10s) |
| Favoritos ⭐ | Marcar hosts favoritos, aparecen al inicio |
| Health-check | TCP dial no-bloqueante, icono verde/rojo |
| Grupo/etiquetas colapsables | Árbol expandible de GroupLabels |
| Proxy SOCKS5 toggle | Activar/desactivar tunnel dinámico desde TUI |

## Fase 4 — UDP mode metrics

| Métrica | Implementación |
|---|---|
| Latencia | Mostrar RTT en panel de detalles modo UDP |
| Packet loss | Leer estadísticas internas de kcp/quic |
| Reconnect status | Barra de progreso de reconexión (ya existe en `notifyConnectionLost`) |
