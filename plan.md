# Plan: Migración completa a ncurses + TSSH 2.0

Basado en el documento de Arquitectura Técnica TSSH 2.0 y los issues
actuales (panic en renderActions, navegación de acciones, new host fuera del TUI).

---

## Fase 0 — Correcciones inmediatas ✓

| # | Tarea | Archivo | Estado |
|---|-------|---------|--------|
| 0.1 | Panic `strings: negative Repeat count` en `renderActions` | `host_model.go` | ✓ |
| 0.2 | Navegación de acciones con ← → + Enter | `host_model.go` | ✓ |
| 0.3 | Ayuda (`?`) implementada (showHelp) | `host_model.go` | ✓ |
| 0.4 | ← → en listModel y menuModel | `tools.go`, `console.go` | ✓ |
| 0.5 | ↓ al final de lista → barra de acciones | `host_model.go` | ✓ |
| 0.6 | ↑ desde barra de acciones → último host | `host_model.go` | ✓ |

---

## Fase 1 — Estabilizar TUI ncurses (fondo blanco siempre visible)

### 1.1 Fondo blanco en host_model ✓
- `bgStyle` (fondo blanco ANSI 15) aplicado en todas las líneas.
- Título con fondo azul, bordes con caracteres de caja.
- `repeatSafe()` helper para evitar padding negativo.
- Guard de tamaño mínimo de terminal (80x24).

### 1.2 Consola ncurses (console.go) ✓
- Migrado de tema oscuro (#1b1b32) a ncurses blanco.
- Fondo blanco ANSI 15, texto negro, azul para items activos.
- Cursor sólido `▐█` en item activo.
- Bordes con caracteres de caja.

### 1.3 Padding seguro (repeatSafe) ✓
- Helper `repeatSafe(n int)` en `host_model.go`.
- Reemplazados todos los `strings.Repeat(" ", N)` sin guard.

### 1.4 Medición ANSI correcta ✓
- `ansi.StringWidth()` usado en `renderActions` y console View.
- `lipgloss.Width()` usado en `bgLine` y `renderHost`.

### 1.5 Attach TUI ncurses (attach.go) ✓
- Migrado de tema púrpura oscuro a ncurses blanco.
- Panel dual (sesiones + preview) con bordes de caja.
- Cursor sólido `▐█` en item activo.

### 1.6 Tools UI ncurses (tools.go) ⬜
- Migrar `listModel`, `textInputModel`, `passwordModel` a ncurses blanco.

### 1.7 Scroll suave en lista de hosts ⬜
- Verificar `scrollOffset` y `availableHeight`.

---

## Fase 2 — New host dentro del TUI ncurses

### 2.1 Formulario de nuevo host como modelo Bubble Tea ⬜
- Reemplazar las llamadas a `promptTextInput`, `promptPassword`,
  `promptList` (que lanzan programas Bubble Tea separados) por un modelo
  interno que renderice el formulario dentro del mismo TUI ncurses.
- **Archivo**: `host_new.go` (nuevo)

### 2.2 Integración con el flujo actual ⬜
- En lugar de salir del TUI, el formulario se ejecuta como sub-modelo.
- **Archivos**: `host_model.go`, `tools_new_host.go`

---

## Fase 3 — Menú contextual y Navegación ncurses ✓

### 3.1 Menú contextual (←/→ sobre host) ✓
- Al pulsar ← o → sobre un host se abre menú contextual flotante.
- Opciones: **Edit**, **Tunnels**, **Delete**.
- ↑↓ para navegar, Enter para ejecutar, Esc/← para cerrar.
- **Archivo**: `host_model.go`

### 3.2 Edit — Abrir config en editor ✓
- Sale del TUI (alt screen), abre `~/.ssh/config` con `$EDITOR`.
- Al cerrar el editor, se reinicia el TUI automáticamente.
- **Archivo**: `host_model.go`

### 3.3 Delete — Eliminar host del config ✓
- Busca el bloque `Host <alias>` en `~/.ssh/config` y lo elimina.
- Refresca la lista de hosts automáticamente.
- **Archivo**: `host_model.go`

### 3.4 Tunnels — Gestión de túneles SSH ✓
- Al seleccionar "Tunnels" sobre un host, se entra al gestor de túneles.
- **Archivo**: `host_model.go`

---

## Fase 4 — Tunnel Manager (completo) ✓

### 4.1 Menú de túneles ✓
- Muestra túneles guardados para el host (cargados de `~/.tssh_tunnels`).
- Dos modos de creación: Manual y Automático.
- Navegación ↑↓ entre opciones, Enter para seleccionar.
- Tecla `d` para entrar en modo borrado de túneles.
- **Archivo**: `host_model.go`

### 4.2 Manual — Formulario de puertos ✓
- Campos: Puerto Remoto, Puerto Local.
- Solo dígitos, Tab para cambiar campo, Enter para crear.
- Crea proceso `ssh -NL <local>:localhost:<remote>` en background.
- **Archivo**: `host_model.go`

### 4.3 Automático — Escaneo de puertos ✓
- Conecta al host vía `SshLogin()` (reusa autenticación guardada).
- Ejecuta `ss -tlnp` / `netstat -tlnp` en el remoto.
- Parsea puertos abiertos y los muestra en lista.
- Seleccionas uno → pide puerto local → crea túnel.
- **Archivo**: `host_model.go`

### 4.4 Persistencia ✓
- Túneles guardados en `~/.tssh_tunnels` (JSON).
- Se recargan al reabrir el menú de túneles.
- Al borrar, se mata el proceso y se elimina del archivo.
- **Archivo**: `host_model.go`

### 4.5 Borrado de túneles ✓
- Tecla `d` en el menú → modo borrado.
- ↑↓ selecciona túnel, Enter confirma, Esc cancela.
- Mata proceso SSH subyacente y elimina entrada.
- **Archivo**: `host_model.go`

---

## Fase 5 — Sesión Manager + Drivers modulares (TSSH 2.0)

### 5.1 Session Manager (núcleo) ⬜
- Nuevo componente central que orquesta todas las operaciones.
- **Archivos**: `internal/session/` (nuevo paquete)

### 5.2 Driver interface ⬜
- Interfaz común para todos los protocolos (SSH, Serial, TCP Raw).
- **Archivos**: `internal/driver/` (nuevo)

### 5.3 SSH Driver refactorizado ⬜
- Extraer lógica SSH actual en un `sshDriver`.
- **Archivos**: `internal/driver/ssh/` (nuevo)

### 5.4 Serial Driver ⬜
- Soporte para `/dev/ttyUSB*`, `/dev/ttyS*`.
- **Archivos**: `internal/driver/serial/` (nuevo)

### 5.5 TCP Raw Driver ⬜
- Conexiones TCP simples sin SSH.
- **Archivos**: `internal/driver/tcp/` (nuevo)

---

## Fase 6 — Discovery, Túneles y Servicios (mejora continua)

### 6.1 Discovery de servicios ⬜
- Escaneo vía `ss -tulpen` categorizando servicios.
- **Archivos**: `internal/discovery/` (nuevo)

### 6.2 Port Manager ⬜
- Asignación automática de puertos locales.
- **Archivos**: `internal/port/` (nuevo)

### 6.3 Service Manager ⬜
- Inventario persistente de servicios detectados.
- **Archivos**: `internal/service/` (nuevo)

### 6.4 Connection Manager ⬜
- Estado de todas las conexiones activas.
- **Archivos**: `internal/connection/` (nuevo)

### 6.5 Config Manager ⬜
- Persistencia en YAML de servidores, túneles, perfiles.
- **Archivos**: `internal/config/` (nuevo)

### 6.6 Log Manager ⬜
- Registro centralizado de eventos.
- **Archivos**: `internal/log/` (nuevo)

---

## Fase 7 — Interfaz ncurses completa (TUI 2.0)

### 7.1 Organización por pestañas ⬜
```
┌──────────────────────────────────────┐
│  tssh — Session Manager              │
├──────────────────────────────────────┤
│  [Hosts] [Tunnels] [Services] [Logs] │  ← pestañas
├──────────────────────────────────────┤
│                                      │
│  ▐█ web-prod-1          [production] │
│    web-prod-2            [production]│
│                                      │
├──────────────────────────────────────┤
│  ▲▼ Navigate  ←→ Tabs  Enter Open   │
└──────────────────────────────────────┘
```

### 7.2 Panel de Túneles ⬜
- Integrar el gestor de túneles como panel dedicado.

### 7.3 Panel de Servicios ⬜
- Mostrar servicios detectados con iconos y colores.

### 7.4 Panel de Logs ⬜
- Logs en tiempo real de conexiones y túneles.

---

## Resumen de archivos

| Fase | Archivos nuevos | Archivos modificados |
|------|----------------|---------------------|
| 0 | — | `host_model.go`, `tools.go`, `console.go` |
| 1 | — | `host_model.go`, `console.go`, `attach.go` |
| 2 | `host_new.go` (pendiente) | `host_model.go`, `tools_new_host.go` |
| 3 | — | `host_model.go` |
| 4 | — | `host_model.go` |
| 5 | `internal/session/`, `internal/driver/` | `tssh/login.go`, `tssh/main.go` |
| 6 | `internal/discovery/`, `internal/port/`, `internal/service/`, `internal/config/`, `internal/log/` | — |
| 7 | — | `host_model.go`, `tssh/main.go` |

---

## Prioridades inmediatas

1. ✅ Fase 0 — Panics, navegación, ↓ a acciones
2. ✅ Fase 1 — Fondo blanco, padding, attach/consola ncurses
3. ⬜ Fase 2 — New host dentro del TUI (formulario inline)
4. ✅ Fase 3 — Menú contextual (Edit/Tunnels/Delete)
5. ✅ Fase 4 — Tunnel Manager (Manual/Auto, escaneo, persistencia)
6. ⬜ Fase 5+ — Session Manager, Drivers, Discovery
